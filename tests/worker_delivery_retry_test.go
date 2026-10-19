package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type failingDeliveryBroker struct {
	*workerevents.MemoryBroker
	fail bool
}

func (b *failingDeliveryBroker) DoubleAck(ctx context.Context, subject string) error {
	if b.fail {
		return errors.New("ACK unavailable")
	}
	return b.MemoryBroker.DoubleAck(ctx, subject)
}

func TestWorkerProcessingPersistsBeforeFailedACKAndRetryIsIdempotent(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("processing-ack-retry"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`, defaultTestKey)
	inputID := sessionEventStringField(t, sent.Data[0], "id")
	broker := &failingDeliveryBroker{MemoryBroker: workerevents.NewMemory(), fail: true}
	envelope := workerevents.EventEnvelope(worker.ExternalID, "processing-retry", inputID, "user", "", []byte(`{"type":"user"}`), time.Now().Add(time.Hour))
	if err := broker.Publish(t.Context(), envelope.EventID, envelope); err != nil {
		t.Fatal(err)
	}
	sub, err := broker.Subscribe(t.Context(), worker.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var delivery workerevents.Delivery
	select {
	case delivery = <-sub.Messages():
	case <-time.After(time.Second):
		t.Fatal("missing delivery")
	}
	acks := workerevents.NewMemoryAcknowledgementStore()
	if err := acks.Put(t.Context(), worker.ExternalID, worker.CurrentWorkerEpoch, envelope.EventID, workerevents.AckRef{AckSubject: delivery.AckSubject, PublicEventID: inputID}); err != nil {
		t.Fatal(err)
	}
	service := newCodeSessionService(app, broker, acks)
	service.SetPublicEventSink(sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil))
	router := chi.NewRouter()
	codesessions.NewHandler(app.cfg, service, nil, nil).RegisterV1Routes(router)
	app.db.SQLDB().SetMaxOpenConns(1)
	update := func() int {
		request := httptest.NewRequest(http.MethodPost, "/code/sessions/"+worker.ExternalID+"/worker/events/delivery", strings.NewReader(`{"worker_epoch":`+quoteJSON(epoch)+`,"updates":[{"event_id":"processing-retry","status":"processed"}]}`))
		request.Header.Set("Authorization", "Bearer "+codeSessionIngressToken(t, app, worker.ExternalID))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	if update() == http.StatusOK {
		t.Fatal("failed ACK reported success")
	}
	first, err := app.db.GetSessionEvent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, inputID)
	if err != nil || first.ProcessedAt.IsZero() || len(broker.Pending(worker.ExternalID)) != 1 {
		t.Fatalf("processing not persisted before ACK: %+v %v", first, err)
	}
	broker.fail = false
	if status := update(); status != http.StatusOK {
		t.Fatalf("retry status = %d", status)
	}
	after, err := app.db.GetSessionEvent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, inputID)
	if err != nil || !after.ProcessedAt.Equal(first.ProcessedAt) || len(broker.Pending(worker.ExternalID)) != 0 {
		t.Fatalf("retry changed processing state: %+v %v", after, err)
	}
}
