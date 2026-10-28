package environments

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
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

func TestSandboxPreparationHandoffFailureCleansPublishedRuntime(t *testing.T) {
	for _, tc := range []struct {
		name                                                           string
		managed, recovered, canceled, parentCanceled, terminationFails bool
		wantTerminations, wantSandboxCleanups                          int
	}{
		{name: "new managed runtime fails after publication", managed: true, canceled: true, wantTerminations: 1, wantSandboxCleanups: 1},
		{name: "parent cancellation cleans the published runtime", managed: true, parentCanceled: true, wantTerminations: 1, wantSandboxCleanups: 1},
		{name: "termination error still cleans sandbox once", managed: true, canceled: true, terminationFails: true, wantTerminations: 1, wantSandboxCleanups: 1},
		{name: "recovery preserves the durable session", managed: true, recovered: true, canceled: true, wantSandboxCleanups: 1},
		{name: "non managed cleanup", canceled: true, wantSandboxCleanups: 1},
		{name: "successful handoff", managed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancelParent := context.WithCancel(t.Context())
			defer cancelParent()
			owner, cancel := context.WithCancelCause(parent)
			defer cancel(nil)
			runtimeCtx, finish := sandboxPreparationBackgroundContext(parent, owner)
			defer finish(false)
			runtime := &handoffCodeSessionRuntime{}
			if tc.terminationFails {
				runtime.terminationError = errors.New("termination failed")
			}
			runner := &Runner{codeSessions: runtime, logger: logging.LoggerOrDefault(nil)}
			var preparation *managedAgentLaunchPreparation
			var launch *managedAgentRuntimeLaunch
			if tc.managed {
				preparation = &managedAgentLaunchPreparation{Session: db.Session{ExternalID: "session_test"}}
				launch = &managedAgentRuntimeLaunch{CodeSessionID: "cse_published", Recovered: tc.recovered}
			}
			failure := errors.New("preparation lease failed after publication")
			if tc.parentCanceled {
				failure = context.Canceled
				cancelParent()
			} else if tc.canceled {
				cancel(failure)
			}
			cleanups := 0
			err := runner.finishSandboxPreparation(finish, preparation, launch, func(cause error) {
				cleanups++
				if !errors.Is(cause, failure) {
					t.Fatalf("cleanup lost handoff cause: %v", cause)
				}
				if len(runtime.terminated) != tc.wantTerminations {
					t.Fatalf("termination before sandbox cleanup = %v, want %d", runtime.terminated, tc.wantTerminations)
				}
			})
			if (tc.canceled || tc.parentCanceled) && (!errors.Is(err, failure) || runtimeCtx.Err() == nil) {
				t.Fatalf("handoff failure lost: err=%v runtime=%v", err, runtimeCtx.Err())
			}
			if !tc.canceled && !tc.parentCanceled && err != nil {
				t.Fatalf("successful handoff failed: %v", err)
			}
			if cleanups != tc.wantSandboxCleanups || len(runtime.terminated) != tc.wantTerminations {
				t.Fatalf("cleanups=%d terminations=%v", cleanups, runtime.terminated)
			}
			if tc.wantTerminations == 1 && (runtime.terminated[0] != "cse_published" || runtime.sessionID != "session_test" || runtime.cleanupContextErr != nil) {
				t.Fatalf("wrong cleanup identity or canceled context: ids=%v session=%s context=%v", runtime.terminated, runtime.sessionID, runtime.cleanupContextErr)
			}
		})
	}
}

type handoffCodeSessionRuntime struct {
	mcpConfigCodeSessionRuntime
	sessionID         string
	cleanupContextErr error
	terminationError  error
}

func (r *handoffCodeSessionRuntime) TerminateManagedAgentCodeSession(ctx context.Context, session db.Session, id string) error {
	r.terminated = append(r.terminated, id)
	r.sessionID = session.ExternalID
	r.cleanupContextErr = ctx.Err()
	return r.terminationError
}
