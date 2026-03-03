package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New creates a JSON-format structured logger at the given level.
// level must be one of: debug, info, warn, error (case-insensitive).
// Unknown levels default to info.
func New(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})
	return slog.New(handler)
}
