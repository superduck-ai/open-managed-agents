package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const requestBindingTimeout = 2 * time.Second

// RequestBindings holds immutable claim metadata, shared by all OMA instances.
// Losing a binding fails the in-flight request; it never authorizes redelivery.
type RequestBindings struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRequestBindings reuses the application Redis pool. The application owns it.
func NewRequestBindings(client *redis.Client, ttl time.Duration) *RequestBindings {
	if client != nil {
		client = client.WithTimeout(requestBindingTimeout)
	}
	return &RequestBindings{client: client, ttl: ttl}
}

func requestBindingKey(key string) string { return "oma:tunnel:request:v1:" + key }

func (s *RequestBindings) create(ctx context.Context, key string, record requestRecord) error {
	data, err := encodeTunnelJSON(record, maxRequestBindingBytes)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, requestBindingTimeout)
	defer cancel()
	// Send PX explicitly, even for whole-second TTLs. NX failures never extend it.
	err = s.client.Do(ctx, "SET", requestBindingKey(key), data, "NX", "PX", max(int64(1), s.ttl.Milliseconds())).Err()
	if errors.Is(err, redis.Nil) {
		return errRequestBindingExists
	}
	return err
}

func (s *RequestBindings) read(ctx context.Context, key string) (requestRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, requestBindingTimeout)
	defer cancel()
	data, err := s.client.Get(ctx, requestBindingKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return requestRecord{}, ErrRequestNotFound
	}
	if err != nil {
		return requestRecord{}, err
	}
	var record requestRecord
	if len(data) > maxRequestBindingBytes {
		return record, fmt.Errorf("decode tunnel binding: metadata exceeds limit")
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("decode tunnel binding: %w", err)
	}
	return record, nil
}

func (s *RequestBindings) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestBindingTimeout)
	defer cancel()
	return s.client.Ping(ctx).Err()
}
