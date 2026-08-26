package tunnels

import (
	"context"
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
	registerTestConnector(t, b, "a", []ChannelDeclaration{{Name: "main"}})
	for _, id := range []string{"first", "second"} {
		if err := b.Enqueue(t.Context(), "tunnel", testQueuedCommand(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Enqueue(t.Context(), "tunnel", testQueuedCommand("overflow")); !errors.Is(err, ErrQueueLimit) {
		t.Fatalf("capacity error = %v", err)
	}
	commands := pollTestCommands(t, b, "a", []ChannelDeclaration{{Name: "main"}}, 2)
	if len(commands) == 1 {
		commands = append(commands, pollTestCommands(t, b, "a", []ChannelDeclaration{{Name: "main"}}, 1)...)
	}
	if len(commands) != 2 {
		t.Fatalf("got %d commands", len(commands))
	}
	prefix, suffix := `{"jsonrpc":"2.0","id":1,"result":{"text":"`, `"}}`
	large := json.RawMessage(prefix + strings.Repeat("x", int(cfg.MaxBodyBytes)-len(prefix)-len(suffix)) + suffix)
	for _, command := range commands {
		response := testTerminalResponse(command.RequestID)
		response.JSONResponse = large
		if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, command.ShardToken, response); err != nil {
			t.Fatalf("accepted request lost terminal capacity: %v", err)
		}
	}
}

func TestNATSBrokerRejectsWrongBindingsAndCanceledResults(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	command := testQueuedCommand("binding")
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	claimed := pollTestCommands(t, b, "a", channels, 1)[0]
	for _, binding := range []struct {
		instance string
		version  int64
		shard    string
		channel  string
	}{{"b", 1, claimed.ShardToken, "main"}, {"a", 2, claimed.ShardToken, "main"}, {"a", 1, "wrong", "main"}, {"a", 1, claimed.ShardToken, "other"}} {
		response := testTerminalResponse(command.RequestID)
		response.Channel = binding.channel
		if err := b.SubmitResponse(t.Context(), "tunnel", binding.instance, binding.version, binding.shard, response); !errors.Is(err, ErrResponseMismatch) {
			t.Fatalf("wrong binding accepted: %v", err)
		}
	}
	if err := b.Cancel(t.Context(), "tunnel", command.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, claimed.ShardToken, testTerminalResponse(command.RequestID)); !errors.Is(err, ErrRequestCanceled) {
		t.Fatalf("canceled result accepted: %v", err)
	}
}

func TestNATSBrokerCanceledQueueIsNeverDispatched(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	command := testQueuedCommand("canceled")
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	if err := b.Cancel(t.Context(), "tunnel", command.RequestID); err != nil {
		t.Fatal(err)
	}
	if commands := pollTestCommands(t, b, "a", channels, 1); len(commands) != 0 {
		t.Fatal("canceled request dispatched")
	}
}

func TestNATSBrokerPollSurfacesDeletedConsumer(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	consumers, err := b.pollConsumers(t.Context(), "tunnel", "a", channels)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.commands.DeleteConsumer(t.Context(), consumers[0].CachedInfo().Name); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := b.pollRound(ctx, consumers, "tunnel", "a", 1, 1); err == nil {
		t.Fatal("consumer failure reported as an empty poll")
	}
}

func TestNATSBrokerRevocationWinsBeforeDeliveryPermission(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	registerTestConnector(t, b, "a", []ChannelDeclaration{{Name: "main"}})
	command := testQueuedCommand("suspended")
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	if err := b.SuspendTokenVersion(t.Context(), "tunnel", 1); err != nil {
		t.Fatal(err)
	}
	if err := b.confirmDelivery(t.Context(), "tunnel", "a", 1, command); !errors.Is(err, ErrTokenRetired) {
		t.Fatalf("stale grant = %v", err)
	}
	if err := b.RegisterConnector(t.Context(), "tunnel", "a", 1, []ChannelDeclaration{{Name: "main"}}); !errors.Is(err, ErrTokenRetired) {
		t.Fatalf("registration revived retired token: %v", err)
	}
}

func TestNATSBrokerRedeliveryNeverDispatchesBoundCommand(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	registerTestConnector(t, b, "a", []ChannelDeclaration{{Name: "main"}})
	command := testQueuedCommand("bound")
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	record, revision, err := b.readRequest(t.Context(), "tunnel", command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	record.State, record.InstanceID, record.TokenVersion, record.ShardToken = "dispatched", "a", 1, "first-shard"
	if err := b.requests.update(t.Context(), brokerKey("tunnel", command.RequestID), record, revision, maxBrokerValueBytes); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, b, "a", []ChannelDeclaration{{Name: "main"}}, 1)
	if len(commands) != 0 {
		t.Fatal("bound request was dispatched again")
	}
}

func TestNATSBrokerFetchHandlesDeadlineBeforeContextTimerFires(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	consumers, err := b.pollConsumers(t.Context(), "tunnel", "a", []ChannelDeclaration{{Name: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	deliveries := make(chan pollDelivery, 1)
	ctx := elapsedFetchDeadline{Context: t.Context()}
	if b.fetchPollMessage(ctx, consumers[0], deliveries) {
		t.Fatal("expired fetch should finish the pull round")
	}
	if len(deliveries) != 0 || len(b.prefetchSlots) != 0 {
		t.Fatal("expired pull surfaced a broker error or leaked a prefetch slot")
	}
}

// Simulate deadline expiry just before context's asynchronous timer marks Err.
type elapsedFetchDeadline struct{ context.Context }

func (elapsedFetchDeadline) Deadline() (time.Time, bool) {
	return time.Now().Add(-time.Second), true
}

func TestNATSBrokerMissingRecordDoesNotDiscardLiveCommand(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	command := testQueuedCommand("temporarily-missing")
	data, err := encodeTunnelJSON(command, maxBrokerValueBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.js.Publish(t.Context(), commandSubject("tunnel", "main", ""), data); err != nil {
		t.Fatal(err)
	}
	if commands := pollTestCommands(t, b, "a", channels, 1); len(commands) != 0 {
		t.Fatal("missing request dispatched")
	}
	info, err := b.commands.Info(t.Context())
	if err != nil || info.State.Msgs != 1 {
		t.Fatalf("live command discarded: %#v, %v", info, err)
	}
	record := requestRecord{TunnelUUID: "tunnel", RequestID: command.RequestID, Channel: "main", CommandType: CommandTypeJSONRPC, ExpiresAt: command.ExpiresAt, State: "queued", Origin: b.responseHub.subject}
	if err := b.requests.create(t.Context(), brokerKey("tunnel", command.RequestID), record, maxBrokerValueBytes); err != nil {
		t.Fatal(err)
	}
	if commands := pollTestCommands(t, b, "a", channels, 1); len(commands) != 1 {
		t.Fatalf("restored request not delivered: %d", len(commands))
	}
}

func TestNATSBrokerTerminalRecoveryAndOldTokenResponse(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	command := testQueuedCommand("terminal")
	waiter, err := b.subscribeResponse(t.Context(), "tunnel", command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, b, "a", channels, 1)
	if len(commands) != 1 {
		t.Fatalf("commands = %d", len(commands))
	}
	if err := b.SuspendTokenVersion(t.Context(), "tunnel", 1); err != nil {
		t.Fatal(err)
	}
	if err := b.responseHub.subscription.Unsubscribe(); err != nil {
		t.Fatal(err)
	}
	response := testTerminalResponse(command.RequestID)
	for range 2 {
		if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, commands[0].ShardToken, response); err != nil {
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
	registerTestConnector(t, brokers[0], "a", channels)
	if err := brokers[0].Enqueue(t.Context(), "tunnel", testQueuedCommand("race")); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan int, 2)
	for i, broker := range brokers {
		group.Add(1)
		go func() {
			defer group.Done()
			commands, err := broker.Poll(t.Context(), "tunnel", []string{"a", "b"}[i], 1, channels, 1, 200*time.Millisecond)
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
		MaxPendingRequests: 32, MaxPendingBytes: 32 << 20, MaxStoredRequests: 64, MaxBodyBytes: 1 << 20, MaxHeaderBytes: 32 << 10, MaxHeaderValueBytes: 8 << 10}
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

func registerTestConnector(t *testing.T, b *Broker, instance string, channels []ChannelDeclaration) {
	t.Helper()
	if err := b.RegisterConnector(t.Context(), "tunnel", instance, 1, channels); err != nil {
		t.Fatal(err)
	}
}

func pollTestCommands(t *testing.T, b *Broker, instance string, channels []ChannelDeclaration, limit int) []ClaimedCommand {
	t.Helper()
	commands, err := b.Poll(t.Context(), "tunnel", instance, 1, channels, limit, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return commands
}

func testQueuedCommand(requestID string) queuedCommand {
	return queuedCommand{RequestID: requestID, CommandType: CommandTypeJSONRPC, Channel: "main", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Second), Headers: http.Header{}, JSONRPC: json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), PayloadSize: 100}
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
