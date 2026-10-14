package vaults

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func (l *redisOAuthRefreshLease) renewHold(ctx context.Context, key, token string) (context.Context, func() error, error) {
	heldCtx, cancel := context.WithCancelCause(ctx)
	renewCtx, stop := context.WithCancel(heldCtx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.renewUntilReleased(renewCtx, key, token, cancel)
	}()
	var once sync.Once
	var releaseErr error
	release := func() error {
		once.Do(func() {
			stop()
			<-done
			cancel(context.Canceled)
			unlockCtx, unlockCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer unlockCancel()
			if err := l.store.CompareAndDelete(unlockCtx, key, token); err != nil {
				releaseErr = fmt.Errorf("release mcp oauth refresh lease: %w", err)
			}
		})
		return releaseErr
	}
	return heldCtx, release, nil
}

func (l *redisOAuthRefreshLease) renewUntilReleased(ctx context.Context, key, token string, cancel context.CancelCauseFunc) {
	interval := l.ttl / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, renewCancel := context.WithTimeout(ctx, interval)
			ok, err := l.store.CompareAndRenew(renewCtx, key, token, l.ttl)
			renewCancel()
			if err != nil {
				cancel(fmt.Errorf("renew mcp oauth refresh lease: %w", err))
				return
			}
			if !ok {
				cancel(errOAuthRefreshLeaseLost)
				return
			}
		}
	}
}
