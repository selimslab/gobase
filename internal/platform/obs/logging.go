// Package obs wires logging, tracing and metrics. It is transport-agnostic:
// nothing here imports net/http.
package obs

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/selimslab/gobase/internal/platform/config"
)

// contextKey is the unexported key type for values this package stores in a
// context, so no other package can collide with it.
type contextKey int

const (
	requestIDKey contextKey = iota
	loggerKey
)

// NewLogger builds the process logger: JSON to w, at the configured level,
// tagged with the service identity so every line is attributable.
func NewLogger(w io.Writer, cfg config.Observability, service, version string) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: ParseLevel(cfg.LogLevel),
	})

	return slog.New(handler).With(
		slog.String("service", service),
		slog.String("version", version),
	)
}

// ParseLevel maps a configured level name to a slog.Level, defaulting to info.
func ParseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
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

// WithRequestID returns a context carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID reports the request ID carried by ctx, or "" when there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)

	return id
}

// WithLogger returns a context carrying a request-scoped logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFrom returns the request-scoped logger in ctx, falling back to the
// default logger so a caller never has to nil-check.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok && logger != nil {
		return logger
	}

	return slog.Default()
}
