package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestPublicWorkerInputConversionFailureIsReturned(t *testing.T) {
	app, codeSession := workerEventRegressionFixture(t, newFakeStore("input-conversion-failure"))
	broker := workerevents.NewMemory()
	err := newCodeSessionService(app, broker, nil).QueuePublicSessionEvents(t.Context(), db.Session{
		WorkspaceUUID: codeSession.WorkspaceUUID, ExternalID: codeSession.SessionExternalID,
	}, []db.SessionEvent{{ExternalID: "invalid", EventType: "user.message", Payload: json.RawMessage(`{`)}})
	if err == nil || len(broker.Pending(codeSession.ExternalID)) != 0 {
		t.Fatalf("invalid public input must fail without publishing: %v", err)
	}
}

func TestWorkerEventExpiryRetainsMessageWhenTerminationFails(t *testing.T) {
	app, codeSession := workerEventRegressionFixture(t, newFakeStore("expiry-retry"))
	broker := workerevents.NewMemory()
	event := workerevents.EventEnvelope(codeSession.ExternalID, "expired", "", "user", "", []byte(`{}`), time.Now().Add(-time.Second))
	if err := broker.Publish(t.Context(), event.EventID, event); err != nil {
		t.Fatal(err)
	}
	lock, err := app.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(t.Context(), `SELECT uuid FROM code_sessions WHERE uuid = $1 FOR UPDATE`, codeSession.UUID); err != nil {
		t.Fatal(err)
	}
	worker := codesessions.NewWorkerEventExpiryWorker(newCodeSessionService(app, broker, nil), nil)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if err := worker.RunOnce(ctx); err == nil {
		t.Fatal("expected termination to time out on the held row lock")
	}
	if len(broker.Pending(codeSession.ExternalID)) != 1 {
		t.Fatal("termination failure removed retry evidence")
	}
	if err := lock.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatalf("retry expiry: %v", err)
	}
	after, err := getCodeSession(app, t.Context(), codeSession.ExternalID)
	if err != nil || after.Status != "terminated" || len(broker.Pending(codeSession.ExternalID)) != 0 {
		t.Fatalf("retry status=%s pending=%d error=%v", after.Status, len(broker.Pending(codeSession.ExternalID)), err)
	}
}

func TestWorkerEventOffloadWorksWithOneDatabaseConnection(t *testing.T) {
	app, codeSession := workerEventRegressionFixture(t, newFakeStore("offload-one-connection"))
	app.db.SQLDB().SetMaxOpenConns(1)
	broker := workerevents.NewMemory()
	payload := json.RawMessage(`{"type":"user","uuid":"large-direct","content":` + quoteJSON(strings.Repeat("x", 950<<10)) + `}`)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := newCodeSessionService(app, broker, nil).QueueRawPublicSessionEvents(ctx, codeSession, []json.RawMessage{payload}); err != nil {
		t.Fatalf("offload with one connection: %v", err)
	}
	pending := broker.Pending(codeSession.ExternalID)
	if len(pending) != 1 || pending[0].PayloadRef == nil {
		t.Fatal("expected one published offloaded event")
	}
}

func TestCancelledPublicationStillExpeditesUnpublishedObjectCleanup(t *testing.T) {
	store := &historyDuringUploadStore{ObjectStore: newFakeStore("cancelled-offload-cleanup")}
	app, codeSession := workerEventRegressionFixture(t, store)
	app.db.SQLDB().SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store.onUpload = cancel
	broker := workerevents.NewMemory()
	payload := json.RawMessage(`{"type":"user","content":` + quoteJSON(strings.Repeat("x", 950<<10)) + `}`)
	err := newCodeSessionService(app, broker, nil).QueueRawPublicSessionEvents(ctx, codeSession, []json.RawMessage{payload})
	if err == nil || len(broker.Pending(codeSession.ExternalID)) != 0 {
		t.Fatalf("cancelled publication must not publish: %v", err)
	}
	var due bool
	if err := app.pool.QueryRow(t.Context(), `SELECT run_after <= NOW() FROM jobs WHERE type = 'object_cleanup' AND payload->>'key' LIKE $1`, "%/"+codeSession.ExternalID+"/%").Scan(&due); err != nil || !due {
		t.Fatalf("unpublished object cleanup was not expedited after cancellation: due=%t error=%v", due, err)
	}
}

func TestWorkerEventExpiryRetriesPurgeAfterDatabaseTermination(t *testing.T) {
	app, codeSession := workerEventRegressionFixture(t, newFakeStore("expiry-purge-retry"))
	broker := &failPurgeOnceBroker{MemoryBroker: workerevents.NewMemory(), failNext: true}
	event := workerevents.EventEnvelope(codeSession.ExternalID, "expired-purge", "", "user", "", []byte(`{}`), time.Now().Add(-time.Second))
	if err := broker.Publish(t.Context(), event.EventID, event); err != nil {
		t.Fatal(err)
	}
	worker := codesessions.NewWorkerEventExpiryWorker(newCodeSessionService(app, broker, nil), nil)
	if err := worker.RunOnce(t.Context()); err == nil {
		t.Fatal("expected injected purge failure")
	}
	after, err := getCodeSession(app, t.Context(), codeSession.ExternalID)
	if err != nil || after.Status != "terminated" || len(broker.Pending(codeSession.ExternalID)) != 1 {
		t.Fatalf("PG must be terminated before purge succeeds: status=%s error=%v", after.Status, err)
	}
	if err := worker.RunOnce(t.Context()); err != nil || len(broker.Pending(codeSession.ExternalID)) != 0 {
		t.Fatalf("purge retry did not succeed: %v", err)
	}
}

func TestWorkerEventExpiryPurgesOrphanWithoutEmptyTenantSQL(t *testing.T) {
	app, codeSession := workerEventRegressionFixture(t, newFakeStore("expiry-orphan"))
	broker := workerevents.NewMemory()
	orphanID := "cse_orphan_" + codeSession.ExternalID
	event := workerevents.EventEnvelope(orphanID, "orphan-event", "", "user", "", []byte(`{}`), time.Now().Add(-time.Second))
	if err := broker.Publish(t.Context(), event.EventID, event); err != nil {
		t.Fatal(err)
	}
	worker := codesessions.NewWorkerEventExpiryWorker(newCodeSessionService(app, broker, nil), nil)
	if err := worker.RunOnce(t.Context()); err != nil || len(broker.Pending(orphanID)) != 0 {
		t.Fatalf("orphan cleanup failed: %v", err)
	}
	after, err := getCodeSession(app, t.Context(), codeSession.ExternalID)
	if err != nil || after.Status != "active" {
		t.Fatalf("unrelated session was affected: status=%s error=%v", after.Status, err)
	}
}

type failPurgeOnceBroker struct {
	*workerevents.MemoryBroker
	failNext bool
}

func (b *failPurgeOnceBroker) PurgeSession(ctx context.Context, sessionID string) error {
	if b.failNext {
		b.failNext = false
		return errors.New("injected purge failure")
	}
	return b.MemoryBroker.PurgeSession(ctx, sessionID)
}

func TestActivationRechecksHistoryAfterOffloadWithOneConnection(t *testing.T) {
	store := &historyDuringUploadStore{ObjectStore: newFakeStore("activation-offload-snapshot")}
	app, codeSession := workerEventRegressionFixture(t, store)
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET status = 'initializing' WHERE uuid = $1`, codeSession.UUID); err != nil {
		t.Fatal(err)
	}
	sendSessionEvents(t, app, codeSession.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":`+quoteJSON(strings.Repeat("x", 950<<10))+`}]}]}`, defaultTestKey)
	app.db.SQLDB().SetMaxOpenConns(1)
	store.onUpload = func() {
		sendSessionEvents(t, app, codeSession.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"arrived during upload"}]}]}`, defaultTestKey)
	}
	broker := workerevents.NewMemory()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := newCodeSessionService(app, broker, nil).ActivateManagedAgentCodeSession(ctx, codeSession); err != nil {
		t.Fatalf("activate with one connection and concurrent history: %v", err)
	}
	pending := broker.Pending(codeSession.ExternalID)
	if len(pending) != 3 || pending[1].PayloadRef == nil || !strings.Contains(string(pending[2].Payload), "arrived during upload") {
		t.Fatalf("activation missed history cutover: %d events", len(pending))
	}
	if store.uploads != 2 {
		t.Fatalf("uploads = %d, want snapshot preparation and re-preparation", store.uploads)
	}
}

type historyDuringUploadStore struct {
	storage.ObjectStore
	onUpload func()
	uploads  int
}

func (s *historyDuringUploadStore) Upload(ctx context.Context, key string, body io.Reader, options storage.UploadOptions) (storage.UploadResult, error) {
	s.uploads++
	if hook := s.onUpload; hook != nil {
		s.onUpload = nil
		hook()
	}
	return s.ObjectStore.Upload(ctx, key, body, options)
}

func workerEventRegressionFixture(t *testing.T, store storage.ObjectStore) (*testApp, db.CodeSession) {
	t.Helper()
	app := newTestAppWithStore(t, nil, store)
	t.Cleanup(app.close)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"worker-event-regression"}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	environment := createEnvironment(t, app, `{"name":"worker-event-regression"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	t.Cleanup(func() { deleteSession(t, app, session.ID) })
	id := launchLocalCodeSession(t, app, session.ID)
	codeSession, err := getCodeSession(app, t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return app, codeSession
}
