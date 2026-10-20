package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func testResponseWaiter(t *testing.T, b *Broker, command queuedCommand) *responseWaiter {
	t.Helper()
	waiter, err := b.subscribeResponse(t.Context(), command.RequestID, command.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(waiter.Close)
	return waiter
}

func TestNATSBrokerMissingAndWrongResponseBindings(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	command := testQueuedCommand("bindings")
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("unclaimed response: %v", err)
	}
	waiter := testResponseWaiter(t, b, command)
	pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
	for _, binding := range []struct {
		tunnel  string
		hash    [32]byte
		channel string
		kind    ResponseType
	}{
		{"other", testTokenHash(), "main", ResponseTypeJSONRPC},
		{"tunnel", [32]byte{1}, "main", ResponseTypeJSONRPC},
		{"tunnel", testTokenHash(), "other", ResponseTypeJSONRPC},
		{"tunnel", testTokenHash(), "main", ResponseTypeOAuth},
	} {
		response := testTerminalResponse(command.RequestID)
		response.Channel, response.ResponseType = binding.channel, binding.kind
		if err := b.SubmitResponse(t.Context(), binding.tunnel, binding.hash, response); !errors.Is(err, ErrResponseMismatch) {
			t.Fatalf("wrong binding: %v", err)
		}
	}
	waiter.Close()
	if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); !errors.Is(err, ErrResponseGone) {
		t.Fatalf("gone waiter: %v", err)
	}
}

func TestNATSBrokerNoACKNeverRedelivers(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	consumers, err := b.pollConsumers(t.Context(), "tunnel", channels)
	if err != nil {
		t.Fatal(err)
	}
	info := consumers[0].CachedInfo()
	if info.Config.MaxDeliver != 1 {
		t.Fatal("redelivery is enabled")
	}
	cfg := info.Config
	cfg.AckWait = 20 * time.Millisecond
	consumer, err := b.commands.UpdateConsumer(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand("no-ack")); err != nil {
		t.Fatal(err)
	}
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if msg := <-batch.Messages(); msg == nil {
		t.Fatal("first delivery missing")
	}
	// Leave the message unacknowledged across multiple AckWait intervals.
	time.Sleep(80 * time.Millisecond)
	for range 2 {
		if commands := pollTestCommands(t, b, channels, 1); len(commands) != 0 {
			t.Fatal("unacknowledged command redelivered")
		}
	}
}

func TestNATSBrokerLocalCancellationDoesNotCancelQueuedCommand(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	command := testQueuedCommand("disconnected-agent")
	waiter := testResponseWaiter(t, b, command)
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	waiter.Close()
	if commands := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1); len(commands) != 1 {
		t.Fatal("local cancellation canceled queued work")
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); !errors.Is(err, ErrResponseGone) {
		t.Fatal(err)
	}
}

func TestNATSBrokerDuplicateBindingCannotOverwriteToken(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	command := testQueuedCommand("duplicate-binding")
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
	command.TunnelID, command.Origin = "tunnel", b.responseHub.subject
	data, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.js.Publish(t.Context(), commandSubject("tunnel", "main"), data); err != nil {
		t.Fatal(err)
	}
	commands, err := b.Poll(t.Context(), "tunnel", [32]byte{1}, []ChannelDeclaration{{Name: "main"}}, 1, 0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("duplicate delivered: %v %v", commands, err)
	}
	record, err := b.readRequestByID(t.Context(), command.RequestID)
	if err != nil || record.TokenHash != testTokenHash() {
		t.Fatal("token binding overwritten")
	}
}

func TestNATSBrokerOriginLossDoesNotPersistFinalResponse(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	command := testQueuedCommand("lost-origin")
	waiter := testResponseWaiter(t, b, command)
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
	if err := b.responseHub.subscription.Unsubscribe(); err != nil {
		t.Fatal(err)
	}
	if err := b.connection.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); !errors.Is(err, nats.ErrNoResponders) {
		t.Fatalf("lost origin: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := waiter.Wait(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected recovery: %v", err)
	}
}

func TestNATSBrokerCrossInstanceResponsesUseImmutableBinding(t *testing.T) {
	a := testNATSBroker(t, brokerTestConfig())
	peers := make([]*Broker, 2)
	for i := range peers {
		peer, err := newBroker(t.Context(), connectTunnelNATS(t, a.connection.ConnectedUrl()), brokerTestConfig(), 1, a.requests)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(peer.Close)
		peers[i] = peer
	}
	command := testQueuedCommand("cross-instance")
	waiter := testResponseWaiter(t, a, command)
	if err := a.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	pollTestCommands(t, peers[0], []ChannelDeclaration{{Name: "main"}}, 1)
	before, err := a.readRequestByID(t.Context(), command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := peers[1].SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := waiter.Wait(t.Context(), nil)
	if err != nil || got.RequestID != command.RequestID {
		t.Fatalf("result: %+v %v", got, err)
	}
	waiter.Close()
	if err := peers[0].SubmitResponse(t.Context(), "tunnel", testTokenHash(), testTerminalResponse(command.RequestID)); err != nil {
		t.Fatalf("duplicate after HTTP completion: %v", err)
	}
	after, err := a.readRequestByID(t.Context(), command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("response mutated binding")
	}
	a.responseHub.mu.Lock()
	receipts, bytes := len(a.responseHub.receipts), a.responseHub.bufferedBytes
	a.responseHub.mu.Unlock()
	if receipts != 1 || bytes != 0 {
		t.Fatalf("receipts=%d retained body bytes=%d", receipts, bytes)
	}
}

func TestNATSBrokerPollHonorsClientLimitAcrossChannels(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}, {Name: "other"}}
	for i := range 40 {
		command := testQueuedCommand(fmt.Sprint(i))
		command.Channel = channels[i%2].Name
		if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
			t.Fatal(err)
		}
	}
	seen := make(map[string]bool)
	for _, limit := range []int{1, 30, int(^uint(0) >> 1)} {
		commands, err := b.Poll(t.Context(), "tunnel", testTokenHash(), channels, limit, 0)
		if err != nil || len(commands) > limit {
			t.Fatalf("limit=%d commands=%d err=%v", limit, len(commands), err)
		}
		if limit == 30 && len(commands) != 30 {
			t.Fatal("server retained old batch limit")
		}
		for _, cmd := range commands {
			if seen[cmd.RequestID] {
				t.Fatal("duplicate delivery")
			}
			seen[cmd.RequestID] = true
		}
	}
	if len(seen) != 40 {
		t.Fatalf("lost commands: %d", len(seen))
	}
}
