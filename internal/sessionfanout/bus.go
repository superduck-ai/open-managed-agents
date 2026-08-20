package sessionfanout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	instanceChannelPrefix = "oma:i:"
	sessionChannelPrefix  = "oma:s:"
)

type Kind string

const (
	KindSessionEvents     Kind = "session_events"
	KindCodeSessionStream Kind = "code_session_stream"
)

type Envelope struct {
	Kind    Kind            `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type Handler func(context.Context, Envelope)

type EventBus interface {
	Publish(context.Context, string, Envelope) error
	Subscribe(context.Context, string) error
	Register(Handler, func())
	Close() error
}

type LocalBus struct {
	mu       sync.RWMutex
	handlers []Handler
}

func NewLocal() *LocalBus {
	return &LocalBus{}
}

func (b *LocalBus) Publish(ctx context.Context, sessionID string, envelope Envelope) error {
	if _, err := sessionChannel(sessionID); err != nil {
		return err
	}
	if err := validateEnvelope(envelope); err != nil {
		return err
	}
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers...)
	b.mu.RUnlock()
	for _, handler := range handlers {
		handler(ctx, envelope)
	}
	return nil
}

func (b *LocalBus) Subscribe(_ context.Context, sessionID string) error {
	_, err := sessionChannel(sessionID)
	return err
}

func (b *LocalBus) Register(handler Handler, _ func()) {
	if handler == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
}

func (b *LocalBus) Close() error {
	return nil
}

type RedisBus struct {
	client        *redis.Client
	pubsub        redisSubscription
	logger        *slog.Logger
	mu            sync.RWMutex
	handlers      []Handler
	resetters     []func()
	subscriptions map[string]*sessionSubscription
	cancel        context.CancelFunc
	done          chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

type redisSubscription interface {
	Receive(context.Context) (any, error)
	Subscribe(context.Context, ...string) error
	Close() error
}

type sessionSubscription struct {
	done      chan struct{}
	err       error
	completed bool
}

func NewRedis(ctx context.Context, client *redis.Client, logger *slog.Logger) (*RedisBus, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	listenCtx, cancel := context.WithCancel(ctx)
	instanceChannel := instanceChannelPrefix + uuid.NewString()
	pubsub := client.Subscribe(listenCtx, instanceChannel)
	if _, err := pubsub.Receive(listenCtx); err != nil {
		cancel()
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribe redis instance channel: %w", err)
	}
	bus := &RedisBus{
		client:        client,
		pubsub:        pubsub,
		logger:        logging.LoggerOrDefault(logger),
		subscriptions: make(map[string]*sessionSubscription),
		cancel:        cancel,
		done:          make(chan struct{}),
	}
	go bus.receive(listenCtx)
	return bus, nil
}

func (b *RedisBus) Publish(ctx context.Context, sessionID string, envelope Envelope) error {
	channel, err := sessionChannel(sessionID)
	if err != nil {
		return err
	}
	if err := validateEnvelope(envelope); err != nil {
		return err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal session fanout envelope: %w", err)
	}
	if err := b.client.Publish(ctx, channel, body).Err(); err != nil {
		return fmt.Errorf("publish redis channel %s: %w", channel, err)
	}
	return nil
}

func (b *RedisBus) Subscribe(ctx context.Context, sessionID string) error {
	channel, err := sessionChannel(sessionID)
	if err != nil {
		return err
	}
	subscription, created := b.sessionSubscription(channel)
	if created {
		if err := b.pubsub.Subscribe(ctx, channel); err != nil {
			b.completeSubscription(channel, subscription, fmt.Errorf("subscribe redis channel %s: %w", channel, err), true)
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-subscription.done:
		return subscription.err
	}
}

func (b *RedisBus) Register(handler Handler, reset func()) {
	if handler == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
	if reset != nil {
		b.resetters = append(b.resetters, reset)
	}
}

func (b *RedisBus) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		b.failPendingSubscriptions(errors.New("session fanout is closed"))
		b.cancel()
		b.closeErr = b.pubsub.Close()
		<-b.done
	})
	return b.closeErr
}

func (b *RedisBus) receive(ctx context.Context) {
	defer close(b.done)
	for {
		item, err := b.pubsub.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.logger.WarnContext(ctx, "session event fanout interrupted", "channel_prefix", sessionChannelPrefix, "error", err)
			b.reset()
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		b.receiveItem(ctx, item)
	}
}

func (b *RedisBus) receiveItem(ctx context.Context, item any) {
	switch item := item.(type) {
	case *redis.Message:
		b.receiveMessage(ctx, item)
	case *redis.Subscription:
		if item.Kind == "subscribe" {
			b.completeSubscription(item.Channel, nil, nil, false)
		}
	}
}

func (b *RedisBus) receiveMessage(ctx context.Context, message *redis.Message) {
	var envelope Envelope
	if err := json.Unmarshal([]byte(message.Payload), &envelope); err != nil {
		b.logger.WarnContext(ctx, "discard malformed session event fanout", "channel", message.Channel, "error", err)
		return
	}
	if err := validateEnvelope(envelope); err != nil {
		b.logger.WarnContext(ctx, "discard invalid session event fanout", "channel", message.Channel, "error", err)
		return
	}
	b.dispatch(ctx, envelope)
}

func (b *RedisBus) sessionSubscription(channel string) (*sessionSubscription, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if subscription, exists := b.subscriptions[channel]; exists {
		return subscription, false
	}
	subscription := &sessionSubscription{done: make(chan struct{})}
	b.subscriptions[channel] = subscription
	return subscription, true
}

func (b *RedisBus) completeSubscription(channel string, expected *sessionSubscription, err error, remove bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subscription, exists := b.subscriptions[channel]
	if !exists || subscription.completed || (expected != nil && subscription != expected) {
		return
	}
	subscription.err = err
	subscription.completed = true
	close(subscription.done)
	if remove {
		delete(b.subscriptions, channel)
	}
}

func (b *RedisBus) failPendingSubscriptions(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for channel, subscription := range b.subscriptions {
		if subscription.completed {
			continue
		}
		subscription.err = err
		subscription.completed = true
		close(subscription.done)
		delete(b.subscriptions, channel)
	}
}

func (b *RedisBus) dispatch(ctx context.Context, envelope Envelope) {
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers...)
	b.mu.RUnlock()
	for _, handler := range handlers {
		handler(ctx, envelope)
	}
}

func (b *RedisBus) reset() {
	b.mu.RLock()
	resetters := append([]func(){}, b.resetters...)
	b.mu.RUnlock()
	for _, reset := range resetters {
		reset()
	}
}

func validateEnvelope(envelope Envelope) error {
	if !envelope.Kind.valid() {
		return fmt.Errorf("unsupported session fanout kind %q", envelope.Kind)
	}
	if len(envelope.Payload) == 0 {
		return errors.New("session fanout payload is required")
	}
	return nil
}

func (kind Kind) valid() bool {
	switch kind {
	case KindSessionEvents, KindCodeSessionStream:
		return true
	default:
		return false
	}
}

func sessionChannel(sessionID string) (string, error) {
	if sessionID == "" {
		return "", errors.New("session fanout session ID is required")
	}
	return sessionChannelPrefix + sessionID, nil
}
