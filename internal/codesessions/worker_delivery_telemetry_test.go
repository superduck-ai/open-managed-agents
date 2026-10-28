package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestRecordFailedWorkerDeliveryUpdates(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	updates := []workerDeliveryUpdate{{EventID: "a", Status: "processed"}, {EventID: "b", Status: "processing"}, {EventID: "c", Status: "bogus"}}
	recordFailedWorkerDeliveryUpdates(canceled, updates[1:], errors.New("private failure"))
	recordFailedWorkerDeliveryUpdates(t.Context(), updates[:1], errors.New("private failure"))
	got := map[string]int64{}
	for _, point := range telemetrytest.Points(t, telemetrytest.Collect(t, reader), "oma.worker_event.updates") {
		status, _ := point.Attributes.Value(attribute.Key("oma.delivery.status"))
		outcome, _ := point.Attributes.Value(attribute.Key("oma.outcome"))
		got[status.AsString()+"/"+outcome.AsString()] = int64(point.Value)
	}
	want := map[string]int64{"processing/canceled": 1, "other/canceled": 1, "processed/error": 1}
	if len(got) != len(want) {
		t.Fatalf("updates = %v", got)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("updates = %v", got)
		}
	}
}

func TestWorkerStreamCloseReason(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		request, stream context.Context
		want            string
	}{
		{canceled, canceled, "client_closed"},
		{t.Context(), canceled, "server_shutdown"},
		{t.Context(), t.Context(), "server_closed"},
	} {
		if got := workerStreamCloseReason(tc.request, tc.stream); got != tc.want {
			t.Fatalf("close reason = %q, want %q", got, tc.want)
		}
	}
}

func TestAnnotateWorkerEventBatch(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	ctx, span := telemetry.Start(t.Context(), "request")
	annotateWorkerEventBatch(ctx, 4, []string{"Private Payload", "stream_event", "stream_event"})
	span.End()
	attrs := map[string]string{}
	for _, attr := range recorder.Ended()[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}
	if attrs["oma.worker_event.count"] != "4" || attrs["oma.worker_event.types"] != `["other","stream_event"]` {
		t.Fatalf("batch attributes: %v", attrs)
	}
}

func TestObserveInputWorkerAvailability(t *testing.T) {
	now := time.Now()
	expired, active := now.Add(-90*time.Second), now.Add(time.Minute)
	for _, tc := range []struct {
		name       string
		session    db.CodeSession
		wantLease  string
		wantWarn   bool
		wantExpiry bool
	}{
		{"retired after idle", db.CodeSession{ExternalID: "cse_a", SessionExternalID: "sesn_a", ConnectionStatus: "disconnected", WorkerStatus: "idle", WorkerLeaseExpiresAt: &expired}, "false", true, true},
		{"never leased", db.CodeSession{ExternalID: "cse_b", SessionExternalID: "sesn_b", ConnectionStatus: "disconnected", WorkerStatus: "idle"}, "false", true, false},
		{"connected", db.CodeSession{ExternalID: "cse_c", SessionExternalID: "sesn_c", ConnectionStatus: "connected", WorkerStatus: "idle", WorkerLeaseExpiresAt: &active}, "true", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			service := &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			recorder := telemetrytest.SpanRecorder(t)
			ctx, span := telemetry.Start(t.Context(), "request")
			service.observeInputWorkerAvailability(ctx, tc.session, 1, now)
			span.End()
			attrs := map[string]string{}
			for _, attr := range recorder.Ended()[0].Attributes() {
				attrs[string(attr.Key)] = attr.Value.Emit()
			}
			if attrs["oma.connection.status"] != tc.session.ConnectionStatus || attrs["oma.worker.status"] != "idle" || attrs["oma.worker.lease_active"] != tc.wantLease {
				t.Fatalf("span attributes: %v", attrs)
			}
			if (logs.Len() > 0) != tc.wantWarn {
				t.Fatalf("warn log presence = %v: %s", logs.Len() > 0, logs.String())
			}
			if !tc.wantWarn {
				return
			}
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			_, hasExpiry := record["lease_expired_s"]
			if record["level"] != "WARN" || record["msg"] != "session input queued without connected worker" || record["code_session_id"] != tc.session.ExternalID || record["session_id"] != tc.session.SessionExternalID || hasExpiry != tc.wantExpiry {
				t.Fatalf("warn record: %v", record)
			}
		})
	}
}
