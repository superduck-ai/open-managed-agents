//go:build integration

package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessiontitles"
)

func TestSessionAutoTitles(t *testing.T) {
	var calls atomic.Int32
	var lastRequest atomic.Pointer[string]
	entered, release := make(chan struct{}, 4), make(chan struct{}, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		payload, _ := io.ReadAll(r.Body)
		lastRequest.Store(new(string(payload)))
		if strings.Contains(string(payload), "race input") {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if strings.Contains(string(payload), "failure input") {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"**会话标题修复**"}],"usage":{"input_tokens":20,"output_tokens":8}}`)
	}))
	defer upstream.Close()
	defer close(release)
	objects := newFakeStore("session-auto-titles")
	app := newPayloadIntegrationAppWithConfig(t, objects, func(cfg *config.Config) {
		cfg.Bootstrap = config.BootstrapConfig{
			WorkspaceName: "default", OrganizationName: "default", WorkspaceExternalID: "workspace_default",
			UserExternalID: "user_default", UserEmail: "admin@example.local", UserName: "Local Admin",
			APIKeyExternalID: "api_key_default", SeedAPIKeys: []config.SeedAPIKey{{ExternalID: "api_key_default", Key: config.DefaultAPIKey}},
		}
	})
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Title model", upstream.URL, "title-test-key", "claude-opus-4-6")
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"auto-titles"}`)
	env := createEnvironment(t, app, `{"name":"auto-titles"}`)
	prefix := `{"agent":` + quoteJSON(agent.ID) + `,"environment_id":` + quoteJSON(env.ID)
	message := func(text string) string {
		return `{"type":"user.message","content":[{"type":"text","text":` + quoteJSON(text) + `}]}`
	}
	worker := sessiontitles.NewWorker(app.db, objects, app.vaultSecrets, slog.New(slog.NewTextHandler(io.Discard, nil)))
	run := func() {
		t.Helper()
		if err := worker.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	assertTitle := func(id string, want *string) {
		t.Helper()
		got := retrieveSession(t, app, id, defaultTestKey)
		if !reflect.DeepEqual(got.Title, want) {
			t.Fatalf("title=%v want=%v", got.Title, want)
		}
	}
	idle := func(id string) db.Session {
		t.Helper()
		s := mustSessionRecord(t, app, id)
		if _, err := app.db.SetSessionStatus(t.Context(), s.WorkspaceUUID, id, "idle"); err != nil {
			t.Fatal(err)
		}
		return mustSessionRecord(t, app, id)
	}
	t.Run("enqueue failure preserves input and later job uses it", func(t *testing.T) {
		s := createSession(t, app, prefix+"}")
		const unblock = "ALTER TABLE jobs DROP CONSTRAINT IF EXISTS reject_title_job"
		if _, err := app.pool.Exec(t.Context(), "ALTER TABLE jobs ADD CONSTRAINT reject_title_job CHECK (type <> 'session_title') NOT VALID"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := app.pool.Exec(context.Background(), unblock); err != nil {
				t.Errorf("restore title job inserts: %v", err)
			}
		})
		const first = "input retained after title enqueue error"
		sendSessionEvents(t, app, s.ID, `{"events":[`+message(first)+`]}`, defaultTestKey)
		events := listSessionEvents(t, app, s.ID, "types=user.message", defaultTestKey)
		if len(events.Data) != 1 || !strings.Contains(string(events.Data[0]), first) {
			t.Fatalf("first user message was not persisted: %v", events.Data)
		}
		if _, err := app.pool.Exec(t.Context(), unblock); err != nil {
			t.Fatal(err)
		}
		idle(s.ID)
		sendSessionEvents(t, app, s.ID, `{"events":[`+message("later input must not replace first")+`]}`, defaultTestKey)
		run()
		if request := lastRequest.Load(); request == nil || !strings.Contains(*request, first) {
			t.Fatal("title model did not receive the first committed user message")
		}
		assertTitle(s.ID, new("会话标题修复"))
	})
	t.Run("existing titles skip first-turn generation", func(t *testing.T) {
		before := calls.Load()
		s := createSession(t, app, prefix+`}`)
		updateSession(t, app, s.ID, `{"title":"Pinned"}`)
		sendSessionEvents(t, app, s.ID, `{"events":[`+message("should not name")+`]}`, defaultTestKey)
		run()
		assertTitle(s.ID, new("Pinned"))

		created := createSession(t, app, prefix+`,"title":"Created title","initial_events":[`+message("ignored")+`]}`)
		run()
		assertTitle(created.ID, new("Created title"))

		idle(created.ID)
		updateSession(t, app, created.ID, `{"title":null}`)
		sendSessionEvents(t, app, created.ID, `{"events":[`+message("too late to name")+`]}`, defaultTestKey)
		run()
		assertTitle(created.ID, nil)
		if calls.Load() != before {
			t.Fatal("title generation ran outside the first eligible turn")
		}
	})
	t.Run("first initial input once with no public events", func(t *testing.T) {
		invisible := strings.Repeat(message("\u200b")+",", 16)
		s := createSession(t, app, prefix+`,"initial_events":[`+invisible+message("first input")+","+message("later input")+`]}`)
		record := mustSessionRecord(t, app, s.ID)
		var ledgerBefore int
		if err := app.pool.QueryRow(t.Context(), "SELECT count(*) FROM usage_records WHERE workspace_uuid=$1", record.WorkspaceUUID).Scan(&ledgerBefore); err != nil {
			t.Fatal(err)
		}
		before := listSessionEvents(t, app, s.ID, "", defaultTestKey)
		filtered := listSessionEvents(t, app, s.ID, "types=user.message&limit=1", defaultTestKey)
		count := calls.Load()
		var cursor struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(before.Data[len(before.Data)-1], &cursor); err != nil {
			t.Fatal(err)
		}
		streamCtx, cancelStream := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancelStream()
		request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, app.baseURL+"/v1/sessions/"+s.ID+"/events/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Api-Key", defaultTestKey)
		request.Header.Set("Last-Event-ID", cursor.ID)
		stream, err := app.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Body.Close()
		if stream.StatusCode != http.StatusOK {
			t.Fatalf("stream status=%d", stream.StatusCode)
		}
		streamData := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(stream.Body)
			for scanner.Scan() {
				if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
					streamData <- strings.TrimPrefix(line, "data: ")
					return
				}
			}
			streamData <- "stream ended"
		}()
		stop := worker.Start(t.Context())
		deadline := time.Now().Add(5 * time.Second)
		for retrieveSession(t, app, s.ID, defaultTestKey).Title == nil && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		stop()
		assertTitle(s.ID, new("会话标题修复"))
		if request := lastRequest.Load(); request == nil || !strings.Contains(*request, "first input") {
			t.Fatal("title model did not receive the first valid input")
		}
		if after := listSessionEvents(t, app, s.ID, "", defaultTestKey); !reflect.DeepEqual(before, after) {
			t.Fatal("auto title changed public events")
		}
		if after := listSessionEvents(t, app, s.ID, "types=user.message&limit=1", defaultTestKey); !reflect.DeepEqual(filtered, after) {
			t.Fatal("auto title changed filtered pagination")
		}
		sendSessionEvents(t, app, s.ID, `{"events":[`+message("third input")+`]}`, defaultTestKey)
		run()
		assertTitle(s.ID, new("会话标题修复"))
		if calls.Load() != count+1 {
			t.Fatal("more than one model request")
		}
		select {
		case data := <-streamData:
			var event struct {
				Type  string          `json:"type"`
				Title json.RawMessage `json:"title"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil || event.Type != "user.message" || len(event.Title) > 0 || !strings.Contains(data, "third input") {
				t.Fatalf("unexpected stream event: %s", data)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("stream did not deliver next user input")
		}
		var ledgerCount, sessionLedgerCount int
		if err := app.pool.QueryRow(t.Context(), `SELECT count(*), count(session_uuid) FROM usage_records WHERE workspace_uuid=$1`, record.WorkspaceUUID).Scan(&ledgerCount, &sessionLedgerCount); err != nil {
			t.Fatal(err)
		}
		if ledgerCount != ledgerBefore+1 || sessionLedgerCount != 0 {
			t.Fatalf("auxiliary ledger counts before=%d after=%d session=%d", ledgerBefore, ledgerCount, sessionLedgerCount)
		}
	})
	t.Run("in-flight result only fills an empty title", func(t *testing.T) {
		for _, manual := range []string{`"Manual winner"`, `null`} {
			s := createSession(t, app, prefix+`,"initial_events":[`+message("race input")+`]}`)
			idle(s.ID)
			before := listSessionEvents(t, app, s.ID, "", defaultTestKey)
			done := make(chan error, 1)
			go func() { done <- worker.RunOnce(t.Context()) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("title request did not start")
			}
			if manual == "null" {
				updateSession(t, app, s.ID, `{"title":"Temporary title"}`)
			}
			updateSession(t, app, s.ID, `{"title":`+manual+`}`)
			release <- struct{}{}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("title job did not finish")
			}
			want, wantExtra := "Manual winner", 1
			if manual == "null" {
				want, wantExtra = "会话标题修复", 2
			}
			assertTitle(s.ID, &want)
			after := listSessionEvents(t, app, s.ID, "", defaultTestKey)
			if len(after.Data) != len(before.Data)+wantExtra {
				t.Fatal("unexpected title events")
			}
		}
	})
	t.Run("metadata stale snapshot preserves generated title", func(t *testing.T) {
		s := createSession(t, app, prefix+`,"initial_events":[`+message("metadata input")+`]}`)
		stale := idle(s.ID)
		run()
		stale.Metadata = json.RawMessage(`{"updated":"yes"}`)
		stale.UpdatedAt = time.Now().UTC()
		if _, _, err := app.db.UpdateSession(t.Context(), stale.WorkspaceUUID, s.ID, stale, false); err != nil {
			t.Fatal(err)
		}
		assertTitle(s.ID, new("会话标题修复"))
	})
	t.Run("normal message blob input and failure fallback", func(t *testing.T) {
		text := "failure input " + strings.Repeat("a", 40000)
		s := createSession(t, app, prefix+`,"title":""}`)
		sendSessionEvents(t, app, s.ID, `{"events":[`+message(text)+`]}`, defaultTestKey)
		before := listSessionEvents(t, app, s.ID, "", defaultTestKey)
		run()
		want := "failure input " + strings.Repeat("a", 82)
		assertTitle(s.ID, new(want))
		if after := listSessionEvents(t, app, s.ID, "", defaultTestKey); !reflect.DeepEqual(before, after) {
			t.Fatal("fallback changed public events")
		}
	})
	t.Run("lease recovery and scope fencing", func(t *testing.T) {
		s := createSession(t, app, prefix+`,"initial_events":[`+message("recovery input")+`]}`)
		jobs, err := app.db.LeaseSessionTitleJobs(t.Context(), "old-worker", 1)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("lease=%v %v", jobs, err)
		}
		job := jobs[0]
		if second, err := app.db.LeaseSessionTitleJobs(t.Context(), "second-worker", 1); err != nil || len(second) != 0 {
			t.Fatalf("double lease=%v %v", second, err)
		}
		foreign := job
		foreign.WorkspaceUUID = "00000000-0000-0000-0000-000000000001"
		if err := app.db.CompleteSessionTitleJob(t.Context(), foreign, "old-worker", "Wrong scope"); err != nil {
			t.Fatal(err)
		}
		if err := app.db.CompleteSessionTitleJob(t.Context(), job, "wrong-worker", "Wrong owner"); err != nil {
			t.Fatal(err)
		}
		assertTitle(s.ID, nil)
		if _, err := app.pool.Exec(t.Context(), `UPDATE jobs SET locked_until=now()-interval '1 second' WHERE workspace_uuid=$1 AND uuid=$2`, job.WorkspaceUUID, job.UUID); err != nil {
			t.Fatal(err)
		}
		if err := app.db.CompleteSessionTitleJob(t.Context(), job, "old-worker", "Expired"); err != nil {
			t.Fatal(err)
		}
		assertTitle(s.ID, nil)
		run()
		assertTitle(s.ID, new("会话标题修复"))
		var status string
		var hasInput bool
		if err := app.pool.QueryRow(t.Context(), `SELECT status, payload ? 'input' FROM jobs WHERE workspace_uuid=$1 AND uuid=$2`, job.WorkspaceUUID, job.UUID).Scan(&status, &hasInput); err != nil {
			t.Fatal(err)
		}
		if status != "completed" || hasInput {
			t.Fatalf("job state=%s input retained=%t", status, hasInput)
		}
	})
	t.Logf("proof: HTTP title retrieval, once-only calls, manual priority, blob input, failure fallback, unchanged events/pagination; model calls=%d", calls.Load())
}
