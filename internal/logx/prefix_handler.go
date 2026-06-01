package logx

import (
	"context"
	"log/slog"
)

// prefixHandler wraps a slog.Handler and prepends a fixed prefix to every
// record's message. Used by Logger.Scoped to add component context without
// relying on attribute grouping.
type prefixHandler struct {
	prefix  string
	handler slog.Handler
}

func newPrefixHandler(prefix string, handler slog.Handler) *prefixHandler {
	return &prefixHandler{prefix: prefix, handler: handler}
}

func (h *prefixHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *prefixHandler) Handle(ctx context.Context, r slog.Record) error {
	r2 := r.Clone()
	r2.Message = h.prefix + r2.Message
	return h.handler.Handle(ctx, r2)
}

func (h *prefixHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return newPrefixHandler(h.prefix, h.handler.WithAttrs(attrs))
}

func (h *prefixHandler) WithGroup(name string) slog.Handler {
	return newPrefixHandler(h.prefix, h.handler.WithGroup(name))
}
