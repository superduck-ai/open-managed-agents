package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestStatementLoggerLevels(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		err     error
		level   string
		message string
	}{
		{"server side cancellation stays an error", context.Background(), context.Canceled, "ERROR", "yourbatis statement failed"},
		{"failure while caller canceled", canceled, errors.New("connection reset"), "ERROR", "yourbatis statement failed"},
		{"caller canceled", canceled, fmt.Errorf("query: %w", context.Canceled), "DEBUG", "yourbatis statement canceled"},
		{"success", context.Background(), nil, "DEBUG", "yourbatis statement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := newStatementLogger(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
			logger.LogSQL(tc.ctx, yourbatis.Event{Statement: yourbatis.Statement{ID: "Mapper.Find"}, SQL: "SELECT 1", Duration: time.Millisecond, Err: tc.err})
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatalf("decode %q: %v", output.String(), err)
			}
			if record["level"] != tc.level || record["msg"] != tc.message || record["statement"] != "Mapper.Find" {
				t.Fatalf("record: %v", record)
			}
		})
	}
}
