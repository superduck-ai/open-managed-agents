package natsclient

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestOpen(t *testing.T) {
	t.Run("rejects an empty URL", func(t *testing.T) {
		_, err := Open(context.Background(), config.NATSConfig{}, testLogger())
		if err == nil || !strings.Contains(err.Error(), "nats.url is required") {
			t.Fatalf("Open() error = %v, want required URL error", err)
		}
	})

	t.Run("rejects a server without JetStream", func(t *testing.T) {
		serverURL := startNATSTestServer(t, false)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := Open(ctx, testNATSConfig(serverURL), testLogger())
		if err == nil || !strings.Contains(err.Error(), "verify nats JetStream") {
			t.Fatalf("Open() error = %v, want JetStream verification error", err)
		}
	})

	t.Run("connects when JetStream is ready", func(t *testing.T) {
		serverURL := startNATSTestServer(t, true)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connection, err := Open(ctx, testNATSConfig(serverURL), testLogger())
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if !connection.IsConnected() {
			t.Fatal("NATS connection is not connected")
		}
		if connection.Opts.Name != clientName {
			t.Fatalf("NATS client name = %q, want %q", connection.Opts.Name, clientName)
		}
		if err := connection.Drain(); err != nil {
			t.Fatalf("Drain() error = %v", err)
		}
	})
}

func startNATSTestServer(t *testing.T, jetStreamEnabled bool) string {
	t.Helper()
	options := &server.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		NoSigs:    true,
		JetStream: jetStreamEnabled,
		StoreDir:  t.TempDir(),
	}
	testServer, err := server.NewServer(options)
	if err != nil {
		t.Fatalf("create NATS test server: %v", err)
	}
	testServer.Start()
	if !testServer.ReadyForConnections(5 * time.Second) {
		testServer.Shutdown()
		t.Fatal("NATS test server did not become ready")
	}
	t.Cleanup(testServer.Shutdown)
	return testServer.ClientURL()
}

func testNATSConfig(serverURL string) config.NATSConfig {
	return config.NATSConfig{
		Enabled:        true,
		URL:            serverURL,
		ConnectTimeout: time.Second,
		DrainTimeout:   time.Second,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
