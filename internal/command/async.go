package command

import (
	"context"
	"strings"

	"claude-bot/internal/taskqueue"
)

func (r *Router) SubmitAsync(ctx context.Context, msg *IncomingMessage, continueSession bool) (*taskqueue.Task, error) {
	if r.tasks == nil {
		return nil, nil
	}
	content := strings.TrimSpace(msg.Content)
	if strings.HasPrefix(content, "/ask") {
		content = strings.TrimSpace(strings.TrimPrefix(content, "/ask"))
	}
	if content == "" {
		content = strings.TrimSpace(msg.Content)
	}
	return r.tasks.Submit(ctx, taskqueue.SubmitRequest{
		UserID:          msg.UserID,
		GroupID:         msg.GroupID,
		Content:         content,
		ContinueSession: continueSession,
		ProgressFn:      msg.ProgressFn,
	})
}
