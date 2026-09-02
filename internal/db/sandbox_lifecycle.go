package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

// SandboxReclaimTarget identifies an immutable sandbox attempt, never a replacement.
type SandboxReclaimTarget struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	SandboxUUID       string
	ProviderSandboxID string
	Reclaiming        bool
}

func (t SandboxReclaimTarget) scope() sandboxLifecycleScope {
	return sandboxLifecycleScope{OrganizationUUID: t.OrganizationUUID, WorkspaceUUID: t.WorkspaceUUID, SandboxUUID: t.SandboxUUID}
}

func (d *DB) ListSandboxReclaimCandidates(ctx context.Context, cutoff time.Time, after string, limit int, reclaim bool) ([]SandboxReclaimTarget, error) {
	rows, err := NewSandboxLifecycleMapper(d.mapperDB).ListCandidates(ctx, cutoff, after, limit, reclaim)
	if err != nil {
		return nil, err
	}
	targets := make([]SandboxReclaimTarget, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, row.reclaimTarget())
	}
	return targets, nil
}

// BeginSandboxReclamation serializes with public input and worker state updates.
// Only a committed claim authorizes a provider deletion. Repeated claims retain
// the same immutable sandbox UUID and cannot target a replacement.
func (d *DB) BeginSandboxReclamation(ctx context.Context, target SandboxReclaimTarget, cutoff time.Time, allowNewClaim bool) (SandboxReclaimTarget, bool, error) {
	var claimed bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxLifecycleMapper(executor)
		current, found, err := mapper.FindTarget(ctx, target.scope())
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if current.State == "stopping" && current.StopReason != nil && *current.StopReason == "idle_timeout" {
			target, claimed = current.reclaimTarget(), true
			return nil
		}
		if !allowNewClaim {
			return nil
		}
		_, found, err = NewSessionMapper(executor).LockSessionForEvents(ctx, current.WorkspaceUUID, current.SessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		_, found, err = NewCodeSessionMapper(executor).LockCodeSessionByExternalID(ctx, current.CodeSessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		current, found, err = mapper.LockTarget(ctx, target.scope())
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		rows, err := mapper.Claim(ctx, current.CodeSessionUUID, current.SandboxUUID, cutoff)
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		if err := mapper.BeginStop(ctx, target.scope()); err != nil {
			return err
		}
		target, claimed = current.reclaimTarget(), true
		target.Reclaiming = true
		return nil
	})
	return target, claimed, err
}

func (d *DB) CompleteSandboxReclamation(ctx context.Context, target SandboxReclaimTarget) (bool, error) {
	var completed bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxLifecycleMapper(executor)
		current, found, err := mapper.FindTarget(ctx, target.scope())
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		// Lock the parent before the worker and work, matching public ingress.
		if _, _, err := NewSessionMapper(executor).LockSessionForEvents(ctx, current.WorkspaceUUID, current.SessionExternalID); err != nil {
			return err
		}
		// Keep the same lock order as claim/ingress, even when the session was terminated.
		_, _, err = NewCodeSessionMapper(executor).LockCodeSessionByExternalID(ctx, current.CodeSessionExternalID)
		if err != nil {
			return err
		}
		locked, found, err := mapper.LockTarget(ctx, target.scope())
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		current = locked
		rows, err := mapper.FinishStop(ctx, target.scope())
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		completed = true
		// Input accepted during deletion uses the existing missing-sandbox recovery path.
		// Without pending input, the stopped sandbox stays reclaimed.
		_, err = NewEnvironmentSandboxMapper(executor).ScheduleRecoveryForCodeSession(ctx, environmentSandboxRecoveryParams{
			CodeSessionExternalID: current.CodeSessionExternalID,
			ProviderSandboxID:     current.ProviderSandboxID,
			LastError:             "sandbox reclaimed after idle timeout",
		})
		return err
	})
	return completed, err
}

func (r sandboxLifecycleRow) reclaimTarget() SandboxReclaimTarget {
	return SandboxReclaimTarget{OrganizationUUID: r.OrganizationUUID, WorkspaceUUID: r.WorkspaceUUID,
		SandboxUUID: r.SandboxUUID, ProviderSandboxID: r.ProviderSandboxID,
		Reclaiming: r.State == "stopping" && r.StopReason != nil && *r.StopReason == "idle_timeout"}
}
