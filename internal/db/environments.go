package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
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
	created, err := getEnvironmentSQLX(ctx, d.sql, `
		insert into environments (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			name, description, config, metadata, scope, provider, resolved_template,
			created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :created_by_api_key_uuid,
			:name, :description, CAST(:config AS jsonb), CAST(:metadata AS jsonb),
			:scope, :provider, :resolved_template, :created_at, :created_at
		)
		returning `+environmentSQLXColumns+`
	`, environmentArguments(env))
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetEnvironment(ctx context.Context, workspaceUUID string, externalID string) (Environment, error) {
	return getEnvironmentSQLX(ctx, d.sql, environmentSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
	`, environmentLookupArguments(workspaceUUID, externalID))
}

func (d *DB) UpdateEnvironment(ctx context.Context, workspaceUUID string, externalID string, next Environment) (Environment, error) {
	arguments := environmentArguments(next)
	arguments["workspace_uuid"] = dbUUID(workspaceUUID)
	arguments["external_id"] = externalID
	updated, err := getEnvironmentSQLX(ctx, d.sql, `
		update environments
		set name = :name,
			description = :description,
			config = CAST(:config AS jsonb),
			metadata = CAST(:metadata AS jsonb),
			scope = :scope,
			resolved_template = :resolved_template,
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		returning `+environmentSQLXColumns+`
	`, arguments)
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	return updated, err
}

func (d *DB) ArchiveEnvironment(ctx context.Context, workspaceUUID string, externalID string) (Environment, error) {
	return getEnvironmentSQLX(ctx, d.sql, `
		update environments
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		returning `+environmentSQLXColumns+`
	`, environmentLookupArguments(workspaceUUID, externalID))
}

func (d *DB) DeleteEnvironment(ctx context.Context, workspaceUUID string, externalID string) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var environmentUUID uuid.UUID
	err = namedGetContext(ctx, tx, &environmentUUID, `
		select uuid
		from environments
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, environmentLookupArguments(workspaceUUID, externalID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	var activeWork int
	if err := namedGetContext(ctx, tx, &activeWork, `
		select count(*)
		from environment_work
		where workspace_uuid = :workspace_uuid
			and environment_uuid = :environment_uuid
			and deleted_at is null
			and state <> 'stopped'
	`, map[string]any{
		"workspace_uuid":   dbUUID(workspaceUUID),
		"environment_uuid": environmentUUID,
	}); err != nil {
		return err
	}
	if activeWork > 0 {
		return ErrInvalidState
	}
	if _, err := namedExecContext(ctx, tx, `
		update environments
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and uuid = :environment_uuid
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":   dbUUID(workspaceUUID),
		"environment_uuid": environmentUUID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) ListEnvironmentsPage(ctx context.Context, params ListEnvironmentsPageParams) ([]Environment, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query, arguments := listEnvironmentsQuery(params)
	environments, err := selectEnvironmentsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(environments) > params.Limit
	if hasMore {
		environments = environments[:params.Limit]
	}
	return environments, hasMore, nil
}

func listEnvironmentsQuery(params ListEnvironmentsPageParams) (string, map[string]any) {
	query := environmentSelectSQL() + `
		where workspace_uuid = :workspace_uuid and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": dbUUID(params.WorkspaceUUID),
		"limit":          params.Limit + 1,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < :cursor_uuid))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = dbUUID(params.Cursor.UUID)
	}
	query += " order by created_at desc, uuid desc limit :limit"
	return query, arguments
}

func (d *DB) CreateEnvironmentKey(ctx context.Context, key EnvironmentKey, keyHash string) error {
	_, err := namedExecContext(ctx, d.sql, `
		insert into environment_keys (
			external_id, organization_uuid, workspace_uuid, environment_uuid,
			environment_external_id, key_hash, status
		)
		values (
			:external_id, :organization_uuid, :workspace_uuid, :environment_uuid,
			:environment_external_id, :key_hash, 'active'
		)
		on conflict (external_id) do update set
			organization_uuid = excluded.organization_uuid,
			workspace_uuid = excluded.workspace_uuid,
			environment_uuid = excluded.environment_uuid,
			environment_external_id = excluded.environment_external_id,
			key_hash = excluded.key_hash,
			status = 'active'
	`, map[string]any{
		"external_id":             key.ExternalID,
		"organization_uuid":       dbUUID(key.OrganizationUUID),
		"workspace_uuid":          dbUUID(key.WorkspaceUUID),
		"environment_uuid":        dbUUID(key.EnvironmentUUID),
		"environment_external_id": key.EnvironmentExternalID,
		"key_hash":                keyHash,
	})
	return err
}

func (d *DB) GetEnvironmentKey(ctx context.Context, keyHash string) (EnvironmentKey, error) {
	var row environmentKeyRow
	err := namedGetContext(ctx, d.sql, &row, `
		with updated as (
			update environment_keys
			set last_used_at = now()
			where key_hash = :key_hash and status = 'active'
			returning uuid, external_id, organization_uuid, workspace_uuid, environment_uuid, environment_external_id
		)
		select updated.uuid, updated.external_id,
			updated.organization_uuid,
			updated.workspace_uuid,
			workspaces.external_id AS workspace_external_id,
			updated.environment_uuid,
			updated.environment_external_id
		from updated
		join workspaces on workspaces.uuid = updated.workspace_uuid
	`, map[string]any{"key_hash": keyHash})
	if errors.Is(err, sql.ErrNoRows) {
		return EnvironmentKey{}, ErrNotFound
	}
	if err != nil {
		return EnvironmentKey{}, err
	}
	return row.key(), nil
}

func (d *DB) CreateEnvironmentWork(ctx context.Context, work EnvironmentWork) (EnvironmentWork, error) {
	work.State = coalesceWorkState(work.State)
	return insertEnvironmentWorkSQLX(ctx, d.sql, work)
}

func (d *DB) GetEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string) (EnvironmentWork, error) {
	return getEnvironmentWorkSQLX(ctx, d.sql, environmentWorkSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
	`, environmentWorkLookupArguments(workspaceUUID, environmentExternalID, workExternalID))
}

func (d *DB) GetLatestEnvironmentWorkByData(ctx context.Context, workspaceUUID string, environmentExternalID, dataType, dataID string) (EnvironmentWork, error) {
	return getEnvironmentWorkSQLX(ctx, d.sql, environmentWorkSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and data->>'type' = :data_type
			and data->>'id' = :data_id
			and deleted_at is null
		order by created_at desc, uuid desc
		limit 1
	`, map[string]any{
		"workspace_uuid":          dbUUID(workspaceUUID),
		"environment_external_id": environmentExternalID,
		"data_type":               dataType,
		"data_id":                 dataID,
	})
}

func (d *DB) ListEnvironmentWorkPage(ctx context.Context, params ListEnvironmentWorkPageParams) ([]EnvironmentWork, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query, arguments := listEnvironmentWorkQuery(params)
	work, err := selectEnvironmentWorkSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(work) > params.Limit
	if hasMore {
		work = work[:params.Limit]
	}
	return work, hasMore, nil
}

func listEnvironmentWorkQuery(params ListEnvironmentWorkPageParams) (string, map[string]any) {
	query := environmentWorkSelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid":          dbUUID(params.WorkspaceUUID),
		"environment_external_id": params.EnvironmentExternalID,
		"limit":                   params.Limit + 1,
	}
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < :cursor_uuid))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = dbUUID(params.Cursor.UUID)
	}
	query += " order by created_at desc, uuid desc limit :limit"
	return query, arguments
}

func (d *DB) PollEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workerID string, claimFor time.Duration) (*EnvironmentWork, error) {
	if claimFor <= 0 {
		claimFor = 5 * time.Second
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if workerID != "" {
		if _, err := namedExecContext(ctx, tx, `
			insert into environment_worker_polls (
				organization_uuid, workspace_uuid, environment_uuid, environment_external_id, worker_id, last_poll_at
			)
			select organization_uuid, workspace_uuid, uuid, external_id, :worker_id, now()
			from environments
			where workspace_uuid = :workspace_uuid
				and external_id = :environment_external_id
				and deleted_at is null
			on conflict (environment_uuid, worker_id) do update set last_poll_at = excluded.last_poll_at
		`, map[string]any{
			"workspace_uuid":          dbUUID(workspaceUUID),
			"environment_external_id": environmentExternalID,
			"worker_id":               workerID,
		}); err != nil {
			return nil, err
		}
	}

	work, err := getEnvironmentWorkSQLX(ctx, tx, `
		update environment_work
		set claimed_by_worker_id = :worker_id,
			claim_expires_at = :claim_expires_at,
			updated_at = now()
		where uuid = (
			select uuid
			from environment_work
			where workspace_uuid = :workspace_uuid
				and environment_external_id = :environment_external_id
				and deleted_at is null
				and state = 'queued'
				and (claim_expires_at is null or claim_expires_at <= now())
			order by created_at asc, uuid asc
			limit 1
			for update skip locked
		)
		returning `+environmentWorkSQLXColumns+`
	`, map[string]any{
		"workspace_uuid":          dbUUID(workspaceUUID),
		"environment_external_id": environmentExternalID,
		"worker_id":               nullableWorkerID(workerID),
		"claim_expires_at":        time.Now().UTC().Add(claimFor),
	})
	if errors.Is(err, ErrNotFound) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &work, nil
}

func (d *DB) PollNextEnvironmentWork(ctx context.Context, workerID string, claimFor time.Duration) (*EnvironmentWork, error) {
	return d.PollNextEnvironmentWorkForRunner(ctx, workerID, claimFor, true)
}

func (d *DB) PollNextEnvironmentWorkForRunner(ctx context.Context, workerID string, claimFor time.Duration, includeSessionWork bool) (*EnvironmentWork, error) {
	if claimFor <= 0 {
		claimFor = 5 * time.Second
	}
	filter := ""
	if !includeSessionWork {
		filter = "and coalesce(data->>'type', '') <> 'session'"
	}
	work, err := getEnvironmentWorkSQLX(ctx, d.sql, `
		update environment_work
		set claimed_by_worker_id = :worker_id,
			claim_expires_at = :claim_expires_at,
			updated_at = now()
		where uuid = (
			select uuid
			from environment_work
			where deleted_at is null
				and state = 'queued'
				and (claim_expires_at is null or claim_expires_at <= now())
				`+filter+`
			order by created_at asc, uuid asc
			limit 1
			for update skip locked
		)
		returning `+environmentWorkSQLXColumns+`
	`, map[string]any{
		"worker_id":        nullableWorkerID(workerID),
		"claim_expires_at": time.Now().UTC().Add(claimFor),
	})
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &work, nil
}

func (d *DB) GetEnvironmentByUUID(ctx context.Context, workspaceUUID, environmentUUID string) (Environment, error) {
	return getEnvironmentSQLX(ctx, d.sql, environmentSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and uuid = :environment_uuid
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":   dbUUID(workspaceUUID),
		"environment_uuid": dbUUID(environmentUUID),
	})
}

func (d *DB) AckEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string) (EnvironmentWork, error) {
	return getEnvironmentWorkSQLX(ctx, d.sql, `
		update environment_work
		set state = case when state = 'queued' then 'starting' else state end,
			acknowledged_at = coalesce(acknowledged_at, now()),
			started_at = coalesce(started_at, now()),
			claim_expires_at = null,
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
			and state in ('queued', 'starting', 'active')
		returning `+environmentWorkSQLXColumns+`
	`, environmentWorkLookupArguments(workspaceUUID, environmentExternalID, workExternalID))
}

func (d *DB) UpdateEnvironmentWorkMetadata(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string, metadata json.RawMessage) (EnvironmentWork, error) {
	arguments := environmentWorkLookupArguments(workspaceUUID, environmentExternalID, workExternalID)
	arguments["metadata"] = jsonArg(metadata)
	return getEnvironmentWorkSQLX(ctx, d.sql, `
		update environment_work
		set metadata = CAST(:metadata AS jsonb),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
		returning `+environmentWorkSQLXColumns+`
	`, arguments)
}

func (d *DB) HeartbeatEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID, expectedLastHeartbeat string, ttlSeconds int, format func(time.Time) string) (WorkHeartbeatResult, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	if ttlSeconds < 5 {
		ttlSeconds = 5
	}
	if ttlSeconds > 300 {
		ttlSeconds = 300
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return WorkHeartbeatResult{}, err
	}
	defer tx.Rollback()

	arguments := environmentWorkLookupArguments(workspaceUUID, environmentExternalID, workExternalID)
	current, err := getEnvironmentWorkSQLX(ctx, tx, environmentWorkSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
		for update
	`, arguments)
	if err != nil {
		return WorkHeartbeatResult{}, err
	}
	if expectedLastHeartbeat != "" {
		if expectedLastHeartbeat == "NO_HEARTBEAT" {
			if current.LatestHeartbeatAt != nil {
				return WorkHeartbeatResult{}, ErrPreconditionFailed
			}
		} else if current.LatestHeartbeatAt == nil || format(*current.LatestHeartbeatAt) != expectedLastHeartbeat {
			return WorkHeartbeatResult{}, ErrPreconditionFailed
		}
	}

	nextState := current.State
	leaseExtended := nextState != "stopping" && nextState != "stopped"
	if nextState == "queued" || nextState == "starting" {
		nextState = "active"
	}
	arguments["work_uuid"] = dbUUID(current.UUID)
	arguments["state"] = nextState
	arguments["ttl_seconds"] = ttlSeconds
	updated, err := getEnvironmentWorkSQLX(ctx, tx, `
		update environment_work
		set state = :state,
			latest_heartbeat_at = now(),
			heartbeat_ttl_seconds = :ttl_seconds,
			updated_at = now()
		where uuid = :work_uuid
			and workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
		returning `+environmentWorkSQLXColumns+`
	`, arguments)
	if err != nil {
		return WorkHeartbeatResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkHeartbeatResult{}, err
	}
	lastHeartbeat := ""
	if updated.LatestHeartbeatAt != nil {
		lastHeartbeat = format(*updated.LatestHeartbeatAt)
	}
	return WorkHeartbeatResult{Work: updated, TTLSeconds: ttlSeconds, LeaseExtended: leaseExtended, LastHeartbeat: lastHeartbeat}, nil
}

func (d *DB) StopEnvironmentWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string, force bool) (EnvironmentWork, error) {
	nextState := "stopped"
	if !force {
		nextState = "stopping"
	}
	arguments := environmentWorkLookupArguments(workspaceUUID, environmentExternalID, workExternalID)
	arguments["state"] = nextState
	return getEnvironmentWorkSQLX(ctx, d.sql, `
		update environment_work
		set state = :state,
			stop_requested_at = coalesce(stop_requested_at, now()),
			stopped_at = case when :state = 'stopped' then coalesce(stopped_at, now()) else stopped_at end,
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
		returning `+environmentWorkSQLXColumns+`
	`, arguments)
}

func (d *DB) EnvironmentWorkStats(ctx context.Context, workspaceUUID string, environmentExternalID string) (EnvironmentWorkStats, error) {
	var row environmentWorkStatsRow
	err := namedGetContext(ctx, d.sql, &row, `
		select
			CAST(count(*) filter (
				where state = 'queued'
					and (claim_expires_at is null or claim_expires_at <= now())
			) AS int) as depth,
			CAST(count(*) filter (where state <> 'stopped') AS int) as pending,
			min(created_at) filter (where state = 'queued') as oldest_queued_at,
			coalesce((
				select CAST(count(distinct worker_id) AS int)
				from environment_worker_polls p
				where p.workspace_uuid = :workspace_uuid
					and p.environment_external_id = :environment_external_id
					and p.last_poll_at > now() - interval '30 seconds'
			), 0) as workers_polling
		from environment_work
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":          dbUUID(workspaceUUID),
		"environment_external_id": environmentExternalID,
	})
	if err != nil {
		return EnvironmentWorkStats{}, err
	}
	stats := EnvironmentWorkStats{
		Depth:          row.Depth,
		Pending:        row.Pending,
		OldestQueuedAt: row.OldestQueuedAt,
	}
	if row.WorkersPolling > 0 {
		stats.WorkersPolling = &row.WorkersPolling
	}
	return stats, nil
}

func (d *DB) CreateEnvironmentSandbox(ctx context.Context, sandbox EnvironmentSandbox) (EnvironmentSandbox, error) {
	return getEnvironmentSandboxSQLX(ctx, d.sql, `
		insert into environment_sandboxes (
			uuid, external_id, organization_uuid, workspace_uuid, environment_uuid,
			environment_external_id, work_uuid, work_external_id, provider, template,
			provider_sandbox_id, state, metadata, last_error, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :environment_uuid,
			:environment_external_id, :work_uuid, :work_external_id, :provider, :template,
			:provider_sandbox_id, :state, CAST(:metadata AS jsonb), :last_error,
			:created_at, :created_at
		)
		returning `+environmentSandboxSQLXColumns+`
	`, environmentSandboxArguments(sandbox))
}

func (d *DB) UpdateEnvironmentSandboxState(ctx context.Context, workspaceUUID string, externalID, state string, providerSandboxID *string, lastError *string, stoppedAt *time.Time) error {
	_, err := namedExecContext(ctx, d.sql, `
		update environment_sandboxes
		set state = :state,
			provider_sandbox_id = coalesce(:provider_sandbox_id, provider_sandbox_id),
			last_error = :last_error,
			stopped_at = coalesce(:stopped_at, stopped_at),
			updated_at = now()
		where workspace_uuid = :workspace_uuid and external_id = :external_id
	`, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"external_id":         externalID,
		"state":               state,
		"provider_sandbox_id": providerSandboxID,
		"last_error":          lastError,
		"stopped_at":          stoppedAt,
	})
	return err
}

func (d *DB) GetActiveEnvironmentSandboxForWork(ctx context.Context, workspaceUUID string, environmentExternalID, workExternalID string) (EnvironmentSandbox, error) {
	return getEnvironmentSandboxSQLX(ctx, d.sql, `
		select `+environmentSandboxSQLXColumns+`
		from environment_sandboxes
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and work_external_id = :work_external_id
			and provider_sandbox_id is not null
			and state in ('creating', 'running', 'stopping')
		order by created_at desc, uuid desc
		limit 1
	`, environmentWorkLookupArguments(workspaceUUID, environmentExternalID, workExternalID))
}

// GetRenewableEnvironmentSandboxForCodeSession resolves the provider sandbox
// owned by a running managed-agent Code Session. Idle and requires-action
// workers intentionally return ErrNotFound so their heartbeats cannot keep the
// sandbox alive indefinitely.
func (d *DB) GetRenewableEnvironmentSandboxForCodeSession(ctx context.Context, codeSessionExternalID string) (EnvironmentSandbox, error) {
	return getEnvironmentSandboxSQLX(ctx, d.sql, `
		select `+environmentSandboxSQLXColumns+`
		from environment_sandboxes
		where uuid = (
			select sandbox.uuid
			from code_sessions code_session
			join environment_work work
				on work.organization_uuid = code_session.organization_uuid
				and work.workspace_uuid = code_session.workspace_uuid
				and work.environment_uuid = code_session.environment_uuid
				and work.environment_external_id = code_session.environment_external_id
				and work.data->>'type' = 'session'
				and work.data->>'id' = code_session.session_external_id
				and work.deleted_at is null
			join environment_sandboxes sandbox
				on sandbox.organization_uuid = code_session.organization_uuid
				and sandbox.workspace_uuid = code_session.workspace_uuid
				and sandbox.environment_uuid = code_session.environment_uuid
				and sandbox.work_uuid = work.uuid
				and sandbox.provider_sandbox_id is not null
				and sandbox.state = 'running'
			where code_session.external_id = :code_session_external_id
				and code_session.status = 'active'
				and code_session.worker_status = 'running'
				and code_session.deleted_at is null
			order by sandbox.created_at desc, sandbox.uuid desc
			limit 1
		)
	`, map[string]any{"code_session_external_id": codeSessionExternalID})
}

const (
	environmentSQLXColumns = `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		created_by_api_key_uuid,
		name, description, config, metadata, scope,
		provider, resolved_template, created_at, updated_at, archived_at, deleted_at`
	environmentSandboxSQLXColumns = `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		environment_uuid,
		environment_external_id, work_uuid,
		work_external_id, provider, template, provider_sandbox_id, state, metadata,
		last_error, created_at, updated_at, stopped_at`
)

type environmentRow struct {
	UUID                uuid.UUID  `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    uuid.UUID  `db:"organization_uuid"`
	WorkspaceUUID       uuid.UUID  `db:"workspace_uuid"`
	CreatedByAPIKeyUUID uuid.UUID  `db:"created_by_api_key_uuid"`
	Name                string     `db:"name"`
	Description         string     `db:"description"`
	Config              []byte     `db:"config"`
	Metadata            []byte     `db:"metadata"`
	Scope               *string    `db:"scope"`
	Provider            string     `db:"provider"`
	ResolvedTemplate    string     `db:"resolved_template"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type environmentKeyRow struct {
	UUID                  uuid.UUID `db:"uuid"`
	ExternalID            string    `db:"external_id"`
	OrganizationUUID      uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID         uuid.UUID `db:"workspace_uuid"`
	WorkspaceExternalID   string    `db:"workspace_external_id"`
	EnvironmentUUID       uuid.UUID `db:"environment_uuid"`
	EnvironmentExternalID string    `db:"environment_external_id"`
}

type environmentWorkStatsRow struct {
	Depth          int        `db:"depth"`
	Pending        int        `db:"pending"`
	OldestQueuedAt *time.Time `db:"oldest_queued_at"`
	WorkersPolling int        `db:"workers_polling"`
}

type environmentSandboxRow struct {
	UUID                  uuid.UUID     `db:"uuid"`
	ExternalID            string        `db:"external_id"`
	OrganizationUUID      uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID         uuid.UUID     `db:"workspace_uuid"`
	EnvironmentUUID       uuid.UUID     `db:"environment_uuid"`
	EnvironmentExternalID string        `db:"environment_external_id"`
	WorkUUID              uuid.NullUUID `db:"work_uuid"`
	WorkExternalID        *string       `db:"work_external_id"`
	Provider              string        `db:"provider"`
	Template              string        `db:"template"`
	ProviderSandboxID     *string       `db:"provider_sandbox_id"`
	State                 string        `db:"state"`
	Metadata              []byte        `db:"metadata"`
	LastError             *string       `db:"last_error"`
	CreatedAt             time.Time     `db:"created_at"`
	UpdatedAt             time.Time     `db:"updated_at"`
	StoppedAt             *time.Time    `db:"stopped_at"`
}

func environmentSelectSQL() string {
	return `select ` + environmentSQLXColumns + ` from environments`
}

func environmentWorkSelectSQL() string {
	return `select ` + environmentWorkSQLXColumns + ` from environment_work`
}

func getEnvironmentSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (Environment, error) {
	var row environmentRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Environment{}, ErrNotFound
		}
		return Environment{}, err
	}
	return row.environment(), nil
}

func selectEnvironmentsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]Environment, error) {
	var rows []environmentRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	environments := make([]Environment, len(rows))
	for index := range rows {
		environments[index] = rows[index].environment()
	}
	return environments, nil
}

func getEnvironmentWorkSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (EnvironmentWork, error) {
	var row environmentWorkRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EnvironmentWork{}, ErrNotFound
		}
		return EnvironmentWork{}, err
	}
	return row.work(), nil
}

func selectEnvironmentWorkSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]EnvironmentWork, error) {
	var rows []environmentWorkRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	work := make([]EnvironmentWork, len(rows))
	for index := range rows {
		work[index] = rows[index].work()
	}
	return work, nil
}

func getEnvironmentSandboxSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (EnvironmentSandbox, error) {
	var row environmentSandboxRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EnvironmentSandbox{}, ErrNotFound
		}
		return EnvironmentSandbox{}, err
	}
	return row.sandbox(), nil
}

func environmentLookupArguments(workspaceUUID string, externalID string) map[string]any {
	return map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    externalID,
	}
}

func environmentWorkLookupArguments(
	workspaceUUID string,
	environmentExternalID string,
	workExternalID string,
) map[string]any {
	return map[string]any{
		"workspace_uuid":          dbUUID(workspaceUUID),
		"environment_external_id": environmentExternalID,
		"work_external_id":        workExternalID,
	}
}

func environmentArguments(env Environment) map[string]any {
	return map[string]any{
		"uuid":                    dbUUID(env.UUID),
		"external_id":             env.ExternalID,
		"organization_uuid":       dbUUID(env.OrganizationUUID),
		"workspace_uuid":          dbUUID(env.WorkspaceUUID),
		"created_by_api_key_uuid": dbUUID(env.CreatedByAPIKeyUUID),
		"name":                    env.Name,
		"description":             env.Description,
		"config":                  jsonArg(env.Config),
		"metadata":                jsonArg(env.Metadata),
		"scope":                   env.Scope,
		"provider":                env.Provider,
		"resolved_template":       env.ResolvedTemplate,
		"created_at":              env.CreatedAt,
		"updated_at":              env.UpdatedAt,
	}
}

func environmentSandboxArguments(sandbox EnvironmentSandbox) map[string]any {
	return map[string]any{
		"uuid":                    dbUUID(sandbox.UUID),
		"external_id":             sandbox.ExternalID,
		"organization_uuid":       dbUUID(sandbox.OrganizationUUID),
		"workspace_uuid":          dbUUID(sandbox.WorkspaceUUID),
		"environment_uuid":        dbUUID(sandbox.EnvironmentUUID),
		"environment_external_id": sandbox.EnvironmentExternalID,
		"work_uuid":               dbNullableUUID(sandbox.WorkUUID),
		"work_external_id":        sandbox.WorkExternalID,
		"provider":                sandbox.Provider,
		"template":                sandbox.Template,
		"provider_sandbox_id":     sandbox.ProviderSandboxID,
		"state":                   sandbox.State,
		"metadata":                jsonArg(sandbox.Metadata),
		"last_error":              sandbox.LastError,
		"created_at":              sandbox.CreatedAt,
	}
}

func (r environmentRow) environment() Environment {
	return Environment{
		UUID:                r.UUID.String(),
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID.String(),
		WorkspaceUUID:       r.WorkspaceUUID.String(),
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID.String(),
		Name:                r.Name,
		Description:         r.Description,
		Config:              copyRaw(r.Config),
		Metadata:            copyRaw(r.Metadata),
		Scope:               r.Scope,
		Provider:            r.Provider,
		ResolvedTemplate:    r.ResolvedTemplate,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		ArchivedAt:          r.ArchivedAt,
		DeletedAt:           r.DeletedAt,
	}
}

func (r environmentKeyRow) key() EnvironmentKey {
	return EnvironmentKey{
		UUID:                  r.UUID.String(),
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID.String(),
		WorkspaceUUID:         r.WorkspaceUUID.String(),
		WorkspaceExternalID:   r.WorkspaceExternalID,
		EnvironmentUUID:       r.EnvironmentUUID.String(),
		EnvironmentExternalID: r.EnvironmentExternalID,
	}
}

func (r environmentSandboxRow) sandbox() EnvironmentSandbox {
	return EnvironmentSandbox{
		UUID:                  r.UUID.String(),
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID.String(),
		WorkspaceUUID:         r.WorkspaceUUID.String(),
		EnvironmentUUID:       r.EnvironmentUUID.String(),
		EnvironmentExternalID: r.EnvironmentExternalID,
		WorkUUID:              nullableUUIDString(r.WorkUUID),
		WorkExternalID:        r.WorkExternalID,
		Provider:              r.Provider,
		Template:              r.Template,
		ProviderSandboxID:     r.ProviderSandboxID,
		State:                 r.State,
		Metadata:              copyRaw(r.Metadata),
		LastError:             r.LastError,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		StoppedAt:             r.StoppedAt,
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
