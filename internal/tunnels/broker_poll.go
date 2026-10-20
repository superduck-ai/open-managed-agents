package tunnels

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	pollRoundWait      = 100 * time.Millisecond
	pollFetchBatchSize = 256
)

type pollResult struct {
	commands []ClaimedCommand
	err      error
}

func (b *Broker) Poll(ctx context.Context, tunnelUUID string, tokenHash [sha256.Size]byte, channels []ChannelDeclaration, limit int, timeout time.Duration) ([]ClaimedCommand, error) {
	if err := validateBrokerChannels(channels); err != nil {
		return nil, err
	}
	if limit < 1 {
		return nil, errPollLimitInvalid
	}
	consumers, err := b.pollConsumers(ctx, tunnelUUID, channels)
	if err != nil {
		return nil, err
	}
	// Rotate the first route without retaining per-Tunnel scheduling state.
	start := int((b.pollCursor.Add(1) - 1) % uint64(len(consumers)))
	consumers = append(consumers[start:], consumers[:start]...)
	pollCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	commands, err := b.pollCommands(pollCtx, consumers, tunnelUUID, tokenHash, limit, timeout <= 0)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(commands) > 0 {
		return commands, nil
	}
	if errors.Is(err, context.DeadlineExceeded) && pollCtx.Err() != nil {
		return nil, nil
	}
	return commands, err
}

func (b *Broker) pollConsumers(ctx context.Context, tunnelUUID string, channels []ChannelDeclaration) ([]jetstream.Consumer, error) {
	consumers := make([]jetstream.Consumer, 0, len(channels))
	for _, channel := range channels {
		subject := commandSubject(tunnelUUID, channel.Name)
		name := brokerKey(subject)
		consumer, err := b.commands.Consumer(ctx, name)
		if errors.Is(err, jetstream.ErrConsumerNotFound) {
			consumer, err = b.commands.CreateConsumer(ctx, jetstream.ConsumerConfig{
				Durable: name, FilterSubject: subject, AckPolicy: jetstream.AckExplicitPolicy,
				MaxDeliver: 1, AckWait: 5 * time.Second, MaxAckPending: 32, MaxWaiting: 128,
				InactiveThreshold: b.cfg.RequestTimeout + time.Minute, MaxRequestExpires: time.Second,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("open tunnel consumer: %w", err)
		}
		consumers = append(consumers, consumer)
	}
	return consumers, nil
}

func (b *Broker) pollCommands(ctx context.Context, consumers []jetstream.Consumer, tunnelUUID string, tokenHash [sha256.Size]byte, limit int, noWait bool) ([]ClaimedCommand, error) {
	commands := make([]ClaimedCommand, 0, min(limit, len(consumers)))
	var firstErr error
	for _, consumer := range consumers {
		if ctx.Err() != nil {
			return commands, ctx.Err()
		}
		if len(commands) == limit {
			break
		}
		// Bound SDK preallocation for a single fetch, independently of queue size
		// and the number of in-flight requests.
		batch, err := consumer.FetchNoWait(min(limit-len(commands), pollFetchBatchSize))
		if err == nil {
			result := b.collectPollBatch(ctx, tunnelUUID, tokenHash, batch)
			commands = append(commands, result.commands...)
			err = result.err
		}
		if err != nil {
			firstErr = err
		}
	}
	if len(commands) > 0 || firstErr != nil || noWait {
		return commands, firstErr
	}
	for start := 0; ctx.Err() == nil; {
		count := min(limit, len(consumers))
		result := b.pollRound(ctx, consumers, start, count, tunnelUUID, tokenHash)
		if len(result.commands) > 0 || result.err != nil {
			return result.commands, result.err
		}
		start = (start + count) % len(consumers)
	}
	return nil, ctx.Err()
}

// A finite round reserves one slot per pull before requesting any messages.
// All pulls settle before returning: no ready delivery is abandoned for a
// different channel's first result, and no worker starts another pull.
func (b *Broker) pollRound(ctx context.Context, consumers []jetstream.Consumer, start, count int, tunnelUUID string, tokenHash [sha256.Size]byte) pollResult {
	wait := pollRoundWait
	if deadline, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(deadline))
	}
	if wait <= 0 {
		return pollResult{err: context.DeadlineExceeded}
	}
	results := make(chan pollResult, count)
	for i := range count {
		consumer := consumers[(start+i)%len(consumers)]
		go func() {
			batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(wait))
			if err != nil {
				results <- pollResult{err: err}
				return
			}
			results <- b.collectPollBatch(ctx, tunnelUUID, tokenHash, batch)
		}()
	}
	var result pollResult
	for range count {
		next := <-results
		result.commands = append(result.commands, next.commands...)
		if next.err != nil {
			result.err = next.err
		}
	}
	return result
}

// Drain the finite SDK batch even on HTTP cancellation, terminating late
// deliveries instead of leaving a background prefetcher or requesting redelivery.
func (b *Broker) collectPollBatch(ctx context.Context, tunnelUUID string, tokenHash [sha256.Size]byte, batch jetstream.MessageBatch) pollResult {
	var result pollResult
	for message := range batch.Messages() {
		if err := ctx.Err(); err != nil {
			_ = message.Term()
			result.err = err
			continue
		}
		command, err := b.bindPollMessage(ctx, tunnelUUID, tokenHash, message)
		if err != nil {
			result.err = err
		}
		if command != nil {
			result.commands = append(result.commands, *command)
		}
	}
	if err := batch.Error(); err != nil {
		result.err = err
	}
	if ctx.Err() != nil {
		result.err = ctx.Err()
	}
	if !b.connection.IsConnected() && result.err == nil {
		result.err = nats.ErrDisconnected
	}
	return result
}

func (b *Broker) bindPollMessage(ctx context.Context, tunnelUUID string, tokenHash [sha256.Size]byte, message jetstream.Msg) (*ClaimedCommand, error) {
	var command queuedCommand
	if err := json.Unmarshal(message.Data(), &command); err != nil {
		_ = message.Term()
		return nil, fmt.Errorf("decode tunnel command: %w", err)
	}
	if message.Subject() != commandSubject(tunnelUUID, command.Channel) || command.Origin == "" || command.TunnelID == "" {
		_ = message.Term()
		return nil, ErrResponseMismatch
	}
	if !b.now().Before(command.ExpiresAt) {
		return nil, message.Term()
	}
	record := requestRecord{Scope: command.Scope, TunnelID: command.TunnelID, TokenHash: tokenHash, Channel: command.Channel,
		CommandType: command.CommandType, ExpiresAt: command.ExpiresAt, Origin: command.Origin}
	if err := b.requests.create(ctx, brokerKey(command.RequestID), record); err != nil {
		_ = message.Term()
		if errors.Is(err, errRequestBindingExists) {
			return nil, nil
		}
		return nil, err
	}
	if err := message.Ack(); err != nil {
		return nil, err
	}
	restoreCtx, cancel := context.WithDeadline(ctx, command.ExpiresAt)
	defer cancel()
	body, err := b.payloads.restore(restoreCtx, command.Scope, command.RequestID, command.JSONRPC, command.PayloadRef, b.cfg.MaxBodyBytes)
	if err != nil {
		return nil, err
	}
	command.JSONRPC = body
	remaining := command.ExpiresAt.Sub(b.now())
	if remaining <= 0 {
		return nil, nil
	}
	return &ClaimedCommand{RequestID: command.RequestID, CommandType: command.CommandType, Channel: command.Channel,
		CreatedAt: command.CreatedAt, Headers: command.Headers, JSONRPC: command.JSONRPC, ResponseTimeout: remaining, expiresAt: command.ExpiresAt}, nil
}
