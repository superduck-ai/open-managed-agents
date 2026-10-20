package workerevents

import (
	"context"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func assertNoBrainWorkSignal(t *testing.T, signals <-chan struct{}) {
	t.Helper()
	select {
	case <-signals:
		t.Fatal("unexpected brain work signal")
	case <-time.After(50 * time.Millisecond):
	}
}

func assertBrainWorkSignal(t *testing.T, signals <-chan struct{}) {
	t.Helper()
	select {
	case <-signals:
	case <-time.After(2 * time.Second):
		t.Fatal("brain work signal not delivered")
	}
}

func TestBrainWorkSignalStopsAfterSubscriberCancellation(t *testing.T) {
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	for name, newSignal := range map[string]func(t *testing.T) BrainWorkSignal{
		"memory": func(*testing.T) BrainWorkSignal { return NewMemory() },
		"nats": func(t *testing.T) BrainWorkSignal {
			broker, err := NewJetStream(t.Context(), connectNATS(t, srv.ClientURL()), config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, Replicas: 1}, "")
			if err != nil {
				t.Fatal(err)
			}
			return broker
		},
	} {
		t.Run(name, func(t *testing.T) {
			signal := newSignal(t)
			ctx, cancel := context.WithCancel(t.Context())
			signals, err := signal.SubscribeBrainWork(ctx)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			time.Sleep(50 * time.Millisecond)
			if err := signal.NotifyBrainWork(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertNoBrainWorkSignal(t, signals)
		})
	}
}

func TestBrainWorkSignalReachesEverySubscriber(t *testing.T) {
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	connection := connectNATS(t, srv.ClientURL())
	publisher, err := NewJetStream(t.Context(), connection, config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, Replicas: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := NewJetStream(t.Context(), connectNATS(t, srv.ClientURL()), config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, Replicas: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	memory := NewMemory()
	remote, err := subscriber.SubscribeBrainWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	local, err := memory.SubscribeBrainWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := subscriber.connection.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := publisher.NotifyBrainWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := memory.NotifyBrainWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertBrainWorkSignal(t, remote)
	assertBrainWorkSignal(t, local)
	if streams := publisher.js.StreamNames(t.Context()); streams.Err() != nil {
		t.Fatal(streams.Err())
	} else {
		for name := range streams.Name() {
			if name == "OMA_BRAIN_TASKS" {
				t.Fatal("brain task stream must not be created")
			}
		}
	}
}

func TestBrainSignalsRespectNamespace(t *testing.T) {
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	conn := connectNATS(t, srv.ClientURL())
	cfg := config.WorkerEventStreamConfig{ConsumerInactiveThreshold: time.Minute, Replicas: 1}
	a, err := NewJetStream(t.Context(), conn, cfg, "env-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewJetStream(t.Context(), conn, cfg, "env-b")
	if err != nil {
		t.Fatal(err)
	}
	signals, err := b.SubscribeBrainWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := a.NotifyBrainWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	assertNoBrainWorkSignal(t, signals)
	if err := b.NotifyBrainWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertBrainWorkSignal(t, signals)
}
