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
	if _, err := NewBroker(t.Context(), nil, brokerTestConfig(), nil); err == nil {
		t.Fatal("accepted nil connection")
	}
	srv := startTunnelNATS(t, server.Options{MaxPayload: 1 << 20})
	if _, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), brokerTestConfig(), 1); err == nil {
		t.Fatal("accepted insufficient max_payload")
	}
}

func TestNATSBrokerConcurrentConnectionsDispatchOnlyOnce(t *testing.T) {
	srv := startTunnelNATS(t, server.Options{})
	brokers := make([]*Broker, 2)
	for i := range brokers {
		broker, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), brokerTestConfig(), 1)
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
		MaxStoredRequests: 64, MaxBodyBytes: 1 << 20, MaxHeaderBytes: 32 << 10, MaxHeaderValueBytes: 8 << 10}
}

func testNATSBroker(t *testing.T, cfg config.TunnelConfig) *Broker {
	t.Helper()
	srv := startTunnelNATS(t, server.Options{})
	broker, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), cfg, 1)
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

func TestNATSBrokerRequestStreamHasAdmissionLimit(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	info, err := b.requests.stream.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.MaxMsgs != 64 || info.Config.MaxMsgsPerSubject != 1 || info.Config.Discard != jetstream.DiscardNew || info.Config.AllowDirect {
		t.Fatalf("request stream = %+v", info.Config)
	}
}

func testTokenHash() [sha256.Size]byte { return sha256.Sum256([]byte("valid-token")) }
