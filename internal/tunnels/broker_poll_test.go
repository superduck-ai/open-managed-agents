package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestNATSBrokerZeroTimeoutOnlyFetchesAvailableWork(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued_%v", queued), func(t *testing.T) {
			b := testNATSBroker(t, brokerTestConfig())
			channels := []ChannelDeclaration{{Name: "main"}}

			pulls := observePollPulls(t, b)
			if queued {
				if err := b.Enqueue(t.Context(), "tunnel", "tunnel", testQueuedCommand("available")); err != nil {
					t.Fatal(err)
				}
			}
			commands, err := b.Poll(t.Context(), "tunnel", testTokenHash(), channels, 25, 0)
			if err != nil {
				t.Fatal(err)
			}
			if (len(commands) == 1) != queued {
				t.Fatalf("queued=%v commands=%d", queued, len(commands))
			}
			request, err := pulls.NextMsg(time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var options struct {
				NoWait bool `json:"no_wait"`
			}
			if err := json.Unmarshal(request.Data, &options); err != nil {
				t.Fatal(err)
			}
			if !options.NoWait {
				t.Fatal("zero timeout waited for future commands")
			}
		})
	}
}

func TestNATSBrokerPollSupportsMoreThan128WaitingTunnels(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	pulls := observePollPulls(t, b)
	baseline := b.connection.NumSubscriptions()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const count = 140
	type pollResult struct {
		index    int
		commands []ClaimedCommand
		err      error
	}
	results := make(chan pollResult, count)
	for i := range count {
		tunnel := fmt.Sprintf("tunnel-%d", i)
		go func() {
			commands, err := b.Poll(ctx, tunnel, testTokenHash(), channels, 1, 15*time.Second)
			results <- pollResult{i, commands, err}
		}()
	}
	// Observe distinct routes so a repeated pull cannot hide a starved Tunnel.
	routes := make(map[string]bool)
	for len(routes) < count {
		request, err := pulls.NextMsg(5 * time.Second)
		if err != nil {
			t.Fatalf("only %d/%d tunnels were consuming: %v", len(routes), count, err)
		}
		routes[request.Subject] = true
	}
	if err := b.Enqueue(t.Context(), "tunnel-139", "tunnel-139", testQueuedCommand("beyond-old-limit")); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-results:
		if result.err != nil || result.index != count-1 || len(result.commands) != 1 {
			t.Fatalf("last tunnel failed: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("last tunnel did not receive its command")
	}
	cancel()
	for range count - 1 {
		result := <-results
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("waiting poll returned %v", result.err)
		}
	}
	waitPollCondition(t, func() bool { return b.connection.NumSubscriptions() == baseline })
}

func TestNATSBrokerPollReturnsAllFetchedBytes(t *testing.T) {
	cfg := brokerTestConfig()
	cfg.MaxBodyBytes = 16 << 20
	b := testNATSBroker(t, cfg)
	channels := []ChannelDeclaration{{Name: "main"}, {Name: "other"}}
	for _, channel := range channels {
		command := testQueuedCommand(channel.Name)
		command.Channel = channel.Name
		command.JSONRPC = json.RawMessage(`{"text":"` + strings.Repeat("x", 1200*1024) + `"}`)
		if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
			t.Fatal(err)
		}
	}
	commands, err := b.Poll(t.Context(), "tunnel", testTokenHash(), channels, 25, time.Second)
	if err != nil || len(commands) != 2 {
		t.Fatalf("commands=%d err=%v", len(commands), err)
	}
}

func observePollPulls(t *testing.T, b *Broker) *nats.Subscription {
	t.Helper()
	sub, err := b.connection.SubscribeSync("$JS.API.CONSUMER.MSG.NEXT.>")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := b.connection.Flush(); err != nil {
		t.Fatal(err)
	}
	return sub
}

func waitPollCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("poll condition did not become true")
}
