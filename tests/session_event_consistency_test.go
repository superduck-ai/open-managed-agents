package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

type unavailableSessionBus struct{ *sessionfanout.LocalBus }

func (b unavailableSessionBus) Subscribe(context.Context, string) error {
	return errors.New("bus unavailable")
}

func TestSessionStreamHistoryConsistency(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "session-event-consistency", `{"model":"claude-opus-4-6","name":"event-consistency"}`, `{"name":"event-consistency"}`)

	for _, mode := range []string{"lost-final-notification", "reversed-and-duplicate-notifications", "unavailable-bus"} {
		t.Run(mode, func(t *testing.T) {
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			session := mustSessionRecord(t, app, response.ID)
			local := sessionfanout.NewLocal()
			var bus sessionfanout.EventBus = local
			if mode == "unavailable-bus" {
				bus = unavailableSessionBus{local}
			}
			service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
			handler := sessionsapi.NewHandler(app.cfg, app.db, service, nil, bus, nil)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				principal := auth.Principal{CredentialType: auth.CredentialTypeAPIKey, WorkspaceUUID: session.WorkspaceUUID, OrganizationUUID: session.OrganizationUUID}
				handler.StreamEvents(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)), session.ExternalID)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Body.Close()
			if stream.StatusCode != http.StatusOK {
				t.Fatalf("stream status %d: %s", stream.StatusCode, readAll(t, stream.Body))
			}

			var created []db.SessionEvent
			for _, label := range []string{"A", "B", "C"} {
				// Source times deliberately run backwards. No bus notification
				// accompanies these committed records.
				event := consistencyTestEvent(label, time.Date(2020, 1, 4-len(created), 0, 0, 0, 0, time.UTC))
				if label == "B" {
					// Old persisted thinking payloads must also be projected as
					// progress signals, without rewriting their retry identity.
					event.EventType = "agent.thinking"
					event.Payload = json.RawMessage(`{"id":"` + event.ExternalID + `","type":"agent.thinking","content":[{"type":"thinking","thinking":"private-thinking"}],"message":{"content":"nested-thinking"},"signature":"private-signature","future_field":"extra-content"}`)
				}
				rows, err := app.db.AppendSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{event}, nil)
				if err != nil {
					t.Fatal(err)
				}
				created = append(created, rows...)
			}
			if mode == "reversed-and-duplicate-notifications" {
				for _, index := range []int{1, 0, 1} {
					raw, err := json.Marshal(struct {
						Events []map[string]any `json:"events"`
					}{Events: []map[string]any{{
						"workspace_uuid": session.WorkspaceUUID, "session_id": session.ExternalID,
						"external_id": created[index].ExternalID, "event_type": "agent.message",
						"payload": map[string]any{"content": "must never be emitted from notification"},
					}}})
					if err != nil {
						t.Fatal(err)
					}
					if err := local.Publish(ctx, session.ExternalID, sessionfanout.Envelope{Kind: sessionfanout.KindSessionEvents, Payload: raw}); err != nil {
						t.Fatal(err)
					}
				}
			}
			history := listSessionEvents(t, app, session.ExternalID, "types[]=agent.message&types[]=agent.thinking&limit=100", defaultTestKey)
			if len(history.Data) != 3 {
				t.Fatalf("history length = %d", len(history.Data))
			}
			scanner := bufio.NewScanner(stream.Body)
			for received, raw := range history.Data {
				frame := assertNextSessionFrameType(t, scanner, created[received].EventType)
				assertSessionEventJSONEqual(t, frame, raw)
				var live map[string]any
				if err := json.Unmarshal(frame, &live); err != nil {
					t.Fatal(err)
				}
				if live["id"] != created[received].ExternalID {
					t.Fatalf("event %d out of commit order: %v", received, live)
				}
				if live["type"] == "agent.thinking" {
					if len(live) != 5 || live["processed_at"] == nil || live["session_thread_id"] == nil {
						t.Fatalf("thinking must contain only progress identity and ordering fields: %v", live)
					}
					stored, err := app.db.GetSessionEvent(ctx, session.WorkspaceUUID, session.ExternalID, created[received].ExternalID)
					if err != nil || !strings.Contains(string(stored.Payload), "private-thinking") {
						t.Fatalf("public projection changed stored retry payload: %v", err)
					}
				}
			}
			deleteSession(t, app, session.ExternalID)
			deleted := false
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "event: session.deleted") {
					deleted = true
				}
			}
			if !deleted || scanner.Err() != nil {
				t.Fatalf("deleted event must precede EOF: deleted=%t error=%v", deleted, scanner.Err())
			}
		})
	}
}

func TestSessionEventClockRollbackAndSnapshot(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "session-event-clock", `{"model":"claude-opus-4-6","name":"event-clock"}`, `{"name":"event-clock"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	ctx := t.Context()
	before, err := app.db.SessionEventWatermark(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	first := consistencyTestEvent("first", before.Add(-time.Hour))
	first.StateChange = &db.SessionEventStateChange{Status: "running"}
	invalid := consistencyTestEvent("invalid", before)
	invalid.ThreadExternalID = new("thread_missing")
	if _, err := app.db.AppendSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{first, invalid}, nil); err == nil {
		t.Fatal("invalid batch succeeded")
	}
	after, err := app.db.SessionEventWatermark(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil || !after.Equal(before) {
		t.Fatalf("failed batch advanced clock: %v, %v", after, err)
	}
	if got := mustSessionRecord(t, app, session.ExternalID).Status; got != session.Status {
		t.Fatalf("failed batch changed status to %s", got)
	}
	second := consistencyTestEvent("second", before.Add(-2*time.Hour))
	rows, err := app.db.AppendSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{first, second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].ProcessedAt.After(before) || !rows[1].ProcessedAt.After(rows[0].ProcessedAt) {
		t.Fatal("event clock did not follow insertion order")
	}
	page := listSessionEvents(t, app, session.ExternalID, "types[]=agent.message&limit=1", defaultTestKey)
	if page.NextPage == nil {
		t.Fatal("missing snapshot cursor")
	}
	wrongOrder := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+session.ExternalID+"/events?beta=true&types[]=agent.message&order=desc&page="+url.QueryEscape(*page.NextPage), nil, defaultTestKey, true)
	assertError(t, wrongOrder, http.StatusBadRequest, "invalid_request_error")
	third := consistencyTestEvent("third", before)
	third.StateChange = &db.SessionEventStateChange{Status: "idle"}
	if _, err := app.db.AppendSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{third}, nil); err != nil {
		t.Fatal(err)
	}
	next := listSessionEvents(t, app, session.ExternalID, "types[]=agent.message&limit=100&page="+url.QueryEscape(*page.NextPage), defaultTestKey)
	if len(next.Data) != 1 || next.NextPage != nil {
		t.Fatalf("snapshot grew during pagination: %+v", next)
	}
	watermark, err := app.db.SessionEventWatermark(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := app.db.AppendSessionEventsIfAbsent(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{first}, nil, []string{"created_at", "processed_at"})
	if err != nil || len(retried) != 0 {
		t.Fatalf("retry inserted events: %v, %v", retried, err)
	}
	if got := mustSessionRecord(t, app, session.ExternalID).Status; got != "idle" {
		t.Fatalf("old retry overwrote later idle: %s", got)
	}
	after, err = app.db.SessionEventWatermark(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil || !after.Equal(watermark) {
		t.Fatalf("retry changed clock: %v, %v", after, err)
	}
	filtered := listSessionEvents(t, app, session.ExternalID, "types[]=agent.message&created_at[gt]="+url.QueryEscape(rows[0].ProcessedAt.Format(time.RFC3339Nano)), defaultTestKey)
	if len(filtered.Data) != 2 {
		t.Fatalf("processed time filter returned %d events", len(filtered.Data))
	}
	// Simulate a clock that moved back after the last committed event.
	future := watermark.Add(time.Hour)
	if _, err := app.pool.Exec(ctx, "UPDATE sessions SET last_event_at=$1 WHERE workspace_uuid=$2 AND uuid=$3", future, session.WorkspaceUUID, session.UUID); err != nil {
		t.Fatal(err)
	}
	rows, err = app.db.AppendSessionEvents(ctx, session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{consistencyTestEvent("after-clock-rollback", before)}, nil)
	if err != nil || !rows[0].ProcessedAt.Equal(future.Add(time.Microsecond)) {
		t.Fatalf("clock rollback broke monotonic order: %v, %v", rows, err)
	}
}

func consistencyTestEvent(text string, sourceTime time.Time) db.SessionEvent {
	id := "sevt_" + uuid.NewV4().String()
	raw, _ := json.Marshal(struct {
		ID          string    `json:"id"`
		Type        string    `json:"type"`
		Content     string    `json:"content"`
		ProcessedAt time.Time `json:"processed_at"`
	}{id, "agent.message", text, sourceTime})
	return db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "agent.message", Payload: raw, CreatedAt: sourceTime, ProcessedAt: sourceTime}
}

func TestWorkerEpochSwitchBeforePublicCommitRejectsLateOutput(t *testing.T) {
	for _, mode := range []string{"public output", "worker state"} {
		t.Run(mode, func(t *testing.T) {
			app, agent, env := newSessionEventTestApp(t, "session-event-epoch-fence", `{"model":"claude-opus-4-6","name":"event-epoch-fence"}`, `{"name":"event-epoch-fence"}`)
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			session := mustSessionRecord(t, app, response.ID)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			codeSession, err := app.db.CreateCodeSession(ctx, db.CreateCodeSessionInput{
				ExternalID: "cse_" + uuid.NewV4().String(), OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID,
				SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID,
				WorkDir: "/workspace", PermissionMode: "bypassPermissions", Model: "claude-opus-4-6", Status: "active", InitialWorkerEpoch: 1,
				Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
			})
			if err != nil {
				t.Fatal(err)
			}
			service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
			sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, nil)
			before, err := app.db.SessionEventWatermark(ctx, session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			blocker, err := app.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			var blockerPID int
			if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid() FROM sessions WHERE workspace_uuid=$1 AND uuid=$2 FOR UPDATE", session.WorkspaceUUID, session.UUID).Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				if mode == "worker state" {
					_, err := service.UpdateWorkerState(ctx, codeSession.ExternalID, db.UpdateCodeSessionWorkerStateInput{
						WorkerEpoch: 1, WorkerStatus: new("running"), ExternalMetadataSet: true, ExternalMetadata: json.RawMessage(`{"late":true}`),
					})
					result <- err
					return
				}
				result <- service.AppendWorkerEvents(ctx, codesessions.CodeSessionStreamRoute{
					CodeSessionID: codeSession.ExternalID, WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID,
				}, 1, []json.RawMessage{json.RawMessage(`{"type":"assistant","uuid":"late-worker","message":{"id":"late-message","role":"assistant","content":"late output"}}`)})
			}()
			// Wait for the public writer to reach its Session lock, after the initial
			// worker activity check has already succeeded. No timing-only sleep barrier.
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				if err := app.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))", blockerPID).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("worker finished before persistence barrier: %v", err)
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if _, err := app.pool.Exec(ctx, "UPDATE code_sessions SET current_worker_epoch=2 WHERE workspace_uuid=$1 AND uuid=$2", session.WorkspaceUUID, codeSession.UUID); err != nil {
				t.Fatal(err)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, db.ErrWorkerEpochMismatch) {
					t.Fatalf("late worker result: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			after, err := app.db.SessionEventWatermark(ctx, session.WorkspaceUUID, session.ExternalID)
			if err != nil || !after.Equal(before) {
				t.Fatalf("late worker advanced public progress: %v, %v", after, err)
			}
			if got := listSessionEvents(t, app, session.ExternalID, "types[]=agent.message", defaultTestKey); len(got.Data) != 0 {
				t.Fatalf("late output persisted: %s", got.Data)
			}
			if got := mustSessionRecord(t, app, session.ExternalID).Status; got != session.Status {
				t.Fatalf("late worker changed state to %s", got)
			}
			if mode == "worker state" {
				afterWorker, err := getCodeSession(app, ctx, codeSession.ExternalID)
				if err != nil || afterWorker.WorkerStatus != codeSession.WorkerStatus || string(afterWorker.WorkerExternalMetadata) != string(codeSession.WorkerExternalMetadata) {
					t.Fatalf("late worker changed private status or metadata: %v", err)
				}
			}

		})
	}
}

func TestSessionSendCommitsPublicInputAndWorkerQueueTogether(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "session-input-commit", `{"model":"claude-opus-4-6","name":"input-commit"}`, `{"name":"input-commit"}`)
	for _, stage := range []string{"inbound", "metadata"} {
		t.Run(stage, func(t *testing.T) {
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			session := mustSessionRecord(t, app, response.ID)
			codeSession, err := app.db.CreateCodeSession(t.Context(), db.CreateCodeSessionInput{
				ExternalID: "cse_" + uuid.NewV4().String(), OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID,
				SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID,
				WorkDir: "/workspace", PermissionMode: "default", Model: "claude-opus-4-6", Status: "active", InitialWorkerEpoch: 1,
				Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
			})
			if err != nil {
				t.Fatal(err)
			}
			metadata := json.RawMessage(`{
    "keep":"unrelated",
    "managed_agent_tool_permission_request:sevt_confirm":{"public_event_id":"sevt_confirm","event_type":"agent.tool_use","request_id":"confirm-request","provider_tool_use_id":"tool-confirm","input":{"command":"pwd"}},
    "managed_agent_tool_permission_request:sevt_answer":{"public_event_id":"sevt_answer","event_type":"agent.custom_tool_use","request_id":"answer-request","provider_tool_use_id":"tool-answer","input":{"questions":[]}}
   }`)
			putCodeSessionWorkerState(t, app, codeSession.ExternalID, `{"worker_epoch":1,"external_metadata":`+string(metadata)+`}`)
			codeSession, err = getCodeSession(app, t.Context(), codeSession.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			body := `{"events":[
    {"type":"user.define_outcome","description":"atomic outcome","rubric":{"type":"text","text":"done"}},
    {"type":"user.message","content":[{"type":"text","text":"before confirmations"}]},
    {"type":"user.tool_confirmation","tool_use_id":"sevt_confirm","result":"allow"},
    {"type":"user.custom_tool_result","custom_tool_use_id":"sevt_answer","content":[{"type":"text","text":"{\"Color\":\"Blue\"}"}]},
    {"type":"user.message","content":[{"type":"text","text":"after confirmations"}]}
   ]}`
			removeFailure := rejectSessionInputCommit(t, app, codeSession.UUID, stage)
			failed := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ExternalID+"/events?beta=true", strings.NewReader(body), defaultTestKey, true)
			assertError(t, failed, http.StatusInternalServerError, "api_error")
			after, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !after.Equal(before) {
				t.Fatalf("failed input advanced clock: %v, %v", after, err)
			}
			if got := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey); len(got.Data) != 0 {
				t.Fatalf("failed input persisted public events: %s", got.Data)
			}
			if got := mustSessionRecord(t, app, session.ExternalID).OutcomeEvaluations; string(got) != "[]" {
				t.Fatalf("failed input changed outcomes: %s", got)
			}
			queued, err := app.db.ListQueuedCodeSessionInboundEvents(t.Context(), codeSession.ExternalID)
			if err != nil || len(queued) != 0 {
				t.Fatalf("failed input partially queued: %v, %v", queued, err)
			}
			reloaded, found, err := app.db.GetCodeSession(t.Context(), codeSession.ExternalID)
			if err != nil || !found || reloaded.LastInboundSequenceNum != 0 || string(reloaded.WorkerExternalMetadata) != string(codeSession.WorkerExternalMetadata) {
				t.Fatalf("failed input changed worker sequence/metadata: %v, %v", found, err)
			}
			removeFailure()

			accepted := sendSessionEvents(t, app, session.ExternalID, body, defaultTestKey)
			if len(accepted.Data) != 5 {
				t.Fatalf("accepted input count = %d", len(accepted.Data))
			}
			queued, err = app.db.ListQueuedCodeSessionInboundEvents(t.Context(), codeSession.ExternalID)
			if err != nil || len(queued) != 4 {
				t.Fatalf("committed inputs: %v, %v", queued, err)
			}
			for i, event := range queued {
				if event.SequenceNum != int64(i+1) {
					t.Fatalf("input sequence = %d at %d", event.SequenceNum, i)
				}
			}
			if queued[0].Source != "public-session" || queued[1].Source != "tool-confirmation" || queued[2].Source != "custom-tool-result" || queued[3].Source != "public-session" {
				t.Fatalf("mixed batch changed input order: %v", queued)
			}
			reloaded, found, err = app.db.GetCodeSession(t.Context(), codeSession.ExternalID)
			if err != nil || !found || reloaded.LastInboundSequenceNum != 4 {
				t.Fatalf("committed worker sequence = %d, %v", reloaded.LastInboundSequenceNum, err)
			}
			var remaining map[string]json.RawMessage
			if err := json.Unmarshal(reloaded.WorkerExternalMetadata, &remaining); err != nil {
				t.Fatal(err)
			}
			if len(remaining) != 1 || string(remaining["keep"]) != `"unrelated"` {
				t.Fatalf("metadata clearing: %s", reloaded.WorkerExternalMetadata)
			}
			history := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey)
			if len(history.Data) != len(accepted.Data) {
				t.Fatalf("history count = %d, accepted = %d", len(history.Data), len(accepted.Data))
			}
			for i, raw := range accepted.Data {
				assertSessionEventJSONEqual(t, raw, history.Data[i])
			}
		})
	}
}

// Reject only this fixture's write, at either the queue or subsequent metadata
// update. A later failure must roll back public events, queue, clock and outcomes.
func rejectSessionInputCommit(t *testing.T, app *testApp, codeSessionUUID, stage string) func() {
	t.Helper()
	name := `"test_input_commit_` + uuid.NewV4().String() + `"`
	table, operation, condition := "code_session_inbound_events", "INSERT", "NEW.code_session_uuid::text = TG_ARGV[0]"
	if stage == "metadata" {
		table, operation, condition = "code_sessions", "UPDATE", "NEW.uuid::text = TG_ARGV[0] AND NEW.worker_external_metadata IS DISTINCT FROM OLD.worker_external_metadata"
	}
	_, err := app.pool.Exec(t.Context(), `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+condition+` THEN RAISE EXCEPTION 'test rejected session input commit'; END IF; RETURN NEW; END $$`)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	cleanup := func() {
		if removed {
			return
		}
		removed = true
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := app.pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+name+` ON `+table); err != nil {
			t.Error(err)
		}
		if _, err := app.pool.Exec(ctx, `DROP FUNCTION `+name+`() `); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(cleanup)
	_, err = app.pool.Exec(t.Context(), `CREATE TRIGGER `+name+` BEFORE `+operation+` ON `+table+` FOR EACH ROW EXECUTE FUNCTION `+name+`('`+strings.ReplaceAll(codeSessionUUID, "'", "''")+`')`)
	if err != nil {
		t.Fatal(err)
	}
	return cleanup
}
