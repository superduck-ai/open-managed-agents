//go:build integration

package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"uuid"
)

func newDSHConfirmationTestStore(t *testing.T, state string) (context.Context, *DB, DSHScope, DSHPermission) {
	t.Helper()
	ctx, database, store := newCodeSessionClaimTestStore(t)
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"'),worker_lease_expires_at=now()+interval '1 hour' WHERE external_id=$1`, id)
	code, found, err := store.GetCodeSession(ctx, id)
	if err != nil || !found {
		t.Fatalf("code=%v %v", found, err)
	}
	scope := DSHScope{CodeSessionID: id, CodeSessionUUID: code.UUID, SessionID: code.SessionExternalID, OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, Epoch: 1}
	primary, err := store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_confirmation", OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: code.SessionExternalID, AgentSnapshot: json.RawMessage(`{}`), Status: "running", Stats: json.RawMessage(`{}`), Usage: json.RawMessage(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	input := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "input", EventType: "user.message", Payload: json.RawMessage(`{"id":"input","type":"user.message","content":[{"type":"text","text":"hello"}]}`), CreatedAt: time.Now().UTC()}
	if _, err := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{input}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ClaimDSHInput(ctx, scope); err != nil {
		t.Fatal(err)
	}
	header, err := json.Marshal(map[string]any{"version": 4, "id": id, "createdAt": 0, "isSeeded": false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendDSHTranscript(ctx, scope, id, nil, []DSHTranscriptRecord{{Offset: 0, Payload: header}}); err != nil {
		t.Fatal(err)
	}
	permission := DSHPermission{RequestID: "dsrp_confirmation", InputID: "input", DSHSessionID: id, ThreadID: primary.ExternalID, ToolUseID: "native-call", Turn: "1", Tool: json.RawMessage(`{"kind":"custom","name":"lookup"}`), Input: json.RawMessage(`{}`), PublicEventID: "public-call", State: state}
	if state == "resolved" {
		permission.Decision = json.RawMessage(`{"behavior":"external_result","external_result":{"content":[{"type":"text","text":"done"}],"isError":false}}`)
	}
	if _, _, err := store.RequestDSHPermission(ctx, scope, permission, nil); err != nil {
		t.Fatal(err)
	}
	return ctx, store, scope, permission
}

func dshConfirmationEvent(eventType, field string) SessionEvent {
	id := "public-call"
	body := map[string]any{"type": eventType, field: id, "content": []map[string]string{{"type": "text", "text": "done"}}, "is_error": false}
	if eventType == "user.tool_confirmation" {
		body["result"] = "allow"
	}
	raw, _ := json.Marshal(body)
	return SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "result", EventType: eventType, ToolUseID: &id, Payload: raw, CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}
}

func TestDSHResolvedToolResultLookupRejectsForeignReferencesPostgres(t *testing.T) {
	ctx, store, scope, permission := newDSHConfirmationTestStore(t, "resolved")
	for _, tc := range []struct {
		name   string
		change func(*DSHScope, *SessionEvent)
	}{
		{name: "wrong organization", change: func(scope *DSHScope, _ *SessionEvent) { scope.OrganizationUUID = uuid.NewV4().String() }},
		{name: "wrong workspace", change: func(scope *DSHScope, _ *SessionEvent) { scope.WorkspaceUUID = uuid.NewV4().String() }},
		{name: "wrong code session", change: func(scope *DSHScope, _ *SessionEvent) { scope.CodeSessionUUID = uuid.NewV4().String() }},
		{name: "native call is not public reference", change: func(_ *DSHScope, event *SessionEvent) { event.ToolUseID = &permission.ToolUseID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := scope
			event := dshConfirmationEvent("user.custom_tool_result", "custom_tool_use_id")
			tc.change(&candidate, &event)
			_, found, err := store.ResolvedDSHPermissionForInput(ctx, candidate, event)
			if err != nil || found {
				t.Fatalf("found=%v error=%v", found, err)
			}
		})
	}
	permission.RequestID = "dsrp_pending"
	permission.PublicEventID = "pending-call"
	permission.ToolUseID = "pending-native"
	permission.State = "pending"
	permission.Decision = nil
	if _, _, err := store.RequestDSHPermission(ctx, scope, permission, nil); err != nil {
		t.Fatal(err)
	}
	event := dshConfirmationEvent("user.custom_tool_result", "custom_tool_use_id")
	event.ToolUseID = &permission.PublicEventID
	if _, found, err := store.ResolvedDSHPermissionForInput(ctx, scope, event); err != nil || found {
		t.Fatalf("pending found=%v error=%v", found, err)
	}
}

func TestDSHToolResultConfirmationPostgres(t *testing.T) {
	for _, tc := range []struct{ kind, field string }{
		{"user.custom_tool_result", "custom_tool_use_id"},
		{"user.tool_result", "tool_use_id"},
		{"user.tool_confirmation", "tool_use_id"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ctx, store, scope, permission := newDSHConfirmationTestStore(t, "pending")
			event := dshConfirmationEvent(tc.kind, tc.field)
			_, acceptErr := store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{event}, nil)
			actual, found, err := store.GetDSHPermission(ctx, scope, permission.RequestID)
			if err != nil || !found {
				t.Fatalf("permission=%v %v", found, err)
			}
			session, found, err := store.GetSession(ctx, scope.WorkspaceUUID, scope.SessionID)
			if err != nil || !found {
				t.Fatalf("session=%v %v", found, err)
			}
			mapper := NewDSHExecutionMapper(store.mapperDB)
			summary, err := mapper.Summary(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			permissions, err := mapper.Permissions(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			_, reason := dshPrimaryState(summary, permissions, false)
			reasonType := ""
			if reason != nil {
				reasonType = reason.Type
			}
			if acceptErr != nil {
				t.Fatalf("valid %s rejected: %v", tc.kind, acceptErr)
			}
			if actual.State != "resolved" || reasonType == "requires_action" {
				t.Fatalf("result did not unblock: permission=%s session=%s", actual.State, session.Status)
			}
			if tc.kind != "user.tool_confirmation" {
				var decision dshResolvedDecision
				if err := json.Unmarshal(actual.Decision, &decision); err != nil {
					t.Fatal(err)
				}
				if decision.Behavior != "external_result" || decision.ExternalResult == nil {
					t.Fatalf("decision=%s", actual.Decision)
				}
				var content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if err := json.Unmarshal(decision.ExternalResult.Content, &content); err != nil {
					t.Fatal(err)
				}
				if len(content) != 1 || content[0].Text != "done" || content[0].Type != "text" || decision.ExternalResult.IsError {
					t.Fatalf("content=%s", decision.ExternalResult.Content)
				}
			}
		})
	}
}

func TestDSHResolvedToolResultLookupPostgres(t *testing.T) {
	for _, tc := range []struct{ kind, field string }{
		{"user.custom_tool_result", "custom_tool_use_id"},
		{"user.tool_result", "tool_use_id"},
		{"user.tool_confirmation", "tool_use_id"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ctx, store, scope, permission := newDSHConfirmationTestStore(t, "resolved")
			event := dshConfirmationEvent(tc.kind, tc.field)
			resolved, found, err := store.ResolvedDSHPermissionForInput(ctx, scope, event)
			if err != nil || !found || resolved.RequestID != permission.RequestID {
				t.Fatalf("resolved permission not found for valid %s: found=%v error=%v", tc.kind, found, err)
			}
		})
	}
}
