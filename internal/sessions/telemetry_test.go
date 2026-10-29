package sessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestSessionTelemetryReasons(t *testing.T) {
	for _, tc := range []struct {
		err                error
		rejection, handoff string
	}{
		{errors.New("arbitrary sensitive details"), "other", "other"},
		{fmt.Errorf("wrapped: %w", db.ErrInvalidState), "archived", "other"},
		{fmt.Errorf("wrapped: %w", db.ErrSessionToolUseNotPending), "tool_use_not_pending", "other"},
		{db.ErrSessionInputConflict, "conflict", "other"},
		{fmt.Errorf("wrapped: %w", db.ErrSessionInputPaused), "paused", "other"},
		{fmt.Errorf("wrapped: %w", codesessions.ErrWorkerEventUnavailable), "other", "worker_event_unavailable"},
	} {
		if got := sessionEventRejectionReason(tc.err); got != tc.rejection {
			t.Errorf("rejection = %q, want %q", got, tc.rejection)
		}
		reader := telemetrytest.MetricReader(t)
		recordSessionHandoffFailure(t.Context(), tc.err)
		telemetrytest.AssertSingle(t, telemetrytest.Collect(t, reader), "oma.session.handoff.failures", 1, map[string]string{"oma.reason": tc.handoff})
	}
}

func TestSessionStatusTelemetry(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	var output bytes.Buffer
	handler := &Handler{logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	handler.observeSessionOutcomeEvents(t.Context(), []db.SessionEvent{{EventType: "user.message", Payload: json.RawMessage(`{"content":"private"}`)}})
	if output.Len() != 0 || len(recorder.Ended()) != 0 {
		t.Fatal("non-status event was logged")
	}
	handler.observeSessionOutcomeEvents(t.Context(), []db.SessionEvent{{EventType: "session.status_running", ExternalID: "evt_status", SessionExternalID: "sesn_status", Payload: json.RawMessage(`{"content":"private"}`)}})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if len(record) != 6 || record["msg"] != "session status event" || record["level"] != "DEBUG" || record["session_id"] != "sesn_status" || record["event_id"] != "evt_status" || record["event_type"] != "session.status_running" {
		t.Fatalf("status log: %v", record)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "oma.session.status" || len(spans[0].Attributes()) != 3 || len(spans[0].Events()) != 0 {
		t.Fatalf("status spans: %v", spans)
	}
}
