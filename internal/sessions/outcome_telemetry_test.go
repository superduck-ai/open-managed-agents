package sessions

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestSessionOutcomeTelemetry(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	var output bytes.Buffer
	handler := &Handler{logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	handler.observeSessionOutcomeEvents(t.Context(), []db.SessionEvent{
		{EventType: "session.error", ExternalID: "evt_malformed", SessionExternalID: "sesn_a", Payload: json.RawMessage(`{"error":{"type":"Private Detail","message":"private message"}}`)},
		{EventType: "session.error", ExternalID: "evt_error", SessionExternalID: "sesn_a", Payload: json.RawMessage(`{"type":"session.error","error":{"type":"unknown_error","message":"private message","retry_status":{"type":"exhausted"}}}`)},
		{EventType: "session.status_idle", ExternalID: "evt_idle", SessionExternalID: "sesn_a", Payload: json.RawMessage(`{"type":"session.status_idle","stop_reason":{"type":"retries_exhausted"}}`)},
		{EventType: "session.status_idle", ExternalID: "evt_unknown", SessionExternalID: "sesn_a", Payload: json.RawMessage(`{"type":"session.status_idle","stop_reason":{"type":"private_reason"}}`)},
	})
	if strings.Contains(output.String(), "private") {
		t.Fatalf("payload content leaked to logs: %s", output.String())
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if len(records) != 4 || records[2]["level"] != "DEBUG" || records[3]["stop_reason"] != "other" || records[0]["level"] != "WARN" || records[0]["error_type"] != "other" || records[0]["retry_status"] != "other" ||
		records[1]["error_type"] != "unknown_error" || records[1]["retry_status"] != "exhausted" ||
		records[2]["msg"] != "session status event" || records[2]["stop_reason"] != "retries_exhausted" {
		t.Fatalf("records: %v", records)
	}
	spans := recorder.Ended()
	if len(spans) != 4 || spans[1].Name() != "oma.session.error" || spans[1].Status().Code != codes.Error || spans[2].Name() != "oma.session.status" {
		t.Fatalf("spans: %v", spans)
	}
	attrs := map[string]string{}
	for _, attr := range spans[2].Attributes() {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}
	if attrs["oma.stop_reason"] != "retries_exhausted" {
		t.Fatalf("status attributes: %v", attrs)
	}
	data := telemetrytest.Collect(t, reader)
	counts := map[string]int64{}
	for _, name := range []string{"oma.session.errors", "oma.session.turns"} {
		for _, point := range telemetrytest.Points(t, data, name) {
			key := name
			for _, attr := range point.Attributes.ToSlice() {
				key += "," + string(attr.Key) + "=" + attr.Value.Emit()
			}
			counts[key] = int64(point.Value)
		}
	}
	for key, want := range map[string]int64{
		"oma.session.errors,error.type=other,oma.retry_status=other":             1,
		"oma.session.errors,error.type=unknown_error,oma.retry_status=exhausted": 1,
		"oma.session.turns,oma.stop_reason=retries_exhausted":                    1,
		"oma.session.turns,oma.stop_reason=other":                                1,
	} {
		if counts[key] != want {
			t.Fatalf("metrics = %v", counts)
		}
	}
}

func TestAnnotateSessionInputShape(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	ctx, span := telemetry.Start(t.Context(), "request")
	annotateSessionInputShape(ctx, []json.RawMessage{
		json.RawMessage(`not json`),
		json.RawMessage(`{"type":"private.custom","content":[{"type":"Private Block"}]}`),
		json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"private text"},{"type":"document","source":{"type":"file","file_id":"file_private"}}]}`),
	})
	span.End()
	attrs := map[string]string{}
	for _, attr := range recorder.Ended()[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}
	if attrs["oma.input.event_types"] != `["other","user.message"]` || attrs["oma.input.block_types"] != `["document","other","text"]` || attrs["oma.input.events"] != "3" || attrs["oma.input.bytes"] == "0" {
		t.Fatalf("input shape attributes: %v", attrs)
	}
	for _, value := range attrs {
		if strings.Contains(value, "private") {
			t.Fatalf("input content leaked: %v", attrs)
		}
	}
}
