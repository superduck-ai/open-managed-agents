package vaults

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type OAuthRefreshLease interface {
	Hold(ctx context.Context, credentialExternalID string) (context.Context, func() error, error)
}

const oauthRefreshLeaseTTL = maxOAuthRefreshCASAttempts*oauthRefreshTimeout + 5*time.Second

func oauthRefreshLeaseKey(credentialExternalID string) string {
	return "vault:mcp_oauth_refresh:" + credentialExternalID
}

type memoryOAuthRefreshLease struct {
	mu   sync.Mutex
	sems map[string]chan struct{}
}

func newMemoryOAuthRefreshLease() *memoryOAuthRefreshLease {
	return &memoryOAuthRefreshLease{sems: map[string]chan struct{}{}}
}

func (l *memoryOAuthRefreshLease) semaphore(credentialExternalID string) chan struct{} {
	key := oauthRefreshLeaseKey(credentialExternalID)
	l.mu.Lock()
	defer l.mu.Unlock()
	sem, ok := l.sems[key]
	if !ok {
		// Cap 1: send acquires, receive releases. No helper goroutine, so ctx
		// cancel cannot strand a holder that already took the lock.
		sem = make(chan struct{}, 1)
		l.sems[key] = sem
	}
	return sem
}

func (l *memoryOAuthRefreshLease) Hold(ctx context.Context, credentialExternalID string) (context.Context, func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	sem := l.semaphore(credentialExternalID)
	select {
	case sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-sem
			return nil, nil, err
		}
		released := false
		return ctx, func() error {
			if released {
				return nil
			}
			released = true
			<-sem
			return nil
		}, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

type oauthRefreshNXStore interface {
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	CompareAndDelete(ctx context.Context, key, value string) error
	CompareAndRenew(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
}

type redisOAuthRefreshLease struct {
	store oauthRefreshNXStore
	ttl   time.Duration
	wait  time.Duration
}

func newRedisOAuthRefreshLease(store oauthRefreshNXStore, ttl, wait time.Duration) *redisOAuthRefreshLease {
	if ttl <= 0 {
		ttl = oauthRefreshLeaseTTL
	}
	if wait <= 0 {
		wait = 50 * time.Millisecond
	}
	return &redisOAuthRefreshLease{store: store, ttl: ttl, wait: wait}
}

func NewRedisOAuthRefreshLease(client *redis.Client) (OAuthRefreshLease, error) {
	if client == nil {
		return nil, errOAuthRefreshLeaseRedisRequired
	}
	return newRedisOAuthRefreshLease(redisOAuthRefreshNXStore{client: client}, oauthRefreshLeaseTTL, 50*time.Millisecond), nil
}

func (l *redisOAuthRefreshLease) Hold(ctx context.Context, credentialExternalID string) (context.Context, func() error, error) {
	token, err := newOAuthRefreshLeaseToken()
	if err != nil {
		return nil, nil, err
	}
	key := oauthRefreshLeaseKey(credentialExternalID)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		ok, err := l.store.SetNX(ctx, key, token, l.ttl)
		if err != nil {
			return nil, nil, fmt.Errorf("acquire mcp oauth refresh lease: %w", err)
		}
		if ok {
			return l.renewHold(ctx, key, token)
		}
		timer := time.NewTimer(l.wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func newOAuthRefreshLeaseToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate mcp oauth refresh lease token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

type redisOAuthRefreshNXStore struct {
	client *redis.Client
}

var oauthRefreshLeaseUnlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

func (s redisOAuthRefreshNXStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, value, ttl).Result()
}

func (s redisOAuthRefreshNXStore) CompareAndDelete(ctx context.Context, key, value string) error {
	return oauthRefreshLeaseUnlockScript.Run(ctx, s.client, []string{key}, value).Err()
}

var oauthRefreshLeaseRenewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

func (s redisOAuthRefreshNXStore) CompareAndRenew(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	result, err := oauthRefreshLeaseRenewScript.Run(ctx, s.client, []string{key}, value, ttl.Milliseconds()).Int64()
	return result == 1, err
}
