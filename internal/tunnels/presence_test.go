package tunnels

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/redisclient"
)

func TestPresenceFailureOnlyAffectsDisplay(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", ContextTimeoutEnabled: true})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	p := NewConnectorPresence(client, time.Minute)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	console := &ConsoleHandler{presence: p, logger: logger}
	snapshots := console.connectorSnapshots(httptest.NewRequest("GET", "/", nil), []db.MCPTunnel{{UUID: "active"}, {UUID: "archived", ArchivedAt: &now}})
	if snapshots["active"].State != "unknown" || snapshots["archived"].State != "disconnected" {
		t.Fatalf("snapshots=%+v", snapshots)
	}
	b := testNATSBroker(t, brokerTestConfig())
	credential := activeConnectorContext()
	credential.TunnelUUID = "tunnel"
	id := "req_0123456789abcdefghijklmn"
	if err := b.Enqueue(t.Context(), "tunnel", credential.TunnelExternalID, testQueuedCommand(id)); err != nil {
		t.Fatal(err)
	}
	h := &ConnectorHandler{cfg: b.cfg, broker: b, presence: p, logger: logger, db: connectorMetadataDatabase{context: credential}}
	if err := h.poll(httptest.NewRecorder(), connectorMetadataRequest("valid-token")); err != nil {
		t.Fatal(err)
	}
	h.db = nil
	if err := h.postResponse(httptest.NewRecorder(), boundResponseRequest(t, "valid-token", credential.TunnelExternalID, testTerminalResponse(id), id)); err != nil {
		t.Fatal(err)
	}
}

func TestPresenceAggregation(t *testing.T) {
	if _, err := snapshotFromPresence(map[string]string{"a": "not json"}); err == nil {
		t.Fatal("corrupt presence shown as offline")
	}
	snapshot, err := snapshotFromPresence(map[string]string{"a": `["main","tools"]`, "b": `["tools"]`})
	want := ConnectorSnapshot{State: "connected", InstanceCount: 2, Channels: []ConnectorChannelSnapshot{{Name: "main", InstanceCount: 1}, {Name: "tools", InstanceCount: 2}}}
	if err != nil || !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

// Run against a disposable Redis 8 instance; CLIENT PAUSE briefly pauses this instance.
func TestPresenceRedis8(t *testing.T) {
	address := os.Getenv("TEST_TUNNEL_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_TUNNEL_REDIS_ADDR requires a disposable Redis 8 instance")
	}
	client, err := redisclient.Open(t.Context(), "redis://"+address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	uuid := "presence-test-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() { _ = client.Del(context.Background(), presenceKey(uuid)).Err() })
	p := NewConnectorPresence(client, time.Second)
	snapshot := func() ConnectorSnapshot {
		t.Helper()
		values, err := p.Snapshots(t.Context(), []string{uuid, "missing-tunnel"})
		if err != nil {
			t.Fatal(err)
		}
		if values["missing-tunnel"].State != "disconnected" {
			t.Fatal("missing record is not offline")
		}
		return values[uuid]
	}
	if snapshot().State != "disconnected" {
		t.Fatal("unexpected initial presence")
	}
	if err := p.Touch(t.Context(), uuid, "a", []ChannelDeclaration{{Name: "main"}, {Name: "tools"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.Touch(t.Context(), uuid, "b", []ChannelDeclaration{{Name: "tools"}}); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(); got.InstanceCount != 2 || len(got.Channels) != 2 || got.Channels[1].InstanceCount != 2 {
		t.Fatalf("aggregate=%+v", got)
	}
	time.Sleep(650 * time.Millisecond)
	if err := p.Touch(t.Context(), uuid, "a", []ChannelDeclaration{{Name: "main"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if got := snapshot(); got.InstanceCount != 1 || len(got.Channels) != 1 || got.Channels[0].Name != "main" {
		t.Fatalf("independent expiry/refresh=%+v", got)
	}
	time.Sleep(600 * time.Millisecond)
	if snapshot().State != "disconnected" {
		t.Fatal("expired field retained")
	}

	b := testNATSBroker(t, brokerTestConfig())
	credential := activeConnectorContext()
	credential.TunnelUUID = uuid
	h := &ConnectorHandler{cfg: b.cfg, broker: b, presence: p, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), db: connectorMetadataDatabase{context: credential}}
	request := connectorMetadataRequest("valid-token")
	request.URL.RawQuery = "channel=tools&timeout_ms=0"
	request.Header.Set(serverInfoHeader, `{"version":2,"channels":[{"name":"main"},{"name":"tools","stateless":true}]}`)
	if err := h.poll(httptest.NewRecorder(), request); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(); len(got.Channels) != 2 {
		t.Fatalf("filtered Poll lost full declaration: %+v", got)
	}
	if err := client.Do(t.Context(), "CLIENT", "PAUSE", 1000, "ALL").Err(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := p.Touch(t.Context(), uuid, "paused", []ChannelDeclaration{{Name: "main"}}); err == nil {
		t.Fatal("paused Redis write should time out")
	}
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		t.Fatalf("presence exceeded 500ms budget with scheduling allowance: %s", elapsed)
	}
}
