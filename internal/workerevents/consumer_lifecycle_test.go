package workerevents

import (
	"context"
	"errors"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func lifecycleBroker(t *testing.T, threshold time.Duration) *JetStreamBroker {
	t.Helper()
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	broker, err := NewJetStream(t.Context(), connectNATS(t, srv.ClientURL()), config.WorkerEventStreamConfig{
		MaxBytes: 1 << 24, MaxMsgSize: 1 << 20, Replicas: 1, ConsumerInactiveThreshold: threshold,
	})
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

func TestConsumerExpiryRetainsUnacknowledgedMessages(t *testing.T) {
	broker := lifecycleBroker(t, 200*time.Millisecond)
	id := "cse_expiry"
	for _, lane := range deliveryLanes {
		consumer, err := broker.laneConsumer(t.Context(), id, lane)
		if err != nil {
			t.Fatal(err)
		}
		info, err := consumer.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		cfg := info.Config
		cfg.BackOff = []time.Duration{50 * time.Millisecond}
		if _, err := broker.js.UpdateConsumer(t.Context(), StreamName, cfg); err != nil {
			t.Fatal(err)
		}
		kind := "user.message"
		if lane == replyLane {
			kind = "control_response"
		}
		event := EventEnvelope(id, kind, "", kind, "", []byte(`{}`), time.Now().Add(time.Hour))
		if err := broker.Publish(t.Context(), kind, event); err != nil {
			t.Fatal(err)
		}
		batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for range batch.Messages() {
			count++
		}
		if batch.Error() != nil || count != 1 {
			t.Fatalf("fetch = %d, %v", count, batch.Error())
		}
	}
	waitConsumerCount(t, broker, 0)
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(t.Context())
	if err != nil || info.State.Msgs != 2 {
		t.Fatalf("expiry removed unACKed messages: %+v %v", info, err)
	}
	sub, err := broker.Subscribe(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	seen := map[string]bool{}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for len(seen) < 2 {
		select {
		case delivery := <-sub.Messages():
			if seen[delivery.Envelope.EventID] {
				t.Fatal("duplicate event before ACK")
			}
			seen[delivery.Envelope.EventID] = true
			if err := broker.DoubleAck(ctx, delivery.AckSubject); err != nil {
				t.Fatal(err)
			}
		case err := <-sub.Errors():
			t.Fatalf("receive: %v", err)
		case <-ctx.Done():
			t.Fatal("recreated consumers did not deliver retained messages")
		}
	}
	if !seen["user.message"] || !seen["control_response"] {
		t.Fatalf("changed event IDs: %v", seen)
	}
	info, err = stream.Info(t.Context())
	if err != nil || info.State.Msgs != 0 {
		t.Fatalf("ACK did not drain queue: %+v %v", info, err)
	}
}

func TestActivePullAndReplacementKeepConsumers(t *testing.T) {
	broker := lifecycleBroker(t, 200*time.Millisecond)
	old, err := broker.Subscribe(t.Context(), "cse_active")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := broker.Subscribe(t.Context(), "cse_active")
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := stream.Info(t.Context())
		if err != nil || info.State.Consumers != 2 {
			t.Fatalf("active pulls expired: %+v %v", info, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	event := EventEnvelope("cse_active", "active", "", "user.message", "", []byte(`{}`), time.Now().Add(time.Hour))
	if err := broker.Publish(t.Context(), event.EventID, event); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case delivery := <-replacement.Messages():
		if delivery.Envelope.EventID != event.EventID {
			t.Fatal("replacement got wrong event")
		}
		if err := broker.DoubleAck(ctx, delivery.AckSubject); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("replacement lost shared consumer")
	}
}

func waitConsumerCount(t *testing.T, broker *JetStreamBroker, count int) {
	t.Helper()
	stream, err := broker.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := stream.Info(t.Context())
		if err == nil && info.State.Consumers == count {
			return
		}
		if err != nil && !errors.Is(err, jetstream.ErrConsumerNotFound) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("consumer count did not reach %d", count)
}
