package tunnels

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestNATSBrokerRejectsMissingJetStreamAndSmallPayload(t *testing.T) {
	if _, err := NewBroker(t.Context(), nil, brokerTestConfig(), nil, testRequestBindings(t)); err == nil {
		t.Fatal("accepted nil connection")
	}
	srv := startTunnelNATS(t, server.Options{MaxPayload: 1 << 20})
	if _, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), brokerTestConfig(), 1, testRequestBindings(t)); err == nil {
		t.Fatal("accepted insufficient max_payload")
	}
}

func TestNATSBrokerConcurrentConnectionsDispatchOnlyOnce(t *testing.T) {
	srv := startTunnelNATS(t, server.Options{})
	bindings := testRequestBindings(t)
	brokers := make([]*Broker, 2)
	for i := range brokers {
		broker, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), brokerTestConfig(), 1, bindings)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(broker.Close)
		brokers[i] = broker
	}
	channels := []ChannelDeclaration{{Name: "main"}}

	if err := brokers[0].Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand("race")); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan int, 2)
	for _, broker := range brokers {
		group.Add(1)
		go func() {
			defer group.Done()
			commands, err := broker.Poll(t.Context(), "tunnel", testTokenHash(), channels, 1, 200*time.Millisecond)
			if err != nil {
				t.Error(err)
			}
			results <- len(commands)
		}()
	}
	group.Wait()
	if count := <-results + <-results; count != 1 {
		t.Fatalf("deliveries = %d", count)
	}
}

func brokerTestConfig() config.TunnelConfig {
	return config.TunnelConfig{RequestTimeout: time.Minute, PollTimeout: time.Second, PresenceTTL: time.Minute, TombstoneTTL: time.Minute,
		CommandStream: config.TunnelCommandStreamConfig{MaxBytes: 513 << 20, MaxMsgs: -1},
		MaxBodyBytes:  1 << 20, MaxHeaderBytes: 32 << 10, MaxHeaderValueBytes: 8 << 10}
}

func testNATSBroker(t *testing.T, cfg config.TunnelConfig) *Broker {
	t.Helper()
	srv := startTunnelNATS(t, server.Options{})
	broker, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), cfg, 1, testRequestBindingsWithTTL(t, brokerRequestRetention(cfg.RequestTimeout, cfg.TombstoneTTL)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	return broker
}

func startTunnelNATS(t *testing.T, opts server.Options) *server.Server {
	t.Helper()
	opts.Host, opts.Port, opts.JetStream, opts.StoreDir, opts.NoLog, opts.NoSigs = "127.0.0.1", -1, true, t.TempDir(), true, true
	if opts.MaxPayload == 0 {
		opts.MaxPayload = maxBrokerValueBytes
	}
	srv, err := server.NewServer(&opts)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS failed to start")
	}
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	return srv
}

func connectTunnelNATS(t *testing.T, url string) *nats.Conn {
	t.Helper()
	connection, err := nats.Connect(url, nats.ReconnectBufSize(-1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	return connection
}

func pollTestCommands(t *testing.T, b *Broker, channels []ChannelDeclaration, limit int) []ClaimedCommand {
	t.Helper()
	commands, err := b.Poll(t.Context(), "tunnel", testTokenHash(), channels, limit, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return commands
}

func testQueuedCommand(requestID string) queuedCommand {
	return queuedCommand{RequestID: requestID, CommandType: CommandTypeJSONRPC, Channel: "main", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Second), Headers: http.Header{}, JSONRPC: json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)}
}

func testTerminalResponse(requestID string) TunnelResponse {
	return TunnelResponse{RequestID: requestID, Channel: "main", ResponseType: ResponseTypeJSONRPC, ResponseCode: 200, JSONResponse: json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{}}`)}
}

func TestNATSBrokerAppliesCommandStreamLimits(t *testing.T) {
	cfg := brokerTestConfig()
	cfg.CommandStream = config.TunnelCommandStreamConfig{MaxBytes: 4 << 20, MaxMsgs: 10}
	b := testNATSBroker(t, cfg)
	info, err := b.commands.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.MaxMsgs != cfg.CommandStream.MaxMsgs || info.Config.MaxBytes != cfg.CommandStream.MaxBytes || info.Config.Discard != jetstream.DiscardNew {
		t.Fatalf("request stream = %+v", info.Config)
	}
}

func TestNATSBrokerRestartUpdatesExistingStream(t *testing.T) {
	srv := startTunnelNATS(t, server.Options{})
	bindings := testRequestBindings(t)
	cfg := brokerTestConfig()
	cfg.CommandStream = config.TunnelCommandStreamConfig{MaxBytes: 4 << 20, MaxMsgs: 10}
	broker, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), cfg, 1, bindings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	channels := []ChannelDeclaration{{Name: "main"}}
	if _, err := broker.Poll(t.Context(), "tunnel", testTokenHash(), channels, 1, 0); err != nil {
		t.Fatal(err)
	}
	info, err := broker.commands.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	previous := info.Config
	previous.MaxBytes = 2 * cfg.CommandStream.MaxBytes
	previous.MaxMsgSize = 2 * maxBrokerValueBytes
	previous.MaxMsgs = 4096
	previous.MaxAge = 2 * cfg.RequestTimeout
	previous.Duplicates = 2 * cfg.RequestTimeout
	if _, err := broker.js.UpdateStream(t.Context(), previous); err != nil {
		t.Fatal(err)
	}
	command := testQueuedCommand("before-restart")
	if err := broker.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	broker.Close()

	restarted, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), cfg, 1, bindings)
	if err != nil {
		t.Fatalf("restart with existing stream: %v", err)
	}
	t.Cleanup(restarted.Close)
	info, err = restarted.commands.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.MaxBytes != cfg.CommandStream.MaxBytes || info.Config.MaxMsgSize != maxBrokerValueBytes || info.Config.MaxMsgs != cfg.CommandStream.MaxMsgs || info.Config.MaxAge != cfg.RequestTimeout || info.Config.Duplicates != cfg.RequestTimeout {
		t.Fatalf("stream configuration after restart = %+v", info.Config)
	}
	if info.State.Msgs != 1 || info.State.Consumers != 1 {
		t.Fatalf("stream state after restart = %+v", info.State)
	}
	commands := pollTestCommands(t, restarted, channels, 1)
	if len(commands) != 1 || commands[0].RequestID != command.RequestID {
		t.Fatalf("pending command after restart = %+v", commands)
	}
}

func testTokenHash() [sha256.Size]byte { return sha256.Sum256([]byte("valid-token")) }
