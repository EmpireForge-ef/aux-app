// Package logging configures the process-wide structured logger (log/slog).
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Setup installs the default slog logger. format is "text" (human-readable,
// default) or "json" (for log aggregators); level is debug|info|warn|error.
// It also routes the standard log package through the same handler.
func Setup(format, level string) {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	var h slog.Handler
	if strings.EqualFold(format, "json") {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
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

// GormWriter adapts GORM's logger.Writer (a Printf-style sink) onto slog, so
// database warnings and errors are emitted as structured logs.
type GormWriter struct{}

// Printf implements gorm logger.Writer.
func (GormWriter) Printf(format string, args ...any) {
	slog.Warn(fmt.Sprintf(format, args...), "component", "gorm")
}
