package liveworker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

// These tests use a running API and its real dependencies. They never migrate,
// seed, restart servers, flush Redis, or purge an entire shared stream.
type liveEnv struct {
	url         string
	database    *db.DB
	service     *codesessions.Service
	credentials *codesessions.SessionCredentials
	broker      *workerevents.JetStreamBroker
	stream      jetstream.Stream
	redis       *redis.Client
	acks        *workerevents.RedisAckStore
	objects     storage.ObjectStore
	key         db.APIKey
	apiKey      string
	agent       db.Agent
	environment db.Environment
}

func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newLiveEnv(t *testing.T) *liveEnv {
	t.Helper()
	baseURL := os.Getenv("LIVE_WORKER_API_URL")
	if baseURL == "" {
		t.Skip("opt in with LIVE_WORKER_API_URL and CONFIG_FILE; uses real shared services")
	}
	if os.Getenv("CONFIG_FILE") == "" {
		t.Fatal("CONFIG_FILE must explicitly identify the running API configuration")
	}
	ctx := t.Context()
	cfg, err := config.Load()
	requireOK(t, err)
	if cfg.CodeSession.JWTSigningPrivateKeyFile == "" {
		t.Fatal("live API requires a shared configured JWT signing key")
	}
	e := &liveEnv{url: strings.TrimRight(baseURL, "/"), apiKey: os.Getenv("TEST_API_KEY")}
	if e.apiKey == "" {
		e.apiKey = "sk-ant-local-default"
	}
	e.request(t, "GET", "/healthz", "", nil, 200)
	e.database, err = db.Open(ctx, cfg, nil)
	requireOK(t, err)
	t.Cleanup(e.database.Close)
	e.key, err = e.database.GetAPIKey(ctx, auth.HashAPIKey(e.apiKey))
	requireOK(t, err)
	connection, err := nats.Connect(cfg.NATS.URL, nats.Timeout(5*time.Second))
	requireOK(t, err)
	t.Cleanup(connection.Close)
	js, err := jetstream.New(connection)
	requireOK(t, err)
	e.stream, err = js.Stream(ctx, workerevents.StreamName)
	requireOK(t, err)
	info, err := e.stream.Info(ctx)
	requireOK(t, err)
	if info.Config.Retention != jetstream.WorkQueuePolicy || info.Config.Replicas != 3 || info.Config.MaxAge != 0 || info.Config.Storage != jetstream.FileStorage || info.Config.MaxBytes != 10<<30 || info.Config.MaxMsgSize != 1<<20 || info.Config.Discard != jetstream.DiscardNew || info.Config.Duplicates != 24*time.Hour {
		t.Fatal("live stream does not match required configuration; refusing to change it")
	}
	e.broker, err = workerevents.NewJetStream(ctx, connection)
	requireOK(t, err)
	redisOptions, err := redis.ParseURL(cfg.Redis.URL)
	requireOK(t, err)
	e.redis = redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = e.redis.Close() })
	requireOK(t, e.redis.Ping(ctx).Err())
	e.acks = workerevents.NewRedisAckStore(e.redis)
	objectClient, err := storage.New(cfg.Storage)
	requireOK(t, err)
	e.objects, err = objectClient.ForBucket(cfg.Storage.S3.Bucket)
	requireOK(t, err)
	e.credentials, err = codesessions.NewSessionCredentials(cfg)
	requireOK(t, err)
	e.service = codesessions.NewServiceWithCredentials(e.database, e.credentials, nil).WithWorkerEventBroker(e.broker).WithWorkerEventState(e.acks, e.objects)
	var created struct {
		ID string `json:"id"`
	}
	name := "live-worker-" + uuid.NewString()
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{"name": name, "model": map[string]string{"id": "claude-opus-4-6"}}, 200), &created))
	e.agent, err = e.database.GetAgent(ctx, e.key.WorkspaceUUID.String(), created.ID)
	requireOK(t, err)
	t.Cleanup(func() {
		// t.Context() 在 Cleanup 前已取消；归档仍需通过 API 执行完整资源清理。
		e.requestContext(context.WithoutCancel(t.Context()), t, "POST", "/v1/agents/"+e.agent.ExternalID+"/archive", e.apiKey, nil, http.StatusOK)
	})
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/environments", e.apiKey, map[string]string{"name": name}, 200), &created))
	e.environment, err = e.database.GetEnvironment(ctx, e.key.WorkspaceUUID.String(), created.ID)
	requireOK(t, err)
	t.Cleanup(func() {
		if err := e.database.DeleteEnvironment(context.Background(), e.key.WorkspaceUUID.String(), e.environment.ExternalID); err != nil {
			t.Errorf("delete test environment: %v", err)
		}
	})
	return e
}

func (e *liveEnv) request(t *testing.T, method, path, token string, body any, want int) []byte {
	t.Helper()
	return e.requestContext(t.Context(), t, method, path, token, body, want)
}

func (e *liveEnv) requestContext(ctx context.Context, t *testing.T, method, path, token string, body any, want int) []byte {
	t.Helper()
	encoded, err := json.Marshal(body)
	requireOK(t, err)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	req, err := http.NewRequestWithContext(ctx, method, e.url+path+separator+"beta=true", bytes.NewReader(encoded))
	requireOK(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(req)
	requireOK(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 20<<20))
	requireOK(t, err)
	if response.StatusCode != want {
		t.Fatalf("%s %s: status=%d want=%d response=%s", method, path, response.StatusCode, want, data[:min(len(data), 512)])
	}
	return data
}

type liveSession struct {
	env     *liveEnv
	session db.Session
	code    db.CodeSession
	token   string
}

func (e *liveEnv) newSession(t *testing.T) *liveSession {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	org, workspace := e.key.OrganizationUUID.String(), e.key.WorkspaceUUID.String()
	sessionID := "sesn_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	snapshot := json.RawMessage(`{"model":{"id":"claude-opus-4-6"}}`)
	// A stopped work record prevents the shared runner from starting an actual
	// sandbox that would compete with this protocol client for the same queue.
	session, _, _, _, err := e.database.CreateSession(ctx, db.CreateSessionInput{
		Session: db.Session{UUID: uuid.NewString(), ExternalID: sessionID, OrganizationUUID: org, WorkspaceUUID: workspace, CreatedByAPIKeyUUID: e.key.UUID.String(), EnvironmentUUID: e.environment.UUID, EnvironmentExternalID: e.environment.ExternalID, AgentUUID: e.agent.UUID, AgentExternalID: e.agent.ExternalID, AgentVersion: e.agent.CurrentVersion, AgentSnapshot: snapshot, Metadata: json.RawMessage(`{}`), Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), OutcomeEvaluations: json.RawMessage(`{}`), Status: "idle", CreatedAt: now},
		Thread:  db.SessionThread{UUID: uuid.NewString(), ExternalID: "thread_" + uuid.NewString(), OrganizationUUID: org, WorkspaceUUID: workspace, AgentSnapshot: snapshot, Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), Status: "idle", CreatedAt: now},
		Work:    db.EnvironmentWork{UUID: uuid.NewString(), ExternalID: "work_" + uuid.NewString(), OrganizationUUID: org, WorkspaceUUID: workspace, EnvironmentUUID: e.environment.UUID, EnvironmentExternalID: e.environment.ExternalID, SessionExternalID: sessionID, State: "stopped", Metadata: json.RawMessage(`{}`), CreatedAt: now},
	})
	requireOK(t, err)
	t.Cleanup(func() {
		_, err := e.database.DeleteSession(context.Background(), workspace, sessionID)
		if err != nil {
			t.Errorf("delete test session: %v", err)
		}
	})
	code, err := e.database.CreateCodeSession(ctx, db.CreateCodeSessionInput{ExternalID: "cse_" + strings.ReplaceAll(uuid.NewString(), "-", ""), OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: session.UUID, SessionExternalID: sessionID, EnvironmentUUID: e.environment.UUID, EnvironmentExternalID: e.environment.ExternalID, Status: "initializing", Model: "claude-opus-4-6", PermissionMode: "default", Metadata: json.RawMessage(`{"config":{}}`), OAuthAccessTokenHash: auth.HashAPIKey(uuid.NewString()), InitialWorkerEpoch: 1, CreatedAt: now})
	requireOK(t, err)
	f := &liveSession{env: e, session: session, code: code}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := e.service.TerminateManagedAgentCodeSession(ctx, session, code.ExternalID); err != nil {
			t.Errorf("terminate test code session: %v", err)
		}
		var keys []string
		iter := e.redis.Scan(ctx, 0, "oma:worker-event-ack:v2:"+code.ExternalID+":*", 100).Iterator()
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		if err := iter.Err(); err != nil {
			t.Errorf("list test ACK keys: %v", err)
		} else if len(keys) > 0 {
			if err := e.redis.Del(ctx, keys...).Err(); err != nil {
				t.Errorf("delete test ACK keys: %v", err)
			}
		}
	})
	requireOK(t, e.service.ActivateManagedAgentCodeSession(ctx, code))
	f.code, _, err = e.database.GetCodeSession(ctx, code.ExternalID)
	requireOK(t, err)
	if f.code.Status != "active" {
		t.Fatal("activation did not commit active state")
	}
	f.issueToken(t)
	var registration struct {
		Epoch string `json:"worker_epoch"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "POST", f.path("/worker/register"), f.token, map[string]string{"session_id": code.ExternalID}, 200), &registration))
	if registration.Epoch != "1" {
		t.Fatalf("unexpected epoch %q", registration.Epoch)
	}
	t.Logf("dedicated code_session=%s public_session=%s", code.ExternalID, sessionID)
	return f
}

func (f *liveSession) issueToken(t *testing.T) {
	t.Helper()
	c, err := f.env.database.GetCodeSessionCredentialContextForIssue(t.Context(), f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.ExternalID)
	requireOK(t, err)
	f.token, err = f.env.credentials.Issue(codesessions.SessionCredentialIdentity{SessionID: c.CodeSessionExternalID, PublicSessionID: c.PublicSessionExternalID, AgentID: c.AgentExternalID, AgentVersion: c.AgentVersion, OrganizationUUID: c.OrganizationUUID, WorkspaceUUID: c.WorkspaceUUID, AccountEmail: c.AccountEmail, WorkerEpoch: f.code.CurrentWorkerEpoch})
	requireOK(t, err)
}

func (f *liveSession) path(suffix string) string {
	return "/v1/code/sessions/" + f.code.ExternalID + suffix
}

type liveEvent struct {
	EventID   string          `json:"event_id"`
	Sequence  int64           `json:"sequence_num"`
	Payload   json.RawMessage `json:"payload"`
	SessionID string          `json:"session_id"`
	SSEID     string          `json:"-"`
}

type liveSSE struct {
	events chan liveEvent
	errors chan error
	cancel context.CancelFunc
}

func (f *liveSession) connect(t *testing.T, cursor string) *liveSSE {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET", f.env.url+f.path("/worker/events/stream")+"?worker_epoch="+strconv.FormatInt(f.code.CurrentWorkerEpoch, 10)+"&cursor="+cursor, nil)
	requireOK(t, err)
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Last-Event-ID", cursor)
	response, err := http.DefaultClient.Do(req)
	requireOK(t, err)
	if response.StatusCode != 200 {
		_ = response.Body.Close()
		t.Fatalf("SSE status=%d", response.StatusCode)
	}
	s := &liveSSE{events: make(chan liveEvent, 16), errors: make(chan error, 1), cancel: cancel}
	go func() {
		defer response.Body.Close()
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 20<<20)
		id := ""
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "id: ") {
				id = strings.TrimPrefix(line, "id: ")
			}
			if strings.HasPrefix(line, "data: ") {
				var event liveEvent
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
					s.errors <- err
					return
				}
				event.SSEID = id
				select {
				case s.events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
		s.errors <- scanner.Err()
	}()
	return s
}

func (s *liveSSE) next(t *testing.T, wait time.Duration) liveEvent {
	t.Helper()
	select {
	case event := <-s.events:
		if event.SSEID != strconv.FormatInt(event.Sequence, 10) {
			t.Fatal("SSE id != sequence")
		}
		return event
	case err := <-s.errors:
		t.Fatalf("SSE ended: %v", err)
	case <-time.After(wait):
		t.Fatal("SSE event timeout")
	}
	return liveEvent{}
}

func (s *liveSSE) blocked(t *testing.T) {
	t.Helper()
	select {
	case e := <-s.events:
		t.Fatalf("unexpected delivery before ACK: %s", e.EventID)
	case err := <-s.errors:
		t.Fatalf("SSE unexpectedly ended: %v", err)
	case <-time.After(400 * time.Millisecond):
	}
}

func (f *liveSession) ack(t *testing.T, eventID, status string, want int) {
	t.Helper()
	f.env.request(t, "POST", f.path("/worker/events/delivery"), f.token, map[string]any{"worker_epoch": strconv.FormatInt(f.code.CurrentWorkerEpoch, 10), "updates": []map[string]string{{"event_id": eventID, "status": status}}}, want)
}

func (f *liveSession) queue(t *testing.T, payload json.RawMessage) workerevents.EnvelopeV1 {
	t.Helper()
	requireOK(t, f.env.service.QueueRawPublicSessionEvents(t.Context(), f.code, []json.RawMessage{payload}))
	subject, err := workerevents.Subject(f.code.ExternalID)
	requireOK(t, err)
	message, err := f.env.stream.GetLastMsgForSubject(t.Context(), subject)
	requireOK(t, err)
	if len(message.Data) > workerevents.MaxMessageBytes {
		t.Fatal("stored envelope exceeds limit")
	}
	var envelope workerevents.EnvelopeV1
	requireOK(t, json.Unmarshal(message.Data, &envelope))
	if envelope.PayloadRef != nil {
		ref := *envelope.PayloadRef
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := f.env.database.ExpediteObjectCleanupJob(ctx, ref.CleanupJobID); err != nil {
				t.Errorf("expedite test object cleanup: %v", err)
			}
			if err := f.env.objects.Delete(ctx, ref.Key, storage.DeleteOptions{}); err != nil {
				t.Errorf("delete test object: %v", err)
			}
		})
	}
	return envelope
}

func payloadFor(id, text string) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{"type": "user", "uuid": id, "message": map[string]string{"role": "user", "content": text}})
	return payload
}

func (f *liveSession) ackInitialize(t *testing.T, s *liveSSE) {
	t.Helper()
	e := s.next(t, 10*time.Second)
	if !bytes.Contains(e.Payload, []byte("initialize")) {
		t.Fatalf("first event is not initialize (id=%s)", e.EventID)
	}
	f.ack(t, e.EventID, "processed", 200)
}

func (f *liveSession) consumer(t *testing.T) *jetstream.ConsumerInfo {
	t.Helper()
	c, err := f.env.stream.Consumer(t.Context(), "oma_worker_"+f.code.ExternalID)
	requireOK(t, err)
	i, err := c.Info(t.Context())
	requireOK(t, err)
	return i
}

func eventually(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("condition did not become true within %s", timeout)
}
