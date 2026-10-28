package environments

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type backgroundContextProvider struct {
	rcloneTestProvider
	started context.Context
	start   func(context.Context) error
}

func (p *backgroundContextProvider) StartBackgroundCommand(ctx context.Context, _, _ string, _ []byte) error {
	p.started = ctx
	if p.start != nil {
		return p.start(ctx)
	}
	return nil
}

func TestSandboxBackgroundStartCanceledDuringPreparation(t *testing.T) {
	for _, reason := range []string{"parent", "deadline", "renewal"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent, cancelParent := context.WithCancel(t.Context())
				defer cancelParent()
				owner, cancelOwner := context.WithTimeout(parent, sandboxPreparationMaxAge)
				defer cancelOwner()
				failure := errors.New("replaced owner")
				calls := 0
				ctx, stop, err := keepSandboxPreparationLease(owner, 30*time.Second, func(context.Context) error {
					calls++
					if reason == "renewal" && calls == 2 {
						return failure
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				defer stop()
				runtimeCtx, finish := sandboxPreparationBackgroundContext(parent, ctx)
				defer finish(false)
				provider := &backgroundContextProvider{start: func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				}}
				result := make(chan error, 1)
				go func() { result <- provider.StartBackgroundCommand(runtimeCtx, "sandbox", "manager", nil) }()
				synctest.Wait()
				if reason == "parent" {
					cancelParent()
				} else {
					time.Sleep(sandboxPreparationMaxAge + time.Second)
				}
				synctest.Wait()
				if err := <-result; err == nil || provider.started.Err() == nil {
					t.Fatalf("preparation cancellation did not stop background start: %v", err)
				}
				if reason == "renewal" && !errors.Is(context.Cause(ctx), failure) {
					t.Fatalf("renewal failure lost: %v", context.Cause(ctx))
				}
			})
		})
	}
}

func TestSandboxBackgroundRejectsFailedHandoff(t *testing.T) {
	for _, reason := range []string{"later_failure", "owner_canceled"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				preparation, cancelPreparation := context.WithCancelCause(t.Context())
				defer cancelPreparation(nil)
				runtimeCtx, finish := sandboxPreparationBackgroundContext(t.Context(), preparation)
				defer finish(false)
				provider := &backgroundContextProvider{}
				if err := provider.StartBackgroundCommand(runtimeCtx, "sandbox", "manager", nil); err != nil {
					t.Fatal(err)
				}
				success := false
				if reason == "owner_canceled" {
					cancelPreparation(errors.New("preparation owner replaced"))
					success = true
				}
				if err := finish(success); err == nil {
					t.Fatal("failed preparation was handed off")
				}
				synctest.Wait()
				if provider.started.Err() == nil {
					t.Fatal("failed preparation left background command alive")
				}
			})
		})
	}
}

func TestSandboxBackgroundSurvivesSuccessfulPreparation(t *testing.T) {
	for _, command := range []string{"rclone", "manager"} {
		t.Run(command, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent, cancelParent := context.WithCancel(t.Context())
				defer cancelParent()
				owner, cancelOwner := context.WithTimeout(parent, sandboxPreparationMaxAge)
				defer cancelOwner()
				renewals := 0
				ctx, stop, err := keepSandboxPreparationLease(owner, 30*time.Second, func(context.Context) error { renewals++; return nil })
				if err != nil {
					t.Fatal(err)
				}
				defer stop()
				runtimeCtx, finish := sandboxPreparationBackgroundContext(parent, ctx)
				defer finish(false)
				provider := &backgroundContextProvider{rcloneTestProvider: rcloneTestProvider{ready: true}}
				runner := &Runner{provider: provider, logger: logging.LoggerOrDefault(nil)}
				if command == "rclone" {
					err = runner.startManagedAgentSessionFilesystem(ctx, runtimeCtx, "sandbox", rcloneFilestoreLaunch{})
				} else {
					err = provider.StartBackgroundCommand(runtimeCtx, "sandbox", "manager", nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := finish(true); err != nil {
					t.Fatal(err)
				}
				stop()
				cancelOwner()
				time.Sleep(sandboxPreparationMaxAge + time.Second)
				synctest.Wait()
				if provider.started == nil || provider.started.Err() != nil {
					t.Fatal("successful preparation canceled the background process context")
				}
				if ctx.Err() == nil || renewals != 1 {
					t.Fatalf("preparation lease not stopped: err=%v renewals=%d", ctx.Err(), renewals)
				}
				cancelParent()
				synctest.Wait()
				if !errors.Is(provider.started.Err(), context.Canceled) {
					t.Fatal("background process escaped parent cancellation")
				}
			})
		})
	}
}
