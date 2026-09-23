package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestPollACKFailureDoesNotDeliverCommand(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	command := testQueuedCommand("ack-failure")
	command.TunnelID, command.Origin = "tunnel", b.responseHub.subject
	data, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	message := &failedACKMessage{data: data, subject: commandSubject("tunnel", "main")}
	result, err := b.bindPollMessage(t.Context(), "tunnel", testTokenHash(), message)
	if !errors.Is(err, nats.ErrDisconnected) || result != nil {
		t.Fatalf("ack failure delivered: %v %v", result, err)
	}
	if _, err := b.readRequestByID(t.Context(), command.RequestID); err != nil {
		t.Fatalf("binding was not created before ack: %v", err)
	}
}

type failedACKMessage struct {
	jetstream.Msg
	data    []byte
	subject string
}

func (m *failedACKMessage) Data() []byte    { return m.data }
func (m *failedACKMessage) Subject() string { return m.subject }
func (m *failedACKMessage) Ack() error      { return nats.ErrDisconnected }

func TestCanceledPollSettlesLateMessagesWithoutRequeue(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	batch := &latePollBatch{messages: make(chan jetstream.Msg)}
	done := make(chan pollResult, 1)
	go func() { done <- b.collectPollBatch(ctx, "tunnel", testTokenHash(), batch) }()
	msg := &terminatedPollMessage{terminated: make(chan struct{})}
	batch.messages <- msg
	close(batch.messages)
	result := <-done
	if !errors.Is(result.err, context.Canceled) || len(result.commands) != 0 {
		t.Fatalf("canceled collection: %+v", result)
	}
	select {
	case <-msg.terminated:
	default:
		t.Fatal("late message was not terminated")
	}
}

type latePollBatch struct{ messages chan jetstream.Msg }

func (b *latePollBatch) Messages() <-chan jetstream.Msg { return b.messages }
func (b *latePollBatch) Error() error                   { return nil }

type terminatedPollMessage struct {
	jetstream.Msg
	terminated chan struct{}
}

func (m *terminatedPollMessage) Term() error { close(m.terminated); return nil }

func TestPollExpiredCommandNeverCreatesBinding(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	command := testQueuedCommand("expired-command")
	if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
		t.Fatal(err)
	}
	b.now = func() time.Time { return command.ExpiresAt.Add(time.Second) }
	if cmds := pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1); len(cmds) != 0 {
		t.Fatal("expired command delivered")
	}
	if _, err := b.readRequestByID(t.Context(), command.RequestID); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("expired binding: %v", err)
	}
}

func TestPollFiniteRoundsRespectSlotsAndStop(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	channels := []ChannelDeclaration{{Name: "main"}, {Name: "other"}}
	consumers, err := b.pollConsumers(t.Context(), "tunnel", channels)
	if err != nil {
		t.Fatal(err)
	}
	pulls := observePollPulls(t, b)
	done := make(chan pollResult, 1)
	go func() { done <- b.pollRound(t.Context(), consumers, 0, 2, "tunnel", testTokenHash()) }()
	for range 2 {
		if _, err := pulls.NextMsg(time.Second); err != nil {
			t.Fatal(err)
		}
	}
	for _, channel := range channels {
		command := testQueuedCommand(channel.Name)
		command.Channel = channel.Name
		if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
			t.Fatal(err)
		}
	}
	result := <-done
	if result.err != nil || len(result.commands) != 2 {
		t.Fatalf("round lost a delivery: %+v", result)
	}
	if _, err := pulls.NextMsg(150 * time.Millisecond); !errors.Is(err, nats.ErrTimeout) {
		t.Fatalf("prefetch continued: %v", err)
	}
}

func TestPollCancellationAndConnectionLossAreBounded(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	pulls := observePollPulls(t, b)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := b.Poll(ctx, "tunnel", testTokenHash(), []ChannelDeclaration{{Name: "main"}}, 1, time.Minute)
		done <- err
	}()
	if _, err := pulls.NextMsg(time.Second); err != nil {
		t.Fatal(err)
	}
	b.connection.Close()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, nats.ErrDisconnected) {
			t.Fatalf("cancellation = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finite pulls did not settle")
	}
}

func TestLocalCompletionReceiptExpiresAndDoesNotSurviveRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "expiry", true: "restart"}[restart], func(t *testing.T) {
			cfg := brokerTestConfig()
			cfg.TombstoneTTL = 50 * time.Millisecond
			b := testNATSBroker(t, cfg)
			command := testQueuedCommand("receipt")
			command.ExpiresAt = time.Now().Add(time.Second)
			waiter := testResponseWaiter(t, b, command)
			if err := b.Enqueue(t.Context(), "tunnel", "tunnel", command); err != nil {
				t.Fatal(err)
			}
			pollTestCommands(t, b, []ChannelDeclaration{{Name: "main"}}, 1)
			response := testTerminalResponse(command.RequestID)
			if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); err != nil {
				t.Fatal(err)
			}
			if _, err := waiter.Wait(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			waiter.Close()
			if restart {
				b.Close()
				fresh, err := newBroker(t.Context(), b.connection, cfg, 1)
				if err != nil {
					t.Fatal(err)
				}
				defer fresh.Close()
				if err := fresh.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); !errors.Is(err, nats.ErrNoResponders) {
					t.Fatalf("receipt survived restart: %v", err)
				}
			} else {
				waitPollCondition(t, func() bool {
					b.responseHub.mu.Lock()
					defer b.responseHub.mu.Unlock()
					return len(b.responseHub.receipts) == 0
				})
				if err := b.SubmitResponse(t.Context(), "tunnel", testTokenHash(), response); !errors.Is(err, ErrResponseGone) {
					t.Fatalf("expired receipt = %v", err)
				}
			}
		})
	}
}

func TestResponseWaitNeverPollsRequestKV(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	waiter := testResponseWaiter(t, b, testQueuedCommand("no-kv-polling"))
	reads, err := b.connection.SubscribeSync("$JS.API.STREAM.MSG.GET.KV_" + requestBucketName)
	if err != nil {
		t.Fatal(err)
	}
	defer reads.Unsubscribe()
	if err := b.connection.Flush(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if _, err := waiter.Wait(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := reads.NextMsg(20 * time.Millisecond); !errors.Is(err, nats.ErrTimeout) {
		t.Fatalf("waiter queried request KV: %v", err)
	}
}
