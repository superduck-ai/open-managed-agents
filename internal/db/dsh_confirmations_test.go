package db

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestResolveDSHConfirmationRejectsInvalidReferences(t *testing.T) {
	publicID, wrongID, emptyID, threadID, otherThread := "public-call", "other-call", "", "thread", "other-thread"
	for _, tc := range []struct {
		name      string
		id        *string
		thread    *string
		state     string
		payload   string
		eventType string
		want      error
	}{
		{name: "missing normalized reference", state: "pending", payload: `{"custom_tool_use_id":"public-call","content":[]}`, want: ErrSessionToolUseNotPending},
		{name: "empty normalized reference", id: &emptyID, state: "pending", payload: `{"tool_use_id":"public-call","content":[]}`, want: ErrSessionToolUseNotPending},
		{name: "wrong normalized reference", id: &wrongID, state: "pending", payload: `{"tool_use_id":"public-call","content":[]}`, want: ErrSessionToolUseNotPending},
		{name: "wrong thread", id: &publicID, thread: &otherThread, state: "pending", payload: `{"content":[]}`, want: ErrSessionToolUseNotPending},
		{name: "resolved permission", id: &publicID, state: "resolved", payload: `{"content":[]}`, want: ErrSessionToolUseNotPending},
		{name: "invalid confirmation", id: &publicID, state: "pending", eventType: "user.tool_confirmation", payload: `{"result":"unknown"}`, want: ErrSessionToolConfirmationInvalid},
		{name: "malformed decision", id: &publicID, state: "pending", payload: `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
			kind := tc.eventType
			if kind == "" {
				kind = "user.custom_tool_result"
			}
			event := SessionEvent{EventType: kind, ToolUseID: tc.id, ThreadExternalID: tc.thread, Payload: json.RawMessage(tc.payload)}
			permissions := []dshPermissionRow{{RequestID: "request", PublicEventID: publicID, ThreadID: threadID, State: tc.state}}
			err := resolveDSHConfirmation(t.Context(), executor, DSHScope{}, permissions, event)
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if executor.execCallCount != 0 || executor.queryCallCount != 0 {
				t.Fatal("invalid confirmation accessed database")
			}
		})
	}
}

func TestResolvedDSHPermissionIgnoresMissingReferences(t *testing.T) {
	empty := ""
	for _, id := range []*string{nil, &empty} {
		_, found, err := (&DB{}).ResolvedDSHPermissionForInput(t.Context(), DSHScope{}, SessionEvent{ToolUseID: id, Payload: json.RawMessage(`{"tool_use_id":"payload-only"}`)})
		if err != nil || found {
			t.Fatalf("found=%v error=%v", found, err)
		}
	}
}

func TestResolveDSHConfirmationUsesNormalizedReference(t *testing.T) {
	id := "public-call"
	executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
	event := SessionEvent{EventType: "user.custom_tool_result", ToolUseID: &id, Payload: json.RawMessage(`{"custom_tool_use_id":"payload-only","content":[{"type":"text","text":"failed"}],"is_error":true}`)}
	permissions := []dshPermissionRow{{RequestID: "request", PublicEventID: id, State: "pending"}}
	if err := resolveDSHConfirmation(t.Context(), executor, DSHScope{}, permissions, event); err != nil {
		t.Fatal(err)
	}
	if executor.execCallCount != 1 || executor.statement.ID != "DSHExecutionMapper.SetPermission" {
		t.Fatalf("writes=%d statement=%s", executor.execCallCount, executor.statement.ID)
	}
	raw, ok := executor.bound.Values()[1].([]byte)
	if !ok {
		t.Fatal("decision is not JSON")
	}
	var decision dshResolvedDecision
	if err := json.Unmarshal(raw, &decision); err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != "external_result" || decision.ExternalResult == nil || !decision.ExternalResult.IsError {
		t.Fatalf("decision=%s", raw)
	}
}
