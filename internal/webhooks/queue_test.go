package webhooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func startQueueServer(t *testing.T, dir string) *server.Server {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: dir, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS startup failed")
	}
	return srv
}
func queueConnection(t *testing.T, srv *server.Server) *nats.Conn {
	t.Helper()
	conn, err := nats.Connect(srv.ClientURL(), nats.ReconnectBufSize(-1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	return conn
}
func createTestQueue(t *testing.T, conn *nats.Conn, cfg config.WebhookStreamConfig, delivery config.WebhookConfig) *Queue {
	t.Helper()
	q, err := NewQueue(t.Context(), conn, cfg, delivery)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func queueStream(t *testing.T, q *Queue) jetstream.Stream {
	t.Helper()
	stream, err := q.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}
func assertQueueMessages(t *testing.T, q *Queue, want uint64) {
	t.Helper()
	info, err := queueStream(t, q).Info(t.Context())
	if err != nil || info.State.Msgs != want {
		t.Fatalf("stream=%+v err=%v want=%d", info, err, want)
	}
}

func TestWebhookQueueLimitsAndPublication(t *testing.T) {
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	cfg := config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: 24 * time.Hour, Replicas: 3}
	if _, err := NewQueue(t.Context(), conn, cfg, config.WebhookConfig{}); err == nil {
		t.Fatal("three replicas accepted on single node")
	}
	cfg.Replicas = 1
	q := createTestQueue(t, conn, cfg, config.WebhookConfig{})
	si, err := queueStream(t, q).Info(t.Context())
	if err != nil || si.Config.MaxBytes != 256<<20 || si.Config.MaxMsgSize != maxMessageBytes {
		t.Fatalf("stream=%+v err=%v", si, err)
	}
	env := testEnvelope(t)
	invalid := env
	invalid.Version = 2
	if err := q.Publish(t.Context(), invalid); err == nil {
		t.Fatal("accepted invalid envelope")
	}
	large := env
	large.Event = json.RawMessage(strings.Replace(string(env.Event), "agent_test", strings.Repeat("x", maxMessageBytes), 1))
	if err := q.Publish(t.Context(), large); err == nil {
		t.Fatal("accepted large envelope")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := q.Publish(ctx, env); err == nil {
		t.Fatal("ignored cancellation")
	}
	for range 2 {
		if err := q.Publish(t.Context(), env); err != nil {
			t.Fatal(err)
		}
	}
	assertQueueMessages(t, q, 1)
	ci, err := q.consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ci.Config.MaxDeliver != 3 || ci.Config.AckWait != time.Minute || ci.Config.MaxAckPending != 3000 || ci.Config.MaxRequestBatch != 10 || len(ci.Config.BackOff) != 0 {
		t.Fatalf("consumer=%+v", ci.Config)
	}
	cfg.MaxBytes = 1
	q = createTestQueue(t, conn, cfg, config.WebhookConfig{MaxAttempts: 10, Timeout: 70 * time.Second})
	if err := q.Publish(t.Context(), testEnvelope(t)); err == nil {
		t.Fatal("full queue accepted message")
	}
	ci, err = q.consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ci.Config.MaxDeliver != 10 || ci.Config.AckWait != 100*time.Second {
		t.Fatalf("consumer=%+v", ci.Config)
	}
}

func TestWebhookQueuePersistenceAndAcknowledgment(t *testing.T) {
	dir := t.TempDir()
	srv := startQueueServer(t, dir)
	conn := queueConnection(t, srv)
	cfg := config.WebhookStreamConfig{MaxBytes: 64 << 20, MaxAge: 24 * time.Hour, Replicas: 1}
	q := createTestQueue(t, conn, cfg, config.WebhookConfig{})
	env := testEnvelope(t)
	if err := q.Publish(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	srv.Shutdown()
	srv.WaitForShutdown()
	q = createTestQueue(t, queueConnection(t, startQueueServer(t, dir)), cfg, config.WebhookConfig{})
	msg, err := q.consumer.Next(jetstream.FetchMaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(msg.Data(), &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Event) != string(env.Event) {
		t.Fatal("restart changed payload")
	}
	if err := msg.DoubleAck(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertQueueMessages(t, q, 0)
}

func TestWebhookQueueExhaustionExpiresWithoutReplay(t *testing.T) {
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 64 << 20, MaxAge: 2 * time.Second, Replicas: 1}, config.WebhookConfig{MaxAttempts: 1})
	ci, err := q.consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ci.Config.AckWait = 50 * time.Millisecond
	q.consumer, err = q.js.UpdateConsumer(t.Context(), StreamName, ci.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.consumer.Next(jetstream.FetchMaxWait(time.Second)); err != nil {
		t.Fatal(err)
	}
	// Simulate process death: no ACK. No second consumption is allowed.
	if _, err := q.consumer.Next(jetstream.FetchMaxWait(150 * time.Millisecond)); err == nil {
		t.Fatal("exhausted message redelivered")
	}
	assertQueueMessages(t, q, 1)
	deadline := time.Now().Add(3 * time.Second)
	for {
		info, err := queueStream(t, q).Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if info.State.Msgs == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exhausted message did not expire")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWebhookQueueUnacknowledgedRedelivery(t *testing.T) {
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 64 << 20, MaxAge: time.Hour, Replicas: 1}, config.WebhookConfig{})
	info, err := q.consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	info.Config.AckWait = 200 * time.Millisecond
	q.consumer, err = q.js.UpdateConsumer(t.Context(), StreamName, info.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
		t.Fatal(err)
	}
	first, err := q.consumer.Next(jetstream.FetchMaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.consumer.Next(jetstream.FetchMaxWait(50 * time.Millisecond)); err == nil {
		t.Fatal("unexpired message claimed twice")
	}
	second, err := q.consumer.Next(jetstream.FetchMaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := second.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.NumDelivered != 2 || string(first.Data()) != string(second.Data()) {
		t.Fatal("redelivery changed payload or counter")
	}
	if err := second.DoubleAck(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertQueueMessages(t, q, 0)
}
