//go:build integration

package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"uuid"
)

type reconnectSSEWriter struct {
	header  http.Header
	mu      sync.Mutex
	body    bytes.Buffer
	ready   chan struct{}
	release chan struct{}
	updates chan struct{}
	once    sync.Once
}

func (w *reconnectSSEWriter) Header() http.Header { return w.header }
func (w *reconnectSSEWriter) WriteHeader(int)     {}
func (w *reconnectSSEWriter) Write(raw []byte) (int, error) {
	w.mu.Lock()
	n, err := w.body.Write(raw)
	w.mu.Unlock()
	select {
	case w.updates <- struct{}{}:
	default:
	}
	return n, err
}
func (w *reconnectSSEWriter) Flush() { w.once.Do(func() { close(w.ready); <-w.release }) }
func (w *reconnectSSEWriter) snapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func newReconnectPostgres(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "stream_reconnect_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := db.Open(ctx, config.Config{Database: config.DatabaseConfig{URL: u.String()}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestQueuedACKReconnectHandlerPostgres(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprintf("processed-before-reconnect-%t", before), func(t *testing.T) {
			store := newReconnectPostgres(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			org, workspace, sessionUUID, envUUID, agentUUID, keyUUID := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
			sid, thread := "sesn_reconnect", "sthr_reconnect"
			_, err := store.ListenerPool().Exec(ctx, `INSERT INTO sessions (uuid,external_id,organization_uuid,workspace_uuid,created_by_api_key_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot,status) VALUES ($1,$2,$3,$4,$5,$6,'env_reconnect',$7,'agent_reconnect',1,'{}','running')`, sessionUUID, sid, org, workspace, keyUUID, envUUID, agentUUID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.CreateSessionThreadIfAbsent(ctx, db.SessionThread{UUID: uuid.NewV4().String(), ExternalID: thread, OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: sessionUUID, SessionExternalID: sid, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			code, err := store.CreateCodeSession(ctx, db.CreateCodeSessionInput{ExternalID: "cse_reconnect", OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: sessionUUID, SessionExternalID: sid, EnvironmentUUID: envUUID, EnvironmentExternalID: "env_reconnect", InitialWorkerEpoch: 1, Metadata: []byte(`{"config":{"worker_protocol":"dsr1"}}`)})
			if err != nil {
				t.Fatal(err)
			}
			input := db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "queued-input", EventType: "user.message", ThreadExternalID: &thread, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Payload: []byte(`{"id":"queued-input","type":"user.message","content":[{"type":"text","text":"exact"}],"processed_at":null}`)}
			if _, err = store.AppendSessionEventsIfAbsent(ctx, workspace, sid, []db.SessionEvent{input}); err != nil {
				t.Fatal(err)
			}
			middle := db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "between-queued-and-ack", EventType: "system.message", ThreadExternalID: &thread, CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC(), Payload: []byte(`{"id":"between-queued-and-ack","type":"system.message","content":[{"type":"text","text":"unchanged"}]}`)}
			if _, err = store.AppendSessionEventsIfAbsent(ctx, workspace, sid, []db.SessionEvent{middle}); err != nil {
				t.Fatal(err)
			}
			credentials, err := codesessions.NewSessionCredentials(config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			h := NewHandler(config.Config{}, store, codesessions.NewServiceWithCredentials(store, credentials, nil), nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			process := func() {
				records, e := store.ProcessSessionInputsForWorker(ctx, code, 1, []string{input.ExternalID})
				if e != nil || len(records) != 1 {
					t.Fatal("actual ACK processing", e, len(records))
				}
				if e = h.PublishCommittedCodeSessionEvents(ctx, code, db.SessionEventChanges{Events: records}); e != nil {
					t.Fatal(e)
				}
			}
			if before {
				process()
			}
			writer := &reconnectSSEWriter{header: make(http.Header), ready: make(chan struct{}), release: make(chan struct{}), updates: make(chan struct{}, 1)}
			request := httptest.NewRequest(http.MethodGet, "/events/stream", nil).WithContext(auth.WithPrincipal(ctx, auth.Principal{CredentialType: auth.CredentialTypeAPIKey, WorkspaceUUID: workspace, OrganizationUUID: org}))
			request.Header.Set("Last-Event-ID", input.ExternalID)
			done := make(chan struct{})
			go func() { defer close(done); h.StreamEvents(writer, request, sid) }()
			select {
			case <-writer.ready:
			case <-ctx.Done():
				t.Fatal("reconnect setup did not finish", writer.snapshot())
			}
			if !before {
				process()
			}
			close(writer.release)
			for {
				text := writer.snapshot()
				var events []map[string]json.RawMessage
				for _, line := range strings.Split(text, "\n") {
					if strings.HasPrefix(line, "data: ") {
						var e map[string]json.RawMessage
						if err = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
							t.Fatal(err)
						}
						events = append(events, e)
					}
				}
				if len(events) >= 2 {
					if len(events) != 2 || string(events[0]["id"]) != `"between-queued-and-ack"` || string(events[1]["id"]) != `"queued-input"` || string(events[1]["processed_at"]) == "null" {
						t.Fatal("reconnect skipped, reordered or duplicated actual committed ACK/history", text)
					}
					cancel()
					<-done
					return
				}
				select {
				case <-writer.updates:
				case <-ctx.Done():
					t.Fatal("processed ACK suppressed through real replay/rememberReplay path", text)
				}
			}
		})
	}
}
