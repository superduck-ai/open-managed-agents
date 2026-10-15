package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestWorkerRegisterDelay(t *testing.T) {
	for _, tc := range []struct {
		name     string
		epoch    int64
		issuedAt *jwt.NumericDate
		measured bool
	}{
		{"legacy", 0, jwt.NewNumericDate(time.Now()), false},
		{"missing timestamp", 1, nil, false},
		{"future timestamp", 1, jwt.NewNumericDate(time.Now().Add(time.Minute)), false},
		{"too old", 1, jwt.NewNumericDate(time.Now().Add(-16 * time.Minute)), false},
		{"recent", 1, jwt.NewNumericDate(time.Now().Add(-time.Minute)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delay, measured := workerRegisterDelay(SessionCredentialClaims{WorkerEpoch: tc.epoch, RegisteredClaims: jwt.RegisteredClaims{IssuedAt: tc.issuedAt}})
			if measured != tc.measured || (measured && (delay < 59*time.Second || delay > 62*time.Second)) || (!measured && delay != 0) {
				t.Fatalf("delay = %s, measured = %t", delay, measured)
			}
		})
	}
}

func TestDeliveryStatusName(t *testing.T) {
	for _, status := range []string{"", "invalid", "received", "processing", "processed"} {
		want := status
		if status == "" || status == "invalid" {
			want = "other"
		}
		if got := deliveryStatusName(status); got != want {
			t.Errorf("status %q = %q, want %q", status, got, want)
		}
	}
}

func TestResumeSandboxDisabled(t *testing.T) {
	service := newTestService(t, nil)
	outcome, err := service.resumeSandbox(t.Context(), db.CodeSession{ExternalID: "cse_disabled"})
	if err != nil || outcome != "disabled" {
		t.Fatalf("resume = %q, %v", outcome, err)
	}
}

func TestToolPermissionRequestTimestampCompatibility(t *testing.T) {
	var old toolPermissionRequest
	if err := json.Unmarshal([]byte(`{"request_id":"old"}`), &old); err != nil {
		t.Fatal(err)
	}
	if !old.RequestedAt.IsZero() {
		t.Fatal("legacy request acquired a timestamp")
	}
	want := time.Now().UTC()
	data, err := json.Marshal(toolPermissionRequest{RequestID: "request", RequestedAt: want})
	if err != nil {
		t.Fatal(err)
	}
	var got toolPermissionRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.RequestedAt != want {
		t.Fatalf("timestamp = %s, want %s", got.RequestedAt, want)
	}
}

type telemetryDeliveryBroker struct {
	workerevents.Broker
	failure error
}

func (b telemetryDeliveryBroker) InProgress(context.Context, string) error { return b.failure }
func (b telemetryDeliveryBroker) DoubleAck(context.Context, string) error  { return b.failure }

func TestWorkerDeliveryTelemetryCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, status              string
		found, published, failure bool
		applied, ignored          int
		spans                     int
	}{
		{"missing ACK", "processed", false, false, false, 0, 1, 0},
		{"progress failure", "processing", true, true, true, 0, 0, 0},
		{"ACK failure", "processed", true, true, true, 0, 0, 0},
		{"legacy ACK", "processed", true, false, false, 1, 0, 0},
		{"progress", "received", true, true, false, 1, 0, 0},
		{"processed", "processed", true, true, false, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
			service := newTestService(t, nil)
			broker := telemetryDeliveryBroker{Broker: workerevents.NewMemory()}
			if tc.failure {
				broker.failure = errors.New("private transport failure")
			}
			service.WithWorkerEventBroker(broker)
			reference := workerevents.AckRef{AckSubject: "ack", Lane: "task", TraceParent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"}
			if tc.published {
				reference.PublishedAt = time.Now().Add(-time.Second)
			}
			if tc.found {
				if err := service.workerEventAcks.Put(t.Context(), "cse_test", 1, "evt_test", reference); err != nil {
					t.Fatal(err)
				}
			}
			result, _, err := service.applyLockedWorkerDeliveryUpdates(t.Context(), "cse_test", 1, []workerDeliveryUpdate{{EventID: "evt_test", Status: tc.status}})
			if result.Applied != tc.applied || result.Ignored != tc.ignored || (err != nil) != tc.failure {
				t.Fatalf("result = %+v, %v", result, err)
			}
			spans := recorder.Ended()
			if len(spans) != tc.spans {
				t.Fatalf("span count = %d, want %d", len(spans), tc.spans)
			}
			if len(spans) > 0 && (spans[0].Name() != "oma.worker_event.ack" || spans[0].Parent().IsValid() || len(spans[0].Links()) != 1 || len(spans[0].Attributes()) != 3) {
				t.Fatalf("unexpected ACK span: %v", spans[0])
			}
		})
	}
}

func TestWorkerDeliverySpanIsLinkedRoot(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	handler := &Handler{}
	handler.observeWorkerEventDelivery(t.Context(), db.CodeSession{ExternalID: "cse_delivery", SessionExternalID: "sesn_delivery"}, 3, workerevents.Delivery{Lane: "task", DeliveryCount: 2, TraceParent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"}, "evt_delivery")
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "oma.worker_event.deliver" || spans[0].Parent().IsValid() || len(spans[0].Links()) != 1 || len(spans[0].Attributes()) != 6 {
		t.Fatalf("delivery spans: %v", spans)
	}
}
