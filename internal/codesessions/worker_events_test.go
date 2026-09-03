package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestPublishInboundEvent(t *testing.T) {
	t.Run("publish failure does not escape the PostgreSQL write boundary", func(t *testing.T) {
		broker := &recordingWorkerEventBroker{publishErr: errors.New("unavailable")}
		service := newTestService(t, nil).WithWorkerEventBroker(broker)
		service.publishInboundEvent(t.Context(), workerEventFixture())
		if len(broker.envelopes) != 1 {
			t.Fatalf("publish calls = %d, want 1", len(broker.envelopes))
		}
	})

	t.Run("publishes the full versioned envelope", func(t *testing.T) {
		broker := &recordingWorkerEventBroker{}
		service := newTestService(t, nil).WithWorkerEventBroker(broker)
		service.publishInboundEvent(t.Context(), workerEventFixture())
		if len(broker.envelopes) != 1 {
			t.Fatalf("publish calls = %d, want 1", len(broker.envelopes))
		}
		envelope := broker.envelopes[0]
		if envelope.Version != 1 || envelope.CodeSessionID != "cse_test" || envelope.EventID != "csev_test" || envelope.PayloadEventID != "payload-test" || envelope.SequenceNum != 9 || envelope.EventType != "user" || envelope.EventSubtype != "message" || string(envelope.Payload) != `{"text":"hello"}` {
			t.Fatalf("envelope = %#v", envelope)
		}
	})
}

func workerEventFixture() db.CodeSessionEvent {
	payloadID := "payload-test"
	return db.CodeSessionEvent{
		ExternalID: "csev_test", CodeSessionExternalID: "cse_test", SequenceNum: 9,
		EventType: "user", EventSubtype: "message", PayloadUUID: &payloadID,
		Payload: json.RawMessage(`{"text":"hello"}`),
	}
}

type recordingWorkerEventBroker struct {
	envelopes  []workerevents.EnvelopeV1
	publishErr error
}

func (b *recordingWorkerEventBroker) Publish(_ context.Context, envelope workerevents.EnvelopeV1) error {
	b.envelopes = append(b.envelopes, envelope)
	return b.publishErr
}

func (b *recordingWorkerEventBroker) Subscribe(context.Context, string) (workerevents.Subscription, error) {
	return nil, errors.New("not implemented")
}
