package platformsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(ctx context.Context, redisURL string) (*RedisStore, error) {
	if strings.TrimSpace(redisURL) == "" {
		return nil, errors.New("redis.url is required")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis.url: %w", err)
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}
	return &RedisStore{client: client}, nil
}

func (s *RedisStore) Save(ctx context.Context, sessionKey string, session Session) error {
	if strings.TrimSpace(sessionKey) == "" {
		return ErrNotFound
	}
	now := time.Now().UTC()
	if session.ExpiresAt == nil {
		expiresAt := now.Add(DefaultTTL)
		session.ExpiresAt = &expiresAt
	}
	ttl := ttlUntil(session.ExpiresAt, now)
	if ttl <= 0 {
		return ErrNotFound
	}
	body, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, storeKey(sessionKey), body, ttl).Err()
}

func (s *RedisStore) Get(ctx context.Context, sessionKey string) (Session, error) {
	if strings.TrimSpace(sessionKey) == "" {
		return Session{}, ErrNotFound
	}
	body, err := s.client.Get(ctx, storeKey(sessionKey)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(body, &session); err != nil {
		return Session{}, err
	}
	if session.Expired(time.Now().UTC()) {
		_ = s.Delete(ctx, sessionKey)
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *RedisStore) Delete(ctx context.Context, sessionKey string) error {
	if strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	return s.client.Del(ctx, storeKey(sessionKey)).Err()
}

func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
