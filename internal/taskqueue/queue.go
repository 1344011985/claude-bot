package taskqueue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"claude-bot/internal/claude"
	"claude-bot/internal/imageutil"
	"claude-bot/internal/memory"
)

type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}

type Queue interface {
	Submit(ctx context.Context, req SubmitRequest) (*Task, error)
	Get(taskID string) (*Task, error)
	ListByUser(userID string, limit int) ([]*Task, error)
	Cancel(taskID string) error
}

type sqliteQueue struct {
	db           *sql.DB
	store        memory.Store
	runner       *claude.Runner
	downloader   *imageutil.Downloader
	selector     *claude.ModelSelector
	systemPrompt string
	logger       Logger
	jobs         chan queueJob
	cancelMu     sync.Mutex
	cancelMap    map[string]context.CancelFunc
}

type queueJob struct {
	taskID      string
	progressFn  func(string)
}

func New(db *sql.DB, store memory.Store, runner *claude.Runner, downloader *imageutil.Downloader, selector *claude.ModelSelector, systemPrompt string, logger Logger, workers int) (Queue, error) {
	if workers <= 0 {
		workers = 2
	}
	q := &sqliteQueue{
		db:           db,
		store:        store,
		runner:       runner,
		downloader:   downloader,
		selector:     selector,
		systemPrompt: systemPrompt,
		logger:       logger,
		jobs:         make(chan queueJob, workers*8),
		cancelMap:    make(map[string]context.CancelFunc),
	}
	if err := q.initSchema(); err != nil {
		return nil, err
	}
	for i := 0; i < workers; i++ {
		go q.worker()
	}
	return q, nil
}

func (q *sqliteQueue) Submit(ctx context.Context, req SubmitRequest) (*Task, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	id, err := newTaskID()
	if err != nil {
		return nil, err
	}
	if _, err := q.db.ExecContext(ctx, `
		INSERT INTO tasks(id, user_id, group_id, content, status, result, error, session_id, continue_session)
		VALUES(?, ?, ?, ?, ?, '', '', '', ?)
	`, id, req.UserID, req.GroupID, content, string(StatusPending), boolToInt(req.ContinueSession)); err != nil {
		return nil, err
	}
	task, err := q.Get(id)
	if err != nil {
		return nil, err
	}
	q.jobs <- queueJob{taskID: id, progressFn: req.ProgressFn}
	return task, nil
}

func (q *sqliteQueue) Get(taskID string) (*Task, error) {
	row := q.db.QueryRow(`
		SELECT id, user_id, group_id, content, status, result, error, session_id, continue_session, created_at, started_at, done_at
		FROM tasks WHERE id = ?
	`, taskID)
	return scanTask(row)
}

func (q *sqliteQueue) ListByUser(userID string, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := q.db.Query(`
		SELECT id, user_id, group_id, content, status, result, error, session_id, continue_session, created_at, started_at, done_at
		FROM tasks WHERE user_id = ? ORDER BY created_at DESC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (q *sqliteQueue) Cancel(taskID string) error {
	task, err := q.Get(taskID)
	if err != nil {
		return err
	}
	if task.Status == StatusDone || task.Status == StatusFailed || task.Status == StatusCancelled {
		return nil
	}
	q.cancelMu.Lock()
	cancel := q.cancelMap[taskID]
	q.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	_, err = q.db.Exec(`UPDATE tasks SET status = ?, error = CASE WHEN error = '' THEN 'cancelled by user' ELSE error END, done_at = CURRENT_TIMESTAMP WHERE id = ? AND status IN (?, ?)`, string(StatusCancelled), taskID, string(StatusPending), string(StatusRunning))
	return err
}

func (q *sqliteQueue) worker() {
	for job := range q.jobs {
		q.runTask(job)
	}
}

func (q *sqliteQueue) runTask(job queueJob) {
	task, err := q.Get(job.taskID)
	if err != nil {
		q.logger.Error("taskqueue get task failed", "task_id", job.taskID, "err", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	q.cancelMu.Lock()
	q.cancelMap[job.taskID] = cancel
	q.cancelMu.Unlock()
	defer func() {
		q.cancelMu.Lock()
		delete(q.cancelMap, job.taskID)
		q.cancelMu.Unlock()
		cancel()
	}()

	if _, err := q.db.Exec(`UPDATE tasks SET status = ?, started_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`, string(StatusRunning), task.ID, string(StatusPending)); err != nil {
		q.logger.Error("taskqueue mark running failed", "task_id", task.ID, "err", err)
		return
	}

	sessionID := ""
	if task.ContinueSession {
		sessionID, _ = q.store.GetSession(task.UserID)
	}
	memories, _ := q.store.GetMemories(task.UserID)
	var promptParts []string
	if q.systemPrompt != "" {
		promptParts = append(promptParts, q.systemPrompt)
	}
	if len(memories) > 0 {
		promptParts = append(promptParts, "## 用户个人记忆\n"+strings.Join(memories, "\n"))
	}
	systemPrompt := strings.Join(promptParts, "\n\n")

	history, _ := q.store.GetHistory(task.UserID, 100)
	userPref, _ := q.store.GetModelPreference(task.UserID)
	modelKey := q.selector.SelectModel(userPref, task.Content, 0, len(history))
	modelName := q.selector.GetModelName(modelKey)

	result, runErr := q.runner.RunWithModel(ctx, task.Content, sessionID, systemPrompt, nil, modelName, job.progressFn)
	if runErr != nil {
		status := StatusFailed
		errText := runErr.Error()
		if ctx.Err() == context.Canceled {
			status = StatusCancelled
			if errText == "" {
				errText = "cancelled by user"
			}
		}
		_, _ = q.db.Exec(`UPDATE tasks SET status = ?, error = ?, done_at = CURRENT_TIMESTAMP WHERE id = ?`, string(status), errText, task.ID)
		return
	}

	if task.ContinueSession && result.SessionID != "" {
		if err := q.store.SaveSession(task.UserID, result.SessionID); err != nil {
			q.logger.Error("taskqueue save session failed", "task_id", task.ID, "err", err)
		}
	}
	if err := q.store.SaveHistory(task.UserID, task.Content, result.Text); err != nil {
		q.logger.Error("taskqueue save history failed", "task_id", task.ID, "err", err)
	}
	if result.Usage != nil {
		cost := q.selector.CalculateCost(modelKey, result.Usage.InputTokens, result.Usage.OutputTokens, result.Usage.CacheCreationTokens, result.Usage.CacheReadTokens)
		_ = q.store.RecordUsage(&memory.UsageRecord{
			UserID:              task.UserID,
			SessionID:           result.SessionID,
			Model:               modelKey,
			InputTokens:         result.Usage.InputTokens,
			OutputTokens:        result.Usage.OutputTokens,
			CacheCreationTokens: result.Usage.CacheCreationTokens,
			CacheReadTokens:     result.Usage.CacheReadTokens,
			TotalCostUSD:        cost,
			CreatedAt:           time.Now(),
		})
	}
	_, _ = q.db.Exec(`UPDATE tasks SET status = ?, result = ?, session_id = ?, done_at = CURRENT_TIMESTAMP WHERE id = ?`, string(StatusDone), result.Text, result.SessionID, task.ID)
}

func (q *sqliteQueue) initSchema() error {
	_, err := q.db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			group_id TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			session_id TEXT NOT NULL DEFAULT '',
			continue_session INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			started_at DATETIME,
			done_at DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_tasks_user_created ON tasks(user_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_tasks_user_status ON tasks(user_id, status);
	`)
	return err
}

func scanTask(scanner interface{ Scan(dest ...any) error }) (*Task, error) {
	var t Task
	var continueSession int
	var createdAt string
	var startedAt sql.NullString
	var doneAt sql.NullString
	if err := scanner.Scan(&t.ID, &t.UserID, &t.GroupID, &t.Content, &t.Status, &t.Result, &t.Error, &t.SessionID, &continueSession, &createdAt, &startedAt, &doneAt); err != nil {
		return nil, err
	}
	t.ContinueSession = continueSession == 1
	t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if startedAt.Valid {
		if ts, err := time.Parse("2006-01-02 15:04:05", startedAt.String); err == nil {
			t.StartedAt = &ts
		}
	}
	if doneAt.Valid {
		if ts, err := time.Parse("2006-01-02 15:04:05", doneAt.String); err == nil {
			t.DoneAt = &ts
		}
	}
	return &t, nil
}

func newTaskID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "task_" + hex.EncodeToString(buf), nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
