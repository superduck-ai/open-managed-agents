package codesessions

import (
	"encoding/json"
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
