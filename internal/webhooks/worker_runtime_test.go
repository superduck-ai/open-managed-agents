package webhooks

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type runtimeStore struct {
	target    db.WebhookDeliveryTarget
	entered   chan time.Duration
	block     bool
	successes atomic.Int32
	failures  atomic.Int32
}

func (s *runtimeStore) FindWebhookDeliveryTarget(ctx context.Context, _, _ string) (db.WebhookDeliveryTarget, bool, error) {
	if s.entered != nil {
		deadline, _ := ctx.Deadline()
		s.entered <- time.Until(deadline)
	}
	if s.block {
		<-ctx.Done()
		return db.WebhookDeliveryTarget{}, false, ctx.Err()
	}
	return s.target, true, nil
}
func (s *runtimeStore) RecordWebhookDeliverySuccess(context.Context, string, string) error {
	s.successes.Add(1)
	return nil
}
func (s *runtimeStore) RecordWebhookDeliveryFailure(context.Context, string, string, string, bool, time.Duration) (bool, error) {
	s.failures.Add(1)
	return false, nil
}

type trackingConsumer struct {
	jetstream.Consumer
	calls         atomic.Int32
	mutex         sync.Mutex
	sessions      []jetstream.ConsumeContext
	closed        []<-chan struct{}
	failAt        int32
	beforeFailure func()
}

func (c *trackingConsumer) Consume(handler jetstream.MessageHandler, opts ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error) {
	if c.calls.Add(1) == c.failAt {
		if c.beforeFailure != nil {
			c.beforeFailure()
		}
		return nil, errors.New("consume initialization unavailable")
	}
	session, err := c.Consumer.Consume(handler, opts...)
	if err == nil {
		c.mutex.Lock()
		c.sessions = append(c.sessions, session)
		c.closed = append(c.closed, session.Closed())
		c.mutex.Unlock()
	}
	return session, err
}

func runtimeTestWorker(t *testing.T, q *Queue, store *runtimeStore, cfg config.WebhookConfig) *Worker {
	t.Helper()
	return &Worker{database: store, queue: q, cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func waitRuntime(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatal("runtime condition timed out")
		case <-tick.C:
		}
	}
}

func TestWebhookConsumeStartupFailureCleanup(t *testing.T) {
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	cfg := config.WebhookConfig{WorkerEnabled: true, Timeout: time.Minute}
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
	store := &runtimeStore{entered: make(chan time.Duration, 3), block: true}
	for range 3 {
		if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
			t.Fatal(err)
		}
	}
	baseSubscriptions := conn.NumSubscriptions()
	tracked := &trackingConsumer{Consumer: q.consumer, failAt: 4, beforeFailure: func() {
		for range 3 {
			select {
			case <-store.entered:
			case <-time.After(5 * time.Second):
				t.Error("callbacks did not start")
			}
		}
	}}
	q.consumer = tracked
	stop, err := runtimeTestWorker(t, q, store, cfg).Start(t.Context())
	if err == nil || stop != nil {
		t.Fatal("partial initialization succeeded")
	}
	for _, closed := range tracked.closed {
		select {
		case <-closed:
		default:
			t.Fatal("startup left a callback running")
		}
	}
	waitRuntime(t, func() bool { return conn.NumSubscriptions() == baseSubscriptions })
	if store.successes.Load()+store.failures.Load() != 0 {
		t.Fatal("cancellation wrote statistics")
	}
	assertQueueMessages(t, q, 3)
}

func TestWebhookConsumeTimeoutAndConcurrentStop(t *testing.T) {
	for _, timeout := range []time.Duration{10 * time.Second, 70 * time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			conn := queueConnection(t, startQueueServer(t, t.TempDir()))
			cfg := config.WebhookConfig{WorkerEnabled: true, Timeout: timeout}
			q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
			store := &runtimeStore{entered: make(chan time.Duration, 10), block: true}
			for range 10 {
				if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
					t.Fatal(err)
				}
			}
			stop, err := runtimeTestWorker(t, q, store, cfg).Start(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(stop)
			for range 10 {
				select {
				case remaining := <-store.entered:
					if remaining > timeout+15*time.Second || remaining < timeout+14*time.Second {
						t.Fatalf("processing budget=%v", remaining)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("callbacks not concurrent")
				}
			}
			info, err := q.consumer.Info(t.Context())
			if err != nil || info.Config.AckWait != max(time.Minute, timeout+30*time.Second) {
				t.Fatalf("ack budget: %v %v", info, err)
			}
			done := make(chan struct{}, 4)
			for range 4 {
				go func() { stop(); done <- struct{}{} }()
			}
			for range 4 {
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("stop blocked")
				}
			}
			if store.successes.Load()+store.failures.Load() != 0 {
				t.Fatal("canceled callback wrote statistics")
			}
			assertQueueMessages(t, q, 10)
		})
	}
}

func TestWebhookConsumeSessionRestartsAndIdleStop(t *testing.T) {
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	cfg := config.WebhookConfig{WorkerEnabled: true}
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
	tracked := &trackingConsumer{Consumer: q.consumer}
	q.consumer = tracked
	baseSubscriptions := conn.NumSubscriptions()
	stop, err := runtimeTestWorker(t, q, &runtimeStore{}, cfg).Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	if tracked.calls.Load() != 10 {
		t.Fatal("incorrect session count")
	}
	tracked.mutex.Lock()
	first := tracked.sessions[0]
	tracked.mutex.Unlock()
	first.Stop()
	waitRuntime(t, func() bool { return tracked.calls.Load() == 11 })
	if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
		t.Fatal(err)
	}
	waitRuntime(t, func() bool {
		info, err := queueStream(t, q).Info(t.Context())
		return err == nil && info.State.Msgs == 0
	})
	stop()
	waitRuntime(t, func() bool { return conn.NumSubscriptions() == baseSubscriptions })
	conn.Close()
	stop()
}

func TestWebhookConsumeNATSRestart(t *testing.T) {
	dir := t.TempDir()
	srv := startQueueServer(t, dir)
	port := srv.Addr().(*net.TCPAddr).Port
	conn := queueConnection(t, srv)
	cfg := config.WebhookConfig{WorkerEnabled: true, AllowInsecure: true, Timeout: time.Second}
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
	received := make(chan struct{}, 2)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204); received <- struct{}{} }))
	defer receiver.Close()
	store := &runtimeStore{target: db.WebhookDeliveryTarget{Status: "enabled", URL: receiver.URL, SigningSecret: "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}}
	stop, err := runtimeTestWorker(t, q, store, cfg).Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
		t.Fatal(err)
	}
	waitRuntime(t, func() bool {
		info, err := queueStream(t, q).Info(t.Context())
		return err == nil && info.State.Msgs == 0
	})
	srv.Shutdown()
	srv.WaitForShutdown()
	waitRuntime(t, func() bool { return !conn.IsConnected() })
	replacement, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: dir, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	replacement.Start()
	t.Cleanup(func() { replacement.Shutdown(); replacement.WaitForShutdown() })
	if !replacement.ReadyForConnections(5 * time.Second) {
		t.Fatal("restart failed")
	}
	waitRuntime(t, conn.IsConnected)
	if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
		t.Fatal(err)
	}
	waitRuntime(t, func() bool {
		info, err := queueStream(t, q).Info(t.Context())
		return err == nil && info.State.Msgs == 0
	})
	if len(received) != 2 || store.successes.Load() != 2 {
		t.Fatal("consumer did not resume")
	}
}

func TestWebhookConsumeDisabledAndCanceledStartup(t *testing.T) {
	stop, err := (&Worker{}).Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stop()
	stop()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stop, err = (&Worker{cfg: config.WebhookConfig{WorkerEnabled: true}}).Start(ctx)
	if !errors.Is(err, context.Canceled) || stop != nil {
		t.Fatal("canceled worker initialized")
	}
}

func TestWebhookConsumeReusesAndClosesHTTPConnection(t *testing.T) {
	var opened, closed atomic.Int32
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	receiver.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			opened.Add(1)
		case http.StateClosed:
			closed.Add(1)
		}
	}
	receiver.Start()
	defer receiver.Close()
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	cfg := config.WebhookConfig{WorkerEnabled: true, AllowInsecure: true, Timeout: time.Second}
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
	store := &runtimeStore{target: db.WebhookDeliveryTarget{Status: "enabled", URL: receiver.URL, SigningSecret: "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}}
	stop, err := runtimeTestWorker(t, q, store, cfg).Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for range 2 {
		if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
			t.Fatal(err)
		}
		waitRuntime(t, func() bool {
			info, err := queueStream(t, q).Info(t.Context())
			return err == nil && info.State.Msgs == 0
		})
	}
	if opened.Load() != 1 || store.successes.Load() != 2 {
		t.Fatalf("connections=%d successes=%d", opened.Load(), store.successes.Load())
	}
	stop()
	waitRuntime(t, func() bool { return closed.Load() == 1 })
}

func TestWebhookConsumePermanentConnectionClosure(t *testing.T) {
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	cfg := config.WebhookConfig{WorkerEnabled: true}
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
	tracked := &trackingConsumer{Consumer: q.consumer}
	q.consumer = tracked
	stop, err := runtimeTestWorker(t, q, &runtimeStore{}, cfg).Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	conn.Close()
	for _, closed := range tracked.closed {
		select {
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("closed connection left a session running")
		}
	}
	time.Sleep(1100 * time.Millisecond)
	if tracked.calls.Load() != 10 {
		t.Fatal("permanently closed connection was restarted")
	}
}
