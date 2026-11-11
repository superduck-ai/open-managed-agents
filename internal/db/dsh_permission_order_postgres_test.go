//go:build integration

package db

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDSHPermissionCommitOrderPostgres(t *testing.T) {
	ctx, store, scope, base := newDSHConfirmationTestStore(t, "resolved")
	stale := time.Now().UTC().Add(-time.Hour)
	var expected []SessionEvent
	var first DSHPermission
	for _, id := range []string{"first", "second"} {
		request := base
		request.RequestID = "permission-" + id
		request.ToolUseID = "native-" + id
		request.PublicEventID = "public-" + id
		request.State = "pending"
		request.Decision = nil
		payload, _ := json.Marshal(map[string]any{"id": request.PublicEventID, "type": "agent.custom_tool_use", "name": "lookup", "input": map[string]any{}, "processed_at": stale.Format(time.RFC3339Nano)})
		input := SessionEvent{ExternalID: request.PublicEventID, EventType: "agent.custom_tool_use", Payload: payload, ProcessedAt: stale, CreatedAt: stale}
		_, changes, err := store.RequestDSHPermission(ctx, scope, request, []SessionEvent{input})
		if err != nil {
			t.Fatal(err)
		}
		if len(changes.Events) == 0 {
			t.Fatal("missing permission publication")
		}
		expected = append(expected, changes.Events...)
		for _, event := range changes.Events {
			if event.ExternalID != request.PublicEventID {
				continue
			}
			var body struct {
				ProcessedAt time.Time `json:"processed_at"`
			}
			if err := json.Unmarshal(event.Payload, &body); err != nil {
				t.Fatal(err)
			}
			if !event.ProcessedAt.After(stale) || !body.ProcessedAt.Equal(event.ProcessedAt) || !event.CreatedAt.Equal(event.ProcessedAt) {
				t.Fatalf("stale or inconsistent processing time: %+v", event)
			}
		}
		if id == "first" {
			first = request
		}
	}
	rows, _, err := store.ListSessionEventsPage(ctx, ListSessionEventsPageParams{WorkspaceUUID: scope.WorkspaceUUID, SessionExternalID: scope.SessionID, Limit: 100, Order: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]int{}
	for i, event := range rows {
		positions[event.ExternalID] = i
	}
	previous := -1
	for _, event := range expected {
		position, ok := positions[event.ExternalID]
		if !ok || position <= previous {
			t.Fatalf("history reordered committed permission event %s", event.ExternalID)
		}
		previous = position
	}
	_, duplicate, err := store.RequestDSHPermission(ctx, scope, first, nil)
	if err != nil || len(duplicate.Events) != 0 {
		t.Fatalf("permission replay republished history: %+v %v", duplicate, err)
	}
	replayed, _, err := store.ListSessionEventsPage(ctx, ListSessionEventsPageParams{WorkspaceUUID: scope.WorkspaceUUID, SessionExternalID: scope.SessionID, Limit: 100, Order: "asc"})
	if err != nil || len(replayed) != len(rows) {
		t.Fatalf("permission replay changed history length: %v", err)
	}
	for i, event := range rows {
		if replayed[i].ExternalID != event.ExternalID || !replayed[i].ProcessedAt.Equal(event.ProcessedAt) || string(replayed[i].Payload) != string(event.Payload) {
			t.Fatalf("permission replay changed persisted event %s", event.ExternalID)
		}
	}
}
