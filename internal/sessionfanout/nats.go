package sessionfanout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	natsSessionPrefix = "oma.s."
	natsFlushTimeout  = 5 * time.Second
)

// NATSBus broadcasts through Core NATS. It shares one broker subscription
// between all local SSE connections interested in the same session.
type NATSBus struct {
	connection *nats.Conn
	flush      func(context.Context) error
	logger     *slog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	statuses   chan nats.Status
	done       chan struct{}

	mu            sync.Mutex
	subscriptions map[string]*natsSubscriptionState
	handlers      []Handler
	resetters     []ResetHandler
	installations sync.WaitGroup
	closed        bool
	closeOnce     sync.Once
}

type natsSubscriptionState struct {
	sessionID    string
	subject      string
	ready        chan struct{}
	result       error
	subscription *nats.Subscription
	references   int
}

func NewNATS(ctx context.Context, connection *nats.Conn, logger *slog.Logger) (*NATSBus, error) {
	if connection == nil || !connection.IsConnected() {
		return nil, nats.ErrDisconnected
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listenCtx, cancel := context.WithCancel(ctx)
	b := &NATSBus{
		connection:    connection,
		flush:         connection.FlushWithContext,
		logger:        logging.LoggerOrDefault(logger),
		ctx:           listenCtx,
		cancel:        cancel,
		statuses:      connection.StatusChanged(nats.RECONNECTING),
		done:          make(chan struct{}),
		subscriptions: make(map[string]*natsSubscriptionState),
	}
	go b.receive()
	return b, nil
}

func (b *NATSBus) Publish(ctx context.Context, sessionID string, envelope Envelope) error {
	subject, err := sessionSubject(sessionID)
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
	if err := b.available(ctx); err != nil {
		return err
	}
	return b.connection.Publish(subject, body)
}

// Subscribe acquires this session's shared broker subscription. Concurrent
// callers wait for the same initial NATS flush before returning.
func (b *NATSBus) Subscribe(ctx context.Context, sessionID string) error {
	subject, err := sessionSubject(sessionID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	if err := b.availableLocked(ctx); err != nil {
		b.mu.Unlock()
		return err
	}
	state := b.subscriptions[subject]
	if state == nil {
		state = &natsSubscriptionState{
			sessionID:  sessionID,
			subject:    subject,
			ready:      make(chan struct{}),
			references: 1,
		}
		b.subscriptions[subject] = state
		b.installations.Add(1)
		go b.installSubscription(state)
	} else {
		state.references++
	}
	b.mu.Unlock()

	if err := b.waitForSubscription(ctx, state); err != nil {
		_ = b.Unsubscribe(sessionID)
		return err
	}
	return nil
}

func (b *NATSBus) installSubscription(state *natsSubscriptionState) {
	defer b.installations.Done()

	subscription, err := b.connection.Subscribe(state.subject, func(message *nats.Msg) {
		b.receiveMessage(state.sessionID, message)
	})
	if err != nil {
		b.completeSubscription(state, nil, fmt.Errorf("subscribe nats session: %w", err))
		return
	}

	flushCtx, cancel := context.WithTimeout(b.ctx, natsFlushTimeout)
	err = b.flush(flushCtx)
	cancel()
	if err != nil {
		b.completeSubscription(state, subscription, fmt.Errorf("confirm nats session subscription: %w", err))
		return
	}
	b.completeSubscription(state, subscription, nil)
}

func (b *NATSBus) completeSubscription(state *natsSubscriptionState, subscription *nats.Subscription, result error) {
	b.mu.Lock()
	active := !b.closed && b.subscriptions[state.subject] == state
	if !active && result == nil {
		result = errNATSSubscriptionInvalidated
	}
	if result != nil && active {
		delete(b.subscriptions, state.subject)
	}
	if result == nil {
		state.subscription = subscription
	}
	state.result = result
	close(state.ready)
	b.mu.Unlock()

	if result != nil && subscription != nil {
		_ = subscription.Unsubscribe()
	}
}

func (b *NATSBus) waitForSubscription(ctx context.Context, state *natsSubscriptionState) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return errNATSBusClosed
	case <-state.ready:
		return state.result
	}
}

// Unsubscribe releases one local SSE reference. The broker subscription is
// removed when its last local consumer leaves.
func (b *NATSBus) Unsubscribe(sessionID string) error {
	subject, err := sessionSubject(sessionID)
	if err != nil {
		return err
	}
	b.mu.Lock()
	state := b.subscriptions[subject]
	if state == nil {
		b.mu.Unlock()
		return nil
	}
	state.references--
	if state.references > 0 {
		b.mu.Unlock()
		return nil
	}
	delete(b.subscriptions, subject)
	subscription := state.subscription
	b.mu.Unlock()

	if subscription != nil {
		return subscription.Unsubscribe()
	}
	return nil
}

func (b *NATSBus) Register(handler Handler, reset ResetHandler) {
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

func (b *NATSBus) Close() error {
	b.closeOnce.Do(func() {
		b.cancel()
		b.mu.Lock()
		b.closed = true
		subscriptions := make([]*nats.Subscription, 0, len(b.subscriptions))
		for subject, state := range b.subscriptions {
			delete(b.subscriptions, subject)
			if state.subscription != nil {
				subscriptions = append(subscriptions, state.subscription)
			}
		}
		b.mu.Unlock()
		for _, subscription := range subscriptions {
			_ = subscription.Unsubscribe()
		}
		b.installations.Wait()
		<-b.done
		b.connection.RemoveStatusListener(b.statuses)
	})
	return nil
}

func (b *NATSBus) available(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.availableLocked(ctx)
}

func (b *NATSBus) availableLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.closed || b.ctx.Err() != nil {
		return errNATSBusClosed
	}
	if !b.connection.IsConnected() {
		return nats.ErrDisconnected
	}
	return nil
}

func sessionSubject(sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	return natsSessionPrefix + sessionID, nil
}
