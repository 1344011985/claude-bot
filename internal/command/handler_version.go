package command

import (
	"context"
	"fmt"
)

// --- /version handler ---

type versionHandler struct{}

func (h *versionHandler) Handle(ctx context.Context, msg *IncomingMessage) (string, error) {
	return fmt.Sprintf("claude-bot commit=%s built=%s", GitCommit, BuildDate), nil
}
