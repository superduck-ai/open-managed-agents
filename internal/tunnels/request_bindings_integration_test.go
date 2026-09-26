package tunnels

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// The configured Redis must be disposable: the failure test briefly pauses it.
func realRequestBindings(t *testing.T, ttl time.Duration) *RequestBindings {
	t.Helper()
	address := os.Getenv("TEST_TUNNEL_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_TUNNEL_REDIS_ADDR requires a disposable Redis 8 instance")
	}
	client := redis.NewClient(&redis.Options{Addr: address, ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	info, err := client.Info(t.Context(), "server").Result()
	if err != nil || !strings.Contains(info, "redis_version:8.") {
		t.Fatalf("Redis 8 required: %v", err)
	}
	return NewRequestBindings(client, ttl)
}

func useRealIntegrationBindings(t *testing.T, b *Broker) {
	t.Helper()
	if os.Getenv("TEST_TUNNEL_REDIS_ADDR") != "" {
		b.requests = realRequestBindings(t, brokerRequestRetention(b.cfg.RequestTimeout, b.cfg.TombstoneTTL))
	}
}

func TestRequestBindingsRedis8(t *testing.T) {
	store := realRequestBindings(t, 2*time.Second)
	peer := realRequestBindings(t, 2*time.Second)
	key := brokerKey(uuid.NewString())
	record := bindingTestRecord()
	if _, err := store.read(t.Context(), key); !errors.Is(err, ErrRequestNotFound) {
		t.Fatal(err)
	}
	// Warm both connections before CLIENT PAUSE, so the failure covers SET itself.
	if err := store.ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := peer.ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := peer.client.Do(t.Context(), "CLIENT", "PAUSE", 250, "ALL").Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	started := time.Now()
	err := store.create(ctx, key, record)
	cancel()
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("paused Redis write: %v", err)
	}
	// An uncertain SET may take effect after the timeout. Never reuse that ID.
	time.Sleep(300 * time.Millisecond)
	key = brokerKey(uuid.NewString())
	var accepted atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		target := store
		if i%2 == 0 {
			target = peer
		}
		group.Go(func() {
			err := target.create(t.Context(), key, record)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, errRequestBindingExists) {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("claims=%d", accepted.Load())
	}
	time.Sleep(500 * time.Millisecond)
	before, err := store.client.PTTL(t.Context(), requestBindingKey(key)).Result()
	if err != nil {
		t.Fatal(err)
	}
	altered := record
	altered.TokenHash[0]++
	if err := peer.create(t.Context(), key, altered); !errors.Is(err, errRequestBindingExists) {
		t.Fatal(err)
	}
	got, err := peer.read(t.Context(), key)
	if err != nil || got != record {
		t.Fatal("cross-instance binding changed", err)
	}
	after, err := store.client.PTTL(t.Context(), requestBindingKey(key)).Result()
	if err != nil || after > before {
		t.Fatal("binding TTL refreshed", err)
	}
	second := brokerKey(uuid.NewString())
	if err := peer.create(t.Context(), second, record); err != nil {
		t.Fatal(err)
	}
	time.Sleep(after + 50*time.Millisecond)
	if _, err := peer.read(t.Context(), key); !errors.Is(err, ErrRequestNotFound) {
		t.Fatal(err)
	}
	if _, err := store.read(t.Context(), second); err != nil {
		t.Fatal("independent record expired", err)
	}
}

func TestRequestBindingsRedis8CrossInstanceResponse(t *testing.T) {
	originBindings := realRequestBindings(t, 2*time.Minute)
	origin := testNATSBroker(t, brokerTestConfig())
	origin.requests = originBindings
	peers := make([]*Broker, 2)
	for i := range peers {
		peer, err := newBroker(t.Context(), connectTunnelNATS(t, origin.connection.ConnectedUrl()), brokerTestConfig(), 1, realRequestBindings(t, 2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(peer.Close)
		peers[i] = peer
	}
	command := testQueuedCommand(uuid.NewString())
	waiter := testResponseWaiter(t, origin, command)
	if err := origin.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, peers[0], []ChannelDeclaration{{Name: "main"}}, 1)
	if len(commands) != 1 {
		t.Fatal("missing claim")
	}
	if err := peers[1].SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	got, err := waiter.Wait(ctx, nil)
	if err != nil || got.RequestID != command.RequestID {
		t.Fatal("cross-instance delivery failed", err)
	}
	waiter.Close()
	if err := peers[0].SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); err != nil {
		t.Fatal("duplicate lost binding", err)
	}
}
