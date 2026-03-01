package observability

import (
	"log/slog"
	"os"
	"strings"
)

func NewLogger(level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	h := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(h)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
