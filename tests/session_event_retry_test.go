package tests

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionEventRetryRejectsConflictingContent(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "session-event-retry", `{"model":"claude-opus-4-6","name":"event-retry"}`, `{"name":"event-retry"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	first := consistencyTestEvent("accepted", time.Now().UTC())
	first.Payload = json.RawMessage(`{"id":` + quoteJSON(first.ExternalID) + `,"type":"agent.message","content":{"count":9007199254740993,"fraction":1e3,"created_at":"nested-original"},"created_at":"old","processed_at":"old"}`)
	rows, err := app.db.AppendSessionEvents(t.Context(), session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{first}, nil)
	if err != nil {
		t.Fatal(err)
	}
	watermark := rows[0].ProcessedAt
	ignored := []string{"created_at", "processed_at"}
	for _, test := range []struct{ name, payload, eventType string }{
		{"different number above float precision", strings.Replace(string(first.Payload), "9007199254740993", "9007199254740992", 1), first.EventType},
		{"changed nested timestamp", strings.Replace(string(first.Payload), "nested-original", "nested-changed", 1), first.EventType},
		{"missing content", `{"id":` + quoteJSON(first.ExternalID) + `,"type":"agent.message"}`, first.EventType},
		{"different type", string(first.Payload), "session.status_idle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			conflict := first
			conflict.Payload = json.RawMessage(test.payload)
			conflict.EventType = test.eventType
			conflict.StateChange = &db.SessionEventStateChange{Status: "terminated"}
			beforeConflict := consistencyTestEvent("must roll back with conflict", time.Now().UTC())
			beforeConflict.StateChange = &db.SessionEventStateChange{Status: "running"}
			created, err := app.db.AppendSessionEventsIfAbsent(t.Context(), session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{beforeConflict, conflict}, nil, ignored)
			if !errors.Is(err, db.ErrSessionEventConflict) || len(created) != 0 {
				t.Fatalf("conflict result = %v, %v", created, err)
			}
			if _, err := app.db.GetSessionEvent(t.Context(), session.WorkspaceUUID, session.ExternalID, beforeConflict.ExternalID); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("partial batch persisted: %v", err)
			}
			progress, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !progress.Equal(watermark) {
				t.Fatalf("conflict advanced clock: %v, %v", progress, err)
			}
			if current := mustSessionRecord(t, app, session.ExternalID); current.Status != session.Status {
				t.Fatalf("conflict changed status to %s", current.Status)
			}
		})
	}
	// PostgreSQL compares JSON values, including exact numbers, rather than raw
	// bytes or float64 values. Only the two top-level server clocks are ignored.
	same := first
	same.Payload = json.RawMessage(` { "content": {"created_at":"nested-original", "fraction": 1000.00, "count": 9007199254740993}, "type":"agent.message", "id":` + quoteJSON(first.ExternalID) + `,"created_at":"new","processed_at":"new" } `)
	same.UUID = uuid.NewV4().String()
	same.CreatedAt = time.Now().Add(time.Hour)
	same.ProcessedAt = same.CreatedAt
	same.StateChange = &db.SessionEventStateChange{Status: "terminated"}
	created, err := app.db.AppendSessionEventsIfAbsent(t.Context(), session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{same}, nil, ignored)
	if err != nil || len(created) != 0 {
		t.Fatalf("equivalent retry = %v, %v", created, err)
	}
	if current := mustSessionRecord(t, app, session.ExternalID); current.Status != session.Status {
		t.Fatalf("retry reapplied state: %s", current.Status)
	}
	stored, err := app.db.GetSessionEvent(t.Context(), session.WorkspaceUUID, session.ExternalID, first.ExternalID)
	if err != nil || string(stored.Payload) != string(rows[0].Payload) || !stored.ProcessedAt.Equal(watermark) {
		t.Fatalf("retry changed original event: %v", err)
	}
	primary, found, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !found {
		t.Fatalf("primary thread: %v", err)
	}
	child := primary
	child.UUID = uuid.NewV4().String()
	child.ExternalID = "sthr_" + uuid.NewV4().String()
	child.ParentThreadUUID = &primary.UUID
	child.ParentThreadExternalID = &primary.ExternalID
	if _, err := app.db.CreateSessionThreadIfAbsent(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	moved := same
	moved.ThreadExternalID = &child.ExternalID
	if _, err := app.db.AppendSessionEventsIfAbsent(t.Context(), session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{moved}, nil, ignored); !errors.Is(err, db.ErrSessionEventConflict) {
		t.Fatalf("retry changed thread: %v", err)
	}
	// An ID already accepted in another Session is a conflict within this workspace.
	other := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	if _, err := app.db.AppendSessionEventsIfAbsent(t.Context(), session.WorkspaceUUID, other.ID, []db.SessionEvent{same}, nil, ignored); !errors.Is(err, db.ErrSessionEventConflict) {
		t.Fatalf("cross-session ID collision: %v", err)
	}
}

func TestWorkerEventRetryReturnsConflictWithoutChangingHistory(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "worker-event-retry", `{"model":"claude-opus-4-6","name":"worker-event-retry"}`, `{"name":"worker-event-retry"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, codeSessionID)
	messageID := uuid.NewV4().String()
	body := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"payload":{"type":"assistant","uuid":` + quoteJSON(messageID) + `,"metadata":{"counter":9007199254740993},"message":{"id":` + quoteJSON(messageID) + `,"role":"assistant","content":[{"type":"text","text":"accepted reply"}]}}}]}`
	postCodeSessionWorkerEvents(t, app, codeSessionID, body)
	before := listSessionEvents(t, app, session.ID, "limit=100", defaultTestKey)
	if !eventPageContains(before, "9007199254740993") {
		t.Fatal("worker number lost precision in public history")
	}
	// Exact worker retry must remain successful with the original durable ID/time.
	postCodeSessionWorkerEvents(t, app, codeSessionID, body)
	conflict := doCodeSessionWorkerRequest(t, app, codeSessionID, "events", strings.Replace(body, "accepted reply", "changed reply", 1))
	assertError(t, conflict, http.StatusConflict, "conflict_error")
	numericConflict := doCodeSessionWorkerRequest(t, app, codeSessionID, "events", strings.Replace(body, "9007199254740993", "9007199254740992", 1))
	assertError(t, numericConflict, http.StatusConflict, "conflict_error")
	after := listSessionEvents(t, app, session.ID, "limit=100", defaultTestKey)
	if len(before.Data) != len(after.Data) {
		t.Fatalf("conflicting retry added history: %d -> %d", len(before.Data), len(after.Data))
	}
	for i := range before.Data {
		if string(before.Data[i]) != string(after.Data[i]) {
			t.Fatalf("history changed on retry: %s / %s", before.Data[i], after.Data[i])
		}
	}
}
