package tunnels

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestNATSBrokerRejectsMissingJetStreamAndSmallPayload(t *testing.T) {
	if _, err := NewBroker(t.Context(), nil, brokerTestConfig()); err == nil {
		t.Fatal("accepted nil connection")
	}
	srv := startTunnelNATS(t, server.Options{MaxPayload: 1 << 20})
	if _, err := newBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), brokerTestConfig(), 1); err == nil {
		t.Fatal("accepted insufficient max_payload")
	}
}

func TestNATSBrokerCapacityReservesLargestTerminal(t *testing.T) {
	cfg := brokerTestConfig()
	cfg.MaxStoredRequests = 2
	b := testNATSBroker(t, cfg)

	for _, id := range []string{"first", "second"} {
		if err := b.Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand("overflow")); !errors.Is(err, ErrQueueLimit) {
		t.Fatalf("capacity error = %v", err)
	}
	commands := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 2)
	if len(commands) == 1 {
		commands = append(commands, pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)...)
	}
	if len(commands) != 2 {
		t.Fatalf("got %d commands", len(commands))
	}
	prefix, suffix := `{"jsonrpc":"2.0","id":1,"result":{"text":"`, `"}}`
	large := json.RawMessage(prefix + strings.Repeat("x", int(cfg.MaxBodyBytes)-len(prefix)-len(suffix)) + suffix)
	for _, command := range commands {
		response := testTerminalResponse(command.RequestID)
		response.JSONResponse = large
		if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); err != nil {
			t.Fatalf("accepted request lost terminal capacity: %v", err)
		}
	}
}

func TestNATSBrokerRejectsWrongBindingsAndCanceledResults(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}

	command := testQueuedCommand("binding")
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	pollTestCommands(t, b, channels, 1)
	for _, binding := range []struct {
		tunnel  string
		hash    [32]byte
		channel string
	}{
		{"other", testTokenHash(), "main"}, {"tunnel", [32]byte{1}, "main"}, {"tunnel", testTokenHash(), "other"},
	} {
		response := testTerminalResponse(command.RequestID)
		response.Channel = binding.channel
		if err := b.SubmitResponse(t.Context(), binding.tunnel, binding.hash, response); !errors.Is(err, ErrResponseMismatch) {
			t.Fatalf("wrong binding accepted: %v", err)
		}
	}
	if err := b.Cancel(t.Context(), "tunnel", command.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); !errors.Is(err, ErrRequestCanceled) {
		t.Fatalf("canceled result accepted: %v", err)
	}
}

func TestNATSBrokerCanceledQueueIsNeverDispatched(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}

	command := testQueuedCommand("canceled")
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	if err := b.Cancel(t.Context(), "tunnel", command.RequestID); err != nil {
		t.Fatal(err)
	}
	if commands := pollTestCommands(t, b, channels, 1); len(commands) != 0 {
		t.Fatal("canceled request dispatched")
	}
}

func TestNATSBrokerPollSurfacesDeletedConsumer(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	consumers, err := b.pollConsumers(t.Context(), "tunnel", channels)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.commands.DeleteConsumer(t.Context(), consumers[0].CachedInfo().Name); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := b.pollCommands(ctx, consumers, "tunnel", testTokenHash(), 1, false); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("consumer failure reported as an empty poll")
	}
}

func TestNATSBrokerRedeliveryNeverDispatchesBoundCommand(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())

	command := testQueuedCommand("bound")
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	record, revision, err := b.readRequest(t.Context(), "tunnel", command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	record.State, record.TokenHash = "dispatched", testTokenHash()
	if err := b.requests.update(t.Context(), brokerKey(command.RequestID), record, revision, maxBrokerValueBytes); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
	if len(commands) != 0 {
		t.Fatal("bound request was dispatched again")
	}
}

func TestNATSBrokerMissingRecordDoesNotDiscardLiveCommand(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}

	command := testQueuedCommand("temporarily-missing")
	data, err := encodeTunnelJSON(command, maxBrokerValueBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.js.Publish(t.Context(), commandSubject("tunnel", "main"), data); err != nil {
		t.Fatal(err)
	}
	if commands := pollTestCommands(t, b, channels, 1); len(commands) != 0 {
		t.Fatal("missing request dispatched")
	}
	info, err := b.commands.Info(t.Context())
	if err != nil || info.State.Msgs != 1 {
		t.Fatalf("live command discarded: %#v, %v", info, err)
	}
	record := requestRecord{TunnelUUID: "tunnel", RequestID: command.RequestID, Channel: "main", CommandType: CommandTypeJSONRPC, ExpiresAt: command.ExpiresAt, State: "queued", Origin: b.responseHub.subject}
	if err := b.requests.create(t.Context(), brokerKey(command.RequestID), record, maxBrokerValueBytes); err != nil {
		t.Fatal(err)
	}
	if commands := pollTestCommands(t, b, channels, 1); len(commands) != 1 {
		t.Fatalf("restored request not delivered: %d", len(commands))
	}
}

func TestNATSBrokerTerminalRecoveryAndOldTokenResponse(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}

	command := testQueuedCommand("terminal")
	waiter, err := b.subscribeResponse(t.Context(), "tunnel", command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, b, channels, 1)
	if len(commands) != 1 {
		t.Fatalf("commands = %d", len(commands))
	}
	if err := b.responseHub.subscription.Unsubscribe(); err != nil {
		t.Fatal(err)
	}
	response := testTerminalResponse(command.RequestID)
	for range 2 {
		if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Cancel(t.Context(), "tunnel", command.RequestID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	got, err := waiter.Wait(ctx, nil)
	if err != nil || string(got.JSONResponse) != string(response.JSONResponse) {
		t.Fatalf("durable recovery = %+v, %v", got, err)
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
