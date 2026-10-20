package workerevents

import (
	"context"
	"sync"

	"github.com/nats-io/nats.go"
)

type BrainWorkSignal interface {
	NotifyBrainWork(ctx context.Context) error
	SubscribeBrainWork(ctx context.Context) (<-chan struct{}, error)
}

func (b *JetStreamBroker) NotifyBrainWork(context.Context) error {
	return b.connection.Publish(b.names.brainWorkSubject, nil)
}

func (b *JetStreamBroker) SubscribeBrainWork(ctx context.Context) (<-chan struct{}, error) {
	signals := make(chan struct{}, 1)
	subscription, err := b.connection.Subscribe(b.names.brainWorkSubject, func(*nats.Msg) { signalBrainWork(signals) })
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = subscription.Unsubscribe()
	}()
	return signals, nil
}

type brainWorkHub struct {
	mu      sync.Mutex
	waiters map[chan struct{}]struct{}
}

func (b *MemoryBroker) NotifyBrainWork(context.Context) error {
	b.brainWork.mu.Lock()
	defer b.brainWork.mu.Unlock()
	for signals := range b.brainWork.waiters {
		signalBrainWork(signals)
	}
	return nil
}

func (b *MemoryBroker) SubscribeBrainWork(ctx context.Context) (<-chan struct{}, error) {
	signals := make(chan struct{}, 1)
	b.brainWork.mu.Lock()
	if b.brainWork.waiters == nil {
		b.brainWork.waiters = make(map[chan struct{}]struct{})
	}
	b.brainWork.waiters[signals] = struct{}{}
	b.brainWork.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.brainWork.mu.Lock()
		delete(b.brainWork.waiters, signals)
		b.brainWork.mu.Unlock()
	}()
	return signals, nil
}

func signalBrainWork(signals chan struct{}) {
	select {
	case signals <- struct{}{}:
	default:
	}
}
