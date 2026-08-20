package sessionfanout

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisBusCloseUnblocksDeadlineLessReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	subscription := newBlockingRedisSubscription()
	bus := &RedisBus{
		pubsub: subscription,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go bus.receive(ctx)

	select {
	case <-subscription.receiveStarted:
	case <-time.After(time.Second):
		t.Fatal("Redis receive loop did not start")
	}

	closed := make(chan error, 1)
	go func() {
		closed <- bus.Close()
	}()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		_ = subscription.Close()
		<-closed
		t.Fatal("Close() blocked on deadline-less Redis receive")
	}

	if err := bus.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestRedisBusSubscribesToSessionChannelAndWaitsForAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	subscription := newAcknowledgingRedisSubscription()
	bus := &RedisBus{
		pubsub:        subscription,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		subscriptions: make(map[string]*sessionSubscription),
		cancel:        cancel,
		done:          make(chan struct{}),
	}
	go bus.receive(ctx)

	subscribed := make(chan error, 1)
	go func() {
		subscribed <- bus.Subscribe(ctx, "session-test")
	}()
	select {
	case <-subscription.subscribeCalled:
	case <-time.After(time.Second):
		t.Fatal("Redis Subscribe() was not called")
	}
	select {
	case err := <-subscribed:
		t.Fatalf("Subscribe() returned before acknowledgement: %v", err)
	default:
	}
	if got, want := subscription.lastChannel(), "oma:s:session-test"; got != want {
		t.Fatalf("subscribed channel = %q, want %q", got, want)
	}
	subscription.acknowledge("oma:s:session-test")
	select {
	case err := <-subscribed:
		if err != nil {
			t.Fatalf("Subscribe() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe() did not return after acknowledgement")
	}
	if err := bus.Subscribe(ctx, "session-test"); err != nil {
		t.Fatalf("second Subscribe() error = %v", err)
	}
	if got := subscription.subscribeCount(); got != 1 {
		t.Fatalf("Redis subscribe count = %d, want 1", got)
	}
	if err := bus.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestValidateEnvelopeKind(t *testing.T) {
	for _, kind := range []Kind{KindSessionEvents, KindCodeSessionStream} {
		envelope := Envelope{Kind: kind, Payload: json.RawMessage(`{}`)}
		if err := validateEnvelope(envelope); err != nil {
			t.Fatalf("validateEnvelope(%q) returned error: %v", kind, err)
		}
	}

	envelope := Envelope{Kind: Kind("unknown"), Payload: json.RawMessage(`{}`)}
	if err := validateEnvelope(envelope); err == nil {
		t.Fatal("validateEnvelope(unknown) error = nil, want unsupported kind error")
	}
}

type blockingRedisSubscription struct {
	receiveStarted chan struct{}
	closed         chan struct{}
	receiveOnce    sync.Once
	closeOnce      sync.Once
}

func newBlockingRedisSubscription() *blockingRedisSubscription {
	return &blockingRedisSubscription{
		receiveStarted: make(chan struct{}),
		closed:         make(chan struct{}),
	}
}

func (s *blockingRedisSubscription) Receive(context.Context) (any, error) {
	s.receiveOnce.Do(func() {
		close(s.receiveStarted)
	})
	<-s.closed
	return nil, errors.New("subscription closed")
}

func (s *blockingRedisSubscription) Subscribe(context.Context, ...string) error {
	return nil
}

func (s *blockingRedisSubscription) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
	return nil
}

type acknowledgingRedisSubscription struct {
	mu              sync.Mutex
	channels        []string
	items           chan any
	closed          chan struct{}
	subscribeCalled chan struct{}
	closeOnce       sync.Once
}

func newAcknowledgingRedisSubscription() *acknowledgingRedisSubscription {
	return &acknowledgingRedisSubscription{
		items:           make(chan any, 1),
		closed:          make(chan struct{}),
		subscribeCalled: make(chan struct{}, 1),
	}
}

func (s *acknowledgingRedisSubscription) Receive(ctx context.Context) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, errors.New("subscription closed")
	case item := <-s.items:
		return item, nil
	}
}

func (s *acknowledgingRedisSubscription) Subscribe(_ context.Context, channels ...string) error {
	s.mu.Lock()
	s.channels = append(s.channels, channels...)
	s.mu.Unlock()
	s.subscribeCalled <- struct{}{}
	return nil
}

func (s *acknowledgingRedisSubscription) acknowledge(channel string) {
	s.items <- &redis.Subscription{Kind: "subscribe", Channel: channel}
}

func (s *acknowledgingRedisSubscription) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
	return nil
}

func (s *acknowledgingRedisSubscription) lastChannel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.channels) == 0 {
		return ""
	}
	return s.channels[len(s.channels)-1]
}

func (s *acknowledgingRedisSubscription) subscribeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.channels)
}
