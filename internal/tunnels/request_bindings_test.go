package tunnels

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func testRequestBindings(t *testing.T) *RequestBindings {
	t.Helper()
	return testRequestBindingsWithTTL(t, 2*time.Minute)
}

func testRequestBindingsWithTTL(t *testing.T, ttl time.Duration) *RequestBindings {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return NewRequestBindings(client, ttl)
}

func bindingTestRecord() requestRecord {
	return requestRecord{TunnelID: "tunnel", TokenHash: testTokenHash(), Channel: "main", CommandType: CommandTypeJSONRPC,
		ExpiresAt: time.Now().Add(time.Minute).UTC(), Origin: "oma.test.response", Scope: payloadTestScope}
}

func TestRequestBindingsFailures(t *testing.T) {
	store := testRequestBindings(t)
	if _, err := store.read(t.Context(), "missing"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatal(err)
	}
	assertTunnelErrorKind(t, connectorResponseError(ErrRequestNotFound), apperr.NotFound)
	for _, data := range []string{"{", string(make([]byte, maxRequestBindingBytes+1))} {
		if err := store.client.Set(t.Context(), requestBindingKey("bad"), data, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		_, err := store.read(t.Context(), "bad")
		if err == nil {
			t.Fatal("accepted corrupt binding")
		}
		assertTunnelErrorKind(t, connectorResponseError(err), apperr.Unavailable)
	}
	if err := store.client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.create(t.Context(), "closed", bindingTestRecord()); err == nil {
		t.Fatal("accepted unavailable Redis")
	}
	if _, err := store.read(t.Context(), "bad"); err == nil || errors.Is(err, ErrRequestNotFound) {
		t.Fatal("Redis failure became not found")
	}
	if err := store.ping(t.Context()); err == nil {
		t.Fatal("unavailable Redis passed health check")
	}
}

func TestRequestBindingsImmutableAndExpire(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRequestBindings(client, time.Minute)
	record := bindingTestRecord()
	if err := store.create(t.Context(), "first", record); err != nil {
		t.Fatal(err)
	}
	server.FastForward(20 * time.Second)
	ttl := server.TTL(requestBindingKey("first"))
	changed := record
	changed.TokenHash[0]++
	if err := store.create(t.Context(), "first", changed); !errors.Is(err, errRequestBindingExists) {
		t.Fatal(err)
	}
	got, err := store.read(t.Context(), "first")
	if err != nil || got != record || server.TTL(requestBindingKey("first")) != ttl {
		t.Fatal("binding or expiry changed")
	}
	if err := store.create(t.Context(), "second", record); err != nil {
		t.Fatal(err)
	}
	server.FastForward(41 * time.Second)
	if _, err := store.read(t.Context(), "first"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatal(err)
	}
	if _, err := store.read(t.Context(), "second"); err != nil {
		t.Fatal("independent binding expired", err)
	}
}

func TestRequestBindingsConcurrentCreate(t *testing.T) {
	store := testRequestBindings(t)
	record := bindingTestRecord()
	var accepted atomic.Int32
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			err := store.create(t.Context(), "same", record)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, errRequestBindingExists) {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d claims", accepted.Load())
	}
}

type bindingCommandHook struct {
	process func(context.Context, redis.Cmder) error
}

func (h bindingCommandHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h bindingCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h bindingCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.process(ctx, cmd); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}

func TestBindingFailureNeverDeliversClaim(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprintf("uncertain=%v", uncertain), func(t *testing.T) {
			b := testNATSBroker(t, brokerTestConfig())
			command := testQueuedCommand("redis-failure")
			if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
				t.Fatal(err)
			}
			// For the ambiguous-write case, the server has accepted the binding but
			// the caller receives a timeout. It must still not deliver the command.
			if uncertain {
				if err := b.requests.create(t.Context(), brokerKey(command.RequestID), bindingTestRecord()); err != nil {
					t.Fatal(err)
				}
			}
			b.requests.client.AddHook(bindingCommandHook{process: func(_ context.Context, cmd redis.Cmder) error {
				if cmd.Name() == "set" {
					return context.DeadlineExceeded
				}
				return nil
			}})
			commands, err := b.Poll(t.Context(), "tunnel", testTokenHash(), []ChannelDeclaration{{Name: "main"}}, 1, 0)
			if err == nil || len(commands) != 0 {
				t.Fatalf("failed binding delivered: %d %v", len(commands), err)
			}
			if commands := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1); len(commands) != 0 {
				t.Fatal("failed binding redelivered")
			}
		})
	}
}

func TestBrokerHasNoRequestCountAdmission(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	const count = 300
	waiters := make([]*responseWaiter, 0, count)
	for i := range count {
		command := testQueuedCommand(fmt.Sprintf("unlimited-%d", i))
		waiter := testResponseWaiter(t, b, command)
		waiters = append(waiters, waiter)
		if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
			t.Fatal(err)
		}
	}
	info, err := b.commands.Info(t.Context())
	if err != nil || info.State.Msgs != count {
		t.Fatalf("queued: %v %v", info, err)
	}
	received := 0
	for received < count {
		commands := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, count)
		if len(commands) == 0 {
			t.Fatal("requests were lost")
		}
		received += len(commands)
	}
	if n, err := b.requests.client.DBSize(t.Context()).Result(); err != nil || n != count {
		t.Fatalf("bindings=%d %v", n, err)
	}
	for _, waiter := range waiters {
		waiter.Close()
	}
	b.responseHub.mu.Lock()
	defer b.responseHub.mu.Unlock()
	if len(b.responseHub.waiters) != 0 {
		t.Fatal("closed waiters retained")
	}
}

func TestResponseWaitDoesNotReadBindings(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	var reads atomic.Int32
	b.requests.client.AddHook(bindingCommandHook{process: func(_ context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "get" {
			reads.Add(1)
		}
		return nil
	}})
	waiter := testResponseWaiter(t, b, testQueuedCommand("local-wait"))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := waiter.Wait(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if reads.Load() != 0 {
		t.Fatal("waiting polled Redis")
	}
}

func TestRequestBindingRespectsCallerTimeout(t *testing.T) {
	// A TCP endpoint that never answers proves deadlines apply to socket I/O.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			buffer := make([]byte, 1024)
			for {
				if _, err := conn.Read(buffer); err != nil {
					return
				}
			}
		}
	}()
	client := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), ContextTimeoutEnabled: true, MaxRetries: -1})
	store := NewRequestBindings(client, time.Minute)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := store.create(ctx, "timeout", bindingTestRecord()); err == nil {
		t.Fatal("unanswered write succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("caller deadline ignored")
	}
	_ = client.Close()
	_ = listener.Close()
	<-done
}

func TestRedisFailureLeavesNATSHealthy(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	if err := b.requests.client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Ping(t.Context()); err != nil {
		t.Fatal("Redis failure changed NATS health", err)
	}
	if err := b.PingRedis(t.Context()); err == nil {
		t.Fatal("Redis failure hidden")
	}
	// Enqueue is independent of bindings; only claim/response require Redis.
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand("offline-redis")); err != nil {
		t.Fatal(err)
	}
}

func TestCommandStorageBudgetStillRejectsFullStream(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	info, err := b.commands.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg := info.Config
	// Use a small test-only budget to exercise the production error mapping.
	cfg.MaxBytes = 2048
	if _, err := b.js.UpdateStream(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		err := b.Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand(fmt.Sprintf("storage-%d", i)))
		if errors.Is(err, errCommandStorageFull) {
			assertTunnelErrorKind(t, ingressQueueError(err), apperr.RateLimited)
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("stream byte budget was ignored")
}

func TestPollReturnsSuccessfulClaimsAlongsideRedisFailure(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	for _, id := range []string{"first", "second"} {
		if err := b.Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand(id)); err != nil {
			t.Fatal(err)
		}
	}
	var writes atomic.Int32
	b.requests.client.AddHook(bindingCommandHook{process: func(_ context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "set" && writes.Add(1) == 2 {
			return context.DeadlineExceeded
		}
		return nil
	}})
	commands, err := b.Poll(t.Context(), "tunnel", testTokenHash(), []ChannelDeclaration{{Name: "main"}}, 2, 0)
	if err != nil || len(commands) != 1 || commands[0].RequestID != "first" {
		t.Fatalf("partial success: %v %v", commands, err)
	}
}
