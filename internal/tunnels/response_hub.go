package tunnels

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

const responseSubscriptionBuffer = 2

type responseDelivery struct {
	payload string
	err     error
}

type responseHub struct {
	client *redis.Client

	startMu sync.Mutex
	mu      sync.Mutex
	pubsub  *redis.PubSub
	waiters map[string]map[*responseSubscription]struct{}
}

type responseSubscription struct {
	hub        *responseHub
	channel    string
	deliveries chan responseDelivery
	closeOnce  sync.Once
}

func newResponseHub(client *redis.Client) *responseHub {
	return &responseHub{
		client:  client,
		waiters: make(map[string]map[*responseSubscription]struct{}),
	}
}

func (h *responseHub) subscribe(ctx context.Context, channel string) (*responseSubscription, error) {
	for {
		if err := h.ensureStarted(ctx); err != nil {
			return nil, err
		}
		h.mu.Lock()
		if h.pubsub == nil {
			h.mu.Unlock()
			continue
		}
		subscription := &responseSubscription{
			hub: h, channel: channel,
			deliveries: make(chan responseDelivery, responseSubscriptionBuffer),
		}
		listeners := h.waiters[channel]
		if listeners == nil {
			listeners = make(map[*responseSubscription]struct{})
			h.waiters[channel] = listeners
		}
		listeners[subscription] = struct{}{}
		h.mu.Unlock()
		return subscription, nil
	}
}

func (h *responseHub) ensureStarted(ctx context.Context) error {
	h.startMu.Lock()
	defer h.startMu.Unlock()
	h.mu.Lock()
	started := h.pubsub != nil
	h.mu.Unlock()
	if started {
		return nil
	}
	pubsub := h.client.PSubscribe(ctx, "oma:tunnel:*:response:*")
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return fmt.Errorf("subscribe tunnel responses: %w", err)
	}
	h.mu.Lock()
	h.pubsub = pubsub
	h.mu.Unlock()
	go h.dispatch(pubsub)
	return nil
}

func (h *responseHub) dispatch(pubsub *redis.PubSub) {
	for {
		message, err := pubsub.ReceiveMessage(context.Background())
		if err != nil {
			h.fail(pubsub, fmt.Errorf("receive tunnel response: %w", err))
			return
		}
		h.mu.Lock()
		for subscription := range h.waiters[message.Channel] {
			offerResponseDelivery(subscription.deliveries, responseDelivery{payload: message.Payload})
		}
		h.mu.Unlock()
	}
}

func (h *responseHub) fail(pubsub *redis.PubSub, err error) {
	h.mu.Lock()
	if h.pubsub != pubsub {
		h.mu.Unlock()
		return
	}
	h.pubsub = nil
	for channel, listeners := range h.waiters {
		for subscription := range listeners {
			offerResponseDelivery(subscription.deliveries, responseDelivery{err: err})
		}
		delete(h.waiters, channel)
	}
	h.mu.Unlock()
	_ = pubsub.Close()
}

func offerResponseDelivery(deliveries chan responseDelivery, delivery responseDelivery) {
	select {
	case deliveries <- delivery:
		return
	default:
	}
	// Notifications are advisory. Keep the newest message so a terminal response
	// cannot be trapped behind an unbounded notification backlog.
	select {
	case <-deliveries:
	default:
	}
	select {
	case deliveries <- delivery:
	default:
	}
}

func (s *responseSubscription) receive(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case delivery, open := <-s.deliveries:
		if !open {
			return "", errors.New("tunnel response subscription closed")
		}
		return delivery.payload, delivery.err
	}
}

func (s *responseSubscription) close() {
	if s == nil || s.hub == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.hub.mu.Lock()
		listeners := s.hub.waiters[s.channel]
		delete(listeners, s)
		if len(listeners) == 0 {
			delete(s.hub.waiters, s.channel)
		}
		close(s.deliveries)
		s.hub.mu.Unlock()
	})
}
