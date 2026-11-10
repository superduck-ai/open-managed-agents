package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type streamValidationBroker struct {
	workerevents.Broker
	failure          error
	acknowledgements int
}

func (b *streamValidationBroker) DoubleAck(context.Context, string) error {
	b.acknowledgements++
	return b.failure
}

func TestWorkerStreamDeliveryPreservesLegacyTaskCleanup(t *testing.T) {
	for _, item := range []struct {
		name           string
		version        int
		protocol, kind string
		expired, fail  bool
		action         workerStreamAction
		acks           int
	}{
		{name: "invalid envelope is not acknowledged", version: 1, protocol: "dsr1", kind: "user.message", action: workerStreamStop},
		{name: "legacy task is acknowledged and skipped", version: 2, protocol: "dsr1", kind: "user.message", action: workerStreamSkip, acks: 1},
		{name: "expired legacy task still uses cleanup", version: 2, protocol: "dsr1", kind: "user.message", expired: true, action: workerStreamSkip, acks: 1},
		{name: "failed legacy acknowledgement stops stream", version: 2, protocol: "dsr1", kind: "user.message", fail: true, action: workerStreamStop, acks: 1},
		{name: "control remains publishable", version: 2, protocol: "dsr1", kind: "user.interrupt", action: workerStreamPublish},
		{name: "CCR task remains publishable", version: 2, protocol: "ccrv2", kind: "user.message", action: workerStreamPublish},
	} {
		t.Run(item.name, func(t *testing.T) {
			broker := &streamValidationBroker{}
			if item.fail {
				broker.failure = errors.New("unavailable")
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := &Handler{logger: logger, service: &Service{workerEvents: broker, logger: logger}}
			code := db.CodeSession{ExternalID: "cse_task", Metadata: json.RawMessage(`{"config":{"worker_protocol":"` + item.protocol + `"}}`)}
			expiry := time.Now().Add(time.Hour)
			if item.expired {
				expiry = time.Now().Add(-time.Hour)
			}
			action := handler.prepareWorkerStreamDelivery(t.Context(), code, workerevents.Delivery{Envelope: workerevents.EnvelopeV1{Version: item.version, CodeSessionID: code.ExternalID, EventType: item.kind, ExpiresAt: expiry}})
			if action != item.action || broker.acknowledgements != item.acks {
				t.Fatalf("action=%v acks=%d", action, broker.acknowledgements)
			}
		})
	}
}
