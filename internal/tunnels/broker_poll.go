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

const (
	brokerPullWindow  = 100 * time.Millisecond
	brokerBatchWindow = 5 * time.Millisecond
)

type pollDelivery struct {
	message jetstream.Msg
	err     error
	release func()
}

func (b *Broker) Poll(ctx context.Context, tunnelUUID, instanceID string, version int64, channels []ChannelDeclaration, limit int, timeout time.Duration) ([]ClaimedCommand, error) {
	if err := validateBrokerChannels(channels); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxPollLimit {
		return nil, ErrQueueLimit
	}
	select {
	case b.pollSlots <- struct{}{}:
		defer func() { <-b.pollSlots }()
	default:
		return nil, ErrBrokerBusy
	}
	if err := b.RegisterConnector(ctx, tunnelUUID, instanceID, version, channels); err != nil {
		return nil, err
	}
	consumers, err := b.pollConsumers(ctx, tunnelUUID, instanceID, channels)
	if err != nil {
		return nil, err
	}
	// Even timeout=0 gets one bounded scan of messages already queued.
	if timeout <= 0 {
		timeout = brokerPullWindow
	}
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for pollCtx.Err() == nil {
		commands, err := b.pollRound(pollCtx, consumers, tunnelUUID, instanceID, version, limit)
		if len(commands) > 0 {
			return commands, nil
		}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				break
			}
			return nil, err
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return []ClaimedCommand{}, nil
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

func (b *Broker) pollRound(ctx context.Context, consumers []jetstream.Consumer, tunnelUUID, instanceID string, version int64, limit int) ([]ClaimedCommand, error) {
	roundCtx, cancel := context.WithTimeout(ctx, brokerPullWindow)
	var workers sync.WaitGroup
	deliveries := make(chan pollDelivery)
	for _, consumer := range consumers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for roundCtx.Err() == nil && b.fetchPollMessage(roundCtx, consumer, deliveries) {
			}
		}()
	}
	finished := make(chan struct{})
	go func() { workers.Wait(); close(deliveries); close(finished) }()
	defer func() { cancel(); <-finished }()
	commands := make([]ClaimedCommand, 0, limit)
	batchBytes := 0
	var batchDeadline <-chan time.Time
	var batchTimer *time.Timer
	defer func() {
		if batchTimer != nil {
			batchTimer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return commands, ctx.Err()
		case <-batchDeadline:
			return commands, nil
		case delivery, open := <-deliveries:
			if !open {
				return commands, nil
			}
			if delivery.err != nil {
				return commands, delivery.err
			}
			messageBytes := len(delivery.message.Data())
			if len(commands) > 0 && batchBytes+messageBytes > maxBrokerValueBytes {
				_ = delivery.message.NakWithDelay(brokerPullWindow)
				delivery.release()
				return commands, nil
			}
			command, err := b.bindPollMessage(ctx, tunnelUUID, instanceID, version, delivery.message)
			delivery.release()
			if err != nil {
				return commands, err
			}
			if command == nil {
				continue
			}
			commands = append(commands, *command)
			batchBytes += messageBytes
			if len(commands) == limit {
				return commands, nil
			}
			if batchTimer == nil {
				batchTimer = time.NewTimer(brokerBatchWindow)
				batchDeadline = batchTimer.C
			}
		}
	}
}

func (b *Broker) fetchPollMessage(ctx context.Context, consumer jetstream.Consumer, deliveries chan<- pollDelivery) bool {
	select {
	case b.prefetchSlots <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	release := func() { <-b.prefetchSlots }
	batch, err := consumer.Fetch(1, jetstream.FetchContext(ctx))
	if err != nil {
		release()
		// FetchContext checks the clock before context's timer necessarily sets
		// Err. Its expired-deadline ErrInvalidOption is an exhausted pull window.
		if deadline, ok := ctx.Deadline(); ok && errors.Is(err, jetstream.ErrInvalidOption) && !time.Now().Before(deadline) {
			return false
		}
		if ctx.Err() == nil {
			select {
			case deliveries <- pollDelivery{err: err}:
			case <-ctx.Done():
			}
		}
		return false
	}
	var message jetstream.Msg
	for msg := range batch.Messages() {
		message = msg
	}
	if message == nil {
		release()
		if err := batch.Error(); err != nil && ctx.Err() == nil {
			select {
			case deliveries <- pollDelivery{err: err}:
			case <-ctx.Done():
			}
		}
		return false
	}
	select {
	case deliveries <- pollDelivery{message: message, release: release}:
		return true
	case <-ctx.Done():
		_ = message.NakWithDelay(brokerPullWindow)
		release()
		return false
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
		_ = message.NakWithDelay(brokerPullWindow)
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
		_ = message.NakWithDelay(brokerPullWindow)
		return nil, err
	}
	record.State, record.InstanceID, record.TokenVersion, record.ShardToken = "dispatched", instanceID, version, shard
	err = b.requests.update(ctx, brokerKey(tunnelUUID, command.RequestID), record, revision, maxBrokerValueBytes)
	if err != nil {
		_ = message.NakWithDelay(brokerPullWindow)
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
