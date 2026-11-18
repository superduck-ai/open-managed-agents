package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func (d *DB) CreateBusinessIdentity(ctx context.Context, params InsertIdentityParams) (IdentityRecord, error) {
	row, err := NewIdentityMapper(d.mapperDB).InsertIdentity(ctx, params)
	if isUniqueViolation(err) {
		return IdentityRecord{}, ErrDuplicate
	}
	return row, err
}

func (d *DB) FindBusinessIdentity(ctx context.Context, workspaceUUID, externalID string) (IdentityRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindIdentity(ctx, workspaceUUID, externalID)
	if err != nil {
		return IdentityRecord{}, err
	}
	if !found {
		return IdentityRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) FindBusinessIdentityByUUID(ctx context.Context, workspaceUUID, identityUUID string) (IdentityRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindIdentityByUUID(ctx, workspaceUUID, identityUUID)
	if err != nil {
		return IdentityRecord{}, err
	}
	if !found {
		return IdentityRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) ListBusinessIdentities(ctx context.Context, params ListIdentityParams) ([]IdentityRecord, error) {
	return NewIdentityMapper(d.mapperDB).ListIdentities(ctx, params)
}

func (d *DB) UpdateBusinessIdentity(ctx context.Context, params UpdateIdentityParams) error {
	count, err := NewIdentityMapper(d.mapperDB).UpdateIdentity(ctx, params)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) SetBusinessIdentityEnabled(ctx context.Context, workspaceUUID, externalID string, enabled bool) error {
	count, err := NewIdentityMapper(d.mapperDB).SetIdentityEnabled(ctx, workspaceUUID, externalID, enabled)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) DeleteBusinessIdentity(ctx context.Context, workspaceUUID, externalID string) error {
	count, err := NewIdentityMapper(d.mapperDB).SoftDeleteIdentity(ctx, workspaceUUID, externalID)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CreateIdentityConfig(ctx context.Context, params InsertIdentityConfigParams) (IdentityConfigRecord, error) {
	row, err := NewIdentityMapper(d.mapperDB).InsertIdentityConfig(ctx, params)
	if isUniqueViolation(err) {
		return IdentityConfigRecord{}, ErrDuplicate
	}
	return row, err
}

func (d *DB) FindActiveIdentityConfig(ctx context.Context, workspaceUUID, identityUUID, agentExternalID string) (IdentityConfigRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindActiveIdentityConfig(ctx, workspaceUUID, identityUUID, agentExternalID)
	if err != nil {
		return IdentityConfigRecord{}, err
	}
	if !found {
		return IdentityConfigRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) ListIdentityConfigs(ctx context.Context, workspaceUUID, identityUUID, agentExternalID, cursor string, limit int) ([]IdentityConfigRecord, error) {
	return NewIdentityMapper(d.mapperDB).ListIdentityConfigs(ctx, workspaceUUID, identityUUID, agentExternalID, cursor, limit)
}

func (d *DB) UpdateIdentityConfig(ctx context.Context, params UpdateIdentityConfigParams) error {
	count, err := NewIdentityMapper(d.mapperDB).UpdateIdentityConfig(ctx, params)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (d *DB) ArchiveIdentityConfigs(ctx context.Context, workspaceUUID, identityUUID string) (int64, error) {
	return NewIdentityMapper(d.mapperDB).ArchiveIdentityConfigs(ctx, workspaceUUID, identityUUID)
}

func (d *DB) FindExecutionContext(ctx context.Context, workspaceUUID, channelExternalID string) (ExecutionContextRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindExecutionContext(ctx, workspaceUUID, channelExternalID)
	if err != nil {
		return ExecutionContextRecord{}, err
	}
	if !found {
		return ExecutionContextRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) SetChannelConnectionStatus(ctx context.Context, workspaceUUID, channelExternalID, status string) error {
	_, err := NewIdentityMapper(d.mapperDB).SetConnectionStatus(ctx, workspaceUUID, channelExternalID, status)
	return err
}

func (d *DB) CloseFixedIdentityEntries(ctx context.Context, workspaceUUID, identityUUID string) (int64, error) {
	return NewIdentityMapper(d.mapperDB).CloseFixedEntries(ctx, workspaceUUID, identityUUID)
}

func (d *DB) CreateScopePairing(ctx context.Context, params InsertScopePairingParams) (ScopePairingRecord, error) {
	row, err := NewIdentityMapper(d.mapperDB).InsertScopePairing(ctx, params)
	if isUniqueViolation(err) {
		return ScopePairingRecord{}, ErrDuplicate
	}
	return row, err
}

func (d *DB) FindScopePairing(ctx context.Context, workspaceUUID, externalID string) (ScopePairingRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindScopePairing(ctx, workspaceUUID, externalID)
	if err != nil {
		return ScopePairingRecord{}, err
	}
	if !found {
		return ScopePairingRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) FindOpenScopePairing(ctx context.Context, channelUUID, scopeKind, platformAccount, remoteID string) (ScopePairingRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindOpenScopePairing(ctx, channelUUID, scopeKind, platformAccount, remoteID)
	if err != nil {
		return ScopePairingRecord{}, err
	}
	if !found {
		return ScopePairingRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) FindPendingScopePairingByCode(ctx context.Context, workspaceUUID, codeHash string, now time.Time) (ScopePairingRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindPendingScopePairingByCode(ctx, FindPendingScopePairingParams{
		WorkspaceUUID: workspaceUUID, CodeHash: codeHash, Now: now,
	})
	if err != nil {
		return ScopePairingRecord{}, err
	}
	if !found {
		return ScopePairingRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) ListScopePairings(ctx context.Context, params ListScopePairingParams) ([]ScopePairingRecord, error) {
	return NewIdentityMapper(d.mapperDB).ListScopePairings(ctx, params)
}

func (d *DB) ActivateScopePairingByCode(ctx context.Context, params ActivateScopePairingParams) error {
	count, err := NewIdentityMapper(d.mapperDB).ActivateScopePairingByCode(ctx, params)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) ActivateScopePairingByID(ctx context.Context, params ActivateScopePairingParams) error {
	count, err := NewIdentityMapper(d.mapperDB).ActivateScopePairingByID(ctx, params)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) RebindScopePairing(ctx context.Context, params RebindScopePairingParams) error {
	count, err := NewIdentityMapper(d.mapperDB).RebindScopePairing(ctx, params)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (d *DB) UnbindScopePairing(ctx context.Context, workspaceUUID, externalID string, expectedRevision int64) error {
	count, err := NewIdentityMapper(d.mapperDB).UnbindScopePairing(ctx, workspaceUUID, externalID, expectedRevision)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (d *DB) UnbindIdentityPairings(ctx context.Context, workspaceUUID, identityUUID string) (int64, error) {
	return NewIdentityMapper(d.mapperDB).UnbindIdentityPairings(ctx, workspaceUUID, identityUUID)
}

func (d *DB) FindPersonalIdentityByActor(ctx context.Context, organizationUUID, workspaceUUID, channelType, appID, actorID string) (IdentityRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindActorIdentity(ctx, organizationUUID, workspaceUUID, channelType, appID, actorID)
	if err == nil && !found {
		err = ErrNotFound
	}
	return row, err
}

func (d *DB) IsPersonalIdentity(ctx context.Context, workspaceUUID, identityUUID string) (bool, error) {
	return NewIdentityMapper(d.mapperDB).IsPersonalIdentity(ctx, workspaceUUID, identityUUID)
}

func (d *DB) ClaimPersonalIdentityByActor(ctx context.Context, organizationUUID, workspaceUUID, channelType, appID, actorID, identityUUID string) error {
	return NewIdentityMapper(d.mapperDB).UpsertActorLink(ctx, organizationUUID, workspaceUUID, channelType, appID, actorID, identityUUID)
}

func (d *DB) BindSessionIdentity(ctx context.Context, params InsertSessionIdentityParams) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return bindSessionIdentityTx(ctx, executor, params)
	})
}

func bindSessionIdentityTx(ctx context.Context, executor yourbatis.Executor, params InsertSessionIdentityParams) error {
	mapper := NewIdentityMapper(executor)
	count, err := mapper.SetSessionIdentity(ctx, params.WorkspaceUUID, params.SessionExternalID, params.IdentityUUID)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	err = mapper.InsertSessionIdentity(ctx, params)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

func (d *DB) FindSessionIdentity(ctx context.Context, workspaceUUID, sessionExternalID string) (SessionIdentityRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindSessionIdentity(ctx, workspaceUUID, sessionExternalID)
	if err != nil {
		return SessionIdentityRecord{}, err
	}
	if !found {
		return SessionIdentityRecord{}, ErrNotFound
	}
	return row, nil
}

func (d *DB) ListIdentitySessions(ctx context.Context, workspaceUUID, identityUUID string, limit int) ([]SessionIdentityRecord, error) {
	return NewIdentityMapper(d.mapperDB).ListIdentitySessions(ctx, workspaceUUID, identityUUID, limit)
}

func (d *DB) BusinessIdentityStats(ctx context.Context, workspaceUUID, identityUUID string, since *time.Time) (IdentityStatsRecord, error) {
	return NewIdentityMapper(d.mapperDB).IdentityStats(ctx, workspaceUUID, identityUUID, since)
}

func (d *DB) ListBusinessIdentityAgents(ctx context.Context, workspaceUUID, identityUUID string, since *time.Time, limit int) ([]IdentityAgentUsageRecord, error) {
	return NewIdentityMapper(d.mapperDB).ListIdentityAgents(ctx, workspaceUUID, identityUUID, since, limit)
}

func (d *DB) ListRunningIdentitySessions(ctx context.Context, workspaceUUID, identityUUID string) ([]string, error) {
	return NewIdentityMapper(d.mapperDB).ListRunningIdentitySessions(ctx, workspaceUUID, identityUUID)
}

func (d *DB) ListAgentIdentityBindings(ctx context.Context, workspaceUUID, agentExternalID string) ([]AgentBindingRecord, error) {
	return NewIdentityMapper(d.mapperDB).ListAgentBindings(ctx, workspaceUUID, agentExternalID)
}

func (d *DB) IsPersonalChannelVault(ctx context.Context, workspaceUUID, vaultExternalID string) (bool, error) {
	return NewIdentityMapper(d.mapperDB).PersonalVault(ctx, workspaceUUID, vaultExternalID)
}

func (d *DB) CreateIdentityCleanupRun(ctx context.Context, params InsertCleanupRunParams) (CleanupRunRecord, error) {
	return NewIdentityMapper(d.mapperDB).InsertCleanupRun(ctx, params)
}

func (d *DB) SaveIdentityCleanupRun(ctx context.Context, workspaceUUID, externalID, state, result string) error {
	count, err := NewIdentityMapper(d.mapperDB).SaveCleanupRun(ctx, workspaceUUID, externalID, state, result)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) FindIdentityCleanupRun(ctx context.Context, workspaceUUID, externalID string) (CleanupRunRecord, error) {
	row, found, err := NewIdentityMapper(d.mapperDB).FindCleanupRun(ctx, workspaceUUID, externalID)
	if err != nil {
		return CleanupRunRecord{}, err
	}
	if !found {
		return CleanupRunRecord{}, ErrNotFound
	}
	return row, nil
}
