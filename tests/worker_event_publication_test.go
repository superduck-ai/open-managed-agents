package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type failingPubAckBroker struct {
	*workerevents.MemoryBroker
	mu        sync.Mutex
	failNext  bool
	published chan struct{}
	release   <-chan struct{}
}

func (b *failingPubAckBroker) Publish(ctx context.Context, messageID string, envelope workerevents.EnvelopeV1) error {
	if err := b.MemoryBroker.Publish(ctx, messageID, envelope); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failNext {
		b.failNext = false
		return errors.New("injected ambiguous PubAck failure")
	}
	if b.published != nil {
		close(b.published)
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestDirectWorkerEventPublicationRetriesWithStableMessageID(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("direct-worker-event-publication-bucket"))
	defer app.close()

	suffix := strings.ReplaceAll(time.Now().UTC().Format("150405.000000000"), ".", "")
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"direct-publication-`+suffix+`"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, `{"name":"direct-publication-`+suffix+`"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	codeSession, err := getCodeSession(app, context.Background(), codeSessionID)
	if err != nil {
		t.Fatal(err)
	}

	broker := &failingPubAckBroker{MemoryBroker: workerevents.NewMemory(), failNext: true}
	service := newCodeSessionService(app, broker, nil)
	payload := json.RawMessage(`{"type":"user","uuid":"direct-retry-` + suffix + `","content":"retry me"}`)
	if err := service.QueueRawPublicSessionEvents(context.Background(), codeSession, []json.RawMessage{payload}); !errors.Is(err, codesessions.ErrWorkerEventUnavailable) {
		t.Fatalf("first publication error = %v, want %v", err, codesessions.ErrWorkerEventUnavailable)
	}
	if err := service.QueueRawPublicSessionEvents(context.Background(), codeSession, []json.RawMessage{payload}); err != nil {
		t.Fatalf("retry publication: %v", err)
	}
	pending := broker.Pending(codeSessionID)
	if len(pending) != 1 {
		t.Fatalf("pending events = %d, want one deduplicated event", len(pending))
	}
	if pending[0].SequenceNum <= 0 || pending[0].PayloadEventID != "direct-retry-"+suffix {
		t.Fatalf("pending event = %#v", pending[0])
	}
}

func TestActivationWaitsForEveryJetStreamPubAck(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("activation-puback-bucket"))
	defer app.close()

	suffix := strings.ReplaceAll(time.Now().UTC().Format("150405.000000000"), ".", "")
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"activation-puback-`+suffix+`"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, `{"name":"activation-puback-`+suffix+`"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	if _, err := app.pool.Exec(context.Background(), `update code_sessions set status = 'initializing' where external_id = $1`, codeSessionID); err != nil {
		t.Fatal(err)
	}
	codeSession, err := getCodeSession(app, context.Background(), codeSessionID)
	if err != nil {
		t.Fatal(err)
	}

	broker := &failingPubAckBroker{MemoryBroker: workerevents.NewMemory(), failNext: true}
	service := newCodeSessionService(app, broker, nil)
	if err := service.ActivateManagedAgentCodeSession(context.Background(), codeSession); !errors.Is(err, codesessions.ErrWorkerEventUnavailable) {
		t.Fatalf("activation error = %v, want %v", err, codesessions.ErrWorkerEventUnavailable)
	}
	afterFailure, err := getCodeSession(app, context.Background(), codeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.Status != "initializing" {
		t.Fatalf("status after failed PubAck = %q, want initializing", afterFailure.Status)
	}
	if err := service.ActivateManagedAgentCodeSession(context.Background(), afterFailure); err != nil {
		t.Fatalf("retry activation: %v", err)
	}
	afterRetry, err := getCodeSession(app, context.Background(), codeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRetry.Status != "active" {
		t.Fatalf("status after acknowledged retry = %q, want active", afterRetry.Status)
	}
	if pending := broker.Pending(codeSessionID); len(pending) != 1 {
		t.Fatalf("startup events after retry = %d, want one deduplicated initialize", len(pending))
	}
}

func TestExpiredJetStreamEventTerminatesCodeSessionAndPurgesSubject(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("worker-event-expiry-bucket"))
	defer app.close()

	suffix := strings.ReplaceAll(time.Now().UTC().Format("150405.000000000"), ".", "")
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"worker-event-expiry-`+suffix+`"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, `{"name":"worker-event-expiry-`+suffix+`"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	expired := workerevents.EventEnvelope(
		codeSessionID,
		"csev_expired_"+suffix,
		"payload_expired_"+suffix,
		"user",
		"",
		json.RawMessage(`{"type":"user"}`),
		time.Now().UTC().Add(-time.Second),
	)
	if err := app.workerEvents.Publish(context.Background(), expired.EventID, expired); err != nil {
		t.Fatal(err)
	}
	if err := codesessions.NewWorkerEventExpiryWorker(newCodeSessionService(app, nil, nil), nil).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	terminated, err := getCodeSession(app, context.Background(), codeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if terminated.Status != "terminated" {
		t.Fatalf("Code Session status = %q, want terminated", terminated.Status)
	}
	if pending := app.workerEvents.Pending(codeSessionID); len(pending) != 0 {
		t.Fatalf("terminated Code Session still has %d JetStream messages", len(pending))
	}
}

func TestToolResponsePublicationClearsPendingOnlyAfterSuccess(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("tool-response-publication"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"control_request","uuid":"approval","request_id":"approval-request","request":{"subtype":"can_use_tool","tool_name":"MysteryTool","tool_use_id":"tool-approval","input":{}}}`))
	tool := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.tool_use", defaultTestKey)
	toolID := sessionEventStringField(t, tool.Data[0], "id")
	session, found, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("session: %v", err)
	}
	// Include a legacy duplicate: clearing the keyed entry must not revive it.
	worker, _, err = app.db.GetCodeSession(t.Context(), worker.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(worker.WorkerExternalMetadata, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata["managed_agent_tool_permission_request"] = metadata["managed_agent_tool_permission_request:"+toolID]
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.UpdateCodeSessionWorkerState(t.Context(), worker.ExternalID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: worker.CurrentWorkerEpoch, ExternalMetadataSet: true, ExternalMetadata: raw}); err != nil {
		t.Fatal(err)
	}
	event := db.SessionEvent{ExternalID: "sevt_confirmation", EventType: "user.tool_confirmation", Payload: json.RawMessage(`{"type":"user.tool_confirmation","tool_use_id":` + quoteJSON(toolID) + `,"result":"allow"}`)}
	broker := &failingPubAckBroker{MemoryBroker: workerevents.NewMemory(), failNext: true}
	service := newCodeSessionService(app, broker, nil)
	if err := service.QueuePublicSessionEvents(t.Context(), session, []db.SessionEvent{event}); err == nil {
		t.Fatal("expected failed publish")
	}
	current, _, err := app.db.GetCodeSession(t.Context(), worker.ExternalID)
	if err != nil || !strings.Contains(string(current.WorkerExternalMetadata), toolID) {
		t.Fatal("failed publish lost request")
	}
	// Queue a running report behind the publication lock. PostgreSQL hands the
	// waiting worker the lock as soon as publication commits; it must see the
	// pending request already cleared, never the old metadata.
	broker.published = make(chan struct{})
	release := make(chan struct{})
	broker.release = release
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	published := make(chan error, 1)
	go func() { published <- service.QueuePublicSessionEvents(t.Context(), session, []db.SessionEvent{event}) }()
	<-broker.published
	reported := make(chan *http.Response, 1)
	go func() {
		reported <- doCodeSessionWorkerRequestWithMethod(t, app, http.MethodPut, worker.ExternalID, "", `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := app.pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_locks waiting JOIN pg_locks held USING (pid) WHERE NOT waiting.granted AND held.relation='code_sessions'::regclass)`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("worker did not wait for publication lock")
		case <-ticker.C:
		}
	}
	close(release)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	response := <-reported
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("running report: %d %s", response.StatusCode, readAll(t, response.Body))
	}

	current, _, err = app.db.GetCodeSession(t.Context(), worker.ExternalID)
	if err != nil || strings.Contains(string(current.WorkerExternalMetadata), toolID) {
		t.Fatal("successful publication retained pending request")
	}
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("resume lost: %s", status)
	}
	if len(broker.Pending(worker.ExternalID)) != 1 {
		t.Fatal("retry duplicated response")
	}
}

func TestToolResponseRejectsWorkerRotationDuringUpload(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("tool-response-epoch")}
	app := newPayloadIntegrationApp(t, objects)
	worker, epoch := newPayloadIntegrationSession(t, app)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"control_request","uuid":"approval","request_id":"approval-request","request":{"subtype":"can_use_tool","tool_name":"MysteryTool","tool_use_id":"tool-approval","input":{"text":`+quoteJSON(strings.Repeat("x", 40000))+`}}}`))
	tool := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.tool_use", defaultTestKey)
	toolID := sessionEventStringField(t, tool.Data[0], "id")
	session, found, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("session: %v", err)
	}
	objects.afterUpload = func(string) error {
		objects.afterUpload = nil
		registerCodeSessionWorker(t, app, worker.ExternalID)
		return nil
	}
	event := db.SessionEvent{ExternalID: "sevt_confirmation", EventType: "user.tool_confirmation", Payload: json.RawMessage(`{"type":"user.tool_confirmation","tool_use_id":` + quoteJSON(toolID) + `,"result":"allow"}`)}
	broker := workerevents.NewMemory()
	service := newCodeSessionService(app, broker, nil)
	if err := service.QueuePublicSessionEvents(t.Context(), session, []db.SessionEvent{event}); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("rotation error: %v", err)
	}
	current, _, err := app.db.GetCodeSession(t.Context(), worker.ExternalID)
	if err != nil || !strings.Contains(string(current.WorkerExternalMetadata), toolID) {
		t.Fatal("old epoch cleared pending request")
	}
	if len(broker.Pending(worker.ExternalID)) != 0 {
		t.Fatal("old epoch published a control response")
	}
}
