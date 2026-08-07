package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type Environment struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	Name                string
	Description         string
	Config              json.RawMessage
	Metadata            json.RawMessage
	Scope               *string
	Provider            string
	ResolvedTemplate    string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
	DeletedAt           *time.Time
}

type EnvironmentPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListEnvironmentsPageParams struct {
	WorkspaceUUID   string
	Limit           int
	Cursor          *EnvironmentPageCursor
	IncludeArchived bool
}

type EnvironmentKey struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	WorkspaceExternalID   string
	EnvironmentUUID       string
	EnvironmentExternalID string
}

type EnvironmentWork struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	EnvironmentUUID       string
	EnvironmentExternalID string
	Data                  json.RawMessage
	Metadata              json.RawMessage
	Secret                *string
	State                 string
	ClaimedByWorkerID     *string
	ClaimExpiresAt        *time.Time
	AcknowledgedAt        *time.Time
	StartedAt             *time.Time
	LatestHeartbeatAt     *time.Time
	HeartbeatTTLSeconds   *int
	StopRequestedAt       *time.Time
	StoppedAt             *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

type EnvironmentWorkPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListEnvironmentWorkPageParams struct {
	WorkspaceUUID         string
	EnvironmentExternalID string
	Limit                 int
	Cursor                *EnvironmentWorkPageCursor
}

type WorkHeartbeatResult struct {
	Work          EnvironmentWork
	TTLSeconds    int
	LeaseExtended bool
	LastHeartbeat string
}

type EnvironmentWorkStats struct {
	Depth          int
	Pending        int
	OldestQueuedAt *time.Time
	WorkersPolling *int
}

type EnvironmentSandbox struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	EnvironmentUUID       string
	EnvironmentExternalID string
	WorkUUID              *string
	WorkExternalID        *string
	Provider              string
	Template              string
	ProviderSandboxID     *string
	State                 string
	Metadata              json.RawMessage
	LastError             *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	StoppedAt             *time.Time
}

func (d *DB) CreateEnvironment(ctx context.Context, env Environment) (Environment, error) {
	mapper := NewEnvironmentMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, environmentWriteParamsFrom(env))
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	if err != nil {
		return Environment{}, err
	}
	return row.environment(), nil
}

func (d *DB) GetEnvironment(ctx context.Context, workspaceUUID string, externalID string) (Environment, error) {
	mapper := NewEnvironmentMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Environment{}, mapNoRows(err)
	}
	return row.environment(), nil
}

func (d *DB) UpdateEnvironment(ctx context.Context, workspaceUUID string, externalID string, next Environment) (Environment, error) {
	params := environmentWriteParamsFrom(next)
	params.WorkspaceUUID = workspaceUUID
	params.ExternalID = externalID
	mapper := NewEnvironmentMapper(d.mapperDB)
	row, err := mapper.UpdateByExternalID(ctx, params)
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	if err != nil {
		return Environment{}, mapNoRows(err)
	}
	return row.environment(), nil
}

func (d *DB) ArchiveEnvironment(ctx context.Context, workspaceUUID string, externalID string) (Environment, error) {
	mapper := NewEnvironmentMapper(d.mapperDB)
	row, err := mapper.ArchiveByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Environment{}, mapNoRows(err)
	}
	return row.environment(), nil
}

func (d *DB) DeleteEnvironment(ctx context.Context, workspaceUUID string, externalID string) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		environmentMapper := NewEnvironmentMapper(executor)
		workMapper := NewEnvironmentWorkMapper(executor)
		environmentUUID, err := environmentMapper.LockUUIDByExternalID(ctx, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		activeWork, err := workMapper.CountActive(ctx, workspaceUUID, environmentUUID)
		if err != nil {
			return err
		}
		if activeWork > 0 {
			return ErrInvalidState
		}
		rowsAffected, err := environmentMapper.SoftDeleteByUUID(ctx, workspaceUUID, environmentUUID)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (d *DB) ListEnvironmentsPage(ctx context.Context, params ListEnvironmentsPageParams) ([]Environment, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewEnvironmentMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, environmentPageParams(params))
	if err != nil {
		return nil, false, err
	}
	environments := environmentsFromRows(rows)
	hasMore := len(environments) > params.Limit
	if hasMore {
		environments = environments[:params.Limit]
	}
	return environments, hasMore, nil
}

func (d *DB) CreateEnvironmentKey(ctx context.Context, key EnvironmentKey, keyHash string) error {
	mapper := NewEnvironmentKeyMapper(d.mapperDB)
	return mapper.Upsert(ctx, environmentKeyUpsertParams{
		ExternalID: key.ExternalID, OrganizationUUID: key.OrganizationUUID,
		WorkspaceUUID: key.WorkspaceUUID, EnvironmentUUID: key.EnvironmentUUID,
		EnvironmentExternalID: key.EnvironmentExternalID, KeyHash: keyHash,
	})
}

func (d *DB) GetEnvironmentKey(ctx context.Context, keyHash string) (EnvironmentKey, error) {
	mapper := NewEnvironmentKeyMapper(d.mapperDB)
	row, err := mapper.FindAndTouchByHash(ctx, keyHash)
	if err != nil {
		return EnvironmentKey{}, mapNoRows(err)
	}
	return row.key(), nil
}

func (d *DB) CreateEnvironmentWork(ctx context.Context, work EnvironmentWork) (EnvironmentWork, error) {
	work.State = coalesceWorkState(work.State)
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, environmentWorkWriteParamsFrom(work))
	if err != nil {
		return EnvironmentWork{}, err
	}
	return row.work(), nil
}

func (d *DB) GetEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string) (EnvironmentWork, error) {
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, environmentExternalID, workExternalID)
	if err != nil {
		return EnvironmentWork{}, mapNoRows(err)
	}
	return row.work(), nil
}

func (d *DB) GetLatestEnvironmentWorkByData(ctx context.Context, workspaceUUID string, environmentExternalID, dataType, dataID string) (EnvironmentWork, error) {
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.FindLatestByData(ctx, workspaceUUID, environmentExternalID, dataType, dataID)
	if err != nil {
		return EnvironmentWork{}, mapNoRows(err)
	}
	return row.work(), nil
}

func (d *DB) ListEnvironmentWorkPage(ctx context.Context, params ListEnvironmentWorkPageParams) ([]EnvironmentWork, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, environmentWorkPageParams(params))
	if err != nil {
		return nil, false, err
	}
	work := environmentWorkFromRows(rows)
	hasMore := len(work) > params.Limit
	if hasMore {
		work = work[:params.Limit]
	}
	return work, hasMore, nil
}

func (d *DB) PollEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workerID string, claimFor time.Duration) (*EnvironmentWork, error) {
	if claimFor <= 0 {
		claimFor = 5 * time.Second
	}
	var claimed *EnvironmentWork
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if workerID != "" {
			pollMapper := NewEnvironmentWorkerPollMapper(executor)
			if err := pollMapper.Upsert(ctx, workspaceUUID, environmentExternalID, workerID); err != nil {
				return err
			}
		}
		workMapper := NewEnvironmentWorkMapper(executor)
		row, err := workMapper.ClaimForEnvironment(
			ctx,
			workspaceUUID,
			environmentExternalID,
			nullableWorkerID(workerID),
			time.Now().UTC().Add(claimFor),
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		work := row.work()
		claimed = &work
		return nil
	})
	return claimed, err
}

func (d *DB) PollNextEnvironmentWork(ctx context.Context, workerID string, claimFor time.Duration) (*EnvironmentWork, error) {
	return d.PollNextEnvironmentWorkForRunner(ctx, workerID, claimFor, true)
}

func (d *DB) PollNextEnvironmentWorkForRunner(ctx context.Context, workerID string, claimFor time.Duration, includeSessionWork bool) (*EnvironmentWork, error) {
	if claimFor <= 0 {
		claimFor = 5 * time.Second
	}
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.ClaimNext(ctx, nullableWorkerID(workerID), time.Now().UTC().Add(claimFor), includeSessionWork)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	work := row.work()
	return &work, nil
}

func (d *DB) GetEnvironmentByUUID(ctx context.Context, workspaceUUID, environmentUUID string) (Environment, error) {
	mapper := NewEnvironmentMapper(d.mapperDB)
	row, err := mapper.FindByUUID(ctx, workspaceUUID, environmentUUID)
	if err != nil {
		return Environment{}, mapNoRows(err)
	}
	return row.environment(), nil
}

func (d *DB) AckEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string) (EnvironmentWork, error) {
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.AckByExternalID(ctx, workspaceUUID, environmentExternalID, workExternalID)
	if err != nil {
		return EnvironmentWork{}, mapNoRows(err)
	}
	return row.work(), nil
}

func (d *DB) UpdateEnvironmentWorkMetadata(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string, metadata json.RawMessage) (EnvironmentWork, error) {
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.UpdateMetadata(ctx, environmentWorkMetadataParams{
		WorkspaceUUID: workspaceUUID, EnvironmentExternalID: environmentExternalID,
		WorkExternalID: workExternalID, Metadata: agentJSONArg(metadata),
	})
	if err != nil {
		return EnvironmentWork{}, mapNoRows(err)
	}
	return row.work(), nil
}

func (d *DB) HeartbeatEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID, expectedLastHeartbeat string, ttlSeconds int, format func(time.Time) string) (WorkHeartbeatResult, error) {
	ttlSeconds = normalizedHeartbeatTTL(ttlSeconds)
	var result WorkHeartbeatResult
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewEnvironmentWorkMapper(executor)
		currentRow, err := mapper.LockByExternalID(ctx, workspaceUUID, environmentExternalID, workExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		current := currentRow.work()
		if !matchesExpectedHeartbeat(current.LatestHeartbeatAt, expectedLastHeartbeat, format) {
			return ErrPreconditionFailed
		}

		nextState := current.State
		leaseExtended := nextState != "stopping" && nextState != "stopped"
		if nextState == "queued" || nextState == "starting" {
			nextState = "active"
		}
		updatedRow, err := mapper.Heartbeat(ctx, environmentWorkHeartbeatParams{
			WorkspaceUUID: workspaceUUID, EnvironmentExternalID: environmentExternalID,
			WorkUUID: current.UUID, State: nextState, TTLSeconds: ttlSeconds,
		})
		if err != nil {
			return mapNoRows(err)
		}
		updated := updatedRow.work()
		lastHeartbeat := ""
		if updated.LatestHeartbeatAt != nil {
			lastHeartbeat = format(*updated.LatestHeartbeatAt)
		}
		result = WorkHeartbeatResult{
			Work: updated, TTLSeconds: ttlSeconds, LeaseExtended: leaseExtended, LastHeartbeat: lastHeartbeat,
		}
		return nil
	})
	return result, err
}

func (d *DB) StopEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string, force bool) (EnvironmentWork, error) {
	nextState := "stopped"
	if !force {
		nextState = "stopping"
	}
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.Stop(ctx, environmentWorkStopParams{
		WorkspaceUUID: workspaceUUID, EnvironmentExternalID: environmentExternalID,
		WorkExternalID: workExternalID, State: nextState,
	})
	if err != nil {
		return EnvironmentWork{}, mapNoRows(err)
	}
	return row.work(), nil
}

func (d *DB) EnvironmentWorkStats(ctx context.Context, workspaceUUID string, environmentExternalID string) (EnvironmentWorkStats, error) {
	mapper := NewEnvironmentWorkMapper(d.mapperDB)
	row, err := mapper.Stats(ctx, workspaceUUID, environmentExternalID)
	if err != nil {
		return EnvironmentWorkStats{}, err
	}
	stats := EnvironmentWorkStats{Depth: row.Depth, Pending: row.Pending, OldestQueuedAt: row.OldestQueuedAt}
	if row.WorkersPolling > 0 {
		stats.WorkersPolling = &row.WorkersPolling
	}
	return stats, nil
}

func (d *DB) CreateEnvironmentSandbox(ctx context.Context, sandbox EnvironmentSandbox) (EnvironmentSandbox, error) {
	mapper := NewEnvironmentSandboxMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, environmentSandboxWriteParamsFrom(sandbox))
	if err != nil {
		return EnvironmentSandbox{}, err
	}
	return row.sandbox(), nil
}

func (d *DB) UpdateEnvironmentSandboxState(ctx context.Context, workspaceUUID string, externalID, state string, providerSandboxID *string, lastError *string, stoppedAt *time.Time) error {
	mapper := NewEnvironmentSandboxMapper(d.mapperDB)
	return mapper.UpdateState(ctx, environmentSandboxStateParams{
		WorkspaceUUID: workspaceUUID, ExternalID: externalID, State: state,
		ProviderSandboxID: providerSandboxID, LastError: lastError, StoppedAt: stoppedAt,
	})
}

func (d *DB) GetActiveEnvironmentSandboxForWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string) (EnvironmentSandbox, error) {
	mapper := NewEnvironmentSandboxMapper(d.mapperDB)
	row, err := mapper.FindActiveForWork(ctx, workspaceUUID, environmentExternalID, workExternalID)
	if err != nil {
		return EnvironmentSandbox{}, mapNoRows(err)
	}
	return row.sandbox(), nil
}

// GetRenewableEnvironmentSandboxForCodeSession resolves the provider sandbox
// owned by a running managed-agent Code Session. Idle and requires-action
// workers intentionally return ErrNotFound so their heartbeats cannot keep the
// sandbox alive indefinitely.
func (d *DB) GetRenewableEnvironmentSandboxForCodeSession(ctx context.Context, codeSessionExternalID string) (EnvironmentSandbox, error) {
	mapper := NewEnvironmentSandboxMapper(d.mapperDB)
	row, err := mapper.FindRenewableByCodeSessionExternalID(ctx, codeSessionExternalID)
	if err != nil {
		return EnvironmentSandbox{}, mapNoRows(err)
	}
	return row.sandbox(), nil
}

func normalizedHeartbeatTTL(ttlSeconds int) int {
	if ttlSeconds <= 0 {
		return 60
	}
	if ttlSeconds < 5 {
		return 5
	}
	if ttlSeconds > 300 {
		return 300
	}
	return ttlSeconds
}

func matchesExpectedHeartbeat(latest *time.Time, expected string, format func(time.Time) string) bool {
	if expected == "" {
		return true
	}
	if expected == "NO_HEARTBEAT" {
		return latest == nil
	}
	return latest != nil && format(*latest) == expected
}

func environmentWriteParamsFrom(env Environment) environmentWriteParams {
	return environmentWriteParams{
		UUID: env.UUID, ExternalID: env.ExternalID, OrganizationUUID: env.OrganizationUUID,
		WorkspaceUUID: env.WorkspaceUUID, CreatedByAPIKeyUUID: env.CreatedByAPIKeyUUID,
		Name: env.Name, Description: env.Description,
		Config: agentJSONArg(env.Config), Metadata: agentJSONArg(env.Metadata),
		Scope: env.Scope, Provider: env.Provider, ResolvedTemplate: env.ResolvedTemplate,
		CreatedAt: env.CreatedAt, UpdatedAt: env.UpdatedAt,
	}
}

func environmentPageParams(params ListEnvironmentsPageParams) environmentPageMapperParams {
	return environmentPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, FetchLimit: params.Limit + 1,
		Cursor: params.Cursor, IncludeArchived: params.IncludeArchived,
	}
}

func environmentWorkWriteParamsFrom(work EnvironmentWork) environmentWorkWriteParams {
	return environmentWorkWriteParams{
		UUID: work.UUID, ExternalID: work.ExternalID, OrganizationUUID: work.OrganizationUUID,
		WorkspaceUUID: work.WorkspaceUUID, EnvironmentUUID: work.EnvironmentUUID,
		EnvironmentExternalID: work.EnvironmentExternalID,
		Data:                  agentJSONArg(work.Data), Metadata: agentJSONArg(work.Metadata),
		Secret: work.Secret, State: work.State, CreatedAt: work.CreatedAt,
	}
}

func environmentWorkPageParams(params ListEnvironmentWorkPageParams) environmentWorkPageMapperParams {
	return environmentWorkPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, EnvironmentExternalID: params.EnvironmentExternalID,
		FetchLimit: params.Limit + 1, Cursor: params.Cursor,
	}
}

func environmentSandboxWriteParamsFrom(sandbox EnvironmentSandbox) environmentSandboxWriteParams {
	return environmentSandboxWriteParams{
		UUID: sandbox.UUID, ExternalID: sandbox.ExternalID, OrganizationUUID: sandbox.OrganizationUUID,
		WorkspaceUUID: sandbox.WorkspaceUUID, EnvironmentUUID: sandbox.EnvironmentUUID,
		EnvironmentExternalID: sandbox.EnvironmentExternalID, WorkUUID: sandbox.WorkUUID,
		WorkExternalID: sandbox.WorkExternalID, Provider: sandbox.Provider, Template: sandbox.Template,
		ProviderSandboxID: sandbox.ProviderSandboxID, State: sandbox.State,
		Metadata:  agentJSONArg(sandbox.Metadata),
		LastError: sandbox.LastError, CreatedAt: sandbox.CreatedAt,
	}
}

func environmentsFromRows(rows []environmentMapperRow) []Environment {
	environments := make([]Environment, len(rows))
	for index := range rows {
		environments[index] = rows[index].environment()
	}
	return environments
}

func environmentWorkFromRows(rows []environmentWorkMapperRow) []EnvironmentWork {
	work := make([]EnvironmentWork, len(rows))
	for index := range rows {
		work[index] = rows[index].work()
	}
	return work
}

func (r environmentMapperRow) environment() Environment {
	return Environment{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		Name: r.Name, Description: r.Description, Config: copyRaw(r.Config), Metadata: copyRaw(r.Metadata),
		Scope: r.Scope, Provider: r.Provider, ResolvedTemplate: r.ResolvedTemplate,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt, DeletedAt: r.DeletedAt,
	}
}

func (r environmentKeyMapperRow) key() EnvironmentKey {
	return EnvironmentKey{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, WorkspaceExternalID: r.WorkspaceExternalID,
		EnvironmentUUID: r.EnvironmentUUID, EnvironmentExternalID: r.EnvironmentExternalID,
	}
}

func (r environmentWorkMapperRow) work() EnvironmentWork {
	return EnvironmentWork{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, EnvironmentUUID: r.EnvironmentUUID,
		EnvironmentExternalID: r.EnvironmentExternalID, Data: copyRaw(r.Data), Metadata: copyRaw(r.Metadata),
		Secret: r.Secret, State: r.State, ClaimedByWorkerID: r.ClaimedByWorkerID,
		ClaimExpiresAt: r.ClaimExpiresAt, AcknowledgedAt: r.AcknowledgedAt, StartedAt: r.StartedAt,
		LatestHeartbeatAt: r.LatestHeartbeatAt, HeartbeatTTLSeconds: r.HeartbeatTTLSeconds,
		StopRequestedAt: r.StopRequestedAt, StoppedAt: r.StoppedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt,
	}
}

func (r environmentSandboxMapperRow) sandbox() EnvironmentSandbox {
	return EnvironmentSandbox{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, EnvironmentUUID: r.EnvironmentUUID,
		EnvironmentExternalID: r.EnvironmentExternalID, WorkUUID: r.WorkUUID,
		WorkExternalID: r.WorkExternalID, Provider: r.Provider, Template: r.Template,
		ProviderSandboxID: r.ProviderSandboxID, State: r.State, Metadata: copyRaw(r.Metadata),
		LastError: r.LastError, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, StoppedAt: r.StoppedAt,
	}
}

func coalesceWorkState(state string) string {
	if state == "" {
		return "queued"
	}
	return state
}

func nullableWorkerID(workerID string) *string {
	if workerID == "" {
		return nil
	}
	return &workerID
}
