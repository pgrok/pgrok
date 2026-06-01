package logx

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Logger wraps *slog.Logger with scoping and builder methods that preserve the
// Logger type.
type Logger struct {
	*slog.Logger
}

// New creates a Logger backed by the given handler.
func New(h slog.Handler) *Logger {
	return &Logger{Logger: slog.New(h)}
}

// NewNoopLogger creates a Logger that discards all output.
func NewNoopLogger() *Logger {
	return New(slog.NewTextHandler(io.Discard, nil))
}

// Scoped returns a child logger whose messages are prefixed with "[name] ".
func (l *Logger) Scoped(name string) *Logger {
	return New(newPrefixHandler("["+name+"] ", l.Handler()))
}

// With returns a child logger that includes the given attributes in every
// record.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{Logger: l.Logger.With(args...)}
}

// WithGroup returns a child logger that nests subsequent attributes under the
// given group name.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{Logger: l.Logger.WithGroup(name)}
}

// FatalContext logs at error level and exits the process with status code 1.
func (l *Logger) FatalContext(ctx context.Context, msg string, args ...any) {
	l.ErrorContext(ctx, msg, args...)
	os.Exit(1)
}
