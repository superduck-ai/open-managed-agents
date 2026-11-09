//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"uuid"
)

func TestDSHPublicationTwoWritersPostgres(t *testing.T) {
	for _, different := range []bool{true, false} {
		for _, proxyFirst := range []bool{true, false} {
			name := "same_content"
			if different {
				name = "conflicting_content"
			}
			if proxyFirst {
				name += "/proxy_first"
			} else {
				name += "/public_first"
			}
			t.Run(name, func(t *testing.T) {
				f := newDSHLockFixture(t)
				execClaimTest(t, f.ctx, f.database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"') WHERE uuid=$1`, f.code.UUID)
				now := time.Now().UTC()
				_, err := f.store.CreateSessionThreadIfAbsent(f.ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_authority", OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, SessionUUID: f.code.SessionUUID, SessionExternalID: f.code.SessionExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: now})
				if err != nil {
					t.Fatal(err)
				}
				id := maevents.StableAssistantEventID(f.code.ExternalID, "model-message", 0, "agent.message")
				proxy := authorityMessage(t, id, "proxy answer", now)
				text := "proxy answer"
				if different {
					text = "Brain answer"
				}
				worker := authorityMessage(t, id, text, now)
				item := DSHPublicEvent{ID: id, Payload: worker.Payload, Events: []SessionEvent{worker}}
				var proxyChanges SessionEventChanges
				var publication DSHPublishResult
				var publishErr error
				publishProxy := func() {
					proxyChanges, err = f.store.AppendSessionEventsIfAbsent(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, []SessionEvent{proxy})
					if err != nil {
						t.Fatal(err)
					}
				}
				publishWorker := func() {
					publication, publishErr = f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
					if publishErr != nil && !(proxyFirst && different && errors.Is(publishErr, ErrDSHPublicConflict)) {
						t.Fatal(publishErr)
					}
				}
				if proxyFirst {
					publishProxy()
					publishWorker()
				} else {
					publishWorker()
					publishProxy()
				}
				stored, err := f.store.GetSessionEvent(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, id)
				if err != nil {
					t.Fatal(err)
				}
				winner := worker
				if proxyFirst {
					winner = proxy
				}
				equal, err := jsoncanonical.Equal(stored.Payload, winner.Payload)
				if err != nil || !equal {
					t.Fatalf("history did not preserve first writer: equal=%v err=%v stored=%s", equal, err, stored.Payload)
				}
				if different && proxyFirst {
					if !errors.Is(publishErr, ErrDSHPublicConflict) || publication.Accepted != 0 {
						t.Fatalf("conflict returned success: %+v %v", publication, publishErr)
					}
					_, found, receiptErr := NewDSHExecutionMapper(f.store.mapperDB).Receipt(f.ctx, f.scope, id)
					if receiptErr != nil || found {
						t.Fatalf("conflict committed receipt: %v %v", found, receiptErr)
					}
					return
				}
				if publication.Accepted != 1 {
					t.Fatalf("publication=%+v", publication)
				}
				if proxyFirst && authorityMessageWrites(publication.Changes.Events, id) != 0 {
					t.Fatalf("second writer changed history: %+v", publication.Changes)
				}
				if !proxyFirst && authorityMessageWrites(proxyChanges.Events, id) != 0 {
					t.Fatalf("proxy changed history: %+v", proxyChanges)
				}
				receipt, found, err := NewDSHExecutionMapper(f.store.mapperDB).Receipt(f.ctx, f.scope, id)
				expected, digestErr := jsoncanonical.Digest(worker.Payload)
				if err != nil || digestErr != nil || !found || receipt != expected {
					t.Fatalf("receipt=%s found=%v err=%v digestErr=%v", receipt, found, err, digestErr)
				}
				storedDigest, err := jsoncanonical.Digest(stored.Payload)
				if err != nil {
					t.Fatal(err)
				}
				if (receipt != storedDigest) != (different && proxyFirst) {
					t.Fatalf("receipt/history mismatch=%v different=%v proxyFirst=%v", receipt != storedDigest, different, proxyFirst)
				}
				replay, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
				if err != nil || replay.Duplicates != 1 || replay.Accepted != 0 {
					t.Fatalf("replay=%+v err=%v", replay, err)
				}
				changed := item
				changed.Payload = authorityMessage(t, id, "changed replay", now).Payload
				if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{changed}); !errors.Is(err, ErrDSHPublicConflict) {
					t.Fatalf("changed replay error=%v", err)
				}
				t.Logf("proxy_first=%v differing_content=%v public_accepted=%d public_history_writes=%d receipt_matches_history=%v", proxyFirst, different, publication.Accepted, authorityMessageWrites(publication.Changes.Events, id), receipt == storedDigest)
			})
		}
	}
}

func authorityMessage(t *testing.T, id, text string, now time.Time) SessionEvent {
	t.Helper()
	payload, err := json.Marshal(struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}{ID: id, Type: "agent.message", Content: []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{Type: "text", Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
	return SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "agent.message", Payload: payload, CreatedAt: now, ProcessedAt: now}
}

func authorityMessageWrites(events []SessionEvent, id string) int {
	count := 0
	for _, event := range events {
		if event.ExternalID == id {
			count++
		}
	}
	return count
}

func TestDSHPublicationDetectsHistoryCorruptionPostgres(t *testing.T) {
	for _, change := range []string{"missing", "content", "type", "thread"} {
		t.Run(change, func(t *testing.T) {
			f := newDSHLockFixture(t)
			execClaimTest(t, f.ctx, f.database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"') WHERE uuid=$1`, f.code.UUID)
			now := time.Now().UTC()
			_, err := f.store.CreateSessionThreadIfAbsent(f.ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "sthr_authority", OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, SessionUUID: f.code.SessionUUID, SessionExternalID: f.code.SessionExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			event := authorityMessage(t, "answer", "original", now)
			item := DSHPublicEvent{ID: event.ExternalID, Payload: event.Payload, Events: []SessionEvent{event}}
			if _, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item}); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "missing":
				execClaimTest(t, f.ctx, f.database, `DELETE FROM session_events WHERE uuid=$1`, event.UUID)
			case "content":
				execClaimTest(t, f.ctx, f.database, `UPDATE session_events SET payload='{"content":[{"type":"text","text":"changed"}]}' WHERE uuid=$1`, event.UUID)
			case "type":
				execClaimTest(t, f.ctx, f.database, `UPDATE session_events SET event_type='system.message' WHERE uuid=$1`, event.UUID)
			case "thread":
				execClaimTest(t, f.ctx, f.database, `UPDATE session_events SET thread_external_id='other' WHERE uuid=$1`, event.UUID)
			}
			fresh := authorityMessage(t, "fresh", "new", now)
			result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{{ID: fresh.ExternalID, Payload: fresh.Payload, Events: []SessionEvent{fresh}}, item})
			if !errors.Is(err, ErrDSHPublicHistoryInconsistent) || result.Accepted != 0 || result.Duplicates != 0 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			_, found, err := NewDSHExecutionMapper(f.store.mapperDB).Receipt(f.ctx, f.scope, fresh.ExternalID)
			if err != nil || found {
				t.Fatalf("batch was not rolled back: found=%v err=%v", found, err)
			}
		})
	}
}
