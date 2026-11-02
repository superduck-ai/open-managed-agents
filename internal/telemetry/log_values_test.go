package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type namedKind string

type namedCount int

type stringerKind struct{}

func (stringerKind) String() string { return "stringer" }

type captureHandler struct {
	records *[]slog.Record
	attrs   []slog.Attr
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h captureHandler) Handle(_ context.Context, record slog.Record) error {
	record.AddAttrs(h.attrs...)
	*h.records = append(*h.records, record)
	return nil
}

func (h captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return captureHandler{records: h.records, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h captureHandler) WithGroup(string) slog.Handler { return h }

func TestScalarValueHandler(t *testing.T) {
	var records []slog.Record
	logger := slog.New(newScalarValueHandler(captureHandler{records: &records})).With("component", namedKind("http"))
	cause := errors.New("cause")
	logger.Info("event", "kind", namedKind("select"), "count", namedCount(3), "stringer", stringerKind{}, "error", cause,
		"struct", struct{ A int }{1}, slog.Group("group", "inner", namedKind("allow")), "duration", time.Second)
	got := map[string]slog.Value{}
	records[0].Attrs(func(attr slog.Attr) bool {
		got[attr.Key] = attr.Value
		return true
	})
	for key, want := range map[string]slog.Kind{"component": slog.KindString, "kind": slog.KindString, "count": slog.KindInt64, "stringer": slog.KindString, "error": slog.KindAny, "struct": slog.KindAny, "group": slog.KindGroup, "duration": slog.KindDuration} {
		if got[key].Kind() != want {
			t.Fatalf("%s kind = %v, want %v", key, got[key].Kind(), want)
		}
	}
	if got["kind"].String() != "select" || got["stringer"].String() != "stringer" || got["group"].Group()[0].Value.Kind() != slog.KindString {
		t.Fatalf("converted values: %v", got)
	}
}
