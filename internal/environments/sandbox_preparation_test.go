package environments

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestSandboxPreparationLeaseRejectsFailedRenewal(t *testing.T) {
	failure := errors.New("provider missing")
	ctx, stop, err := keepSandboxPreparationLease(t.Context(), 30*time.Second, func(context.Context) error { return failure })
	if !errors.Is(err, failure) || ctx != nil || stop != nil {
		t.Fatalf("initial renewal = (%v,%v,%v)", ctx, stop == nil, err)
	}
}

func TestSandboxPreparationLeaseRejectsCanceledOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	_, _, err := keepSandboxPreparationLease(ctx, 30*time.Second, func(context.Context) error { calls++; return nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("canceled owner: calls=%d err=%v", calls, err)
	}
}

func TestSandboxPreparationLeaseStopsOnRenewalFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("terminal or replaced sandbox")
		var calls atomic.Int64
		ctx, stop, err := keepSandboxPreparationLease(t.Context(), 30*time.Second, func(context.Context) error {
			if calls.Add(1) == 2 {
				return failure
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(40 * time.Second)
		if !errors.Is(context.Cause(ctx), failure) {
			t.Fatalf("owner not canceled: %v", context.Cause(ctx))
		}
		stop()
		if calls.Load() != 2 {
			t.Fatalf("renewed after failure: %d", calls.Load())
		}
	})
}

func TestSandboxPreparationLeaseSurvivesLongPreparationAndHandsOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ttl := 30 * time.Second
		expires := time.Now().Add(ttl)
		var calls atomic.Int64
		ctx, stop, err := keepSandboxPreparationLease(t.Context(), ttl, func(context.Context) error {
			if time.Now().After(expires) {
				return errors.New("creating sandbox paused")
			}
			expires = time.Now().Add(ttl)
			calls.Add(1)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(75 * time.Second)
		if ctx.Err() != nil || calls.Load() < 7 {
			t.Fatalf("preparation expired: calls=%d cause=%v", calls.Load(), context.Cause(ctx))
		}
		stop()
		count := calls.Load()
		time.Sleep(time.Minute)
		if calls.Load() != count {
			t.Fatal("renewed after handoff")
		}
	})
}

func TestSandboxPreparationLeaseStopsAtOwnerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		owner, cancel := context.WithTimeout(t.Context(), sandboxPreparationMaxAge)
		defer cancel()
		var calls atomic.Int64
		ctx, stop, err := keepSandboxPreparationLease(owner, 30*time.Second, func(context.Context) error { calls.Add(1); return nil })
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(sandboxPreparationMaxAge + time.Second)
		if !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
			t.Fatalf("unbounded creating owner: %v", context.Cause(ctx))
		}
		stop()
		count := calls.Load()
		time.Sleep(time.Minute)
		if calls.Load() != count {
			t.Fatal("renewed expired preparation")
		}
	})
}
