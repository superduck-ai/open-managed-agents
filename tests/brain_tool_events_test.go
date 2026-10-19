package tests

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestBrainModelToolEventPrecedesEndAndDeduplicatesApproval(t *testing.T) {
	for _, test := range []struct {
		name, tool, errorType string
		publishTool           bool
	}{
		{name: "failed response", tool: "Bash", errorType: "incomplete_response"},
		{name: "permission required", tool: "MysteryTool"},
		{name: "denied tool", tool: "AskUserQuestion"},
		{name: "allowed tool", tool: "Bash", publishTool: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("brain-tool-events"))
			worker, _ := newPayloadIntegrationSession(t, app)
			claimed, err := registerTaskTestWorker(t, app, worker)
			if err != nil {
				t.Fatal(err)
			}
			service := newCodeSessionService(app, nil, nil)
			service.SetPublicEventSink(sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil))
			request, err := service.BeginModelRequest(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, worker.ExternalID, "", worker.Model)
			if err != nil {
				t.Fatal(err)
			}
			result := codesessions.ModelRequestResult{EndedAt: time.Now(), ErrorType: test.errorType,
				Messages: []codesessions.ModelRequestMessage{{ID: "sevt_thinking", Type: "agent.thinking",
					Content: []codesessions.ModelRequestContent{{Type: "thinking", Thinking: new("run the command")}}},
					{ID: "sevt_message", Type: "agent.message", Content: []codesessions.ModelRequestContent{{Type: "text", Text: new("Executing command")}}}},
				ToolUses: []codesessions.ModelRequestToolUse{{ID: "call_bash", Name: test.tool, Input: json.RawMessage(`{"command":"printf hello"}`)}},
			}
			if err := service.EndModelRequest(t.Context(), request, result); err != nil {
				t.Fatal(err)
			}
			events := listSessionEvents(t, app, worker.SessionExternalID, "order=asc&limit=100", defaultTestKey)
			var types []string
			for _, event := range events.Data {
				types = append(types, sessionEventStringField(t, event, "type"))
			}
			want := []string{"span.model_request_start", "span.model_request_end"}
			if test.errorType == "" {
				want = slices.Insert(want, 1, "agent.thinking", "agent.message")
			}
			if test.publishTool {
				want = slices.Insert(want, 3, "agent.tool_use")
			}
			if !slices.Equal(types, want) {
				t.Fatalf("model events = %v, want %v", types, want)
			}
			if !test.publishTool {
				return
			}
			postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(fmt.Sprint(claimed.CurrentWorkerEpoch),
				`{"type":"control_request","uuid":"approve-bash","request_id":"approve-bash","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"call_bash","input":{"command":"printf hello"}}}`))
			tools := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.tool_use", defaultTestKey)
			if len(tools.Data) != 1 || sessionEventStringField(t, tools.Data[0], "id") != sessionEventStringField(t, events.Data[3], "id") {
				t.Fatalf("approval duplicated the model tool event: %s", tools.Data)
			}
		})
	}
}
