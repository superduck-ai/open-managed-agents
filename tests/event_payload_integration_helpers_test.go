package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

// Each fixture owns a schema so GC and fault injection cannot touch other tests' jobs.
func newPayloadIntegrationApp(t *testing.T, objects storage.ObjectStore) *testApp {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "payload_test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := admin.Exec(context.Background(), "create schema "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec(context.Background(), "drop schema "+schema+" cascade"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	databaseURL, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := databaseURL.Query()
	query.Set("search_path", schema)
	databaseURL.RawQuery = query.Encode()
	cfg.Database.URL = databaseURL.String()
	app := newTestAppWithStore(t, &cfg, objects)
	t.Cleanup(app.close)
	return app
}

func newPayloadIntegrationSession(t *testing.T, app *testApp) (db.CodeSession, string) {
	t.Helper()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"payload-integration"}`)
	env := createEnvironment(t, app, `{"name":"payload-integration"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, codeID)
	record, found, err := app.db.GetCodeSession(context.Background(), codeID)
	if err != nil || !found {
		t.Fatalf("load session: %v %t", err, found)
	}
	return record, epoch
}

func sizedPrivatePayload(id string, size int) string {
	prefix := `{"type":"assistant","uuid":` + quoteJSON(id) + `,"message":{"role":"assistant","content":"`
	suffix := `"}}`
	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}

func internalPayloadRequest(epoch string, payloads ...string) string {
	events := make([]string, len(payloads))
	for i, payload := range payloads {
		events[i] = `{"payload":` + payload + `}`
	}
	return `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[` + strings.Join(events, ",") + `]}`
}

func assertPublicPayloadText(t *testing.T, events []json.RawMessage, original string) {
	t.Helper()
	var input struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(original), &input); err != nil {
		t.Fatal(err)
	}
	if input.Message.Content == "" {
		t.Fatal("fixture must contain message text")
	}
	for _, event := range events {
		if strings.Contains(string(event), quoteJSON(input.Message.Content)) {
			return
		}
	}
	t.Fatal("public history omitted or truncated the original message text")
}

// Hooks run outside the lock so tests can coordinate two in-flight uploads.
type payloadFaultStore struct {
	*fakeStore
	mu              sync.Mutex
	afterUpload     func(string) error
	wrongUploadSize bool
	deleteOptions   []storage.DeleteOptions
}

func (s *payloadFaultStore) Upload(ctx context.Context, key string, body io.Reader, opts storage.UploadOptions) (storage.UploadResult, error) {
	s.mu.Lock()
	result, err := s.fakeStore.Upload(ctx, key, body, opts)
	s.mu.Unlock()
	if err == nil && s.afterUpload != nil {
		err = s.afterUpload(key)
	}
	if s.wrongUploadSize {
		result.Size++
	}
	return result, err
}
func (s *payloadFaultStore) Open(ctx context.Context, key string, byteRange *storage.ByteRange) (storage.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeStore.Open(ctx, key, byteRange)
}
func (s *payloadFaultStore) Delete(ctx context.Context, key string, opts storage.DeleteOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteOptions = append(s.deleteOptions, opts)
	return s.fakeStore.Delete(ctx, key, opts)
}

func assertPayloadSQLCount(t *testing.T, app *testApp, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := app.pool.QueryRow(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count = %d, want %d (%s)", got, want, fmt.Sprint(args...))
	}
}
