//go:build integration || e2e || e2b_integration

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

func TestSessionInitialEvents(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("initial-events-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"initial-events-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"initial-events-environment"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	prefix := `{"agent":` + quoteJSON(agent.ID) + `,"environment_id":` + quoteJSON(env.ID)
	message := `{"type":"user.message","content":[{"type":"text","text":"A"}]}`
	t.Run("failure validation is atomic", func(t *testing.T) {
		for _, raw := range []string{`null`, `[{"type":"user.interrupt"}]`, `[` + message + `,{"type":"user.message","content":[]}]`, `[` + strings.Repeat(message+`,`, 50) + message + `]`} {
			before := listSessions(t, app, "agent_id="+agent.ID)
			response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(prefix+`,"initial_events":`+raw+`}`), defaultTestKey, true)
			assertError(t, response, http.StatusBadRequest, "invalid_request_error")
			after := listSessions(t, app, "agent_id="+agent.ID)
			if len(before.Data) != len(after.Data) {
				t.Fatal("invalid batch created session")
			}
		}
	})
	t.Run("failure request body exceeds 32 MiB", func(t *testing.T) {
		body := prefix + `,"initial_events":[{"type":"user.message","content":[{"type":"text","text":"` + strings.Repeat("x", 32<<20) + `"}]}]}`
		response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, response, http.StatusRequestEntityTooLarge, "invalid_request_error")
	})
	for _, test := range []struct {
		name, extra, status string
		messages            int
	}{
		{"success omitted", "", "idle", 0}, {"success empty", `,"initial_events":[]`, "idle", 0},
		{"success ordered nonempty", `,"initial_events":[` + message + `,{"type":"user.message","content":[{"type":"text","text":"B"}]}]`, "running", 2},
		{"success outcome only", `,"initial_events":[{"type":"user.define_outcome","description":"done","rubric":{"type":"text","content":"done"}}]`, "running", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := createSession(t, app, prefix+test.extra+`}`)
			defer cleanupSession(t, app, session.ID)
			if session.Status != test.status {
				t.Fatalf("status=%s want=%s", session.Status, test.status)
			}
			record := mustSessionRecord(t, app, session.ID)
			events, more, err := app.db.ListSessionEventsPage(t.Context(), db.ListSessionEventsPageParams{WorkspaceUUID: record.WorkspaceUUID, SessionExternalID: session.ID, Limit: 100, Order: "asc"})
			if err != nil {
				t.Fatal(err)
			}
			if more {
				t.Fatal("unexpected history pagination")
			}
			var texts []string
			for _, event := range events {
				if event.EventType == "user.message" {
					var payload struct {
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					}
					if e := json.Unmarshal(event.Payload, &payload); e != nil {
						t.Fatal(e)
					}
					texts = append(texts, payload.Content[0].Text)
					if event.ExternalID == "" || !event.ProcessedAt.IsZero() {
						t.Fatal("invalid input identity or processing state")
					}
				}
			}
			if len(texts) != test.messages || (test.messages == 2 && (texts[0] != "A" || texts[1] != "B")) {
				t.Fatalf("messages=%v", texts)
			}
			if test.status == "idle" && len(events) != 0 {
				t.Fatal("empty session has events")
			}
			if strings.Contains(test.name, "outcome") {
				var outcomes []any
				if json.Unmarshal(session.OutcomeEvaluations, &outcomes) != nil || len(outcomes) != 1 {
					t.Fatal("outcome missing")
				}
			}
		})
	}
	t.Run("success large initial message round trips through payload storage", func(t *testing.T) {
		text := strings.Repeat("x", (4<<20)+1)
		raw, err := json.Marshal(map[string]any{"agent": agent.ID, "environment_id": env.ID, "initial_events": []any{map[string]any{"type": "user.message", "content": []any{map[string]any{"type": "text", "text": text}}}}})
		if err != nil {
			t.Fatal(err)
		}
		session := createSession(t, app, string(raw))
		defer cleanupSession(t, app, session.ID)
		history := listSessionEvents(t, app, session.ID, "", defaultTestKey)
		found := false
		for _, event := range history.Data {
			var payload struct {
				Type    string `json:"type"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(event, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Type == "user.message" {
				found = true
				if len(payload.Content) != 1 || payload.Content[0].Text != text {
					t.Fatal("large payload changed")
				}
			}
		}
		if !found {
			t.Fatal("large input missing")
		}
	})
	t.Run("failure event persistence rolls back creation", func(t *testing.T) {
		base := createSession(t, app, prefix+`}`)
		defer cleanupSession(t, app, base.ID)
		session := mustSessionRecord(t, app, base.ID)
		thread, found, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, base.ID)
		if err != nil || !found {
			t.Fatal(err)
		}
		session.UUID = uuid.NewV4().String()
		session.ExternalID = "sesn_initial_rollback_" + strings.ReplaceAll(session.UUID, "-", "")
		thread.UUID = uuid.NewV4().String()
		thread.ExternalID = "sthr_initial_rollback_" + strings.ReplaceAll(thread.UUID, "-", "")
		now := time.Now().UTC()
		input := db.CreateSessionInput{Session: session, Thread: thread, Work: db.EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: "work_initial_" + strings.ReplaceAll(session.UUID, "-", ""), OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID, State: "queued", Metadata: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now}}
		makeEvent := func(id string) db.SessionEvent {
			return db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "user.message", Payload: json.RawMessage(message), CreatedAt: now}
		}
		first, second := makeEvent("sevt_initial_first"), makeEvent("sevt_initial_second")
		missing := "sthr_missing_initial"
		second.ThreadExternalID = &missing
		_, _, _, _, _, err = app.db.CreateSessionWithEvents(t.Context(), input, []db.SessionEvent{first, second})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("expected missing thread after first insert, got %v", err)
		}
		_, found, err = app.db.GetSession(t.Context(), session.WorkspaceUUID, session.ExternalID)
		if err != nil || found {
			t.Fatalf("partial session persisted: found=%v err=%v", found, err)
		}
	})
}
