package sessionfanout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

type Handler func(context.Context, string, Envelope)

type ResetHandler func(context.Context, string)

type EventBus interface {
	Publish(context.Context, string, Envelope) error
	Subscribe(context.Context, string) error
	Unsubscribe(string) error
	Register(Handler, ResetHandler)
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
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if err := validateEnvelope(envelope); err != nil {
		return err
	}
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers...)
	b.mu.RUnlock()
	for _, handler := range handlers {
		handler(ctx, sessionID, envelope)
	}
	return nil
}

func (b *LocalBus) Subscribe(_ context.Context, sessionID string) error {
	return validateSessionID(sessionID)
}

func (b *LocalBus) Unsubscribe(sessionID string) error {
	return validateSessionID(sessionID)
}

func (b *LocalBus) Register(handler Handler, _ ResetHandler) {
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

func validateEnvelope(envelope Envelope) error {
	if !envelope.Kind.valid() {
		return fmt.Errorf("unsupported session fanout kind %q", envelope.Kind)
	}
	if len(envelope.Payload) == 0 {
		return errors.New("session fanout payload is required")
	}
	if !json.Valid(envelope.Payload) {
		return errors.New("session fanout payload must be valid JSON")
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

func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return errEmptySessionID
	}
	for _, c := range sessionID {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return errInvalidSubject
	}
	return nil
}
