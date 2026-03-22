package httpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"claude-bot/internal/taskqueue"
)

type Server struct {
	addr  string
	queue taskqueue.Queue
	log   *slog.Logger
	http  *http.Server
}

type chatRequest struct {
	ChatID     string `json:"chat_id"`
	UserID     string `json:"user_id"`
	SenderName string `json:"sender_name"`
	Content    string `json:"content"`

	// legacy compat
	User string `json:"user"`
	Msg  string `json:"msg"`
}

type chatResponse struct {
	OK     bool   `json:"ok,omitempty"`
	Reply  string `json:"reply,omitempty"`
	TaskID string `json:"task_id,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

func New(addr string, queue taskqueue.Queue, log *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{addr: addr, queue: queue, log: log}
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/chat", s.handleChat)
	mux.HandleFunc("/task", s.handleTask)
	mux.HandleFunc("/cancel", s.handleCancel)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

func (s *Server) Start() error {
	s.log.Info("http bridge starting", "addr", s.addr)
	err := s.http.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.log.Error("panic in http bridge /chat", "panic", fmt.Sprintf("%v", rec))
			writeJSON(w, http.StatusInternalServerError, chatResponse{Error: fmt.Sprintf("internal panic: %v", rec)})
		}
	}()

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, chatResponse{Error: "method not allowed"})
		return
	}
	if s.queue == nil {
		writeJSON(w, http.StatusServiceUnavailable, chatResponse{Error: "task queue not configured"})
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, chatResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = strings.TrimSpace(req.User)
	}
	if userID == "" {
		userID = "http_bridge_user"
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		content = strings.TrimSpace(req.Msg)
	}
	if content == "" {
		writeJSON(w, http.StatusBadRequest, chatResponse{Error: "content/msg is required"})
		return
	}

	task, err := s.queue.Submit(r.Context(), taskqueue.SubmitRequest{
		UserID:          userID,
		GroupID:         strings.TrimSpace(req.ChatID),
		Content:         content,
		ContinueSession: false,
	})
	if err != nil {
		s.log.Error("http bridge submit failed", "err", err, "user_id", userID)
		writeJSON(w, http.StatusInternalServerError, chatResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, chatResponse{OK: true, TaskID: task.ID, Status: string(task.Status)})
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, chatResponse{Error: "method not allowed"})
		return
	}
	if s.queue == nil {
		writeJSON(w, http.StatusServiceUnavailable, chatResponse{Error: "task queue not configured"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, chatResponse{Error: "id is required"})
		return
	}
	task, err := s.queue.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, chatResponse{Error: "task not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": task})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, chatResponse{Error: "method not allowed"})
		return
	}
	if s.queue == nil {
		writeJSON(w, http.StatusServiceUnavailable, chatResponse{Error: "task queue not configured"})
		return
	}
	var body struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, chatResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	body.TaskID = strings.TrimSpace(body.TaskID)
	if body.TaskID == "" {
		writeJSON(w, http.StatusBadRequest, chatResponse{Error: "task_id is required"})
		return
	}
	if err := s.queue.Cancel(body.TaskID); err != nil {
		writeJSON(w, http.StatusInternalServerError, chatResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
