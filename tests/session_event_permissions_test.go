package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerPermissionCommitsEventsAndDeliveryTogether(t *testing.T) {
	for _, tc := range []struct {
		name, policy, stage, source string
		enabled                     bool
	}{
		{"ask metadata failure", "always_ask", "metadata", "tool-confirmation", true},
		{"ask public failure", "always_ask", "public", "tool-confirmation", true},
		{"allow queue failure", "always_allow", "inbound", "auto-approve", true},
		{"deny queue failure", "always_allow", "inbound", "auto-deny", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, agent, env := newSessionEventTestApp(t, "permission-transaction", fmt.Sprintf(`{"model":"claude-opus-4-6","name":"permission-transaction","tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":%t,"permission_policy":{"type":%q}}}]}`, tc.enabled, tc.policy), `{"name":"permission-transaction"}`)
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			session := mustSessionRecord(t, app, response.ID)
			codeSessionID := launchLocalCodeSession(t, app, session.ExternalID)
			epoch := registerCodeSessionWorker(t, app, codeSessionID)
			putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running","external_metadata":{"keep":"unrelated"}}`)
			worker, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			historyBefore := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey)
			body := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"payload":{"type":"control_request","uuid":"control-permission","request_id":"request-permission","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"tool-permission","input":{"command":"pwd","number":9007199254740993}}}}]}`
			var removeFailure func()
			if tc.stage == "public" {
				removeFailure = rejectPublicSessionEventWrites(t, app, session.UUID, "")
			} else {
				removeFailure = rejectSessionInputCommit(t, app, worker.UUID, tc.stage)
			}
			defer removeFailure()
			assertError(t, doCodeSessionWorkerRequest(t, app, codeSessionID, "events", body), http.StatusInternalServerError, "api_error")
			afterFailure, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !afterFailure.Equal(before) {
				t.Fatalf("failed permission advanced public clock: %v, %v", afterFailure, err)
			}
			if got := mustSessionRecord(t, app, session.ExternalID).Status; got != "running" {
				t.Fatalf("failed permission changed public state: %s", got)
			}
			historyAfter := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey)
			if !reflect.DeepEqual(historyBefore.Data, historyAfter.Data) {
				t.Fatal("failed permission changed public history")
			}
			after, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || after.LastInboundSequenceNum != worker.LastInboundSequenceNum || string(after.WorkerExternalMetadata) != string(worker.WorkerExternalMetadata) {
				t.Fatalf("failed permission changed inbound sequence or metadata: %v", err)
			}
			removeFailure()
			postCodeSessionWorkerEvents(t, app, codeSessionID, body)
			public := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey)
			tool := sessionEventObjectByType(t, public, "agent.tool_use")
			toolID, ok := tool["id"].(string)
			if !ok || toolID == "" || !eventPageContains(public, "9007199254740993") {
				t.Fatal("permission public event lost identity or input precision")
			}
			// The neighboring integer rounds to the same float64. It must still
			// conflict, without replacing the saved request or queuing a response.
			changed := strings.ReplaceAll(body, "9007199254740993", "9007199254740992")
			assertError(t, doCodeSessionWorkerRequest(t, app, codeSessionID, "events", changed), http.StatusConflict, "conflict_error")
			if tc.policy == "always_ask" {
				after, err = getCodeSession(app, t.Context(), codeSessionID)
				if err != nil || !strings.Contains(string(after.WorkerExternalMetadata), toolID) || !strings.Contains(string(after.WorkerExternalMetadata), "9007199254740993") {
					t.Fatalf("pending request missing or changed: %v", err)
				}
				if after.LastInboundSequenceNum != worker.LastInboundSequenceNum {
					t.Fatal("ask request queued an automatic response")
				}
				sendSessionEvents(t, app, session.ExternalID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(toolID)+`,"result":"allow"}]}`, defaultTestKey)
				putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
			}
			committed, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || committed.LastInboundSequenceNum != worker.LastInboundSequenceNum+1 {
				t.Fatalf("permission response did not commit exactly once: %v", err)
			}
			watermark, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			postCodeSessionWorkerEvents(t, app, codeSessionID, body)
			afterRetry, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !afterRetry.Equal(watermark) {
				t.Fatalf("permission retry advanced public clock: %v, %v", afterRetry, err)
			}
			after, err = getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || after.LastInboundSequenceNum != committed.LastInboundSequenceNum || string(after.WorkerExternalMetadata) != string(committed.WorkerExternalMetadata) {
				t.Fatalf("permission retry repeated response or resurrected request: %v", err)
			}
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(after.WorkerExternalMetadata, &metadata); err != nil || string(metadata["keep"]) != `"unrelated"` || len(metadata) != 1 {
				t.Fatalf("permission metadata not cleared or unrelated value lost: %s, %v", after.WorkerExternalMetadata, err)
			}
			if got := mustSessionRecord(t, app, session.ExternalID).Status; got != "running" {
				t.Fatalf("permission retry moved session back to idle: %s", got)
			}
			_, _, raw := latestCodeSessionInboundEventForSource(t, app, codeSessionID, tc.source)
			if !strings.Contains(string(raw), "9007199254740993") {
				t.Fatalf("worker permission response lost input precision: %s", raw)
			}
			queued, err := app.db.ListQueuedCodeSessionInboundEvents(t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, input := range queued {
				if input.Source == tc.source {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("queued permission responses = %d", count)
			}
		})
	}
}
