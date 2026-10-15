package logging

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

type contextRecordHandler struct {
	records *[]slog.Record
	attrs   []slog.Attr
	group   string
	mutate  bool
}

func (h *contextRecordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *contextRecordHandler) Handle(_ context.Context, record slog.Record) error {
	record = record.Clone()
	record.AddAttrs(h.attrs...)
	if h.mutate {
		record.AddAttrs(slog.Bool("mutated", true))
	}
	*h.records = append(*h.records, record)
	return nil
}
func (h *contextRecordHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}
func (h *contextRecordHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.group = name
	return &next
}

func TestContextHandlerWithoutExtractor(t *testing.T) {
	var records []slog.Record
	slog.New(NewContextHandler(&contextRecordHandler{records: &records}, nil)).Info("event")
	if len(records) != 1 || records[0].NumAttrs() != 0 {
		t.Fatal("nil extractor must preserve record")
	}
}

func TestContextHandlerKeepsExplicitAttrs(t *testing.T) {
	var records []slog.Record
	handler := NewContextHandler(&contextRecordHandler{records: &records}, func(context.Context) []slog.Attr {
		return []slog.Attr{slog.String("trace_id", "trace"), slog.String("request_id", "context")}
	})
	slog.New(handler).Info("event", "request_id", "explicit")
	if len(records) != 1 {
		t.Fatal("record was not forwarded")
	}
	var requestIDs []string
	records[0].Attrs(func(attr slog.Attr) bool {
		if attr.Key == "request_id" {
			requestIDs = append(requestIDs, attr.Value.String())
		}
		return true
	})
	if len(requestIDs) != 1 || requestIDs[0] != "explicit" || records[0].NumAttrs() != 2 {
		t.Fatalf("request_id attrs: %v", requestIDs)
	}
}

func TestLevelHandlerFiltersRecords(t *testing.T) {
	var records []slog.Record
	handler := NewLevelHandler(&contextRecordHandler{records: &records}, slog.LevelWarn)
	logger := slog.New(handler).With("component", "test").WithGroup("scope")
	logger.Info("filtered")
	if len(records) != 0 {
		t.Fatal("info was not filtered")
	}
	logger.Warn("visible")
	if len(records) != 1 || records[0].Level != slog.LevelWarn {
		t.Fatal("warning was not forwarded")
	}
}

func TestContextHandlerEnrichesAndFansOut(t *testing.T) {
	var first, second []slog.Record
	handler := NewContextHandler(slog.NewMultiHandler(
		&contextRecordHandler{records: &first, mutate: true},
		&contextRecordHandler{records: &second},
	), func(context.Context) []slog.Attr {
		return []slog.Attr{slog.String("trace_id", "trace"), slog.String("request_id", "request")}
	})
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "event", 0)
	record.AddAttrs(slog.String("first", "1"), slog.String("second", "2"), slog.String("third", "3"), slog.String("fourth", "4"), slog.String("fifth", "5"))
	if err := handler.WithAttrs([]slog.Attr{slog.String("component", "test")}).WithGroup("scope").Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatal("both handlers must receive record")
	}
	for index, received := range []slog.Record{first[0], second[0]} {
		attrs := map[string]string{}
		received.Attrs(func(attr slog.Attr) bool { attrs[attr.Key] = attr.Value.String(); return true })
		if attrs["trace_id"] != "trace" || attrs["request_id"] != "request" || attrs["component"] != "test" {
			t.Fatalf("missing context attrs: %v", attrs)
		}
		if index == 1 && attrs["mutated"] != "" {
			t.Fatal("mutation leaked between handlers")
		}
	}
	if record.NumAttrs() != 5 {
		t.Fatal("original record was mutated")
	}
}
