package porttunnel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

func startRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	instance := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: instance.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return instance, client
}

func startNATS(t *testing.T) *nats.Conn {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS failed to start")
	}
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	connection, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	return connection
}

func TestRedisOwnerStoreReleaseKeepsNewerOwner(t *testing.T) {
	instance, client := startRedis(t)
	store := NewRedisOwnerStore(client)
	ctx := context.Background()
	if err := store.Claim(ctx, "cs-1", "instance-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Claim(ctx, "cs-1", "instance-b"); err != nil {
		t.Fatal(err)
	}
	if err := store.Release(ctx, "cs-1", "instance-a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := instance.Get(ownerKey("cs-1")); got != "instance-b" {
		t.Fatalf("owner after stale release = %q", got)
	}
	if err := store.Refresh(ctx, "cs-1", "instance-a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := instance.Get(ownerKey("cs-1")); got != "instance-b" {
		t.Fatalf("stale refresh replaced owner: %q", got)
	}
	if err := store.Release(ctx, "cs-1", "instance-b"); err != nil {
		t.Fatal(err)
	}
	if instance.Exists(ownerKey("cs-1")) {
		t.Fatal("owner record survived release")
	}
}

func TestRedisOwnerStoreClaimsAndRefreshesWithTTL(t *testing.T) {
	instance, client := startRedis(t)
	store := NewRedisOwnerStore(client)
	ctx := context.Background()
	if err := store.Claim(ctx, "cs-1", "instance-a"); err != nil {
		t.Fatal(err)
	}
	if ttl := instance.TTL(ownerKey("cs-1")); ttl != ownerTTL {
		t.Fatalf("claim ttl = %s", ttl)
	}
	instance.FastForward(50 * time.Second)
	if err := store.Refresh(ctx, "cs-1", "instance-a"); err != nil {
		t.Fatal(err)
	}
	if ttl := instance.TTL(ownerKey("cs-1")); ttl != ownerTTL {
		t.Fatalf("refreshed ttl = %s", ttl)
	}
	instance.FastForward(2 * ownerTTL)
	if err := store.Refresh(ctx, "cs-1", "instance-a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := instance.Get(ownerKey("cs-1")); got != "instance-a" {
		t.Fatalf("expired owner was not restored: %q", got)
	}
}

func TestRegistryWaitOnlineTimesOutWithoutTunnel(t *testing.T) {
	registry := NewRegistry("owner", nil, NewNATSOnlineNotifier(startNATS(t)), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := registry.WaitOnline(ctx, TestCodeSessionUUID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitOnline = %v", err)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if len(registry.waiters) != 0 {
		t.Fatalf("waiters leaked: %v", registry.waiters)
	}
}

func TestRegistryPublishesOnlineNotificationAcrossInstances(t *testing.T) {
	connection := startNATS(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, redisClient := startRedis(t)
	waiting := NewRegistry("instance-a", nil, NewNATSOnlineNotifier(connection), logger)
	owning := NewRegistry("instance-b", NewRedisOwnerStore(redisClient), NewNATSOnlineNotifier(connection), logger)
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result <- waiting.WaitOnline(ctx, TestCodeSessionUUID)
	}()
	time.Sleep(100 * time.Millisecond)
	conn := &Conn{identity: Identity{CodeSessionUUID: TestCodeSessionUUID}, closed: make(chan struct{})}
	owning.register(conn)
	if err := <-result; err != nil {
		t.Fatalf("WaitOnline = %v", err)
	}
	owner, err := redisClient.Get(context.Background(), ownerKey(TestCodeSessionUUID)).Result()
	if err != nil || owner != "instance-b" {
		t.Fatalf("owner = %q, %v", owner, err)
	}
	if _, err := waiting.Lookup(context.Background(), TestCodeSessionUUID); !errors.Is(err, ErrNoTunnel) {
		t.Fatalf("remote tunnel resolved locally: %v", err)
	}
	owning.unregister(conn)
	if exists, _ := redisClient.Exists(context.Background(), ownerKey(TestCodeSessionUUID)).Result(); exists != 0 {
		t.Fatal("owner survived unregister")
	}
}

func TestRegistryCloseAllAsksClientsToReconnectAndReleasesOwners(t *testing.T) {
	_, redisClient := startRedis(t)
	server := StartTunnelServer(t, TunnelServerOptions{Owner: "10.0.0.1:38090#instance-a", Owners: NewRedisOwnerStore(redisClient)})
	client := server.Connect(t)
	WaitFor(t, func() bool {
		owner, _ := redisClient.Get(context.Background(), ownerKey(TestCodeSessionUUID)).Result()
		return owner == "10.0.0.1:38090#instance-a"
	})
	server.Registry.CloseAll()
	select {
	case <-client.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client was not disconnected")
	}
	if code := client.CloseCode.Load(); code != websocket.CloseServiceRestart {
		t.Fatalf("close code = %d", code)
	}
	if exists, _ := redisClient.Exists(context.Background(), ownerKey(TestCodeSessionUUID)).Result(); exists != 0 {
		t.Fatal("owner survived shutdown")
	}
	if _, err := server.Registry.Lookup(context.Background(), TestCodeSessionUUID); !errors.Is(err, ErrNoTunnel) {
		t.Fatalf("lookup after shutdown = %v", err)
	}
}
