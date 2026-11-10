package codesessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"testing"
	"time"
)

func TestModelToolCannotDispatchSelfHostedBeforeWorkerRequestIdentityExists(t *testing.T) {
	request := &ModelRequest{CodeSessionID: "cse_test"}
	tool := ModelRequestToolUse{ID: "provider-call", Name: "Write", Input: json.RawMessage(`{"file_path":"/workspace/test","content":"exact"}`)}
	identity := parseClaudeToolIdentity("Write")
	event, err := modelToolUsePayload(request, tool, identity, resolvedToolPermissionAllow, true, time.Now())
	if err != nil || len(event) != 0 {
		t.Fatalf("self-hosted model preview became executable before callback registration: %s %v", event, err)
	}
	for _, permission := range []resolvedToolPermission{resolvedToolPermissionAsk, resolvedToolPermissionDeny} {
		event, err := modelToolUsePayload(request, tool, identity, permission, false, time.Now())
		if err != nil || len(event) != 0 {
			t.Fatal("gated model tool leaked")
		}
	}
	event, err = modelToolUsePayload(request, tool, identity, resolvedToolPermissionAllow, false, time.Now())
	if err != nil || len(event) == 0 {
		t.Fatal("cloud model publication changed", err)
	}
	var got struct {
		Type       string          `json:"type"`
		Permission string          `json:"evaluated_permission"`
		Input      json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(event, &got); err != nil || got.Type != "agent.tool_use" || got.Permission != "allow" {
		t.Fatal("cloud event changed", err)
	}
	tool.InputRejected = true
	event, err = modelToolUsePayload(request, tool, identity, resolvedToolPermissionAllow, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(event, &got); err != nil || got.Permission != "deny" {
		t.Fatal("rejected input became executable", err)
	}
}

func TestDSRPublicToolIdentityScopesModelRequests(t *testing.T) {
	tool := ModelRequestToolUse{ID: "functions.Read:2", Name: "Read", Input: json.RawMessage(`{"file_path":"/workspace/data"}`)}
	var previous string
	for _, start := range []string{"request-a", "request-b"} {
		request := &ModelRequest{StartID: start, CodeSessionID: "code", codeSession: db.CodeSession{Metadata: json.RawMessage(`{"config":{"worker_protocol":"dsr1"}}`)}}
		raw, err := modelToolUsePayload(request, tool, parseClaudeToolIdentity("Read"), resolvedToolPermissionAllow, false, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		callID := maevents.ModelToolCallID(start, tool.ID)
		if event.ID != maevents.ToolUseEventID("code", callID, "") || event.ID == previous || modelToolCallIDs(request, []string{tool.ID})[0] != callID {
			t.Fatalf("wrong scoped tool: %s", raw)
		}
		previous = event.ID
	}
	if modelToolCallID(&ModelRequest{StartID: "legacy"}, tool.ID) != tool.ID {
		t.Fatal("legacy identity changed")
	}
}
