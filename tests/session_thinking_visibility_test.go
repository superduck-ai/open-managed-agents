//go:build integration || e2e || e2b_integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionHistoryHidesStoredThinkingContent(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-thinking-visibility-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"session-thinking-visibility-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"session-thinking-visibility-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer cleanupSession(t, app, session.ID)

	now := time.Now().UTC()
	eventID := "sevt_legacy_thinking_" + uuid.NewV4().String()[:8]
	legacy := db.SessionEvent{
		UUID: uuid.NewV4().String(), ExternalID: eventID, EventType: "agent.thinking",
		Payload:     json.RawMessage(`{"type":"agent.thinking","id":` + quoteJSON(eventID) + `,"content":[{"type":"thinking","thinking":"legacy private thinking"}]}`),
		ProcessedAt: now, CreatedAt: now,
	}
	ids := getDefaultDBIDs(t, app.pool)
	if _, err := app.db.AppendSessionEvents(context.Background(), ids.WorkspaceUUID, session.ID, []db.SessionEvent{legacy}, nil); err != nil {
		t.Fatalf("append legacy thinking event: %v", err)
	}
	var stored []byte
	if err := app.pool.QueryRow(context.Background(), `select payload::text from session_events where external_id = $1`, eventID).Scan(&stored); err != nil {
		t.Fatalf("load stored payload: %v", err)
	}
	if !bytes.Contains(stored, []byte("legacy private thinking")) {
		t.Fatalf("stored payload was modified: %s", stored)
	}

	history := listSessionEvents(t, app, session.ID, "", defaultTestKey)
	found := false
	for _, event := range history.Data {
		if !bytes.Contains(event, []byte(eventID)) {
			continue
		}
		found = true
		if bytes.Contains(event, []byte("legacy private thinking")) || bytes.Contains(event, []byte(`"content"`)) {
			t.Fatalf("history exposed stored thinking content: %s", event)
		}
	}
	if !found {
		t.Fatalf("legacy thinking event missing from history: %s", history.Data)
	}
}
