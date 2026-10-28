package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
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
			reader := telemetrytest.MetricReader(t)
			observeWorkerRegistered(t.Context(), SessionCredentialClaims{WorkerEpoch: tc.epoch, RegisteredClaims: jwt.RegisteredClaims{IssuedAt: tc.issuedAt}})
			data := telemetrytest.Collect(t, reader)
			if !tc.measured {
				if len(data) != 0 {
					t.Fatalf("unmeasured delay recorded: %v", data)
				}
				return
			}
			if point := telemetrytest.Single(t, data, "oma.worker.register.delay"); point.Count != 1 || point.Value < 59 || point.Value > 62 {
				t.Fatalf("register delay: %+v", point)
			}
		})
	}
}

func TestWorkerEventUpdateStatusIsBounded(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	for _, status := range []string{"", "invalid", "received", "processing", "processed"} {
		recordWorkerEventUpdate(t.Context(), status, "applied")
	}
	statuses := map[string]float64{}
	for _, point := range telemetrytest.Points(t, telemetrytest.Collect(t, reader), "oma.worker_event.updates") {
		status, _ := point.Attributes.Value("oma.delivery.status")
		statuses[status.AsString()] = point.Value
	}
	if len(statuses) != 4 || statuses["other"] != 2 || statuses["processed"] != 1 {
		t.Fatalf("statuses: %v", statuses)
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
			recorder := telemetrytest.SpanRecorder(t)
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
			references := map[string]workerevents.AckRef{}
			if tc.found {
				if err := service.workerEventAcks.Put(t.Context(), "cse_test", 1, "evt_test", reference); err != nil {
					t.Fatal(err)
				}
				references["evt_test"] = reference
			}
			result, _, err := service.applyLockedWorkerDeliveryUpdates(t.Context(), "cse_test", 1, []workerDeliveryUpdate{{EventID: "evt_test", Status: tc.status}}, references)
			if result.Applied != tc.applied || result.Ignored != tc.ignored || (err != nil) != tc.failure {
				t.Fatalf("result = %+v, %v", result, err)
			}
			if len(recorder.Ended()) != 0 {
				t.Fatal("ACK span emitted before commit")
			}
			if err == nil {
				observeSettledWorkerDeliveries(t.Context(), "cse_test", result.settled)
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
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	handler := &Handler{}
	handler.observeWorkerEventDelivery(t.Context(), db.CodeSession{ExternalID: "cse_delivery", SessionExternalID: "sesn_delivery"}, 3, workerevents.Delivery{Lane: "task", DeliveryCount: 2, TraceParent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"}, "evt_delivery")
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "oma.worker_event.deliver" || spans[0].Parent().IsValid() || len(spans[0].Links()) != 1 || len(spans[0].Attributes()) != 6 {
		t.Fatalf("delivery spans: %v", spans)
	}
	telemetrytest.AssertSingle(t, telemetrytest.Collect(t, reader), "oma.worker_event.redeliveries", 1, map[string]string{"oma.worker_event.lane": "task"})
}

func TestWorkerEventAckJoinsRequestTrace(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	ctx, request := telemetry.Start(t.Context(), "POST delivery")
	observeWorkerEventAck(ctx, "cse_ack", "evt_ack", workerevents.AckRef{Lane: "reply", PublishedAt: time.Now().Add(-2 * time.Second), TraceParent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"})
	request.End()
	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].Name() != "oma.worker_event.ack" || spans[0].Parent().SpanID() != request.SpanContext().SpanID() || len(spans[0].Links()) != 1 {
		t.Fatalf("ack spans: %v", spans)
	}
	telemetrytest.AssertSingle(t, telemetrytest.Collect(t, reader), "oma.worker_event.processed.latency", 2, map[string]string{"oma.worker_event.lane": "reply"})
}

type unusedSandboxTimeoutExtender struct{}

func (unusedSandboxTimeoutExtender) SetTimeout(context.Context, string, time.Duration) error {
	return errors.New("brain claim sessions must not extend sandbox timeouts")
}

func TestResumeSandboxSkipsBrainClaimTransport(t *testing.T) {
	service := newTestService(t, nil)
	service.sandboxTimeoutExtender = unusedSandboxTimeoutExtender{}
	outcome, err := service.resumeSandbox(t.Context(), db.CodeSession{ExternalID: "cse_claim", Metadata: []byte(`{"config":{"worker_transport":"claim"}}`)})
	if err != nil || outcome != "brain_claim" {
		t.Fatalf("resume = %q, %v", outcome, err)
	}
}

func TestBrainClaimPollOutcomes(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	startedAt := time.Now().Add(-time.Second)
	observeBrainClaimPoll(t.Context(), startedAt, false, errors.New("database unavailable"))
	observeBrainClaimPoll(t.Context(), startedAt, false, context.Canceled)
	observeBrainClaimPoll(t.Context(), startedAt, false, nil)
	observeBrainClaimPoll(t.Context(), startedAt, true, nil)
	outcomes := map[string]uint64{}
	for _, point := range telemetrytest.Points(t, telemetrytest.Collect(t, reader), "oma.brain_claim.poll.duration") {
		outcome, _ := point.Attributes.Value(telemetry.AttrOutcome)
		outcomes[outcome.AsString()] = point.Count
	}
	if len(outcomes) != 4 || outcomes["error"] != 1 || outcomes["canceled"] != 1 || outcomes["empty"] != 1 || outcomes["claimed"] != 1 {
		t.Fatalf("poll outcomes: %v", outcomes)
	}
}

func TestBrainClaimIssueLinksDispatchTrace(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	observeBrainClaimIssued(t.Context(), "worker-a", db.CodeSessionClaim{CodeSessionExternalID: "cse_takeover", WorkerEpoch: 4})
	if data := telemetrytest.Collect(t, reader); len(data) != 0 {
		t.Fatalf("unrequested claim recorded queue wait: %v", data)
	}
	requestedAt := time.Now().Add(-3 * time.Second)
	observeBrainClaimIssued(t.Context(), "worker-a", db.CodeSessionClaim{
		CodeSessionExternalID: "cse_requested", SessionExternalID: "sesn_requested", WorkerEpoch: 5,
		RequestedAt: &requestedAt, TraceParent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
	})
	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].Name() != "oma.brain_claim.issue" || len(spans[0].Links()) != 0 {
		t.Fatalf("takeover claim spans: %v", spans)
	}
	if spans[1].Parent().IsValid() || len(spans[1].Links()) != 1 || spans[1].Links()[0].SpanContext.TraceID().String() != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("requested claim span: parent=%v links=%v", spans[1].Parent(), spans[1].Links())
	}
	if point := telemetrytest.Single(t, telemetrytest.Collect(t, reader), "oma.brain_claim.queue_wait"); point.Count != 1 || point.Value < 3 || point.Value > 5 {
		t.Fatalf("queue wait: %+v", point)
	}
}

func TestBrainClaimReleaseAndExhaustionCounters(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	recordBrainClaimRelease(t.Context(), BrainClaimShutdown, nil)
	recordBrainClaimRelease(t.Context(), BrainClaimIdle, db.ErrPreconditionFailed)
	recordBrainClaimRelease(t.Context(), BrainClaimAdmissionFailed, errors.New("database unavailable"))
	recordBrainClaimExhausted(t.Context(), "permanent")
	data := telemetrytest.Collect(t, reader)
	releases := map[string]float64{}
	for _, point := range telemetrytest.Points(t, data, "oma.brain_claim.releases") {
		reason, _ := point.Attributes.Value(telemetry.AttrReason)
		outcome, _ := point.Attributes.Value(telemetry.AttrOutcome)
		releases[reason.AsString()+"/"+outcome.AsString()] = point.Value
	}
	if len(releases) != 3 || releases["shutdown/ok"] != 1 || releases["idle/conflict"] != 1 || releases["admission_failed/error"] != 1 {
		t.Fatalf("releases: %v", releases)
	}
	telemetrytest.AssertSingle(t, data, "oma.brain_claim.exhausted", 1, map[string]string{"oma.reason": "permanent"})
}
