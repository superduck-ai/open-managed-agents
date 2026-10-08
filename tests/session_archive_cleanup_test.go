package tests

import (
	"context"
	"database/sql"
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
	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestSessionCleanupEnqueueFailureRollsBack(t *testing.T) {
	f := newSessionCleanupFixture(t)
	worker, epoch := newPayloadIntegrationSession(t, f.app)
	f.cleanup.Configure(nil)
	response := doSessionRequest(t, f.app, http.MethodPost, "/v1/sessions/"+worker.SessionExternalID+"/archive?beta=true", nil, defaultTestKey, true)
	assertError(t, response, http.StatusInternalServerError, "api_error")
	session, found, err := f.app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found || session.ArchivedAt != nil || session.Status != "idle" {
		t.Fatalf("failed enqueue committed archive: %+v %v", session, err)
	}
	oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); err != nil {
		t.Fatalf("failed enqueue revoked worker: %v", err)
	}
	if events := listSessionEvents(t, f.app, worker.SessionExternalID, "types[]=session.status_terminated", defaultTestKey); len(events.Data) != 0 {
		t.Fatalf("failed enqueue persisted termination: %s", events.Data)
	}
}

func TestSessionCleanupRetriesAfterRestart(t *testing.T) {
	f := newSessionCleanupFixture(t)
	worker, epoch := newPayloadIntegrationSession(t, f.app)
	historical, err := f.app.db.CreateCodeSession(t.Context(), db.CreateCodeSessionInput{
		ExternalID: "cse_history_" + worker.ExternalID, OrganizationUUID: worker.OrganizationUUID,
		WorkspaceUUID: worker.WorkspaceUUID, SessionUUID: worker.SessionUUID,
		SessionExternalID: worker.SessionExternalID, EnvironmentUUID: worker.EnvironmentUUID,
		EnvironmentExternalID: worker.EnvironmentExternalID, WorkDir: worker.WorkDir,
		PermissionMode: worker.PermissionMode, Model: worker.Model, Status: "terminated",
		Metadata: []byte(`{}`), InitialWorkerEpoch: 1, CreatedAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.seedConsumer(t, worker.ExternalID)
	f.seedConsumer(t, historical.ExternalID)
	f.seedConsumer(t, "cse_retained")
	targets := []string{worker.ExternalID, historical.ExternalID}
	slices.Sort(targets)
	f.broker.failTarget = targets[1]
	f.broker.fail.Store(true)
	f.start(t)
	archived := archiveSession(t, f.app, worker.SessionExternalID)
	if archived.Status != "terminated" || archived.ArchivedAt == nil {
		t.Fatalf("archive = %+v", archived)
	}
	oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("archived worker epoch remained valid: %v", err)
	}
	jobID := f.jobID(t, worker.SessionUUID)
	deploymentWait(t, "failed cleanup persisted for retry", 20*time.Second, func() bool {
		job, err := f.client.JobGet(t.Context(), jobID)
		return err == nil && job.State == "retryable" && job.Attempt > 0
	})
	info, err := f.stream.Info(t.Context())
	if err != nil || info.State.Consumers != 4 || info.State.Msgs == 0 {
		t.Fatalf("partial cleanup did not preserve remaining queues: %+v %v", info, err)
	}
	f.stop(t)
	f.broker.fail.Store(false)
	f.start(t)
	deploymentWait(t, "persisted cleanup completed after restart", 30*time.Second, func() bool {
		job, err := f.client.JobGet(t.Context(), jobID)
		return err == nil && job.State == "completed" && job.Attempt >= 2
	})
	info, err = f.stream.Info(t.Context())
	if err != nil || info.State.Consumers != 2 || info.State.Msgs != 2 {
		t.Fatalf("cleanup changed unrelated queues: %+v %v", info, err)
	}
	if history := listSessionEvents(t, f.app, worker.SessionExternalID, "types[]=session.status_terminated", defaultTestKey); len(history.Data) != 1 {
		t.Fatalf("archive lost history: %s", history.Data)
	}
	previousCalls := f.broker.calls.Load()
	archiveSession(t, f.app, worker.SessionExternalID)
	deploymentWait(t, "repeated archive safely cleaned", 10*time.Second, func() bool {
		job, err := f.client.JobGet(t.Context(), f.jobID(t, worker.SessionUUID))
		return err == nil && job.State == "completed" && f.broker.calls.Load() >= previousCalls+2
	})
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
		t.Fatalf("test must observe fence before queued cleanup runs: %+v %v", info, err)
	}
	reconnect, err := f.app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnect.Body.Close()
	if reconnect.StatusCode == http.StatusOK {
		t.Fatal("old worker reconnected after archive")
	}
	f.start(t)
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
	f.start(t)
	retireCleanupSession(t, f, worker, action)
	jobID := f.jobID(t, worker.SessionUUID)
	deploymentWait(t, "initial archive cleanup finished before subscribe", 5*time.Second, func() bool {
		job, err := f.client.JobGet(t.Context(), jobID)
		return err == nil && job.State == "completed"
	})
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
	deploymentWait(t, "late subscription reclaimed by independent job", 5*time.Second, func() bool {
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
	var jobs int
	if err := f.app.pool.QueryRow(t.Context(), `SELECT count(*) FROM public.river_job WHERE kind='session_archive_cleanup' AND args->>'session_uuid'=$1`, worker.SessionUUID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("live replacement queued archive cleanup: %d %v", jobs, err)
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
	cleanup *sessions.SessionCleanup
	broker  *archivePurgeBroker
	workers *river.Workers
	client  *river.Client[*sql.Tx]
	stream  jetstream.Stream
}

type archivePurgeBroker struct {
	workerevents.Broker
	fail             atomic.Bool
	calls            atomic.Int32
	failTarget       string
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
	if b.fail.Load() && (b.failTarget == "" || b.failTarget == id) {
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
	f := &sessionCleanupFixture{app: app, broker: &archivePurgeBroker{Broker: broker}, workers: river.NewWorkers(), stream: stream}
	f.cleanup = sessions.NewSessionCleanup(app.db, f.broker)
	f.cleanup.Register(f.workers)
	f.configure(t)
	t.Cleanup(func() { f.stop(t) })
	app.server.Close()
	app.server = httptest.NewServer(api.NewServer(api.ServerDeps{
		Config: app.cfg, DB: app.db, ObjectStore: app.store,
		CodeSessionCredentials: app.credentials, FilestoreCredentials: app.filestoreCredentials,
		VaultSecrets: app.vaultSecrets, WorkerEventBroker: f.broker, SessionCleanup: f.cleanup,
	}))
	app.baseURL = app.server.URL
	app.client = app.server.Client()
	return f
}

func (f *sessionCleanupFixture) configure(t *testing.T) {
	t.Helper()
	client, err := riverjobs.NewClient(f.app.db, nil, f.workers, map[string]river.QueueConfig{sessions.SessionCleanupQueue: {MaxWorkers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	f.client = client
	f.cleanup.Configure(client)
}

func (f *sessionCleanupFixture) start(t *testing.T) {
	t.Helper()
	if f.client == nil {
		f.configure(t)
	}
	if err := f.client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (f *sessionCleanupFixture) stop(t *testing.T) {
	t.Helper()
	if f.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.client.Stop(ctx); err != nil {
			t.Error(err)
		}
		f.client = nil
	}
}

func (f *sessionCleanupFixture) jobID(t *testing.T, sessionUUID string) int64 {
	t.Helper()
	var id int64
	if err := f.app.pool.QueryRow(t.Context(), `SELECT id FROM public.river_job WHERE kind='session_archive_cleanup' AND args->>'session_uuid'=$1 ORDER BY id DESC LIMIT 1`, sessionUUID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
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

func TestSessionRetirementEnqueueFailureRollsBack(t *testing.T) {
	for _, action := range []string{"delete", "terminated"} {
		t.Run(action, func(t *testing.T) {
			f := newSessionCleanupFixture(t)
			worker, epoch := newPayloadIntegrationSession(t, f.app)
			f.cleanup.Configure(nil)
			if action == "delete" {
				response := doSessionRequest(t, f.app, http.MethodDelete, "/v1/sessions/"+worker.SessionExternalID+"?beta=true", nil, defaultTestKey, true)
				assertError(t, response, http.StatusInternalServerError, "api_error")
			} else if _, err := f.app.db.AppendSessionEventsIfAbsent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, []db.SessionEvent{terminationEvent(worker)}); err == nil {
				t.Fatal("termination committed without cleanup enqueue")
			}
			session, found, err := f.app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
			if err != nil || !found || session.Status != "idle" {
				t.Fatalf("retirement was not rolled back: %+v %v", session, err)
			}
			oldEpoch, err := strconv.ParseInt(epoch, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.app.db.ValidateCodeSessionWorkerEpoch(t.Context(), worker.ExternalID, oldEpoch); err != nil {
				t.Fatalf("failed enqueue revoked worker: %v", err)
			}
			if history := listSessionEvents(t, f.app, worker.SessionExternalID, "types[]=session.status_terminated", defaultTestKey); len(history.Data) != 0 {
				t.Fatalf("failed enqueue retained termination: %s", history.Data)
			}
		})
	}
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
			f.start(t)
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
	if err := f.cleanup.ReclaimClosedSubscription(t.Context(), worker, "child-close"); err != nil {
		t.Fatal(err)
	}
	info, err := f.stream.Info(t.Context())
	if err != nil || info.State.Consumers != 2 || info.State.Msgs != 2 {
		t.Fatalf("child archive removed parent queues: %+v %v", info, err)
	}
	f.start(t)
	if _, _, err := f.app.db.ArchiveSessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, primary.ExternalID); err != nil {
		t.Fatal(err)
	}
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
		if _, err := f.app.db.AppendSessionEventsIfAbsent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, []db.SessionEvent{terminationEvent(worker)}); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown action %q", action)
	}
}
