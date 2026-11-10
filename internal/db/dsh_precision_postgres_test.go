//go:build integration

package db

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
)

func newDSHPrecisionFixture(t *testing.T) dshLockFixture {
	t.Helper()
	f := newDSHLockFixture(t)
	execClaimTest(t, f.ctx, f.database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"') WHERE uuid=$1`, f.code.UUID)
	_, err := f.store.CreateSessionThreadIfAbsent(f.ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_precision", OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, SessionUUID: f.code.SessionUUID, SessionExternalID: f.code.SessionExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDSHPublicPrecisionPostgres(t *testing.T) {
	f := newDSHPrecisionFixture(t)
	now := time.Now().UTC()
	raw := []byte(`{"id":"result","type":"agent.tool_result","content":[{"number":9007199254740993,"decimal":0.123456789012345678901}]}`)
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "result", EventType: "agent.tool_result", Payload: raw, CreatedAt: now, ProcessedAt: now}
	item := DSHPublicEvent{ID: event.ExternalID, Payload: raw, Events: []SessionEvent{event}}
	if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); err != nil {
		t.Fatal(err)
	}
	changed := item
	changed.Payload = []byte(`{"id":"result","type":"agent.tool_result","content":[{"number":9007199254740992,"decimal":0.123456789012345678901}]}`)
	if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{changed}); !errors.Is(err, ErrDSHPublicConflict) {
		t.Fatalf("changed integer=%v", err)
	}
	stored, err := f.store.GetSessionEvent(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, event.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if equal, err := jsoncanonical.Equal(stored.Payload, raw); err != nil || !equal {
		t.Fatalf("stored=%s err=%v", stored.Payload, err)
	}
	item.Payload = []byte(`{ "content":[{"decimal":0.1234567890123456789010,"number":9007199254740993.0}], "type":"agent.tool_result","id":"result" }`)
	result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
	if err != nil || result.Duplicates != 1 {
		t.Fatalf("replay=%+v err=%v", result, err)
	}
}

func TestDSHLegacyReceiptPrecisionPostgres(t *testing.T) {
	f := newDSHPrecisionFixture(t)
	now := time.Now().UTC()
	raw := []byte(`{"id":"legacy","type":"agent.message","content":[{"number":9007199254740992}]}`)
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "legacy", EventType: "agent.message", Payload: raw, CreatedAt: now, ProcessedAt: now}
	item := DSHPublicEvent{ID: event.ExternalID, Payload: raw, Events: []SessionEvent{event}}
	if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); err != nil {
		t.Fatal(err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(canonical)
	execClaimTest(t, f.ctx, f.database, `UPDATE dsh_public_receipts SET payload_digest=$1 WHERE code_session_uuid=$2 AND event_id=$3`, "jcs-sha256-v1:"+hex.EncodeToString(hash[:]), f.scope.CodeSessionUUID, item.ID)
	changed := item
	changed.Payload = []byte(`{"id":"legacy","type":"agent.message","content":[{"number":9007199254740993}]}`)
	if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{changed}); !errors.Is(err, ErrDSHPublicConflict) {
		t.Fatalf("legacy collision accepted: %v", err)
	}
	result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
	if err != nil || result.Duplicates != 1 {
		t.Fatalf("legacy replay=%+v err=%v", result, err)
	}
	execClaimTest(t, f.ctx, f.database, `UPDATE session_events SET payload='{"content":[{"number":9007199254740993}]}' WHERE uuid=$1`, event.UUID)
	if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); !errors.Is(err, ErrDSHPublicHistoryInconsistent) {
		t.Fatalf("legacy ignored changed history: %v", err)
	}
}

func TestDSHPermissionPrecisionPostgres(t *testing.T) {
	f := newDSHPrecisionFixture(t)
	now := time.Now().UTC()
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "input", EventType: "user.message", Payload: []byte(`{"id":"input","type":"user.message","content":[{"type":"text","text":"hello"}]}`), CreatedAt: now}
	if _, err := f.store.AppendSessionEvents(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, []SessionEvent{event}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.ClaimDSHInput(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AppendDSHTranscript(f.ctx, f.scope, f.scope.CodeSessionID, nil, []DSHTranscriptRecord{{Offset: 0, Payload: []byte(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	permission := DSHPermission{RequestID: "permission", InputID: "input", DSHSessionID: f.scope.CodeSessionID, ThreadID: "sthr_precision", ToolUseID: "call", Turn: "1", Tool: []byte(`{"kind":"builtin","name":"bash"}`), Input: []byte(`{"id":9007199254740993,"decimal":0.123456789012345678901}`), PublicEventID: "tool", State: "pending"}
	if _, _, err := f.store.RequestDSHPermission(f.ctx, f.scope, permission, nil); err != nil {
		t.Fatal(err)
	}
	changed := permission
	changed.Input = []byte(`{"id":9007199254740992,"decimal":0.123456789012345678901}`)
	if _, _, err := f.store.RequestDSHPermission(f.ctx, f.scope, changed, nil); !errors.Is(err, ErrDSHPermissionConflict) {
		t.Fatalf("permission collision=%v", err)
	}
	stored, found, err := f.store.GetDSHPermission(f.ctx, f.scope, permission.RequestID)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if equal, err := jsoncanonical.Equal(stored.Input, permission.Input); err != nil || !equal {
		t.Fatalf("stored=%s err=%v", stored.Input, err)
	}
	permission.Input = []byte(`{"decimal":0.1234567890123456789010,"id":9007199254740993.0}`)
	if _, _, err := f.store.RequestDSHPermission(f.ctx, f.scope, permission, nil); err != nil {
		t.Fatalf("equivalent retry=%v", err)
	}
}

func TestDSHLegacyStatusReceiptPostgres(t *testing.T) {
	f := newDSHPrecisionFixture(t)
	now := time.Now().UTC()
	raw := []byte(`{"id":"status","type":"session.thread_status_running","session_thread_id":"sthr_precision"}`)
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "status", EventType: "session.thread_status_running", StatusThreadID: "sthr_precision", Payload: raw, CreatedAt: now, ProcessedAt: now}
	item := DSHPublicEvent{ID: event.ExternalID, Payload: raw, Events: []SessionEvent{event}}
	if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetSessionEvent(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, event.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no-op status history=%v", err)
	}
	result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
	if err != nil || result.Duplicates != 1 {
		t.Fatalf("new receipt replay=%+v err=%v", result, err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(canonical)
	execClaimTest(t, f.ctx, f.database, `UPDATE dsh_public_receipts SET payload_digest=$1 WHERE code_session_uuid=$2 AND event_id=$3`, "jcs-sha256-v1:"+hex.EncodeToString(hash[:]), f.scope.CodeSessionUUID, item.ID)
	if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); !errors.Is(err, ErrDSHPublicHistoryInconsistent) {
		t.Fatalf("unverifiable old receipt=%v", err)
	}
}
