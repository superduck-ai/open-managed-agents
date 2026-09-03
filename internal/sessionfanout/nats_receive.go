package sessionfanout

import (
	"encoding/json"

	"github.com/nats-io/nats.go"
)

// receive clears ephemeral state once when a connection gap starts. nats.go
// keeps the subscriptions and restores them automatically after reconnecting.
func (b *NATSBus) receive() {
	defer close(b.done)
	for {
		select {
		case <-b.ctx.Done():
			return
		case _, ok := <-b.statuses:
			if !ok {
				return
			}
			b.resetActiveSessions()
		}
	}
}

func (b *NATSBus) receiveMessage(sessionID string, message *nats.Msg) {
	if b.ctx.Err() != nil {
		return
	}
	var envelope Envelope
	if err := json.Unmarshal(message.Data, &envelope); err != nil {
		b.logger.WarnContext(b.ctx, "discard malformed session event fanout", "session_id", sessionID, "error", err)
		return
	}
	if err := validateEnvelope(envelope); err != nil {
		b.logger.WarnContext(b.ctx, "discard invalid session event fanout", "session_id", sessionID, "error", err)
		return
	}
	b.mu.Lock()
	handlers := append([]Handler(nil), b.handlers...)
	b.mu.Unlock()
	for _, handler := range handlers {
		handler(b.ctx, sessionID, envelope)
	}
}

func (b *NATSBus) resetActiveSessions() {
	b.mu.Lock()
	sessionIDs := make([]string, 0, len(b.subscriptions))
	for _, state := range b.subscriptions {
		sessionIDs = append(sessionIDs, state.sessionID)
	}
	b.mu.Unlock()
	for _, sessionID := range sessionIDs {
		b.runResetters(sessionID)
	}
}

func (b *NATSBus) runResetters(sessionID string) {
	b.mu.Lock()
	resetters := append([]ResetHandler(nil), b.resetters...)
	b.mu.Unlock()
	for _, reset := range resetters {
		reset(b.ctx, sessionID)
	}
}
