package command

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"qq-claude-bot/internal/claude"
	"qq-claude-bot/internal/imageutil"
	"qq-claude-bot/internal/memory"
	"qq-claude-bot/internal/newsearch"
)

// modelSwitchPatterns matches natural language model switching requests.
var modelSwitchPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:切换|换|改|设置|设定)(?:模型|model)?(?:为|到|成)\s*(haiku|sonnet|opus|auto)`),
	regexp.MustCompile(`(?i)(?:使用|用)\s*(haiku|sonnet|opus|auto)\s*(?:模型|model)?`),
	regexp.MustCompile(`(?i)(?:模型|model)\s*(?:切换|换|改|设置|设定)(?:为|到|成)?\s*(haiku|sonnet|opus|auto)`),
}

// modelDisplayNames maps model keys to friendly display names.
var modelDisplayNames = map[string]string{
	"haiku":  "Haiku (快速轻量)",
	"sonnet": "Sonnet (均衡)",
	"opus":   "Opus (最强)",
	"auto":   "自动选择",
}

// IncomingMessage holds the parsed incoming message from any channel.
type IncomingMessage struct {
	UserID     string
	GroupID    string          // empty for direct messages
	Content    string
	ImageURLs  []string        // attachment image URLs, may be empty
	ProgressFn func(string)    // optional: called with partial AI output (for streaming)
}

// Handler processes an incoming message and returns a reply string.
type Handler interface {
	Handle(ctx context.Context, msg *IncomingMessage) (string, error)
}

// Router dispatches messages to the appropriate Handler based on command prefix.
type Router struct {
	handlers map[string]Handler
	fallback Handler
	store    memory.Store
}

// Version info injected via ldflags from main package.
var (
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// NewRouter wires up all command handlers.
func NewRouter(store memory.Store, runner *claude.Runner, downloader *imageutil.Downloader, selector *claude.ModelSelector, systemPrompt string, logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}) *Router {
	askH := &askHandler{
		store:        store,
		runner:       runner,
		downloader:   downloader,
		selector:     selector,
		systemPrompt: systemPrompt,
		logger:       logger,
	}
	newsSearcher := newsearch.NewSearcher()
	r := &Router{
		handlers: map[string]Handler{
			"/ask":      askH,
			"/new":      &newHandler{store: store},
			"/remember": &rememberHandler{store: store},
			"/forget":   &forgetHandler{store: store},
			"/history":  &historyHandler{store: store},
			"/help":     &helpHandler{},
			"/version":  &versionHandler{},
			"/news":     &newsHandler{searcher: newsSearcher, logger: logger},
		},
		fallback: askH,
		store:    store,
	}
	return r
}

// Route dispatches the message to the correct handler.
func (r *Router) Route(ctx context.Context, msg *IncomingMessage) (string, error) {
	// Pre-processing: check for model switch intent
	if model, ok := r.detectModelSwitch(msg.Content); ok {
		if err := r.store.SetModelPreference(msg.UserID, model); err != nil {
			return "模型切换失败，请稍后重试。", nil
		}
		displayName := modelDisplayNames[model]
		return fmt.Sprintf("已切换模型为 %s", displayName), nil
	}

	if !strings.HasPrefix(msg.Content, "/") {
		return r.fallback.Handle(ctx, msg)
	}

	parts := strings.SplitN(msg.Content, " ", 2)
	cmd := strings.ToLower(parts[0])

	h, ok := r.handlers[cmd]
	if !ok {
		return fmt.Sprintf("未知指令 %q，输入 /help 查看可用指令", cmd), nil
	}
	return h.Handle(ctx, msg)
}

// detectModelSwitch checks if the message is a model switching request.
func (r *Router) detectModelSwitch(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	for _, pat := range modelSwitchPatterns {
		if matches := pat.FindStringSubmatch(trimmed); len(matches) >= 2 {
			model := strings.ToLower(matches[1])
			if _, ok := modelDisplayNames[model]; ok {
				return model, true
			}
		}
	}
	return "", false
}

// --- /ask handler ---

type askHandler struct {
	store        memory.Store
	runner       *claude.Runner
	downloader   *imageutil.Downloader
	selector     *claude.ModelSelector
	systemPrompt string
	logger       interface {
		Error(msg string, args ...any)
		Info(msg string, args ...any)
	}
}

func (h *askHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	prompt := strings.TrimPrefix(msg.Content, "/ask")
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "请提供问题内容，例如：/ask 你好", nil
	}

	sessionID, err := h.store.GetSession(msg.UserID)
	if err != nil {
		h.logger.Error("get session failed", "user_id", msg.UserID, "err", err)
		sessionID = ""
	}

	memories, err := h.store.GetMemories(msg.UserID)
	if err != nil {
		h.logger.Error("get memories failed", "user_id", msg.UserID, "err", err)
		memories = nil
	}

	var promptParts []string
	if h.systemPrompt != "" {
		promptParts = append(promptParts, h.systemPrompt)
	}
	if len(memories) > 0 {
		promptParts = append(promptParts, "## 用户个人记忆\n"+strings.Join(memories, "\n"))
	}
	systemPrompt := strings.Join(promptParts, "\n\n")

	var imagePaths []string
	if h.downloader != nil {
		for _, url := range msg.ImageURLs {
			path, err := h.downloader.Download(url)
			if err != nil {
				h.logger.Error("download image failed", "url", url, "err", err)
				continue
			}
			imagePaths = append(imagePaths, path)
		}
	}

	history, _ := h.store.GetHistory(msg.UserID, 100)
	conversationTurns := len(history)

	userPref, _ := h.store.GetModelPreference(msg.UserID)
	modelKey := h.selector.SelectModel(userPref, prompt, len(imagePaths), conversationTurns)
	modelName := h.selector.GetModelName(modelKey)

	h.logger.Info("selected model", "user_id", msg.UserID, "model", modelKey, "preference", userPref)

	// Run with selected model, pass progress callback for streaming
	result, err := h.runner.RunWithModel(ctx, prompt, sessionID, systemPrompt, imagePaths, modelName, msg.ProgressFn)
	if err != nil {
		return fmt.Sprintf("执行出错：%v", err), nil
	}

	if err := h.store.SaveSession(msg.UserID, result.SessionID); err != nil {
		h.logger.Error("save session failed", "user_id", msg.UserID, "err", err)
	}
	if err := h.store.SaveHistory(msg.UserID, prompt, result.Text); err != nil {
		h.logger.Error("save history failed", "user_id", msg.UserID, "err", err)
	}

	if result.Usage != nil {
		cost := h.selector.CalculateCost(
			modelKey,
			result.Usage.InputTokens,
			result.Usage.OutputTokens,
			result.Usage.CacheCreationTokens,
			result.Usage.CacheReadTokens,
		)
		usageRecord := &memory.UsageRecord{
			UserID:              msg.UserID,
			SessionID:           result.SessionID,
			Model:               modelKey,
			InputTokens:         result.Usage.InputTokens,
			OutputTokens:        result.Usage.OutputTokens,
			CacheCreationTokens: result.Usage.CacheCreationTokens,
			CacheReadTokens:     result.Usage.CacheReadTokens,
			TotalCostUSD:        cost,
			CreatedAt:           time.Now(),
		}
		if err := h.store.RecordUsage(usageRecord); err != nil {
			h.logger.Error("record usage failed", "err", err)
		}
	}

	return result.Text, nil
}

// --- /new handler ---

type newHandler struct{ store memory.Store }

func (h *newHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	if err := h.store.DeleteSession(msg.UserID); err != nil {
		return "清除会话失败，请稍后重试。", nil
	}
	return "已开启新对话，之前的上下文已清除。", nil
}

// --- /remember handler ---

type rememberHandler struct{ store memory.Store }

func (h *rememberHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	content := strings.TrimSpace(strings.TrimPrefix(msg.Content, "/remember"))
	if content == "" {
		return "请提供要记住的内容，例如：/remember 我喜欢简洁的回答", nil
	}
	if err := h.store.AddMemory(msg.UserID, content); err != nil {
		return "保存记忆失败，请稍后重试。", nil
	}
	return fmt.Sprintf("已记住：%s", content), nil
}

// --- /forget handler ---

type forgetHandler struct{ store memory.Store }

func (h *forgetHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	if err := h.store.DeleteMemories(msg.UserID); err != nil {
		return "清除记忆失败，请稍后重试。", nil
	}
	return "已清除所有记忆。", nil
}

// --- /history handler ---

type historyHandler struct{ store memory.Store }

func (h *historyHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	n := 5
	parts := strings.Fields(msg.Content)
	if len(parts) >= 2 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			n = v
		}
	}

	entries, err := h.store.GetHistory(msg.UserID, n)
	if err != nil {
		return "获取历史记录失败，请稍后重试。", nil
	}
	if len(entries) == 0 {
		return "暂无对话历史。", nil
	}

	var sb strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&sb, "[%d] 你：%s\n    Bot：%s\n", i+1, e.Input, e.Response)
	}
	return strings.TrimSpace(sb.String()), nil
}

// --- /help handler ---

type helpHandler struct{}

func (h *helpHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	return `/ask <问题>     — 向 Claude 提问（续接上下文）
/new            — 开启新对话，清除当前 session
/remember <内容> — 保存长期记忆，每次对话自动注入
/forget         — 清除所有长期记忆
/history [n]    — 查看最近 n 条对话（默认 5）
/news [关键词]   — 搜索最新新闻（不带关键词则显示热点）
/help           — 显示此帮助
/version        — 显示版本信息
直接发消息等同于 /ask

模型切换：发送"切换模型为sonnet"、"使用opus"等即可切换
可选模型：haiku(快速) / sonnet(均衡) / opus(最强) / auto(自动)`, nil
}

// --- /version handler ---

type versionHandler struct{}

func (h *versionHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	return fmt.Sprintf("qq-claude-bot commit=%s built=%s", GitCommit, BuildDate), nil
}

// --- /news handler ---

type newsHandler struct {
	searcher *newsearch.Searcher
	logger   interface {
		Error(msg string, args ...any)
		Info(msg string, args ...any)
	}
}

func (h *newsHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	query := strings.TrimSpace(strings.TrimPrefix(msg.Content, "/news"))

	h.logger.Info("news search requested", "user_id", msg.UserID, "query", query)

	var result string
	var err error

	if query == "" {
		result, err = h.searcher.GetHotNews(ctx)
	} else {
		items, searchErr := h.searcher.Search(ctx, query)
		if searchErr != nil {
			err = searchErr
		} else {
			result = newsearch.FormatNewsItems(items)
		}
	}

	if err != nil {
		h.logger.Error("news search failed", "err", err)
		return "获取新闻失败，请稍后重试。可能的原因：网络连接问题或搜索引擎不可用。", nil
	}

	return result, nil
}
