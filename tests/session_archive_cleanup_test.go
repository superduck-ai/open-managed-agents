package tests

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestSessionCleanupFailureKeepsRetirement(t *testing.T) {
	for _, action := range []string{"archive", "delete", "terminated"} {
		t.Run(action, func(t *testing.T) {
			f := newSessionCleanupFixture(t)
			worker, epoch := newPayloadIntegrationSession(t, f.app)
			f.seedConsumer(t, worker.ExternalID)
			f.broker.fail.Store(true)
			retireCleanupSession(t, f, worker, action)
			oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); !errors.Is(err, db.ErrWorkerEpochMismatch) {
				t.Fatalf("cleanup failure kept worker valid: %v", err)
			}
			retired, err := f.app.db.IsSessionRetired(t.Context(), worker.OrganizationUUID, worker.WorkspaceUUID, worker.SessionUUID)
			if err != nil || !retired {
				t.Fatalf("cleanup failure undid retirement: %t %v", retired, err)
			}
			info, err := f.stream.Info(t.Context())
			if err != nil || info.State.Consumers != 2 || info.State.Msgs != 2 || f.broker.calls.Load() == 0 {
				t.Fatalf("failure did not retain resources for inactivity fallback: %+v %v", info, err)
			}
		})
	}
}

func TestSessionArchiveClosesExistingWorkerStreamBeforeCleanup(t *testing.T) {
	f := newSessionCleanupFixture(t)
	worker, epoch := newPayloadIntegrationSession(t, f.app)
	token := codeSessionIngressToken(t, f.app, worker.ExternalID)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path := f.app.baseURL + "/v1/code/sessions/" + worker.ExternalID + "/worker/events/stream?worker_epoch=" + epoch
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := f.app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("worker stream = %d", response.StatusCode)
	}
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, response.Body)
		done <- err
	}()
	f.broker.fail.Store(true)
	archiveSession(t, f.app, worker.SessionExternalID)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker stream did not close cleanly: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("worker stream remained open after archive")
	}
	info, err := f.stream.Info(t.Context())
	if err != nil || info.State.Consumers != 2 {
		t.Fatalf("failed cleanup must preserve queues while the epoch fence closes the stream: %+v %v", info, err)
	}
	reconnect, err := f.app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnect.Body.Close()
	if reconnect.StatusCode == http.StatusOK {
		t.Fatal("old worker reconnected after archive")
	}
	f.broker.fail.Store(false)
	archiveSession(t, f.app, worker.SessionExternalID)
	deploymentWait(t, "archived stream consumers reclaimed", 10*time.Second, func() bool {
		info, err := f.stream.Info(t.Context())
		return err == nil && info.State.Consumers == 0 && info.State.Msgs == 0
	})
}

func TestSessionArchiveReclaimsFailedLateWorkerSubscription(t *testing.T) {
	assertLateWorkerSubscriptionReclaimed(t, true, "archive")
}

func TestSessionArchiveReclaimsLateWorkerSubscription(t *testing.T) {
	assertLateWorkerSubscriptionReclaimed(t, false, "archive")
}

func assertLateWorkerSubscriptionReclaimed(t *testing.T, failSubscribe bool, action string) {
	t.Helper()
	f := newSessionCleanupFixture(t)
	worker, epoch := newPayloadIntegrationSession(t, f.app)
	f.broker.subscribeStarted = make(chan struct{}, 1)
	f.broker.subscribeRelease = make(chan struct{})
	f.broker.failSubscribe = failSubscribe
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req := archiveWorkerStreamRequest(t, f.app, ctx, worker.ExternalID, epoch)
	done := make(chan *http.Response, 1)
	requestErrors := make(chan error, 1)
	go func() {
		response, err := f.app.client.Do(req)
		if err != nil {
			requestErrors <- err
			return
		}
		done <- response
	}()
	select {
	case <-f.broker.subscribeStarted:
	case <-ctx.Done():
		t.Fatal("worker did not reach subscription creation")
	}
	retireCleanupSession(t, f, worker, action)
	info, err := f.stream.Info(t.Context())
	if err != nil || info.State.Consumers != 0 {
		t.Fatalf("initial cleanup did not finish: %+v %v", info, err)
	}
	previousCalls := f.broker.calls.Load()
	close(f.broker.subscribeRelease)
	select {
	case response := <-done:
		defer response.Body.Close()
		if response.StatusCode == http.StatusOK {
			t.Fatal("late archived worker entered SSE")
		}
	case err := <-requestErrors:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("late worker did not leave subscription setup")
	}
	deploymentWait(t, "late subscription reclaimed directly", 5*time.Second, func() bool {
		info, err := f.stream.Info(t.Context())
		return err == nil && info.State.Consumers == 0 && f.broker.calls.Load() > previousCalls
	})
}

func TestWorkerReplacementKeepsSharedConsumers(t *testing.T) {
	f := newSessionCleanupFixture(t)
	worker, epoch := newPayloadIntegrationSession(t, f.app)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	response, err := f.app.client.Do(archiveWorkerStreamRequest(t, f.app, ctx, worker.ExternalID, epoch))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("worker stream = %d", response.StatusCode)
	}
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, response.Body)
		done <- err
	}()
	nextEpoch, _, err := f.app.db.RegisterCodeSessionWorker(t.Context(), worker.ExternalID, db.CodeSessionWorkerBinding{TokenSessionID: worker.ExternalID}, time.Minute)
	if err != nil || strconv.FormatInt(nextEpoch, 10) == epoch {
		t.Fatalf("replacement did not advance epoch: %d %v", nextEpoch, err)
	}
	f.seedConsumer(t, worker.ExternalID)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("old worker stream remained open")
	}
	info, err := f.stream.Info(t.Context())
	if err != nil || info.State.Consumers != 2 || info.State.Msgs != 2 || f.broker.calls.Load() != 0 {
		t.Fatalf("old worker cleanup removed replacement queues: %+v %v", info, err)
	}
}

func archiveWorkerStreamRequest(t *testing.T, app *testApp, ctx context.Context, codeSessionID, epoch string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/code/sessions/"+codeSessionID+"/worker/events/stream?worker_epoch="+epoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionIngressToken(t, app, codeSessionID))
	return req
}

type sessionCleanupFixture struct {
	app     *testApp
	service *codesessions.Service
	broker  *archivePurgeBroker
	stream  jetstream.Stream
}

type archivePurgeBroker struct {
	workerevents.Broker
	fail             atomic.Bool
	calls            atomic.Int32
	subscribeStarted chan struct{}
	subscribeRelease chan struct{}
	failSubscribe    bool
}

func (b *archivePurgeBroker) Subscribe(ctx context.Context, id string) (workerevents.Subscription, error) {
	if b.subscribeStarted != nil {
		select {
		case b.subscribeStarted <- struct{}{}:
		default:
		}
		select {
		case <-b.subscribeRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	sub, err := b.Broker.Subscribe(ctx, id)
	if err != nil || !b.failSubscribe {
		return sub, err
	}
	if err := sub.Close(); err != nil {
		return nil, err
	}
	return nil, errors.New("injected failure after consumer creation")
}

func (b *archivePurgeBroker) PurgeSession(ctx context.Context, id string) error {
	b.calls.Add(1)
	if b.fail.Load() {
		return errors.New("injected consumer cleanup failure")
	}
	return b.Broker.PurgeSession(ctx, id)
}

func newSessionCleanupFixture(t *testing.T) *sessionCleanupFixture {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("archive-consumers"))
	srv, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS startup timeout")
	}
	connection, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	broker, err := workerevents.NewJetStream(t.Context(), connection, config.WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, Replicas: 1, MaxBytes: 1 << 24, MaxMsgSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(t.Context(), workerevents.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	f := &sessionCleanupFixture{app: app, broker: &archivePurgeBroker{Broker: broker}, stream: stream}
	f.service = codesessions.NewServiceWithCredentials(app.db, app.credentials, nil).WithWorkerEventBroker(f.broker)
	app.server.Close()
	app.server = httptest.NewServer(api.NewServer(api.ServerDeps{
		Config: app.cfg, DB: app.db, ObjectStore: app.store,
		CodeSessionCredentials: app.credentials, FilestoreCredentials: app.filestoreCredentials,
		VaultSecrets: app.vaultSecrets, WorkerEventBroker: f.broker,
	}))
	app.baseURL = app.server.URL
	app.client = app.server.Client()
	return f
}

func (f *sessionCleanupFixture) seedConsumer(t *testing.T, id string) {
	t.Helper()
	sub, err := f.broker.Subscribe(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := sub.Close(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"user.message", "control_response"} {
		event := workerevents.EventEnvelope(id, id+kind, "", kind, "", []byte(`{}`), time.Now().Add(time.Hour))
		if err := f.broker.Publish(t.Context(), event.EventID, event); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionTerminationWaitsForCompleteBatch(t *testing.T) {
	for _, history := range []bool{false, true} {
		t.Run(strconv.FormatBool(history), func(t *testing.T) {
			f := newSessionCleanupFixture(t)
			worker, epoch := newPayloadIntegrationSession(t, f.app)
			missingBlob := uuid.NewV4().String()
			final := cleanupBatchFinalEvent(worker)
			final.PayloadBlobUUID = &missingBlob
			if _, err := appendCleanupEventBatch(t.Context(), f.app.db, worker, []db.SessionEvent{terminationEvent(worker), final}, history); err == nil {
				t.Fatal("batch accepted a missing payload blob")
			}
			session, found, err := f.app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
			if err != nil || !found || session.Status != "idle" {
				t.Fatalf("failed batch retained termination: %+v %v", session, err)
			}
			oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); err != nil {
				t.Fatalf("failed batch revoked worker: %v", err)
			}
			if events := listSessionEvents(t, f.app, worker.SessionExternalID, "types[]=session.status_terminated", defaultTestKey); len(events.Data) != 0 {
				t.Fatalf("failed batch persisted termination: %s", events.Data)
			}
		})
	}
}

func TestSessionTerminationBatchAndReplay(t *testing.T) {
	for _, history := range []bool{false, true} {
		t.Run(strconv.FormatBool(history), func(t *testing.T) {
			f := newSessionCleanupFixture(t)
			worker, _ := newPayloadIntegrationSession(t, f.app)
			final := cleanupBatchFinalEvent(worker)
			events := []db.SessionEvent{terminationEvent(worker), final}
			created, err := appendCleanupEventBatch(t.Context(), f.app.db, worker, events, history)
			if err != nil || !slices.ContainsFunc(created.Events, func(event db.SessionEvent) bool { return event.ExternalID == final.ExternalID }) || len(created.RetiredCodeSessionIDs) != 1 {
				t.Fatalf("batch incomplete or retirement IDs wrong: %+v error=%v", created, err)
			}
			retired, found, err := f.app.db.GetCodeSession(t.Context(), worker.ExternalID)
			if err != nil || !found || retired.Status != "terminated" || retired.CurrentWorkerEpoch <= worker.CurrentWorkerEpoch {
				t.Fatalf("worker not retired: %+v %v", retired, err)
			}
			replayed, err := f.app.db.AppendSessionEventsIfAbsent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, events)
			if err != nil || len(replayed.Events) != 0 || len(replayed.RetiredCodeSessionIDs) != 0 {
				t.Fatalf("replay repeated retirement: %+v error=%v", replayed, err)
			}
			duplicate := terminationEvent(worker)
			duplicate.UUID = uuid.NewV4().String()
			duplicate.ExternalID += "_duplicate"
			if changes, err := appendCleanupEventBatch(t.Context(), f.app.db, worker, []db.SessionEvent{duplicate}, history); err != nil || len(changes.RetiredCodeSessionIDs) != 0 {
				t.Fatalf("unchanged terminal status repeated retirement: %+v error=%v", changes, err)
			}
			after, found, err := f.app.db.GetCodeSession(t.Context(), worker.ExternalID)
			if err != nil || !found || after.CurrentWorkerEpoch != retired.CurrentWorkerEpoch {
				t.Fatalf("replay advanced retired epoch: %+v %v", after, err)
			}
		})
	}
}

func TestSessionTerminationBatchKeepsActiveWorkers(t *testing.T) {
	for _, history := range []bool{false, true} {
		t.Run(strconv.FormatBool(history), func(t *testing.T) {
			f := newSessionCleanupFixture(t)
			worker, epoch := newPayloadIntegrationSession(t, f.app)
			running := terminationEvent(worker)
			running.UUID = uuid.NewV4().String()
			running.ExternalID += "_running"
			running.EventType = "session.status_running"
			if changes, err := appendCleanupEventBatch(t.Context(), f.app.db, worker, []db.SessionEvent{terminationEvent(worker), running}, history); err != nil || len(changes.RetiredCodeSessionIDs) != 0 {
				t.Fatalf("active session retirement: %+v error=%v", changes, err)
			}
			session, found, err := f.app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
			if err != nil || !found || session.Status != "running" {
				t.Fatalf("final batch status: %+v %v", session, err)
			}
			oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); err != nil {
				t.Fatalf("active worker revoked: %v", err)
			}
		})
	}
}

func cleanupBatchFinalEvent(worker db.CodeSession) db.SessionEvent {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return db.SessionEvent{
		UUID: uuid.NewV4().String(), ExternalID: "sevt_final_" + worker.ExternalID, EventType: "agent.message",
		Payload: []byte(`{"content":[{"type":"text","text":"final"}]}`), CreatedAt: now, ProcessedAt: now,
	}
}

func appendCleanupEventBatch(ctx context.Context, database *db.DB, worker db.CodeSession, events []db.SessionEvent, history bool) (db.SessionEventChanges, error) {
	if history {
		return database.AppendSessionEventsIfAbsent(ctx, worker.WorkspaceUUID, worker.SessionExternalID, events)
	}
	return database.AppendSessionEvents(ctx, worker.WorkspaceUUID, worker.SessionExternalID, events, nil)
}

func TestSessionRetirementReclaimsAllWorkers(t *testing.T) {
	for _, action := range []string{"delete", "terminated"} {
		t.Run(action, func(t *testing.T) {
			f := newSessionCleanupFixture(t)
			worker, epoch := newPayloadIntegrationSession(t, f.app)
			historical, err := f.app.db.CreateCodeSession(t.Context(), db.CreateCodeSessionInput{
				ExternalID: "cse_history_" + worker.ExternalID, OrganizationUUID: worker.OrganizationUUID, WorkspaceUUID: worker.WorkspaceUUID,
				SessionUUID: worker.SessionUUID, SessionExternalID: worker.SessionExternalID, EnvironmentUUID: worker.EnvironmentUUID,
				EnvironmentExternalID: worker.EnvironmentExternalID, WorkDir: worker.WorkDir, PermissionMode: worker.PermissionMode,
				Model: worker.Model, Status: "terminated", Metadata: []byte(`{}`), InitialWorkerEpoch: 1, CreatedAt: time.Now().Add(-time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{worker.ExternalID, historical.ExternalID, "cse_retained"} {
				f.seedConsumer(t, id)
			}
			retireCleanupSession(t, f, worker, action)
			oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); !errors.Is(err, db.ErrWorkerEpochMismatch) {
				t.Fatalf("retired epoch valid: %v", err)
			}
			if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), historical.ExternalID, 1); !errors.Is(err, db.ErrWorkerEpochMismatch) {
				t.Fatalf("historical epoch valid: %v", err)
			}
			deploymentWait(t, "retirement cleanup completed", 10*time.Second, func() bool {
				info, err := f.stream.Info(t.Context())
				return err == nil && info.State.Consumers == 2 && info.State.Msgs == 2
			})
			retired, err := f.app.db.IsSessionRetired(t.Context(), worker.OrganizationUUID, worker.WorkspaceUUID, worker.SessionUUID)
			if err != nil || !retired {
				t.Fatalf("retirement marker unavailable: %t %v", retired, err)
			}
			for _, scope := range [][3]string{{uuid.NewV4().String(), worker.WorkspaceUUID, worker.SessionUUID}, {worker.OrganizationUUID, uuid.NewV4().String(), worker.SessionUUID}, {worker.OrganizationUUID, worker.WorkspaceUUID, uuid.NewV4().String()}} {
				retired, err := f.app.db.IsSessionRetired(t.Context(), scope[0], scope[1], scope[2])
				if err != nil || retired {
					t.Fatalf("unknown or foreign scope marked retired: %t %v", retired, err)
				}
			}
		})
	}
}

func TestSessionRetirementReclaimsLateSubscriptions(t *testing.T) {
	for _, action := range []string{"delete", "terminated"} {
		t.Run(action, func(t *testing.T) { assertLateWorkerSubscriptionReclaimed(t, false, action) })
	}
}

func TestChildThreadArchiveKeepsSessionConsumers(t *testing.T) {
	f := newSessionCleanupFixture(t)
	worker, epoch := newPayloadIntegrationSession(t, f.app)
	primary, found, err := f.app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("primary: %v", err)
	}
	child := primary
	child.UUID = uuid.NewV4().String()
	child.ExternalID = "sthread_child"
	child.ParentThreadUUID = &primary.UUID
	child.ParentThreadExternalID = &primary.ExternalID
	child.Status = "idle"
	child, err = f.app.db.CreateSessionThreadIfAbsent(t.Context(), child)
	if err != nil {
		t.Fatal(err)
	}
	f.seedConsumer(t, worker.ExternalID)
	if _, _, err := f.app.db.ArchiveSessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, child.ExternalID); err != nil {
		t.Fatal(err)
	}
	oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); err != nil {
		t.Fatalf("child archive revoked session worker: %v", err)
	}
	retired, err := f.app.db.IsSessionRetired(t.Context(), worker.OrganizationUUID, worker.WorkspaceUUID, worker.SessionUUID)
	if err != nil || retired {
		t.Fatalf("child archive retired parent: %v", err)
	}
	info, err := f.stream.Info(t.Context())
	if err != nil || info.State.Consumers != 2 || info.State.Msgs != 2 {
		t.Fatalf("child archive removed parent queues: %+v %v", info, err)
	}
	_, removal, err := f.app.db.ArchiveSessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, primary.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	f.service.PurgeWorkerEvents(t.Context(), removal.CodeSessionIDs)
	if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("whole-session thread termination retained epoch: %v", err)
	}
	deploymentWait(t, "last thread termination reclaimed parent queues", 10*time.Second, func() bool {
		info, err := f.stream.Info(t.Context())
		return err == nil && info.State.Consumers == 0 && info.State.Msgs == 0
	})
}

func terminationEvent(worker db.CodeSession) db.SessionEvent {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "sevt_terminal_" + worker.ExternalID, EventType: "session.status_terminated", CreatedAt: now, ProcessedAt: now}
}

func retireCleanupSession(t *testing.T, f *sessionCleanupFixture, worker db.CodeSession, action string) {
	t.Helper()
	switch action {
	case "archive":
		archiveSession(t, f.app, worker.SessionExternalID)
	case "delete":
		response := doSessionRequest(t, f.app, http.MethodDelete, "/v1/sessions/"+worker.SessionExternalID+"?beta=true", nil, defaultTestKey, true)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("delete = %d", response.StatusCode)
		}
	case "terminated":
		changes, err := f.app.db.AppendSessionEventsIfAbsent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, []db.SessionEvent{terminationEvent(worker)})
		if err != nil {
			t.Fatal(err)
		}
		f.service.PurgeWorkerEvents(t.Context(), changes.RetiredCodeSessionIDs)
	default:
		t.Fatalf("unknown action %q", action)
	}
}
