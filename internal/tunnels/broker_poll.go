package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const brokerRedeliveryDelay = 100 * time.Millisecond

type pollDelivery struct {
	message jetstream.Msg
	err     error
}

func (b *Broker) Poll(ctx context.Context, tunnelUUID, instanceID string, version int64, channels []ChannelDeclaration, limit int, timeout time.Duration) ([]ClaimedCommand, error) {
	if err := validateBrokerChannels(channels); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxPollLimit {
		return nil, ErrQueueLimit
	}
	if err := b.RegisterConnector(ctx, tunnelUUID, instanceID, version, channels); err != nil {
		return nil, err
	}
	consumers, err := b.pollConsumers(ctx, tunnelUUID, instanceID, channels)
	if err != nil {
		return nil, err
	}
	pollCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	commands, err := b.pollCommands(pollCtx, consumers, tunnelUUID, instanceID, version, limit, timeout <= 0)
	if len(commands) > 0 {
		return commands, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) && pollCtx.Err() != nil {
		return []ClaimedCommand{}, nil
	}
	return commands, err
}

func (b *Broker) pollConsumers(ctx context.Context, tunnelUUID, instanceID string, channels []ChannelDeclaration) ([]jetstream.Consumer, error) {
	consumers := make([]jetstream.Consumer, 0, len(channels))
	for _, channel := range channels {
		target := ""
		if channel.ProcessAffinity {
			target = instanceID
		}
		subject := commandSubject(tunnelUUID, channel.Name, target)
		name := brokerKey(subject)
		consumer, err := b.commands.Consumer(ctx, name)
		if errors.Is(err, jetstream.ErrConsumerNotFound) {
			consumer, err = b.commands.CreateConsumer(ctx, jetstream.ConsumerConfig{
				Durable: name, FilterSubject: subject, AckPolicy: jetstream.AckExplicitPolicy,
				AckWait: 5 * time.Second, MaxAckPending: 32, MaxWaiting: 128,
				InactiveThreshold: b.cfg.RequestTimeout + time.Minute,
				MaxRequestBatch:   1, MaxRequestExpires: time.Second,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("open tunnel consumer: %w", err)
		}
		consumers = append(consumers, consumer)
	}
	return consumers, nil
}

// Each HTTP Poll owns its consumption; durable consumers are shared by route.
func (b *Broker) pollCommands(ctx context.Context, consumers []jetstream.Consumer, tunnelUUID, instanceID string, version int64, limit int, noWait bool) ([]ClaimedCommand, error) {
	pullCtx, cancel := context.WithCancel(ctx)
	deliveries := make(chan pollDelivery)
	var workers sync.WaitGroup
	for _, consumer := range consumers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if noWait {
				fetchAvailablePollMessages(pullCtx, consumer, deliveries)
			} else {
				consumePollChannel(pullCtx, consumer, deliveries)
			}
		}()
	}
	finished := make(chan struct{})
	// SDK error callbacks may finish after their subscription closes. Keep the
	// delivery channel open so late callbacks can observe cancellation safely.
	go func() { workers.Wait(); close(finished) }()
	// Workers drain after cancellation. Do not delay the HTTP response while
	// NATS flushes an unsubscribe, especially during a network interruption.
	defer cancel()
	commands := make([]ClaimedCommand, 0, limit)
	batchBytes := 0
	for len(commands) < limit {
		delivery, open := nextPollDelivery(ctx, deliveries, finished, len(commands) == 0)
		if !open {
			return commands, ctx.Err()
		}
		if delivery.err != nil {
			return commands, delivery.err
		}
		messageBytes := len(delivery.message.Data())
		if len(commands) > 0 && batchBytes+messageBytes > maxBrokerValueBytes {
			_ = delivery.message.NakWithDelay(brokerRedeliveryDelay)
			break
		}
		command, err := b.bindPollMessage(ctx, tunnelUUID, instanceID, version, delivery.message)
		if err != nil {
			return commands, err
		}
		if command != nil {
			commands = append(commands, *command)
			batchBytes += messageBytes
		}
	}
	return commands, nil
}

func nextPollDelivery(ctx context.Context, deliveries <-chan pollDelivery, finished <-chan struct{}, wait bool) (pollDelivery, bool) {
	if !wait {
		// A batch only includes deliveries already ready; never wait to fill it.
		select {
		case delivery, open := <-deliveries:
			return delivery, open
		default:
			return pollDelivery{}, false
		}
	}
	select {
	case delivery, open := <-deliveries:
		return delivery, open
	case <-ctx.Done():
		return pollDelivery{}, false
	case <-finished:
		return pollDelivery{}, false
	}
}

func sendPollDelivery(ctx context.Context, deliveries chan<- pollDelivery, delivery pollDelivery) bool {
	select {
	case deliveries <- delivery:
		return true
	case <-ctx.Done():
		if delivery.message != nil {
			_ = delivery.message.NakWithDelay(brokerRedeliveryDelay)
		}
		return false
	}
}

func consumePollChannel(ctx context.Context, consumer jetstream.Consumer, deliveries chan<- pollDelivery) {
	// Match the durable consumer's single-message pull and one-second expiry.
	// The callback hands off its message before the SDK requests another one.
	consumption, err := consumer.Consume(func(message jetstream.Msg) {
		sendPollDelivery(ctx, deliveries, pollDelivery{message: message})
	}, jetstream.PullMaxMessages(1), jetstream.PullExpiry(time.Second),
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			sendPollDelivery(ctx, deliveries, pollDelivery{err: err})
		}))
	if err != nil {
		sendPollDelivery(ctx, deliveries, pollDelivery{err: err})
		return
	}
	closed := consumption.Closed()
	select {
	case <-ctx.Done():
		// Drain lets callbacks return any buffered, unbound work via NAK.
		consumption.Drain()
		<-closed
	case <-closed:
		// A connection can close before the SDK's error callback runs. An
		// unexpectedly closed consumer must not become an empty successful Poll.
		sendPollDelivery(ctx, deliveries, pollDelivery{err: jetstream.ErrMsgIteratorClosed})
	}
}

func fetchAvailablePollMessages(ctx context.Context, consumer jetstream.Consumer, deliveries chan<- pollDelivery) {
	for ctx.Err() == nil {
		message, err := fetchAvailablePollMessage(ctx, consumer)
		if errors.Is(err, jetstream.ErrNoMessages) {
			return
		}
		if !sendPollDelivery(ctx, deliveries, pollDelivery{message: message, err: err}) || err != nil {
			return
		}
	}
}

func fetchAvailablePollMessage(ctx context.Context, consumer jetstream.Consumer) (jetstream.Msg, error) {
	// FetchNoWait asks only for available work; it never waits for a new command.
	batch, err := consumer.FetchNoWait(1)
	if err != nil {
		return nil, err
	}
	select {
	case message, open := <-batch.Messages():
		if open {
			return message, nil
		}
		if err := batch.Error(); err != nil {
			return nil, err
		}
		return nil, jetstream.ErrNoMessages
	case <-ctx.Done():
		// FetchNoWait has no context option. Its SDK timeout bounds this cleanup;
		// a canceled HTTP request does not have to wait for the network round trip.
		go releasePollBatch(batch)
		return nil, ctx.Err()
	}
}

func releasePollBatch(batch jetstream.MessageBatch) {
	for message := range batch.Messages() {
		_ = message.NakWithDelay(brokerRedeliveryDelay)
	}
}

func (b *Broker) bindPollMessage(ctx context.Context, tunnelUUID, instanceID string, version int64, message jetstream.Msg) (*ClaimedCommand, error) {
	var command queuedCommand
	if err := json.Unmarshal(message.Data(), &command); err != nil {
		_ = message.Term()
		return nil, fmt.Errorf("decode tunnel command: %w", err)
	}
	if message.Subject() != commandSubject(tunnelUUID, command.Channel, command.TargetInstance) {
		_ = message.Term()
		return nil, ErrResponseMismatch
	}
	if !b.now().Before(command.ExpiresAt) {
		return nil, message.DoubleAck(ctx)
	}
	record, revision, err := b.readRequest(ctx, tunnelUUID, command.RequestID)
	if err != nil {
		_ = message.NakWithDelay(brokerRedeliveryDelay)
		if errors.Is(err, ErrRequestNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if record.State != "queued" {
		return nil, message.DoubleAck(ctx)
	}
	if record.Channel != command.Channel || record.CommandType != command.CommandType || !record.ExpiresAt.Equal(command.ExpiresAt) {
		_ = message.Term()
		return nil, ErrResponseMismatch
	}
	shard, err := randomOpaqueToken(24)
	if err != nil {
		_ = message.NakWithDelay(brokerRedeliveryDelay)
		return nil, err
	}
	record.State, record.InstanceID, record.TokenVersion, record.ShardToken = "dispatched", instanceID, version, shard
	err = b.requests.update(ctx, brokerKey(tunnelUUID, command.RequestID), record, revision, maxBrokerValueBytes)
	if err != nil {
		_ = message.NakWithDelay(brokerRedeliveryDelay)
		if brokerCASConflict(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := b.confirmDelivery(ctx, tunnelUUID, instanceID, version, command); err != nil {
		_ = b.Cancel(ctx, tunnelUUID, command.RequestID)
		_ = message.Ack()
		return nil, err
	}
	if err := message.DoubleAck(ctx); err != nil {
		return nil, err
	}
	remaining := command.ExpiresAt.Sub(b.now())
	if remaining <= 0 {
		return nil, nil
	}
	return &ClaimedCommand{RequestID: command.RequestID, ShardToken: shard, CommandType: command.CommandType, Channel: command.Channel,
		CreatedAt: command.CreatedAt, Headers: command.Headers, JSONRPC: command.JSONRPC, ResponseTimeout: remaining, expiresAt: command.ExpiresAt}, nil
}
