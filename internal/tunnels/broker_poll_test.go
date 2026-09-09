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
	"github.com/nats-io/nats.go/jetstream"
)

func TestPollBatchDoesNotWaitForAnotherDelivery(t *testing.T) {
	deliveries := make(chan pollDelivery, 1)
	if _, open := nextPollDelivery(t.Context(), deliveries, nil, false); open {
		t.Fatal("empty buffer produced a delivery")
	}
	expected := errors.New("ready delivery")
	deliveries <- pollDelivery{err: expected}
	delivery, open := nextPollDelivery(t.Context(), deliveries, nil, false)
	if !open || !errors.Is(delivery.err, expected) {
		t.Fatalf("ready delivery lost: %+v, %v", delivery, open)
	}
}

func TestNATSBrokerCanceledPollReturnsUnboundMessages(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	command := testQueuedCommand("unbound")
	if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	consumers, err := b.pollConsumers(t.Context(), "tunnel", "a", channels)
	if err != nil {
		t.Fatal(err)
	}
	for _, noWait := range []bool{false, true} {
		t.Run(fmt.Sprintf("no_wait_%v", noWait), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				deliveries := make(chan pollDelivery) // Deliberately leave the fetched message unbound.
				if noWait {
					fetchAvailablePollMessages(ctx, consumers[0], deliveries)
				} else {
					consumePollChannel(ctx, consumers[0], deliveries)
				}
			}()
			waitPollCondition(t, func() bool {
				info, err := consumers[0].Info(t.Context())
				return err == nil && info.NumAckPending == 1
			})
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("canceled consumer did not stop")
			}
			record, _, err := b.readRequest(t.Context(), "tunnel", command.RequestID)
			if err != nil || record.State != "queued" {
				t.Fatalf("unbound state: %+v, %v", record, err)
			}
			// NAK should make the message available before the five-second AckWait.
			batch, err := consumers[0].Fetch(1, jetstream.FetchMaxWait(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			message := <-batch.Messages()
			if message == nil {
				t.Fatalf("unbound message was not returned: %v", batch.Error())
			}
			if noWait {
				_ = message.Ack()
			} else {
				_ = message.Nak()
			}
		})
	}
}

func TestNATSBrokerPollCancellationDoesNotWaitForNATSDrain(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	pulls := observePollPulls(t, b)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := b.Poll(ctx, "tunnel", "a", 1, channels, 25, time.Minute)
		done <- err
	}()
	if _, err := pulls.NextMsg(time.Second); err != nil {
		t.Fatal(err)
	}
	// A closed connection must not leave HTTP cancellation waiting on drain.
	b.connection.Close()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("connection failure returned an empty successful poll")
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation waited for NATS cleanup")
	}
}

func TestFetchAvailablePollCancellationReturnsLateMessage(t *testing.T) {
	batch := &delayedPollBatch{messages: make(chan jetstream.Msg, 1)}
	consumer := &delayedPollConsumer{batch: batch, started: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := fetchAvailablePollMessage(ctx, consumer); done <- err }()
	<-consumer.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	message := &returnedPollMessage{returned: make(chan struct{})}
	batch.messages <- message
	close(batch.messages)
	select {
	case <-message.returned:
	case <-time.After(time.Second):
		t.Fatal("late unbound delivery was not returned")
	}
}

type delayedPollConsumer struct {
	jetstream.Consumer
	batch   jetstream.MessageBatch
	started chan struct{}
}

func (c *delayedPollConsumer) FetchNoWait(int) (jetstream.MessageBatch, error) {
	close(c.started)
	return c.batch, nil
}

type delayedPollBatch struct{ messages chan jetstream.Msg }

func (b *delayedPollBatch) Messages() <-chan jetstream.Msg { return b.messages }
func (b *delayedPollBatch) Error() error                   { return nil }

type returnedPollMessage struct {
	jetstream.Msg
	returned chan struct{}
}

func (m *returnedPollMessage) NakWithDelay(time.Duration) error { close(m.returned); return nil }

func TestNATSBrokerZeroTimeoutOnlyFetchesAvailableWork(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued_%v", queued), func(t *testing.T) {
			b := testNATSBroker(t, brokerTestConfig())
			channels := []ChannelDeclaration{{Name: "main"}}
			registerTestConnector(t, b, "a", channels)
			pulls := observePollPulls(t, b)
			if queued {
				if err := b.Enqueue(t.Context(), "tunnel", testQueuedCommand("available")); err != nil {
					t.Fatal(err)
				}
			}
			commands, err := b.Poll(t.Context(), "tunnel", "a", 1, channels, 25, 0)
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

func TestNATSBrokerPollKeepsPullAliveUntilDelivery(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, b, "a", channels)
	pulls := observePollPulls(t, b)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	results := make(chan error, 1)
	go func() {
		commands, err := b.Poll(ctx, "tunnel", "a", 1, channels, 25, 2*time.Second)
		if err == nil && (len(commands) != 1 || commands[0].RequestID != "late") {
			err = fmt.Errorf("unexpected commands: %+v", commands)
		}
		results <- err
	}()
	if _, err := pulls.NextMsg(time.Second); err != nil {
		t.Fatal(err)
	}
	// An idle Poll must not recreate its pull every 100ms.
	if _, err := pulls.NextMsg(350 * time.Millisecond); !errors.Is(err, nats.ErrTimeout) {
		t.Fatalf("idle pull was restarted: %v", err)
	}
	if err := b.Enqueue(t.Context(), "tunnel", testQueuedCommand("late")); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
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
			commands, err := b.Poll(ctx, tunnel, "a", 1, channels, 1, 15*time.Second)
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
	if err := b.Enqueue(t.Context(), "tunnel-139", testQueuedCommand("beyond-old-limit")); err != nil {
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

func TestNATSBrokerPollBatchBytesReturnExcessWork(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}, {Name: "other"}}
	registerTestConnector(t, b, "a", channels)
	for _, channel := range channels {
		command := testQueuedCommand(channel.Name)
		command.Channel = channel.Name
		command.JSONRPC = json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"text":"` + strings.Repeat("x", 1200*1024) + `"}}`)
		if err := b.Enqueue(t.Context(), "tunnel", command); err != nil {
			t.Fatal(err)
		}
	}
	seen := make(map[string]bool)
	for range 2 {
		commands, err := b.Poll(t.Context(), "tunnel", "a", 1, channels, 25, time.Second)
		if err != nil || len(commands) != 1 {
			t.Fatalf("oversize batch: count=%d err=%v", len(commands), err)
		}
		if seen[commands[0].RequestID] {
			t.Fatal("command was dispatched twice")
		}
		seen[commands[0].RequestID] = true
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
