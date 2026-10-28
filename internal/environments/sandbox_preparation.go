package environments

import (
	"context"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const sandboxPreparationMaxAge = 3 * time.Minute

func (r *Runner) startSandboxPreparation(ctx context.Context, record db.EnvironmentSandbox, work db.EnvironmentWork, providerID string) (context.Context, func(), error) {
	if err := r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceUUID, record.ExternalID, "creating", &providerID, nil, nil); err != nil {
		return nil, nil, err
	}
	preparationCtx, cancelPreparation := context.WithDeadline(ctx, record.CreatedAt.Add(sandboxPreparationMaxAge))
	ttl := r.cfg.E2B.SandboxTimeout
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	leaseCtx, stopLease, err := keepSandboxPreparationLease(preparationCtx, ttl, func(renewCtx context.Context) error {
		return r.renewSandboxPreparation(renewCtx, record, work, providerID, ttl)
	})
	if err != nil {
		cancelPreparation()
		return nil, nil, err
	}
	return leaseCtx, func() { stopLease(); cancelPreparation() }, nil
}

func (r *Runner) renewSandboxPreparation(ctx context.Context, record db.EnvironmentSandbox, work db.EnvironmentWork, providerID string, ttl time.Duration) error {
	current, err := r.db.GetEnvironmentWork(ctx, work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
	if err != nil {
		return err
	}
	if current.UUID != work.UUID || (current.State != "starting" && current.State != "active") {
		return db.ErrInvalidState
	}
	session, found, err := r.db.GetSessionByUUID(ctx, work.WorkspaceUUID, work.SessionUUID)
	if err != nil {
		return err
	}
	if !found || session.OrganizationUUID != work.OrganizationUUID || session.ArchivedAt != nil || session.DeletedAt != nil || session.Status == "terminated" {
		return db.ErrInvalidState
	}
	sandbox, found, err := r.db.GetPendingEnvironmentSandboxForWork(ctx, work)
	if err != nil {
		return err
	}
	if !found || sandbox.UUID != record.UUID || (sandbox.State != "creating" && sandbox.State != "running") || sandbox.ProviderSandboxID == nil || *sandbox.ProviderSandboxID != providerID {
		return db.ErrInvalidState
	}
	return r.provider.SetTimeout(ctx, providerID, ttl)
}

func keepSandboxPreparationLease(ctx context.Context, ttl time.Duration, renew func(context.Context) error) (context.Context, func(), error) {
	interval := max(ttl/3, time.Nanosecond)
	leaseCtx, cancel := context.WithCancelCause(ctx)
	refresh := func() error {
		if err := leaseCtx.Err(); err != nil {
			return err
		}
		requestCtx, finish := context.WithTimeout(leaseCtx, interval)
		defer finish()
		return renew(requestCtx)
	}
	if err := refresh(); err != nil {
		cancel(err)
		return nil, nil, fmt.Errorf("renew sandbox preparation lease: %w", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				if err := refresh(); err != nil {
					cancel(fmt.Errorf("renew sandbox preparation lease: %w", err))
					return
				}
			}
		}
	}()
	return leaseCtx, func() { cancel(nil); <-done }, nil
}

func sandboxPreparationBackgroundContext(parent, preparation context.Context) (context.Context, func(bool) error) {
	runtimeCtx, cancel := context.WithCancelCause(parent)
	stopPropagation := context.AfterFunc(preparation, func() { cancel(context.Cause(preparation)) })
	finished := false
	return runtimeCtx, func(success bool) error {
		if finished {
			return nil
		}
		finished = true
		detached := stopPropagation()
		if !success || !detached || preparation.Err() != nil {
			cancel(context.Cause(preparation))
		}
		return context.Cause(runtimeCtx)
	}
}

func (r *Runner) finishSandboxPreparation(ctx context.Context, record db.EnvironmentSandbox, work *db.EnvironmentWork, providerID string, finishBackground func(bool) error) error {
	err := finishBackground(true)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, providerID, err)
	}
	return err
}
