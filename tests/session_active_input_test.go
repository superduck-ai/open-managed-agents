package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSessionRejectsInputAtBudgetPause(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("budget-input"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"id":"budget-idle","uuid":"budget-idle","type":"session.status_idle","stop_reason":{"type":"budget_reached"}}`))
	response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+worker.SessionExternalID+"/events", strings.NewReader(`{"events":[{"type":"user.message","content":[{"type":"text","text":"blocked"}]}]}`), defaultTestKey, true)
	assertError(t, response, http.StatusBadRequest, "invalid_request_error")
}

func TestSessionQueuesActiveInputs(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("active-inputs"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	app.db.SQLDB().SetMaxOpenConns(1)
	send := func(text string) string {
		t.Helper()
		sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":`+quoteJSON(text)+`}]}]}`, defaultTestKey)
		if len(sent.Data) != 1 || sessionInputProcessedAt(t, sent.Data[0]) != "" {
			t.Fatalf("input not queued: %s", sent.Data)
		}
		return sessionEventStringField(t, sent.Data[0], "id")
	}
	first := send("first")
	consumePublicInput(t, app, worker, epoch, first)
	putCodeSessionWorkerState(t, app, worker.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	second := send("second")
	third := send("third")
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"id":"first-idle","uuid":"first-idle","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`))
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("queued inputs lost running status: %s", status)
	}
	history := listSessionEvents(t, app, worker.SessionExternalID, "types[]=user.message", defaultTestKey)
	if len(history.Data) != 3 || sessionInputProcessedAt(t, history.Data[0]) == "" || sessionInputProcessedAt(t, history.Data[1]) != "" || sessionInputProcessedAt(t, history.Data[2]) != "" {
		t.Fatalf("processing states: %s", history.Data)
	}
	consumePublicInput(t, app, worker, epoch, second)
	consumePublicInput(t, app, worker, epoch, third)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"id":"last-idle","uuid":"last-idle","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`))
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "idle" {
		t.Fatalf("drained session status: %s", status)
	}
	history = listSessionEvents(t, app, worker.SessionExternalID, "types[]=user.message", defaultTestKey)
	for i, id := range []string{first, second, third} {
		if sessionEventStringField(t, history.Data[i], "id") != id || sessionInputProcessedAt(t, history.Data[i]) == "" {
			t.Fatalf("processed history order: %s", history.Data)
		}
	}
}
