package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"log/slog"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionTelemetryReasons(t *testing.T) {
	for _, tc := range []struct {
		err                error
		rejection, handoff string
	}{
		{errors.New("arbitrary sensitive details"), "other", "other"},
		{fmt.Errorf("wrapped: %w", db.ErrInvalidState), "archived", "other"},
		{db.ErrSessionInputConflict, "conflict", "other"},
		{fmt.Errorf("wrapped: %w", codesessions.ErrWorkerEventUnavailable), "other", "worker_event_unavailable"},
	} {
		if got := sessionEventRejectionReason(tc.err); got != tc.rejection {
			t.Errorf("rejection = %q, want %q", got, tc.rejection)
		}
		if got := sessionHandoffFailureReason(tc.err); got != tc.handoff {
			t.Errorf("handoff = %q, want %q", got, tc.handoff)
		}
	}
}

func TestSessionStatusTelemetry(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	var output bytes.Buffer
	handler := &Handler{logger: slog.New(slog.NewJSONHandler(&output, nil))}
	handler.logSessionStatusEvents(t.Context(), []db.SessionEvent{{EventType: "user.message", Payload: json.RawMessage(`{"content":"private"}`)}})
	if output.Len() != 0 || len(recorder.Ended()) != 0 {
		t.Fatal("non-status event was logged")
	}
	handler.logSessionStatusEvents(t.Context(), []db.SessionEvent{{EventType: "session.status_running", ExternalID: "evt_status", SessionExternalID: "sesn_status", Payload: json.RawMessage(`{"content":"private"}`)}})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if len(record) != 6 || record["msg"] != "session status event" || record["level"] != "INFO" || record["session_id"] != "sesn_status" || record["event_id"] != "evt_status" || record["event_type"] != "session.status_running" {
		t.Fatalf("status log: %v", record)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "oma.session.status" || len(spans[0].Attributes()) != 3 || len(spans[0].Events()) != 0 {
		t.Fatalf("status spans: %v", spans)
	}
}
