package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestFailedSSEWritePreservesNewerAcknowledgementReference(t *testing.T) {
	app, codeSession := workerEventRegressionFixture(t, newFakeStore("sse-write-race"))
	registerCodeSessionWorker(t, app, codeSession.ExternalID)
	codeSession, err := getCodeSession(app, t.Context(), codeSession.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	broker := workerevents.NewMemory()
	event := workerevents.EventEnvelope(codeSession.ExternalID, "sse-write-event", "payload-write-event", "user", "", []byte(`{"type":"user"}`), time.Now().Add(time.Hour))
	if err := broker.Publish(t.Context(), event.EventID, event); err != nil {
		t.Fatal(err)
	}
	acks := workerevents.NewMemoryAcknowledgementStore()
	epoch := codeSession.CurrentWorkerEpoch
	replacement := workerevents.AckRef{AckSubject: "newer-delivery-reply"}
	writer := &failedSSEWriter{header: make(http.Header), onWrite: func() {
		// A reconnect receives the same event and replaces the reply while the old write fails.
		if err := acks.Put(t.Context(), codeSession.ExternalID, epoch, event.PayloadEventID, replacement); err != nil {
			t.Fatal(err)
		}
	}}
	router := chi.NewRouter()
	codesessions.NewHandler(app.cfg, newCodeSessionService(app, broker, acks), nil, nil).RegisterV1Routes(router)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/code/sessions/"+codeSession.ExternalID+"/worker/events/stream?worker_epoch="+strconv.FormatInt(epoch, 10), nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+codeSessionIngressToken(t, app, codeSession.ExternalID))
	router.ServeHTTP(writer, request)
	got, found, err := acks.Get(t.Context(), codeSession.ExternalID, epoch, event.PayloadEventID)
	if writer.status != http.StatusOK || writer.writes != 1 || err != nil || !found || got != replacement {
		t.Fatalf("status=%d writes=%d mapping=(%+v, %t, %v)", writer.status, writer.writes, got, found, err)
	}
	if len(broker.Pending(codeSession.ExternalID)) != 1 {
		t.Fatal("failed SSE write acknowledged the message")
	}
}

type failedSSEWriter struct {
	header  http.Header
	status  int
	writes  int
	onWrite func()
}

func (w *failedSSEWriter) Header() http.Header    { return w.header }
func (w *failedSSEWriter) WriteHeader(status int) { w.status = status }
func (w *failedSSEWriter) Flush()                 {}
func (w *failedSSEWriter) Write([]byte) (int, error) {
	w.writes++
	w.onWrite()
	return 0, io.ErrClosedPipe
}
