package porttunnel

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	ownerTTL           = 60 * time.Second
	ownerRefreshPeriod = 20 * time.Second
	ownerKeyPrefix     = "oma:session_port:owner:"
)

type OwnerStore interface {
	Get(ctx context.Context, codeSessionUUID string) (string, error)
	Claim(ctx context.Context, codeSessionUUID, owner string) error
	Refresh(ctx context.Context, codeSessionUUID, owner string) error
	Release(ctx context.Context, codeSessionUUID, owner string) error
}

type RedisOwnerStore struct {
	client *redis.Client
}

func NewRedisOwnerStore(client *redis.Client) *RedisOwnerStore {
	return &RedisOwnerStore{client: client}
}

func ownerKey(codeSessionUUID string) string {
	return ownerKeyPrefix + codeSessionUUID
}

var refreshOwnerScript = redis.NewScript(`if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("PEXPIRE", KEYS[1], ARGV[2]) end return redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2], "NX") and 1 or 0`)

var releaseOwnerScript = redis.NewScript(`if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`)

func (s *RedisOwnerStore) Get(ctx context.Context, codeSessionUUID string) (string, error) {
	owner, err := s.client.Get(ctx, ownerKey(codeSessionUUID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return owner, err
}

func (s *RedisOwnerStore) Claim(ctx context.Context, codeSessionUUID, owner string) error {
	return s.client.Set(ctx, ownerKey(codeSessionUUID), owner, ownerTTL).Err()
}

func (s *RedisOwnerStore) Refresh(ctx context.Context, codeSessionUUID, owner string) error {
	return refreshOwnerScript.Run(ctx, s.client, []string{ownerKey(codeSessionUUID)}, owner, ownerTTL.Milliseconds()).Err()
}

func (s *RedisOwnerStore) Release(ctx context.Context, codeSessionUUID, owner string) error {
	return releaseOwnerScript.Run(ctx, s.client, []string{ownerKey(codeSessionUUID)}, owner).Err()
}
