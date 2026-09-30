package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestSessionWorkerInterruptedResult(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		interrupt bool
		errorType string
		subtype   string
		newTurn   bool
		idleFirst bool
		want      string
	}{
		{name: "connection cancelled without interrupt", errorType: "cancelled", want: "retries_exhausted"},
		{name: "upstream failure after interrupt", interrupt: true, errorType: "http_error", want: "retries_exhausted"},
		{name: "budget exhausted after interrupt", interrupt: true, errorType: "cancelled", subtype: "error_max_budget_usd", want: "retries_exhausted"},
		{name: "previous turn interrupted", interrupt: true, errorType: "cancelled", newTurn: true, want: "retries_exhausted"},
		{name: "interrupt result before idle", interrupt: true, errorType: "cancelled", subtype: "error_during_execution", want: "end_turn"},
		{name: "interrupt result after idle", interrupt: true, errorType: "cancelled", idleFirst: true, want: "end_turn"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("interrupt-result"))
			record, epoch := newPayloadIntegrationSession(t, app)
			workerState := func(status string) {
				putCodeSessionWorkerState(t, app, record.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":%q}`, epoch, status))
			}
			workerState("running")
			service := newCodeSessionService(app, nil, nil)
			sink := sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil)
			service.SetPublicEventSink(sink)
			request, err := service.BeginModelRequest(t.Context(), record.WorkspaceUUID, record.SessionExternalID, record.ExternalID, "", "model-mock")
			if err != nil {
				t.Fatal(err)
			}
			if scenario.interrupt {
				resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+record.SessionExternalID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.interrupt"}]}`), defaultTestKey, true)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("interrupt status = %d", resp.StatusCode)
				}
			}
			if err := service.EndModelRequest(t.Context(), request, codesessions.ModelRequestResult{
				EndedAt: time.Now().UTC(), ErrorType: scenario.errorType,
				Usage: codesessions.ModelRequestUsage{InputTokens: new(int64(9)), OutputTokens: new(int64(1))},
			}); err != nil {
				t.Fatal(err)
			}
			if scenario.newTurn {
				workerState("idle")
				workerState("running")
			}
			if scenario.idleFirst {
				workerState("idle")
			}
			result := internalPayloadRequest(epoch, fmt.Sprintf(`{"type":"result","uuid":"interrupted-result","is_error":true,"subtype":%q}`, scenario.subtype))
			postCodeSessionWorkerEvents(t, app, record.ExternalID, result)
			workerState("idle")
			if status := retrieveSession(t, app, record.SessionExternalID, defaultTestKey).Status; status != "idle" {
				t.Fatalf("session status = %s, want idle", status)
			}
			events := listSessionEvents(t, app, record.SessionExternalID, "types[]=session.status_idle&order=desc", defaultTestKey)
			idle := sessionEventObjectByType(t, events, "session.status_idle")
			if reason := idle["stop_reason"].(map[string]any)["type"]; reason != scenario.want {
				t.Fatalf("stop reason = %v, want %s", reason, scenario.want)
			}
			errors := listSessionEvents(t, app, record.SessionExternalID, "types[]=session.error", defaultTestKey)
			wantErrors := 1
			if scenario.want == "end_turn" {
				wantErrors = 0
			}
			if len(errors.Data) != wantErrors {
				t.Fatalf("session errors = %d, want %d", len(errors.Data), wantErrors)
			}
			postCodeSessionWorkerEvents(t, app, record.ExternalID, result)
			after := listSessionEvents(t, app, record.SessionExternalID, "types[]=session.error", defaultTestKey)
			if len(after.Data) != wantErrors {
				t.Fatalf("redelivery changed error count: %d", len(after.Data))
			}
		})
	}
}
