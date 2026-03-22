package taskqueue

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Task struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	GroupID         string     `json:"group_id"`
	Content         string     `json:"content"`
	Status          Status     `json:"status"`
	Result          string     `json:"result"`
	Error           string     `json:"error"`
	SessionID       string     `json:"session_id"`
	ContinueSession bool       `json:"continue_session"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	DoneAt          *time.Time `json:"done_at,omitempty"`
}

type SubmitRequest struct {
	UserID          string
	GroupID         string
	Content         string
	ContinueSession bool
	ProgressFn      func(string)
}
