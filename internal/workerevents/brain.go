package workerevents

import (
	"context"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const brainWorkSubject = "oma.brain.work.available"

type BrainWorkSignal interface {
	NotifyBrainWork(ctx context.Context) error
	SubscribeBrainWork(ctx context.Context) (<-chan struct{}, error)
}

func (b *JetStreamBroker) NotifyBrainWork(context.Context) error {
	return b.connection.Publish(brainWorkSubject, nil)
}

func (b *JetStreamBroker) SubscribeBrainWork(ctx context.Context) (<-chan struct{}, error) {
	return b.subscribeWorkSubject(ctx, brainWorkSubject)
}

func (b *JetStreamBroker) subscribeWorkSubject(ctx context.Context, subject string) (<-chan struct{}, error) {
	signals := make(chan struct{}, 1)
	subscription, err := b.connection.Subscribe(subject, func(*nats.Msg) { signalBrainWork(signals) })
	if err != nil {
		return nil, err
	}
	flush, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := b.connection.FlushWithContext(flush); err != nil {
		_ = subscription.Unsubscribe()
		return nil, err
	}
	go func() { <-ctx.Done(); _ = subscription.Unsubscribe() }()
	return signals, nil
}

type brainWorkHub struct {
	mu      sync.Mutex
	waiters map[string]map[chan struct{}]struct{}
}

func (b *MemoryBroker) NotifyBrainWork(context.Context) error {
	b.brainWork.notify("")
	return nil
}

func (b *MemoryBroker) SubscribeBrainWork(ctx context.Context) (<-chan struct{}, error) {
	return b.brainWork.subscribe(ctx, "")
}

func (h *brainWorkHub) notify(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for signals := range h.waiters[key] {
		signalBrainWork(signals)
	}
}

func (h *brainWorkHub) subscribe(ctx context.Context, key string) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	signals := make(chan struct{}, 1)
	h.mu.Lock()
	if h.waiters == nil {
		h.waiters = make(map[string]map[chan struct{}]struct{})
	}
	if h.waiters[key] == nil {
		h.waiters[key] = make(map[chan struct{}]struct{})
	}
	h.waiters[key][signals] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.waiters[key], signals)
		if len(h.waiters[key]) == 0 {
			delete(h.waiters, key)
		}
		h.mu.Unlock()
	}()
	return signals, nil
}

func signalBrainWork(signals chan struct{}) {
	select {
	case signals <- struct{}{}:
	default:
	}
}
