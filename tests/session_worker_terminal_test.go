//go:build integration || e2e || e2b_integration

package tests

import (
	"fmt"
	"testing"
)

func TestSessionWorkerResultEndsTurn(t *testing.T) {
	for _, test := range []struct {
		name    string
		subtype string
		failed  bool
		reason  string
	}{
		{"failure", "error_during_execution", true, "retries_exhausted"},
		{"cancelled", "cancelled", false, "end_turn"},
		{"success", "success", false, "end_turn"},
		{"legacy success", "", false, "end_turn"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("result-terminal"))
			code, epoch := newPayloadIntegrationSession(t, app)
			state := func(status string) {
				putCodeSessionWorkerState(t, app, code.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":%q}`, epoch, status))
			}
			state("running")
			state("idle")
			assertWorkerIdleReasons(t, app, code.SessionExternalID)
			if status := retrieveSession(t, app, code.SessionExternalID, defaultTestKey).Status; status != "running" {
				t.Fatalf("idle ended unfinished turn: %s", status)
			}
			event := fmt.Sprintf(`{"type":"result","uuid":"terminal-result","subtype":%q,"is_error":%t,"result":"private result details"}`, test.subtype, test.failed)
			for range 2 {
				postCodeSessionWorkerEvents(t, app, code.ExternalID, internalPayloadRequest(epoch, event))
				state("idle")
			}
			assertWorkerIdleReasons(t, app, code.SessionExternalID, test.reason)
			failures := listSessionEvents(t, app, code.SessionExternalID, "types[]=session.error", defaultTestKey)
			expected := 0
			if test.failed {
				expected = 1
			}
			if len(failures.Data) != expected {
				t.Fatalf("errors: %s", failures.Data)
			}
			input := sendSessionEvents(t, app, code.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"Continue"}]}]}`, defaultTestKey)
			state("idle")
			postCodeSessionWorkerEvents(t, app, code.ExternalID, internalPayloadRequest(epoch, event))
			if status := retrieveSession(t, app, code.SessionExternalID, defaultTestKey).Status; status != "running" {
				t.Fatalf("late idle/result ended next turn: %s", status)
			}
			epoch = registerCodeSessionWorker(t, app, code.ExternalID)
			consumePublicInput(t, app, code, epoch, sessionEventStringField(t, input.Data[0], "id"))
			state("running")
			state("idle")
			assertWorkerIdleReasons(t, app, code.SessionExternalID, test.reason)
			postCodeSessionWorkerEvents(t, app, code.ExternalID, internalPayloadRequest(epoch, `{"type":"result","uuid":"next-result","subtype":"success","is_error":false}`))
			assertWorkerIdleReasons(t, app, code.SessionExternalID, test.reason, "end_turn")
		})
	}
}
