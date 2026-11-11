//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"github.com/superduck-ai/yourbatis"
)

func TestDSHCoordinationLegacyReplayPostgres(t *testing.T) {
	for _, kind := range []string{"session.thread_created", "agent.thread_message_sent", "agent.thread_message_received"} {
		t.Run(kind, func(t *testing.T) {
			f, primary := newDSRTurnBoundaryFixture(t)
			created := coordinationThreadFixture(t, f, "child", "root")
			item := created
			if kind != "session.thread_created" {
				item = coordinationMessageFixture(kind, "child")
			}
			digest, err := jsoncanonical.Digest(item.Payload)
			if err != nil {
				t.Fatal(err)
			}
			err = f.store.mapperDB.Transaction(f.ctx, func(executor yourbatis.Executor) error {
				session, primary, err := lockDSHSession(f.ctx, executor, f.scope)
				if err != nil {
					return err
				}
				if err = createDSHThread(f.ctx, executor, f.scope, session, primary, created); err != nil {
					return err
				}
				if _, err = insertDSHHistory(f.ctx, executor, &session, primary, item.Events); err != nil {
					return err
				}
				_, err = NewDSHExecutionMapper(executor).InsertReceipt(f.ctx, f.scope, item.ID, digest)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			before, err := NewSessionEventMapper(f.store.mapperDB).FindByExternalID(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, item.Events[0].ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			preceding := coordinationPublicFixture("preceding", "system.message", `{"id":"preceding","type":"system.message","content":[{"type":"text","text":"before replay"}]}`)
			execClaimTest(t, f.ctx, f.database, `UPDATE session_events SET payload = payload || '{"content":"changed"}'::jsonb WHERE uuid=$1`, before.UUID)
			result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{preceding, item})
			if !errors.Is(err, ErrDSHPublicHistoryInconsistent) {
				t.Fatalf("expected altered history conflict, got result=%+v err=%v", result, err)
			}
			if result.Accepted != 0 || result.Duplicates != 0 || len(result.Changes.Events) != 0 {
				t.Fatalf("failed batch returned committed changes: %+v", result)
			}
			if _, found, err := NewDSHExecutionMapper(f.store.mapperDB).Receipt(f.ctx, f.scope, preceding.ID); err != nil || found {
				t.Fatalf("preceding receipt escaped rollback: found=%v err=%v", found, err)
			}
			if _, err = NewSessionEventMapper(f.store.mapperDB).FindByExternalID(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, preceding.ID); !errors.Is(mapNoRows(err), ErrNotFound) {
				t.Fatalf("preceding history escaped rollback: %v", err)
			}
			execClaimTest(t, f.ctx, f.database, `UPDATE session_events SET payload=$2 WHERE uuid=$1`, before.UUID, before.Payload)
			result, err = f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
			if err != nil || result.Duplicates != 1 {
				t.Fatalf("legacy replay: %+v %v", result, err)
			}
			if kind != "session.thread_created" {
				assertCoordinationMirror(t, result.Changes.Events, item.Events[0], "child", primary.ExternalID)
			}
			repeat, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
			if err != nil || repeat.Duplicates != 1 || len(repeat.Changes.Events) != 0 {
				t.Fatalf("repeat replay: %+v %v", repeat, err)
			}
			after, err := NewSessionEventMapper(f.store.mapperDB).FindByExternalID(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, item.Events[0].ExternalID)
			if err != nil || string(after.Payload) != string(before.Payload) {
				t.Fatalf("legacy history changed: %v", err)
			}
			receipt, found, err := NewDSHExecutionMapper(f.store.mapperDB).Receipt(f.ctx, f.scope, item.ID)
			if err != nil || !found || receipt != digest {
				t.Fatalf("legacy receipt changed: found=%v err=%v", found, err)
			}
			if kind != "session.thread_created" {
				execClaimTest(t, f.ctx, f.database, `UPDATE session_events SET payload = payload || '{"content":"changed"}'::jsonb WHERE payload->>'original_event_id'=$1`, item.Events[0].ExternalID)
				_, err = f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{preceding, item})
				if !errors.Is(err, ErrDSHPublicHistoryInconsistent) {
					t.Fatalf("corrupt mirror accepted: %v", err)
				}
				if _, found, err := NewDSHExecutionMapper(f.store.mapperDB).Receipt(f.ctx, f.scope, preceding.ID); err != nil || found {
					t.Fatalf("preceding receipt escaped mirror conflict rollback: %v %v", found, err)
				}
			}
		})
	}
}

func TestDSHCoordinationNestedThreadPostgres(t *testing.T) {
	f, _ := newDSRTurnBoundaryFixture(t)
	for _, pair := range [][2]string{{"child", "root"}, {"sibling", "root"}, {"grandchild", "child"}} {
		item := coordinationThreadFixture(t, f, pair[0], pair[1])
		result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
		if err != nil || result.Accepted != 1 {
			t.Fatalf("nested thread creation failed: %+v %v", result, err)
		}
	}
	grandchild, err := NewSessionThreadMapper(f.store.mapperDB).FindByExternalID(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, "grandchild")
	if err != nil || grandchild.ParentThreadExternalID == nil || *grandchild.ParentThreadExternalID != "child" {
		t.Fatalf("nested parent not persisted: %+v %v", grandchild, err)
	}
	for _, kind := range []string{"agent.thread_message_sent", "agent.thread_message_received"} {
		t.Run(kind, func(t *testing.T) {
			item := coordinationMessageFixture(kind, "grandchild")
			if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("expected rejection of root-to-grandchild message, got %v", err)
			}
			parentID := "child"
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(item.Payload, &fields); err != nil {
				t.Fatal(err)
			}
			fields["session_thread_id"] = json.RawMessage(`"child"`)
			item.Payload, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			item.Events[0].Payload = item.Payload
			item.Events[0].ThreadExternalID = &parentID
			result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
			if err != nil || result.Accepted != 1 {
				t.Fatalf("nested publication: %+v %v", result, err)
			}
			assertCoordinationMirror(t, result.Changes.Events, item.Events[0], "grandchild", "child")
			repeat, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
			if err != nil || repeat.Duplicates != 1 || len(repeat.Changes.Events) != 0 {
				t.Fatalf("nested replay: %+v %v", repeat, err)
			}
		})
	}
	t.Run("unrelated siblings", func(t *testing.T) {
		owner := "child"
		item := coordinationPublicFixture("sideways", "agent.thread_message_sent", `{"id":"sideways","type":"agent.thread_message_sent","session_thread_id":"child","to_session_thread_id":"sibling","content":[]}`)
		item.Events[0].ThreadExternalID = &owner
		if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("expected unrelated threads to be rejected, got %v", err)
		}
	})
	t.Run("child sends to parent", func(t *testing.T) {
		owner := "grandchild"
		item := coordinationPublicFixture("reply", "agent.thread_message_sent", `{"id":"reply","type":"agent.thread_message_sent","session_thread_id":"grandchild","to_session_thread_id":"child","content":[]}`)
		item.Events[0].ThreadExternalID = &owner
		result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
		if err != nil || result.Accepted != 1 {
			t.Fatalf("child reply: %+v %v", result, err)
		}
		assertCoordinationMirror(t, result.Changes.Events, item.Events[0], "child", owner)
	})
	for _, kind := range []string{"agent.thread_message_sent", "agent.thread_message_received"} {
		item := coordinationMessageFixture(kind, "child")
		result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
		if err != nil || result.Accepted != 1 {
			t.Fatalf("direct-child control failed: %+v %v", result, err)
		}
		replay, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
		if err != nil || replay.Duplicates != 1 || len(replay.Changes.Events) != 0 {
			t.Fatalf("direct-child replay failed: %+v %v", replay, err)
		}
	}
}

func coordinationThreadFixture(t *testing.T, f dshLockFixture, id, parent string) DSHPublicEvent {
	t.Helper()
	if _, err := f.store.AppendDSHTranscript(f.ctx, f.scope, id, &parent, []DSHTranscriptRecord{{Offset: 0, Payload: []byte(`{"id":"` + id + `"}`)}}); err != nil {
		t.Fatal(err)
	}
	item := coordinationPublicFixture("created-"+id, "session.thread_created", `{"id":"created-`+id+`","type":"session.thread_created","session_thread_id":"`+id+`","parent_thread_id":"`+f.scope.CodeSessionID+`","agent_snapshot":{"name":"Reader"}}`)
	item.DSHSessionID = id
	item.Thread = &SessionThread{ExternalID: id, AgentSnapshot: []byte(`{"name":"Reader"}`), Status: "idle", Usage: []byte(`{}`), Stats: []byte(`{}`), CreatedAt: time.Now().UTC()}
	return item
}

func coordinationMessageFixture(kind, child string) DSHPublicEvent {
	field := "to_session_thread_id"
	if kind == "agent.thread_message_received" {
		field = "from_session_thread_id"
	}
	id := kind + "-" + child
	return coordinationPublicFixture(id, kind, `{"id":"`+id+`","type":"`+kind+`","`+field+`":"`+child+`","content":[{"type":"text","text":"task"}]}`)
}

func coordinationPublicFixture(id, kind, raw string) DSHPublicEvent {
	at := time.Now().UTC()
	return DSHPublicEvent{ID: id, Payload: []byte(raw), Events: []SessionEvent{{UUID: uuid.NewV4().String(), ExternalID: id, EventType: kind, Payload: []byte(raw), CreatedAt: at, ProcessedAt: at}}}
}

func assertCoordinationMirror(t *testing.T, events []SessionEvent, original SessionEvent, peer, owner string) {
	t.Helper()
	matches := 0
	for _, event := range events {
		var payload struct {
			OriginalID string `json:"original_event_id"`
			From       string `json:"from_session_thread_id"`
			To         string `json:"to_session_thread_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.OriginalID != original.ExternalID {
			continue
		}
		matches++
		if event.ThreadExternalID == nil || *event.ThreadExternalID != peer {
			t.Fatal("wrong mirror owner")
		}
		if original.EventType == "agent.thread_message_sent" {
			if event.EventType != "agent.thread_message_received" || payload.From != owner {
				t.Fatal("wrong sender")
			}
		} else if event.EventType != "agent.thread_message_sent" || payload.To != owner {
			t.Fatal("wrong recipient")
		}
	}
	if matches != 1 {
		t.Fatalf("expected one mirror, got %d", matches)
	}
}
