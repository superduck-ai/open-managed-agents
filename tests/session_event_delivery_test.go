package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestWorkerReplyPublicationFailureKeepsCommittedHistory(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			app, agent, env := newSessionEventTestApp(t, "reply-publication", fmt.Sprintf(`{"model":"claude-opus-4-6","name":"reply-publication","tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":%t,"permission_policy":{"type":"always_allow"}}}]}`, enabled), `{"name":"reply-publication"}`)
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			codeSessionID := launchLocalCodeSession(t, app, response.ID)
			worker, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			broker := &failingPubAckBroker{MemoryBroker: workerevents.NewMemory(), failNext: true}
			service := newCodeSessionService(app, broker, nil)
			sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil)
			route := codesessions.CodeSessionStreamRoute{CodeSessionID: codeSessionID, WorkspaceUUID: worker.WorkspaceUUID, SessionExternalID: response.ID}
			payloads := []json.RawMessage{json.RawMessage(`{"type":"control_request","uuid":"reply-retry","request_id":"reply-retry","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"reply-tool","input":{"command":"pwd","number":9007199254740993}}}`)}
			err = service.AppendWorkerEvents(t.Context(), route, worker.CurrentWorkerEpoch, payloads)
			if !errors.Is(err, codesessions.ErrWorkerEventUnavailable) {
				t.Fatalf("publication error = %v", err)
			}
			before := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			if !eventPageContains(before, "9007199254740993") {
				t.Fatal("publication failure rolled back committed history")
			}
			session := mustSessionRecord(t, app, response.ID)
			watermark, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, response.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.AppendWorkerEvents(t.Context(), route, worker.CurrentWorkerEpoch, payloads); err != nil {
				t.Fatal(err)
			}
			after := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			if !reflect.DeepEqual(before.Data, after.Data) {
				t.Fatal("retry changed history")
			}
			progress, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, response.ID)
			if err != nil || !progress.Equal(watermark) {
				t.Fatalf("retry advanced clock: %v", err)
			}
			pending := broker.Pending(codeSessionID)
			if len(pending) != 1 {
				t.Fatalf("reply retry created %d messages", len(pending))
			}
			var reply struct {
				Response struct {
					Response struct {
						Behavior     string `json:"behavior"`
						UpdatedInput struct {
							Number json.Number `json:"number"`
						} `json:"updatedInput"`
					} `json:"response"`
				} `json:"response"`
			}
			if err := json.Unmarshal(pending[0].Payload, &reply); err != nil {
				t.Fatal(err)
			}
			want := "deny"
			if enabled {
				want = "allow"
			}
			if reply.Response.Response.Behavior != want {
				t.Fatalf("reply = %s", pending[0].Payload)
			}
			if enabled && reply.Response.Response.UpdatedInput.Number.String() != "9007199254740993" {
				t.Fatalf("lost input precision: %s", pending[0].Payload)
			}
		})
	}
}
