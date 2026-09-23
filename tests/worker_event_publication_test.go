package tests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type failingPubAckBroker struct {
	*workerevents.MemoryBroker
	mu       sync.Mutex
	failNext bool
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
