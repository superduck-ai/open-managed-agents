package tunnels

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	responseSubscriptionBuffer = 16
	responseHubBytes           = 64 << 20
)

type responseEnvelope struct {
	ReceiptOnly bool              `json:"receipt_only,omitempty"`
	Scope       payloadScope      `json:"scope,omitempty"`
	PayloadRef  *payloadReference `json:"payload_ref,omitempty"`
	Key         string            `json:"key"`
	Response    *TunnelResponse   `json:"response"`
}

type bufferedResponse struct {
	scope      payloadScope
	payloadRef *payloadReference
	response   TunnelResponse
	bytes      int
}

type responseReceipt struct {
	expiresAt time.Time
	timer     *time.Timer
}

type responseHub struct {
	connection    *nats.Conn
	subject       string
	subscription  *nats.Subscription
	mu            sync.Mutex
	waiters       map[string]*responseWaiter
	receipts      map[string]responseReceipt
	maxWaiters    int
	bufferedBytes int
	closed        bool
}

type responseWaiter struct {
	broker        *Broker
	key           string
	deadline      time.Time
	wake          chan struct{}
	done          chan struct{}
	closed        bool
	accepting     bool
	notifications []bufferedResponse
	final         *bufferedResponse
	bytes         int
}

func newResponseHub(ctx context.Context, connection *nats.Conn, maxWaiters int) (*responseHub, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	id, err := randomOpaqueToken(24)
	if err != nil {
		return nil, err
	}
	h := &responseHub{connection: connection, subject: "oma.tunnel.response.v1." + brokerKey(id),
		waiters: make(map[string]*responseWaiter), receipts: make(map[string]responseReceipt), maxWaiters: maxWaiters}
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
	if h.closed || envelope.Response == nil {
		return "gone"
	}
	if receipt, ok := h.receipts[envelope.Key]; ok && time.Now().Before(receipt.expiresAt) {
		return "accepted"
	}
	if envelope.ReceiptOnly {
		return "gone"
	}
	w := h.waiters[envelope.Key]
	if w == nil || !w.accepting || !w.broker.now().Before(w.deadline) {
		return "gone"
	}
	if h.bufferedBytes+size > responseHubBytes {
		return "full"
	}
	item := bufferedResponse{response: *envelope.Response, bytes: size, scope: envelope.Scope, payloadRef: envelope.PayloadRef}
	if envelope.Response.terminal() {
		// Keep a dedicated final slot so a full notification queue cannot prevent
		// completion. Global byte accounting still includes the final response.
		w.final, w.accepting = &item, false
		h.rememberCompletion(w)
	} else {
		if len(w.notifications) >= responseSubscriptionBuffer || w.bytes+size > maxBrokerValueBytes {
			return "full"
		}
		w.notifications = append(w.notifications, item)
	}
	w.bytes += size
	h.bufferedBytes += size
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return "accepted"
}

// Caller holds h.mu. Receipts contain no response body and are never persisted.
func (h *responseHub) rememberCompletion(w *responseWaiter) {
	expiry := w.deadline.Add(w.broker.cfg.TombstoneTTL)
	key := w.key
	timer := time.AfterFunc(time.Until(expiry), func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.receipts, key)
	})
	h.receipts[w.key] = responseReceipt{expiresAt: expiry, timer: timer}
}

func (h *responseHub) forwardData(ctx context.Context, origin string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	message, err := h.connection.RequestWithContext(ctx, origin, data)
	if err != nil {
		return fmt.Errorf("forward tunnel response: %w", err)
	}
	switch string(message.Data) {
	case "accepted":
		return nil
	case "full":
		return ErrResponseBackpressure
	case "gone":
		return ErrResponseGone
	default:
		return ErrResponseMismatch
	}
}

func (b *Broker) subscribeResponse(ctx context.Context, requestID string, deadline time.Time) (*responseWaiter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h := b.responseHub
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nats.ErrConnectionClosed
	}
	key := brokerKey(requestID)
	if len(h.waiters) >= h.maxWaiters {
		return nil, ErrQueueLimit
	}
	if h.waiters[key] != nil {
		return nil, ErrResponseMismatch
	}
	if _, exists := h.receipts[key]; exists {
		return nil, ErrResponseMismatch
	}
	w := &responseWaiter{broker: b, key: key, deadline: deadline, wake: make(chan struct{}, 1), done: make(chan struct{}), accepting: true}
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
	w.closeLocked()
	delete(h.waiters, w.key)
}

func (w *responseWaiter) closeLocked() {
	w.closed, w.accepting = true, false
	w.broker.responseHub.bufferedBytes -= w.bytes
	w.bytes, w.notifications, w.final = 0, nil, nil
	close(w.done)
}

func (h *responseHub) close() {
	_ = h.subscription.Unsubscribe()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for key, w := range h.waiters {
		w.closeLocked()
		delete(h.waiters, key)
	}
	for key, receipt := range h.receipts {
		receipt.timer.Stop()
		delete(h.receipts, key)
	}
}

func (w *responseWaiter) drain(ctx context.Context, onNotification func(TunnelResponse)) error {
	h := w.broker.responseHub
	h.mu.Lock()
	count := len(w.notifications)
	h.mu.Unlock()
	for range count {
		h.mu.Lock()
		if len(w.notifications) == 0 {
			h.mu.Unlock()
			return nil
		}
		response := w.notifications[0]
		w.notifications[0] = bufferedResponse{}
		w.notifications = w.notifications[1:]
		w.bytes -= response.bytes
		h.mu.Unlock()
		restored, err := w.restoreResponse(ctx, response)
		if err == nil && onNotification != nil {
			onNotification(restored)
		}
		h.mu.Lock()
		h.bufferedBytes -= response.bytes
		h.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *responseWaiter) Wait(ctx context.Context, onNotification func(TunnelResponse)) (TunnelResponse, error) {
	ctx, cancel := context.WithDeadline(ctx, w.deadline)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return TunnelResponse{}, err
		}
		if err := w.drain(ctx, onNotification); err != nil {
			return TunnelResponse{}, err
		}
		h := w.broker.responseHub
		h.mu.Lock()
		// A notification may have arrived between drain and the final response.
		if w.final != nil && len(w.notifications) == 0 {
			final := *w.final
			w.final = nil
			w.bytes -= final.bytes
			h.bufferedBytes -= final.bytes
			h.mu.Unlock()
			return w.restoreResponse(ctx, final)
		}
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return TunnelResponse{}, ctx.Err()
		case <-w.done:
			return TunnelResponse{}, ErrResponseGone
		case <-w.wake:
		}
	}
}
