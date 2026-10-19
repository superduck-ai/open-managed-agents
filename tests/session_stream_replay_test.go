package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"uuid"
)

func TestSessionStreamReplay(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("stream-replay"))
	code, epoch := newPayloadIntegrationSession(t, app)
	postCodeSessionWorkerEvents(t, app, code.ExternalID, internalPayloadRequest(epoch, `{"type":"system.message","id":"replay_cursor","uuid":"replay_cursor","content":[]}`))
	history := listSessionEvents(t, app, code.SessionExternalID, "", defaultTestKey)
	var cursor struct {
		ID string `json:"id"`
	}
	if len(history.Data) == 0 {
		t.Fatal("missing initial history")
	}
	if err := json.Unmarshal(history.Data[len(history.Data)-1], &cursor); err != nil {
		t.Fatal(err)
	}
	stored, err := app.db.GetSessionEvent(t.Context(), code.WorkspaceUUID, code.SessionExternalID, cursor.ID)
	if err != nil {
		t.Fatal(err)
	}
	open := func(sessionID, cursorID string) *http.Response {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+sessionID+"/events/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Api-Key", defaultTestKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
		req.Header.Set("Last-Event-ID", cursorID)
		response, err := app.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		return response
	}
	t.Run("invalid cursor", func(t *testing.T) {
		response := open(code.SessionExternalID, "missing")
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})
	t.Run("another session cursor", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"other"}`)
		env := createEnvironment(t, app, `{"name":"other"}`)
		other := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
		response := open(other.ID, cursor.ID)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})
	pending := stored
	pending.UUID = uuid.NewV4().String()
	pending.ExternalID = "queued_replay_input"
	pending.EventType = "user.message"
	pending.CreatedAt = stored.ProcessedAt.Add(time.Microsecond)
	pending.ProcessedAt = time.Time{}
	pending.Payload = json.RawMessage(`{"id":"queued_replay_input","type":"user.message","processed_at":null,"content":[]}`)
	if _, err := app.db.AppendSessionEventsIfAbsent(t.Context(), code.WorkspaceUUID, code.SessionExternalID, []db.SessionEvent{pending}); err != nil {
		t.Fatal(err)
	}
	var records []db.SessionEvent
	for i := range 1002 {
		id := fmt.Sprintf("replay_%04d", i)
		record := stored
		record.UUID = uuid.NewV4().String()
		record.ExternalID = id
		record.ProcessedAt = stored.ProcessedAt.Add(time.Duration(i+1) * time.Millisecond)
		record.Payload = json.RawMessage(fmt.Sprintf(`{"id":%q,"type":"system.message","content":[]}`, id))
		records = append(records, record)
	}
	if _, err := app.db.AppendSessionEventsIfAbsent(t.Context(), code.WorkspaceUUID, code.SessionExternalID, records); err != nil {
		t.Fatal(err)
	}
	response := open(code.SessionExternalID, cursor.ID)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", response.StatusCode, readAll(t, response.Body))
	}
	scanner := bufio.NewScanner(response.Body)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.ID != records[count].ExternalID {
			t.Fatalf("event %d = %q, want %q", count, event.ID, records[count].ExternalID)
		}
		count++
		if count == len(records) {
			return
		}
	}
	t.Fatalf("replay ended at %d: %v", count, scanner.Err())
}
