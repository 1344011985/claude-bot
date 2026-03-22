package feishu

import (
	"context"
	"fmt"
	"time"

	"claude-bot/internal/command"
)

func (b *Bot) dispatchAsyncOrSync(ctx context.Context, userID, chatID, content string, progressFn func(string)) string {
	msg := &command.IncomingMessage{
		UserID:     userID,
		GroupID:    chatID,
		Content:    content,
		ProgressFn: progressFn,
	}
	if b.router != nil {
		if task, err := b.router.SubmitAsync(ctx, msg, true); err == nil && task != nil {
			result, waitErr := b.waitTaskResult(ctx, task.ID, 10*time.Minute)
			if waitErr == nil {
				return result
			}
			b.logger.Warn("async task failed, fallback to sync dispatch", "task_id", task.ID, "err", waitErr)
		} else if err != nil {
			b.logger.Warn("submit async failed, fallback to sync dispatch", "err", err)
		}
	}
	return b.dispatch(ctx, userID, chatID, content, progressFn)
}

func (b *Bot) waitTaskResult(ctx context.Context, taskID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("wait task timeout: %s", taskID)
		}
		task, err := b.router.Tasks().Get(taskID)
		if err != nil {
			return "", err
		}
		switch task.Status {
		case "done":
			return task.Result, nil
		case "failed", "cancelled":
			if task.Error != "" {
				return "", fmt.Errorf(task.Error)
			}
			return "", fmt.Errorf("task %s ended with status %s", taskID, task.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
