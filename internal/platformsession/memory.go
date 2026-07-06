package platformsession

import (
	"context"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]Session
	now      func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: map[string]Session{},
		now:      time.Now,
	}
}

func (s *MemoryStore) Save(_ context.Context, sessionKey string, session Session) error {
	if strings.TrimSpace(sessionKey) == "" {
		return ErrNotFound
	}
	if session.ExpiresAt == nil {
		expiresAt := s.now().UTC().Add(DefaultTTL)
		session.ExpiresAt = &expiresAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[storeKey(sessionKey)] = session
	return nil
}

func (s *MemoryStore) Get(_ context.Context, sessionKey string) (Session, error) {
	if strings.TrimSpace(sessionKey) == "" {
		return Session{}, ErrNotFound
	}
	key := storeKey(sessionKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok {
		return Session{}, ErrNotFound
	}
	if session.Expired(s.now().UTC()) {
		delete(s.sessions, key)
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *MemoryStore) Delete(_ context.Context, sessionKey string) error {
	if strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, storeKey(sessionKey))
	return nil
}
