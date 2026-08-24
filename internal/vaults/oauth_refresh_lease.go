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

// oauthRefreshLease serializes one IdP refresh_token exchange per credential.
// Hold blocks until this caller may exchange, then release must be called.
// TTL on Redis adapters outlives oauthRefreshTimeout so a crashed holder
// cannot pin the key forever.
type oauthRefreshLease interface {
	Hold(ctx context.Context, credentialExternalID string) (release func(), err error)
}

const oauthRefreshLeaseTTL = oauthRefreshTimeout + 5*time.Second

func oauthRefreshLeaseKey(credentialExternalID string) string {
	id := credentialExternalID
	if id == "" {
		id = "<anonymous>"
	}
	return "vault:mcp_oauth_refresh:" + id
}

type memoryOAuthRefreshLease struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newMemoryOAuthRefreshLease() *memoryOAuthRefreshLease {
	return &memoryOAuthRefreshLease{locks: map[string]*sync.Mutex{}}
}

func (l *memoryOAuthRefreshLease) mutex(credentialExternalID string) *sync.Mutex {
	key := oauthRefreshLeaseKey(credentialExternalID)
	l.mu.Lock()
	defer l.mu.Unlock()
	lock, ok := l.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	return lock
}

func (l *memoryOAuthRefreshLease) Hold(ctx context.Context, credentialExternalID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lock := l.mutex(credentialExternalID)
	lock.Lock()
	released := false
	return func() {
		if released {
			return
		}
		released = true
		lock.Unlock()
	}, nil
}

type oauthRefreshNXStore interface {
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	CompareAndDelete(ctx context.Context, key, value string) error
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

func newClientOAuthRefreshLease(client *redis.Client) oauthRefreshLease {
	if client == nil {
		return newMemoryOAuthRefreshLease()
	}
	return newRedisOAuthRefreshLease(redisOAuthRefreshNXStore{client: client}, oauthRefreshLeaseTTL, 50*time.Millisecond)
}

func (l *redisOAuthRefreshLease) Hold(ctx context.Context, credentialExternalID string) (func(), error) {
	token, err := newOAuthRefreshLeaseToken()
	if err != nil {
		return nil, err
	}
	key := oauthRefreshLeaseKey(credentialExternalID)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ok, err := l.store.SetNX(ctx, key, token, l.ttl)
		if err != nil {
			return nil, fmt.Errorf("acquire mcp oauth refresh lease: %w", err)
		}
		if ok {
			released := false
			return func() {
				if released {
					return
				}
				released = true
				_ = l.store.CompareAndDelete(context.WithoutCancel(ctx), key, token)
			}, nil
		}
		timer := time.NewTimer(l.wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
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
