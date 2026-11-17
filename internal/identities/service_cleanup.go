package identities

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const CleanupQueue = "identity_cleanup"

type CleanupEnqueuer interface {
	EnqueueIdentityCleanup(ctx context.Context, workspaceUUID, identityUUID, cleanupID string) error
}

type ClearResult struct {
	CleanupID string
	State     string
	Outcome   CleanupOutcome
}

func (s *Service) UseCleanupQueue(jobs CleanupEnqueuer) {
	s.jobs = jobs
}

func (s *Service) ClearIdentity(ctx context.Context, principal auth.Principal, identityID string) (ClearResult, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return ClearResult{}, err
	}
	running, err := s.store.ListRunningIdentitySessions(ctx, principal.WorkspaceUUID, identity.UUID)
	if err != nil {
		return ClearResult{}, unavailable(err)
	}
	if CleanupBlocked(CleanupPreview{RunningSessions: running}) {
		outcome := CleanupOutcome{Completed: false, RunningSessions: running, PersonalVaultsDeleted: false}
		message := "身份仍有未结束的执行或授权等待，请先停止后再清理"
		if len(running) > 0 {
			message += "：" + strings.Join(running, ",")
		}
		return ClearResult{State: "conflict", Outcome: outcome}, conflict(message, nil)
	}
	shared := s.sharedVaults(ctx, principal.WorkspaceUUID, identity.UUID)
	cleanupID, err := ids.New("idcl_")
	if err != nil {
		return ClearResult{}, unavailable(err)
	}
	if _, err := s.store.CreateIdentityCleanupRun(ctx, db.InsertCleanupRunParams{
		UUID: uuid.NewString(), ExternalID: cleanupID, OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID: principal.WorkspaceUUID, IdentityUUID: identity.UUID, State: "queued", Result: "{}",
	}); err != nil {
		return ClearResult{}, unavailable(err)
	}
	if s.jobs != nil {
		if err := s.jobs.EnqueueIdentityCleanup(ctx, principal.WorkspaceUUID, identity.UUID, cleanupID); err != nil {
			return ClearResult{}, unavailable(err)
		}
		return ClearResult{CleanupID: cleanupID, State: "queued", Outcome: CleanupOutcome{Completed: false, RetainedSharedVaults: shared, PersonalVaultsDeleted: false}}, nil
	}
	outcome := s.ApplyCleanup(ctx, principal.WorkspaceUUID, identity.UUID, shared)
	state := "completed"
	if !outcome.Completed {
		state = "partial"
	}
	if err := s.store.SaveIdentityCleanupRun(ctx, principal.WorkspaceUUID, cleanupID, state, mustJSON(outcome)); err != nil {
		outcome.Failures = append(outcome.Failures, "cleanup status was not saved")
		outcome.Completed = false
		state = "partial"
	}
	return ClearResult{CleanupID: cleanupID, State: state, Outcome: outcome}, nil
}

func (s *Service) ApplyCleanup(ctx context.Context, workspaceUUID, identityUUID string, shared []string) CleanupOutcome {
	if shared == nil {
		shared = s.sharedVaults(ctx, workspaceUUID, identityUUID)
	}
	outcome := CleanupOutcome{RetainedSharedVaults: append([]string{}, shared...), PersonalVaultsDeleted: false}
	archived, err := s.store.ArchiveIdentityConfigs(ctx, workspaceUUID, identityUUID)
	if err != nil {
		outcome.Failures = append(outcome.Failures, "archive identity config failed")
	} else {
		outcome.ArchivedConfigs = int(archived)
	}
	unbound, err := s.store.UnbindIdentityPairings(ctx, workspaceUUID, identityUUID)
	if err != nil {
		outcome.Failures = append(outcome.Failures, "unbind pairings failed")
	} else {
		outcome.UnboundPairings = int(unbound)
	}
	closed, err := s.store.CloseFixedIdentityEntries(ctx, workspaceUUID, identityUUID)
	if err != nil {
		outcome.Failures = append(outcome.Failures, "close fixed entries failed")
	} else {
		outcome.ClosedEntries = int(closed)
	}
	return FinishCleanup(outcome)
}

func (s *Service) sharedVaults(ctx context.Context, workspaceUUID, identityUUID string) []string {
	rows, err := s.store.ListIdentityConfigs(ctx, workspaceUUID, identityUUID, "", "", pageMax)
	if err != nil {
		return []string{}
	}
	seen := map[string]struct{}{}
	var ids []string
	for _, row := range rows {
		for _, id := range decodeList(row.VaultMounts) {
			if _, ok := seen[id]; ok || id == "" {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if ids == nil {
		return []string{}
	}
	return ids
}

func (s *Service) GetCleanup(ctx context.Context, principal auth.Principal, cleanupID string) (ClearResult, error) {
	row, err := s.store.FindIdentityCleanupRun(ctx, principal.WorkspaceUUID, cleanupID)
	if err != nil {
		return ClearResult{}, mapStore(err, "清理任务不存在")
	}
	var outcome CleanupOutcome
	_ = json.Unmarshal([]byte(row.Result), &outcome)
	return ClearResult{CleanupID: row.ExternalID, State: row.State, Outcome: outcome}, nil
}

func (s *Service) Stats(ctx context.Context, principal auth.Principal, identityID, window string) (db.IdentityStatsRecord, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return db.IdentityStatsRecord{}, err
	}
	stats, err := s.store.BusinessIdentityStats(ctx, principal.WorkspaceUUID, identity.UUID, windowStart(window))
	return stats, mapStore(err, "执行身份不存在")
}

func (s *Service) IdentityAgents(ctx context.Context, principal auth.Principal, identityID, window string) ([]db.IdentityAgentUsageRecord, error) {
	identity, err := s.GetIdentity(ctx, principal, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListBusinessIdentityAgents(ctx, principal.WorkspaceUUID, identity.UUID, windowStart(window), pageMax)
	return rows, mapStore(err, "执行身份不存在")
}

func windowStart(window string) *time.Time {
	now := time.Now().UTC()
	switch window {
	case "all":
		return nil
	case "7d":
		value := now.AddDate(0, 0, -7)
		return &value
	default:
		value := now.AddDate(0, 0, -30)
		return &value
	}
}
