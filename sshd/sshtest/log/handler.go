package log

import (
	"context"
	"log/slog"
)

// Handler implements slog.Handler to capture logs.
type Handler struct {
	capture *Capture
	attrs   []slog.Attr
	groups  []string
}

// NewHandler creates a new capture handler.
func NewHandler(capture *Capture) *Handler {
	return &Handler{capture: capture}
}

// Enabled returns true for all levels.
func (h *Handler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle captures the log record.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	entry := Entry{
		Timestamp: r.Time,
		Level:     r.Level,
		Message:   r.Message,
		Attrs:     make(map[string]any),
	}

	// Add handler attrs
	for _, attr := range h.attrs {
		entry.Attrs[attr.Key] = attr.Value.Any()
	}

	// Add record attrs
	r.Attrs(func(a slog.Attr) bool {
		entry.Attrs[a.Key] = a.Value.Any()
		return true
	})

	h.capture.Add(entry)
	return nil
}

// WithAttrs returns a new handler with additional attributes.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := &Handler{
		capture: h.capture,
		attrs:   make([]slog.Attr, len(h.attrs)+len(attrs)),
		groups:  h.groups,
	}
	copy(newHandler.attrs, h.attrs)
	copy(newHandler.attrs[len(h.attrs):], attrs)
	return newHandler
}

// WithGroup returns a new handler with an additional group.
func (h *Handler) WithGroup(name string) slog.Handler {
	newHandler := &Handler{
		capture: h.capture,
		attrs:   h.attrs,
		groups:  append(append([]string{}, h.groups...), name),
	}
	return newHandler
}

// Verify Handler implements slog.Handler
var _ slog.Handler = (*Handler)(nil)
