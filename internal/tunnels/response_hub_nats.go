package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	responseSubscriptionBuffer = 16
	responseHubBytes           = 64 << 20
	responseRecoveryInterval   = 250 * time.Millisecond
)

type responseEnvelope struct {
	Key      string          `json:"key"`
	Response *TunnelResponse `json:"response,omitempty"`
}

type bufferedResponse struct {
	response TunnelResponse
	bytes    int
}

type responseHub struct {
	connection    *nats.Conn
	subject       string
	subscription  *nats.Subscription
	mu            sync.Mutex
	waiters       map[string]*responseWaiter
	maxWaiters    int
	bufferedBytes int
	closed        bool
}

type responseWaiter struct {
	broker        *Broker
	tunnelUUID    string
	requestID     string
	key           string
	wake          chan struct{}
	done          chan struct{}
	closed        bool
	accepting     bool
	notifications []bufferedResponse
	bytes         int
}

func newResponseHub(ctx context.Context, connection *nats.Conn, maxWaiters int) (*responseHub, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	id, err := randomOpaqueToken(24)
	if err != nil {
		return nil, err
	}
	h := &responseHub{connection: connection, subject: "oma.tunnel.response.v1." + brokerKey(id), waiters: make(map[string]*responseWaiter), maxWaiters: maxWaiters}
	h.subscription, err = connection.Subscribe(h.subject, h.receive)
	if err != nil {
		return nil, err
	}
	if err := connection.FlushWithContext(ctx); err != nil {
		_ = h.subscription.Unsubscribe()
		return nil, err
	}
	return h, nil
}

func (h *responseHub) receive(message *nats.Msg) {
	var envelope responseEnvelope
	if err := json.Unmarshal(message.Data, &envelope); err != nil {
		return
	}
	status := h.accept(envelope, len(message.Data))
	if message.Reply != "" {
		_ = message.Respond([]byte(status))
	}
}

func (h *responseHub) accept(envelope responseEnvelope, size int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.waiters[envelope.Key]
	if w == nil || !w.accepting || h.closed {
		return "gone"
	}
	if envelope.Response != nil {
		if len(w.notifications) >= responseSubscriptionBuffer || w.bytes+size > maxBrokerValueBytes || h.bufferedBytes+size > responseHubBytes {
			return "full"
		}
		w.notifications = append(w.notifications, bufferedResponse{response: *envelope.Response, bytes: size})
		w.bytes += size
		h.bufferedBytes += size
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return "accepted"
}

func (h *responseHub) forward(ctx context.Context, origin, tunnelUUID string, response TunnelResponse) error {
	data, err := encodeTunnelJSON(responseEnvelope{Key: brokerKey(tunnelUUID, response.RequestID), Response: &response}, maxBrokerValueBytes)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	message, err := h.connection.RequestWithContext(ctx, origin, data)
	if err != nil {
		return fmt.Errorf("forward tunnel notification: %w", err)
	}
	switch string(message.Data) {
	case "accepted":
		return nil
	case "full":
		return ErrResponseBackpressure
	case "gone":
		return ErrRequestCanceled
	default:
		return ErrResponseMismatch
	}
}

func (b *Broker) subscribeResponse(ctx context.Context, tunnelUUID, requestID string) (*responseWaiter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h := b.responseHub
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nats.ErrConnectionClosed
	}
	key := brokerKey(tunnelUUID, requestID)
	if len(h.waiters) >= h.maxWaiters {
		return nil, ErrQueueLimit
	}
	if h.waiters[key] != nil {
		return nil, ErrResponseMismatch
	}
	w := &responseWaiter{broker: b, tunnelUUID: tunnelUUID, requestID: requestID, key: key, wake: make(chan struct{}, 1), done: make(chan struct{}), accepting: true}
	h.waiters[key] = w
	return w, nil
}

func (w *responseWaiter) Close() {
	h := w.broker.responseHub
	h.mu.Lock()
	defer h.mu.Unlock()
	if w.closed {
		return
	}
	w.closed, w.accepting = true, false
	h.bufferedBytes -= w.bytes
	w.bytes, w.notifications = 0, nil
	delete(h.waiters, w.key)
	close(w.done)
}

func (h *responseHub) close() {
	_ = h.subscription.Unsubscribe()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for key, w := range h.waiters {
		w.closed, w.accepting = true, false
		h.bufferedBytes -= w.bytes
		w.bytes, w.notifications = 0, nil
		close(w.done)
		delete(h.waiters, key)
	}
}

func (w *responseWaiter) drain(final bool, onNotification func(TunnelResponse)) {
	h := w.broker.responseHub
	h.mu.Lock()
	if final {
		w.accepting = false
	}
	count := len(w.notifications)
	h.mu.Unlock()
	for range count {
		h.mu.Lock()
		if len(w.notifications) == 0 {
			h.mu.Unlock()
			return
		}
		response := w.notifications[0]
		w.notifications[0] = bufferedResponse{}
		w.notifications = w.notifications[1:]
		w.bytes -= response.bytes
		h.mu.Unlock()
		if onNotification != nil {
			onNotification(response.response)
		}
		h.mu.Lock()
		h.bufferedBytes -= response.bytes
		h.mu.Unlock()
	}
}

func (w *responseWaiter) Wait(ctx context.Context, onNotification func(TunnelResponse)) (TunnelResponse, error) {
	ticker := time.NewTicker(responseRecoveryInterval)
	defer ticker.Stop()
	for {
		readCtx, cancel := context.WithTimeout(ctx, time.Second)
		response, state, err := w.broker.GetResponse(readCtx, w.tunnelUUID, w.requestID)
		cancel()
		if err != nil && !brokerWaitableError(err) {
			return TunnelResponse{}, err
		}
		if err == nil {
			switch state {
			case "completed":
				if response == nil {
					return TunnelResponse{}, errors.New("completed tunnel request has no response")
				}
				w.drain(true, onNotification)
				return *response, nil
			case "expired":
				return TunnelResponse{}, ErrRequestExpired
			case "canceled":
				return TunnelResponse{}, ErrRequestCanceled
			}
		}
		w.drain(false, onNotification)
		select {
		case <-ctx.Done():
			return TunnelResponse{}, ctx.Err()
		case <-w.done:
			return TunnelResponse{}, ErrRequestCanceled
		case <-w.wake:
		case <-ticker.C:
		}
	}
}
