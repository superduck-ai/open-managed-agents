package dreams

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sessionRuntime interface {
	StopSessionRuntime(ctx context.Context, workspaceUUID, sessionUUID string) error
}

type reclaimStore interface {
	ListDreamsAwaitingRuntimeReclaim(ctx context.Context, limit int) ([]db.Dream, error)
	GetSession(ctx context.Context, workspaceUUID, externalID string) (db.Session, bool, error)
	SetSessionStatus(ctx context.Context, workspaceUUID, externalID, status string) error
}

// terminalRuntimeReclaimer kills leftover sandboxes for terminal Dreams, marks
// Environment Work stopped, and idles an internal Session that is still
// running so InternalSessionArchiver can archive it. Archive itself does not
// stop runtime.
type terminalRuntimeReclaimer struct {
	store   reclaimStore
	runtime sessionRuntime
}

func newTerminalRuntimeReclaimer(store reclaimStore, runtime sessionRuntime) *terminalRuntimeReclaimer {
	if store == nil || runtime == nil {
		return nil
	}
	return &terminalRuntimeReclaimer{store: store, runtime: runtime}
}

func (r *terminalRuntimeReclaimer) RunOnce(ctx context.Context) error {
	if r == nil || r.store == nil || r.runtime == nil {
		return nil
	}
	dreams, err := r.store.ListDreamsAwaitingRuntimeReclaim(ctx, dreamWorkerBatch)
	if err != nil {
		return err
	}
	var errs []error
	for _, dream := range dreams {
		if err := r.reclaim(ctx, dream); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *terminalRuntimeReclaimer) reclaim(ctx context.Context, dream db.Dream) error {
	if dream.InternalSessionUUID == "" {
		return nil
	}
	if err := r.runtime.StopSessionRuntime(ctx, dream.WorkspaceUUID, dream.InternalSessionUUID); err != nil {
		return err
	}
	output, err := dreamOutput(dream.Outputs)
	if err != nil {
		return nil
	}
	session, found, err := r.store.GetSession(ctx, dream.WorkspaceUUID, output.InternalSessionID)
	if err != nil || !found {
		return err
	}
	if session.Status != "running" && session.Status != "rescheduling" {
		return nil
	}
	return r.store.SetSessionStatus(ctx, dream.WorkspaceUUID, output.InternalSessionID, "idle")
}
