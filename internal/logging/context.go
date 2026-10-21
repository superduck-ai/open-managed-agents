package logging

import (
	"context"
	"log/slog"
)

type ContextAttrsFunc func(context.Context) []slog.Attr

type ContextHandler struct {
	next    slog.Handler
	extract ContextAttrsFunc
}

func NewContextHandler(next slog.Handler, extract ContextAttrsFunc) *ContextHandler {
	return &ContextHandler{next: next, extract: extract}
}

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if attrs := missingAttrs(record, h.extract(ctx)); len(attrs) > 0 {
		record = record.Clone()
		record.AddAttrs(attrs...)
	}
	return h.next.Handle(ctx, record)
}

func missingAttrs(record slog.Record, attrs []slog.Attr) []slog.Attr {
	if len(attrs) == 0 {
		return nil
	}
	present := make(map[string]bool, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		present[attr.Key] = true
		return true
	})
	missing := attrs[:0]
	for _, attr := range attrs {
		if !present[attr.Key] {
			missing = append(missing, attr)
		}
	}
	return missing
}

func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{next: h.next.WithAttrs(attrs), extract: h.extract}
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{next: h.next.WithGroup(name), extract: h.extract}
}

type LevelHandler struct {
	next  slog.Handler
	level slog.Leveler
}

func NewLevelHandler(next slog.Handler, level slog.Leveler) *LevelHandler {
	return &LevelHandler{next: next, level: level}
}

func (h *LevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level.Level() && h.next.Enabled(ctx, level)
}

func (h *LevelHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.next.Handle(ctx, record)
}

func (h *LevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LevelHandler{next: h.next.WithAttrs(attrs), level: h.level}
}

func (h *LevelHandler) WithGroup(name string) slog.Handler {
	return &LevelHandler{next: h.next.WithGroup(name), level: h.level}
}
