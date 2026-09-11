package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNATSResponseBackpressureKeepsAcceptedNotifications(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	command := testQueuedCommand("notifications")
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
		t.Fatal("command not dispatched")
	}
	notification := TunnelResponse{RequestID: command.RequestID, Channel: "main", ResponseType: ResponseTypeJSONRPCNotify, JSONResponse: json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/progress"}`)}
	for range responseSubscriptionBuffer {
		if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, commands[0].ShardToken, notification); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, commands[0].ShardToken, notification); !errors.Is(err, ErrResponseBackpressure) {
		t.Fatalf("full buffer = %v", err)
	}
	if err := b.SubmitResponse(t.Context(), "tunnel", "a", 1, commands[0].ShardToken, testTerminalResponse(command.RequestID)); err != nil {
		t.Fatalf("terminal blocked behind notifications: %v", err)
	}
	seen := 0
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := waiter.Wait(ctx, func(TunnelResponse) { seen++ }); err != nil {
		t.Fatal(err)
	}
	if seen != responseSubscriptionBuffer {
		t.Fatalf("preserved %d accepted notifications", seen)
	}
	if status := b.responseHub.accept(responseEnvelope{Key: waiter.key, Response: &notification}, 100); status != "gone" {
		t.Fatalf("notification accepted after terminal: %s", status)
	}
	if b.responseHub.bufferedBytes != 0 {
		t.Fatalf("buffer leaked %d bytes", b.responseHub.bufferedBytes)
	}
}

func TestNATSResponseSlowWriterStillConsumesGlobalBudget(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	waiter, err := b.subscribeResponse(t.Context(), "tunnel", "slow")
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	notification := &TunnelResponse{ResponseType: ResponseTypeJSONRPCNotify}
	if status := b.responseHub.accept(responseEnvelope{Key: waiter.key, Response: notification}, 100); status != "accepted" {
		t.Fatal(status)
	}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { waiter.drain(false, func(TunnelResponse) { close(started); <-release }); close(finished) }()
	<-started
	b.responseHub.mu.Lock()
	retained := b.responseHub.bufferedBytes
	b.responseHub.mu.Unlock()
	waiter.Close()
	close(release)
	<-finished
	if retained != 100 || b.responseHub.bufferedBytes != 0 {
		t.Fatalf("in-flight budget=%d remaining=%d", retained, b.responseHub.bufferedBytes)
	}
}
