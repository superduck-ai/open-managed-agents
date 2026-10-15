package vaults

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type failingRenewalStore struct {
	*fakeOAuthRefreshNXStore
	err  error
	wait bool
}

func (s failingRenewalStore) CompareAndRenew(ctx context.Context, _, _ string, _ time.Duration) (bool, error) {
	if s.wait {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return false, s.err
}

func TestRedisOAuthRefreshLeaseRenewalFailureCancelsHolder(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		t.Run(map[bool]string{false: "redis error", true: "redis timeout"}[waiting], func(t *testing.T) {
			failure := errors.New("redis unavailable")
			store := failingRenewalStore{fakeOAuthRefreshNXStore: newFakeOAuthRefreshNXStore(), err: failure, wait: waiting}
			lease := newRedisOAuthRefreshLease(store, 90*time.Millisecond, time.Millisecond)
			ctx, release, err := lease.Hold(t.Context(), "vcrd_a")
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("renewal failure did not cancel refresh")
			}
			if waiting {
				failure = context.DeadlineExceeded
			}
			if !errors.Is(context.Cause(ctx), failure) {
				t.Fatalf("lease cancellation cause = %v", context.Cause(ctx))
			}
		})
	}
}

func TestRedisOAuthRefreshLeaseLossPreservesReplacement(t *testing.T) {
	store := newFakeOAuthRefreshNXStore()
	lease := newRedisOAuthRefreshLease(store, 90*time.Millisecond, time.Millisecond)
	ctx, release, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	key := oauthRefreshLeaseKey("vcrd_a")
	store.mu.Lock()
	store.values[key] = "replacement"
	store.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("lost ownership did not cancel refresh")
	}
	if !errors.Is(context.Cause(ctx), errOAuthRefreshLeaseLost) {
		t.Fatal(context.Cause(ctx))
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.values[key] != "replacement" {
		t.Fatal("old holder deleted replacement lease")
	}
}

func TestRedisOAuthRefreshLeaseReleaseCancelsHolder(t *testing.T) {
	lease := newRedisOAuthRefreshLease(newFakeOAuthRefreshNXStore(), time.Second, time.Millisecond)
	ctx, release, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("released lease context remains usable")
	}
	_, next, err := lease.Hold(t.Context(), "vcrd_a")
	if err != nil {
		t.Fatal(err)
	}
	defer next()
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisOAuthRefreshLeaseRenewScriptChecksOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	store := redisOAuthRefreshNXStore{client: client}
	ctx := t.Context()
	if ok, err := store.CompareAndRenew(ctx, "lease", "missing", time.Second); err != nil || ok {
		t.Fatalf("missing lease renewed: %v", err)
	}
	if ok, err := store.SetNX(ctx, "lease", "holder", time.Second); err != nil || !ok {
		t.Fatal(err)
	}
	server.FastForward(400 * time.Millisecond)
	if ok, err := store.CompareAndRenew(ctx, "lease", "other", time.Second); err != nil || ok {
		t.Fatalf("wrong holder renewed: %v", err)
	}
	if server.TTL("lease") != 600*time.Millisecond {
		t.Fatal("wrong holder changed TTL")
	}
	if ok, err := store.CompareAndRenew(ctx, "lease", "holder", time.Second); err != nil || !ok {
		t.Fatal(err)
	}
	if server.TTL("lease") != time.Second {
		t.Fatal("renewal did not extend TTL")
	}
	server.FastForward(1100 * time.Millisecond)
	if ok, err := store.CompareAndRenew(ctx, "lease", "holder", time.Second); err != nil || ok {
		t.Fatalf("expired lease revived: %v", err)
	}
}
