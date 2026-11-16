package environments

import (
	"context"
	"errors"
	"time"

	"golang.org/x/mod/semver"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

type managerResumeStore interface {
	FindManagerResumeTarget(context.Context, string, string) (db.ManagerResumeTarget, bool, error)
	WithManagerResumeLock(context.Context, string, func() error) error
	SetManagerResumePhase(context.Context, db.ManagerResumeTarget, string) error
}

type managerResumeProvider interface {
	GetStatus(context.Context, string) (e2bruntime.SandboxStatus, error)
	RenewTimeout(context.Context, string, time.Duration) error
	SetTimeout(context.Context, string, time.Duration) error
}

type managerResumeController struct {
	store    managerResumeStore
	provider managerResumeProvider
	required string
	slots    chan struct{}
	upgrade  func(context.Context, string, db.ManagerResumeTarget) error
}

func (r *Runner) SetTimeout(ctx context.Context, sandboxID string, timeout time.Duration) error {
	if r.managerResumes == nil {
		return r.provider.SetTimeout(ctx, sandboxID, timeout)
	}
	return r.managerResumes.resume(ctx, sandboxID, timeout)
}

func (c *managerResumeController) resume(ctx context.Context, sandboxID string, timeout time.Duration) error {
	status, err := c.provider.GetStatus(ctx, sandboxID)
	if err != nil {
		return err
	}
	if status.WorkspaceUUID == "" {
		if status.State == "paused" && c.required != "" {
			return errManagerScopeMissing
		}
		return c.provider.SetTimeout(ctx, sandboxID, timeout)
	}
	target, found, err := c.store.FindManagerResumeTarget(ctx, status.WorkspaceUUID, sandboxID)
	if err != nil {
		return err
	}
	if c.required == "" && found && target.Phase != "" {
		return errManagerResumeVersionRequired
	}
	if !found || c.required == "" {
		return c.provider.SetTimeout(ctx, sandboxID, timeout)
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	return c.store.WithManagerResumeLock(ctx, target.SandboxUUID, func() error {
		current, found, err := c.store.FindManagerResumeTarget(ctx, status.WorkspaceUUID, sandboxID)
		if err != nil {
			return err
		}
		if !found || current.SandboxUUID != target.SandboxUUID {
			return db.ErrInvalidState
		}
		return c.resumeLocked(ctx, sandboxID, timeout, current)
	})
}

func (c *managerResumeController) resumeLocked(ctx context.Context, sandboxID string, timeout time.Duration, target db.ManagerResumeTarget) error {
	status, err := c.provider.GetStatus(ctx, sandboxID)
	if err != nil {
		return err
	}
	if status.WorkspaceUUID != target.WorkspaceUUID {
		return db.ErrInvalidState
	}
	if status.State == "running" && target.Phase == "" {
		renewErr := c.provider.RenewTimeout(ctx, sandboxID, timeout)
		current, err := c.provider.GetStatus(ctx, sandboxID)
		if err != nil {
			return errors.Join(renewErr, err)
		}
		if current.WorkspaceUUID != target.WorkspaceUUID {
			return db.ErrInvalidState
		}
		if current.State == "running" {
			return renewErr
		}
		if current.State != "paused" {
			return errors.Join(renewErr, db.ErrInvalidState)
		}
		status = current
	}
	if status.State != "paused" && status.State != "running" {
		return db.ErrInvalidState
	}
	if target.Transport != "claim" {
		if target.Phase == "" && semver.IsValid("v"+target.Version) && semver.Compare("v"+target.Version, "v"+c.required) >= 0 {
			return c.provider.SetTimeout(ctx, sandboxID, timeout)
		}
		return errManagerTransportUnsupported
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return db.ErrPreconditionFailed
	}
	if target.Phase == "" {
		if err := c.store.SetManagerResumePhase(ctx, target, "checking"); err != nil {
			return err
		}
		target.Phase = "checking"
	}
	if err := c.provider.SetTimeout(ctx, sandboxID, 150*time.Second); err != nil {
		return err
	}
	if err := c.upgrade(ctx, sandboxID, target); err != nil {
		return err
	}
	return c.provider.SetTimeout(ctx, sandboxID, timeout)
}
