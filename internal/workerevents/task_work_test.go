package workerevents

import (
	"context"
	server "github.com/nats-io/nats-server/v2/server"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"testing"
	"time"
)

func TestTaskWorkSignalsAreScopedAndEphemeral(t *testing.T) {
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	publisher, err := NewJetStream(t.Context(), connectNATS(t, srv.ClientURL()), config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, Replicas: 1})
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := NewJetStream(t.Context(), connectNATS(t, srv.ClientURL()), config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, Replicas: 1})
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]TaskWorkSignal{"memory": {NewMemory(), nil}, "nats": {publisher, subscriber}} {
		t.Run(name, func(t *testing.T) {
			sender, receiver := pair[0], pair[1]
			if receiver == nil {
				receiver = sender
			}
			if err := sender.NotifyTaskWork(t.Context(), "cse_task"); err != nil {
				t.Fatal(err)
			}
			if name == "nats" {
				if err := publisher.connection.FlushTimeout(time.Second); err != nil {
					t.Fatal(err)
				}
			}
			signals, err := receiver.SubscribeTaskWork(t.Context(), "cse_task")
			if err != nil {
				t.Fatal(err)
			}
			other, err := receiver.SubscribeTaskWork(t.Context(), "cse_other")
			if err != nil {
				t.Fatal(err)
			}
			assertNoBrainWorkSignal(t, signals)
			if err := sender.NotifyTaskWork(t.Context(), "cse_task"); err != nil {
				t.Fatal(err)
			}
			assertBrainWorkSignal(t, signals)
			assertNoBrainWorkSignal(t, other)
		})
	}
	stream, err := publisher.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 0 {
		t.Fatalf("task hints persisted %d messages", info.State.Msgs)
	}

}
func TestTaskWorkSignalsCancelAndCoalesce(t *testing.T) {
	broker := NewMemory()
	ctx, cancel := context.WithCancel(t.Context())
	signals, err := broker.SubscribeTaskWork(ctx, "cse_task")
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if err := broker.NotifyTaskWork(t.Context(), "cse_task"); err != nil {
			t.Fatal(err)
		}
	}
	assertBrainWorkSignal(t, signals)
	assertNoBrainWorkSignal(t, signals)
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		broker.brainWork.mu.Lock()
		count := len(broker.brainWork.waiters)
		broker.brainWork.mu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber leaked")
		}
		time.Sleep(time.Millisecond)
	}
	if err := broker.NotifyTaskWork(t.Context(), "cse_task"); err != nil {
		t.Fatal(err)
	}
	assertNoBrainWorkSignal(t, signals)
}
