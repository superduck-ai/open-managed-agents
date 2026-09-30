package tests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

func newWebhookTestQueue(t *testing.T, cfg config.WebhookConfig) (*webhooks.Queue, jetstream.Stream) {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("webhook NATS not ready")
	}
	conn, err := nats.Connect(srv.ClientURL(), nats.ReconnectBufSize(-1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	queue, err := webhooks.NewQueue(t.Context(), conn, config.WebhookStreamConfig{MaxBytes: 64 << 20, MaxAge: 24 * time.Hour, Replicas: 1}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(t.Context(), webhooks.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	return queue, stream
}

func queuedWebhookEvents(t *testing.T, app *testApp) []webhooks.Event {
	t.Helper()
	info, err := app.webhookStream.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	events := make([]webhooks.Event, 0, info.State.Msgs)
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		msg, err := app.webhookStream.GetMsg(context.Background(), seq)
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var envelope webhooks.Envelope
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatal(err)
		}
		var event webhooks.Event
		if err := json.Unmarshal(envelope.Event, &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func assertWebhookQueueCount(t *testing.T, app *testApp, want int) {
	t.Helper()
	info, err := app.webhookStream.Info(context.Background())
	if err != nil || int(info.State.Msgs) != want {
		t.Fatalf("queue count: info=%+v err=%v want=%d", info, err, want)
	}
}

func startWebhookWorker(t *testing.T, worker *webhooks.Worker) func() {
	t.Helper()
	stop, err := worker.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return stop
}

func waitWebhookCondition(t *testing.T, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("webhook condition did not complete", ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitWebhookQueueEmpty(t *testing.T, app *testApp) {
	t.Helper()
	waitWebhookCondition(t, func() bool {
		info, err := app.webhookStream.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return info.State.Msgs == 0
	})
}

func drainWebhookQueue(t *testing.T, app *testApp, worker *webhooks.Worker) {
	t.Helper()
	stop := startWebhookWorker(t, worker)
	defer stop()
	waitWebhookQueueEmpty(t, app)
}
