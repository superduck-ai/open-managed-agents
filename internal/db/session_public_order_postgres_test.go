//go:build integration

package db

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"uuid"
)

func TestSessionPublicOrderDoesNotBackdateConcurrentProducerPostgres(t *testing.T) {
	ctx, store, scope, _ := newDSHConfirmationTestStore(t, "resolved")
	code, found, err := store.GetCodeSession(ctx, scope.CodeSessionID)
	if err != nil || !found {
		t.Fatal(err)
	}
	primary, found, err := store.GetPrimarySessionThread(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil || !found {
		t.Fatal(err)
	}
	child, err := store.CreateSessionThreadIfAbsent(ctx, SessionThread{UUID: uuid.NewV4().String(), ExternalID: "order-child", OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: code.SessionUUID, SessionExternalID: scope.SessionID, ParentThreadUUID: &primary.UUID, ParentThreadExternalID: &primary.ExternalID, AgentSnapshot: []byte(`{"name":"Reader"}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	early := time.Now().UTC().Truncate(time.Microsecond)
	rawParent, _ := json.Marshal(map[string]any{"id": "parent-start", "type": "span.model_request_start", "request_id": "parent-start", "session_thread_id": primary.ExternalID, "model": "controlled-model", "processed_at": early.Format(time.RFC3339Nano)})
	later := early.Add(time.Millisecond)
	rawChild, _ := json.Marshal(map[string]any{"id": "child-idle", "type": "session.thread_status_idle", "session_thread_id": child.ExternalID, "stop_reason": map[string]string{"type": "end_turn"}, "processed_at": later.Format(time.RFC3339Nano)})
	parent := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "parent-start", EventType: "span.model_request_start", ThreadExternalID: &primary.ExternalID, Payload: rawParent, CreatedAt: early, ProcessedAt: early}
	idle := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "child-idle", EventType: "session.thread_status_idle", ThreadExternalID: &child.ExternalID, StatusThreadID: child.ExternalID, Payload: rawChild, CreatedAt: later, ProcessedAt: later}
	appendParent := make(chan struct{})
	returned := make(chan SessionEventChanges, 1)
	failed := make(chan error, 1)
	go func() {
		<-appendParent
		changes, err := store.AppendSessionEventsIfAbsent(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{parent})
		if err != nil {
			failed <- err
			return
		}
		returned <- changes
	}()
	first, err := store.AppendSessionEventsIfAbsent(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{idle})
	close(appendParent)
	if err != nil {
		t.Fatal(err)
	}
	var second SessionEventChanges
	select {
	case err := <-failed:
		t.Fatal(err)
	case second = <-returned:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	emitted := []string{}
	byID := map[string]SessionEvent{}
	for _, batch := range []SessionEventChanges{first, second} {
		for _, event := range batch.Events {
			if event.ExternalID == parent.ExternalID || event.ExternalID == idle.ExternalID {
				emitted = append(emitted, event.ExternalID)
				byID[event.ExternalID] = event
			}
		}
	}
	if !reflect.DeepEqual(emitted, []string{"child-idle", "parent-start"}) {
		t.Fatal("controlled commit order was not established", emitted)
	}
	history, _, err := store.ListSessionEventsPage(ctx, ListSessionEventsPageParams{WorkspaceUUID: scope.WorkspaceUUID, SessionExternalID: scope.SessionID, Limit: 100, Order: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	persisted := []string{}
	for _, event := range history {
		source, exists := byID[event.ExternalID]
		if !exists {
			continue
		}
		if !event.ProcessedAt.Equal(source.ProcessedAt) || string(event.Payload) != string(source.Payload) {
			t.Fatal("publication/history payload or timestamp changed", event.ExternalID)
		}
		persisted = append(persisted, event.ExternalID)
	}
	if !reflect.DeepEqual(emitted, persisted) {
		t.Fatalf("public emission order %v differs from repeatable persisted order %v after delayed earlier-stamped parent insert", emitted, persisted)
	}
}

func TestDSRQueuedCursorPreservesProcessedACKPostgres(t *testing.T) {
	ctx, store, scope, _ := newDSHConfirmationTestStore(t, "resolved")
	code, found, err := store.GetCodeSession(ctx, scope.CodeSessionID)
	if err != nil || !found {
		t.Fatal(err)
	}
	primary, found, err := store.GetPrimarySessionThread(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil || !found {
		t.Fatal(err)
	}
	queued := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "queued-cursor", EventType: "user.message", Payload: []byte(`{"id":"queued-cursor","type":"user.message","content":[{"type":"text","text":"next"}],"processed_at":null}`), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if _, err = store.AppendSessionEvents(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{queued}, nil); err != nil {
		t.Fatal(err)
	}
	cursor, err := store.GetSessionEvent(ctx, scope.WorkspaceUUID, scope.SessionID, queued.ExternalID)
	if err != nil || !cursor.ProcessedAt.IsZero() {
		t.Fatal("queued cursor was not established", err)
	}
	middle := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "before-ack", EventType: "agent.thinking", ThreadExternalID: &primary.ExternalID, Payload: []byte(`{"id":"before-ack","type":"agent.thinking"}`), CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}
	if _, err = store.AppendSessionEventsIfAbsent(ctx, scope.WorkspaceUUID, scope.SessionID, []SessionEvent{middle}); err != nil {
		t.Fatal(err)
	}
	processed, err := store.ProcessSessionInputsForWorker(ctx, code, scope.Epoch, []string{queued.ExternalID})
	if err != nil || len(processed) != 1 || processed[0].ProcessedAt.IsZero() {
		t.Fatal("processing ACK not established", err)
	}
	rows, _, err := store.ListSessionEventsPage(ctx, ListSessionEventsPageParams{WorkspaceUUID: scope.WorkspaceUUID, SessionExternalID: scope.SessionID, ProcessedOnly: true, Limit: 100, Order: "asc", Cursor: &SessionEventPageCursor{ExternalID: cursor.ExternalID, AtQueuedPosition: true}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, event := range rows {
		if event.ExternalID == middle.ExternalID || event.ExternalID == queued.ExternalID {
			ids = append(ids, event.ExternalID)
		}
	}
	if !reflect.DeepEqual(ids, []string{middle.ExternalID, queued.ExternalID}) {
		t.Fatalf("mutable queued cursor skipped committed history or processing ACK: %v", ids)
	}
	replay, err := store.ProcessSessionInputsForWorker(ctx, code, scope.Epoch, []string{queued.ExternalID})
	if err != nil || len(replay) != 0 {
		t.Fatal("ACK replay republished", err)
	}
	after, err := store.GetSessionEvent(ctx, scope.WorkspaceUUID, scope.SessionID, queued.ExternalID)
	if err != nil || !after.ProcessedAt.Equal(processed[0].ProcessedAt) {
		t.Fatal("ACK replay changed stable processing time", err)
	}
}
