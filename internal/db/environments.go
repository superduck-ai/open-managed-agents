package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Environment struct {
	ID                int64
	UUID              string
	ExternalID        string
	OrganizationID    int64
	WorkspaceID       int64
	CreatedByAPIKeyID int64
	Name              string
	Description       string
	Config            json.RawMessage
	Metadata          json.RawMessage
	Scope             *string
	Provider          string
	ResolvedTemplate  string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	DeletedAt         *time.Time
}

type EnvironmentPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type ListEnvironmentsPageParams struct {
	WorkspaceID     int64
	Limit           int
	Cursor          *EnvironmentPageCursor
	IncludeArchived bool
}

type EnvironmentKey struct {
	ID                     int64
	ExternalID             string
	OrganizationID         int64
	OrganizationExternalID string
	WorkspaceID            int64
	WorkspaceUUID          string
	WorkspaceExternalID    string
	EnvironmentID          int64
	EnvironmentExternalID  string
}

type EnvironmentWork struct {
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	EnvironmentID         int64
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
	ID        int64
}

type ListEnvironmentWorkPageParams struct {
	WorkspaceID           int64
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
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	EnvironmentID         int64
	EnvironmentExternalID string
	WorkID                *int64
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
	created, err := scanEnvironment(d.Pool.QueryRow(ctx, `
		insert into environments (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			name, description, config, metadata, scope, provider, resolved_template,
			created_at, updated_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7, $8::jsonb, $9::jsonb, $10, $11, $12,
			$13, $13
		)
		returning id, uuid::text, external_id, organization_id, workspace_id, created_by_api_key_id,
			name, description, config, metadata, scope, provider, resolved_template,
			created_at, updated_at, archived_at, deleted_at
	`, env.UUID, env.ExternalID, env.OrganizationID, env.WorkspaceID, env.CreatedByAPIKeyID,
		env.Name, env.Description, jsonArg(env.Config), jsonArg(env.Metadata), env.Scope, env.Provider,
		env.ResolvedTemplate, env.CreatedAt))
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetEnvironment(ctx context.Context, workspaceID int64, externalID string) (Environment, error) {
	return scanEnvironment(d.Pool.QueryRow(ctx, environmentSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) UpdateEnvironment(ctx context.Context, workspaceID int64, externalID string, next Environment) (Environment, error) {
	updated, err := scanEnvironment(d.Pool.QueryRow(ctx, `
		update environments
		set name = $3,
			description = $4,
			config = $5::jsonb,
			metadata = $6::jsonb,
			scope = $7,
			resolved_template = $8,
			updated_at = $9
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id, created_by_api_key_id,
			name, description, config, metadata, scope, provider, resolved_template,
			created_at, updated_at, archived_at, deleted_at
	`, workspaceID, externalID, next.Name, next.Description, jsonArg(next.Config), jsonArg(next.Metadata),
		next.Scope, next.ResolvedTemplate, next.UpdatedAt))
	if isUniqueViolation(err) {
		return Environment{}, ErrDuplicate
	}
	return updated, err
}

func (d *DB) ArchiveEnvironment(ctx context.Context, workspaceID int64, externalID string) (Environment, error) {
	return scanEnvironment(d.Pool.QueryRow(ctx, `
		update environments
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id, created_by_api_key_id,
			name, description, config, metadata, scope, provider, resolved_template,
			created_at, updated_at, archived_at, deleted_at
	`, workspaceID, externalID))
}

func (d *DB) DeleteEnvironment(ctx context.Context, workspaceID int64, externalID string) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var environmentID int64
	if err := tx.QueryRow(ctx, `
		select id
		from environments
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, externalID).Scan(&environmentID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}

	var activeWork int
	if err := tx.QueryRow(ctx, `
		select count(*)
		from environment_work
		where workspace_id = $1
			and environment_id = $2
			and deleted_at is null
			and state <> 'stopped'
	`, workspaceID, environmentID).Scan(&activeWork); err != nil {
		return err
	}
	if activeWork > 0 {
		return ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		update environments
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where workspace_id = $1 and id = $2 and deleted_at is null
	`, workspaceID, environmentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) ListEnvironmentsPage(ctx context.Context, params ListEnvironmentsPageParams) ([]Environment, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := environmentSelectSQL() + `
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	environments, err := scanEnvironmentRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(environments) > params.Limit
	if hasMore {
		environments = environments[:params.Limit]
	}
	return environments, hasMore, nil
}

func (d *DB) CreateEnvironmentKey(ctx context.Context, key EnvironmentKey, keyHash string) error {
	_, err := d.Pool.Exec(ctx, `
		insert into environment_keys (
			external_id, organization_id, workspace_id, environment_id,
			environment_external_id, key_hash, status
		)
		values ($1, $2, $3, $4, $5, $6, 'active')
		on conflict (external_id) do update set
			organization_id = excluded.organization_id,
			workspace_id = excluded.workspace_id,
			environment_id = excluded.environment_id,
			environment_external_id = excluded.environment_external_id,
			key_hash = excluded.key_hash,
			status = 'active'
	`, key.ExternalID, key.OrganizationID, key.WorkspaceID, key.EnvironmentID, key.EnvironmentExternalID, keyHash)
	return err
}

func (d *DB) GetEnvironmentKey(ctx context.Context, keyHash string) (EnvironmentKey, error) {
	var key EnvironmentKey
	err := d.Pool.QueryRow(ctx, `
		with updated as (
			update environment_keys
			set last_used_at = now()
			where key_hash = $1 and status = 'active'
			returning id, external_id, organization_id, workspace_id, environment_id, environment_external_id
		)
		select updated.id, updated.external_id, updated.organization_id, organizations.external_id,
			updated.workspace_id, workspaces.uuid::text, workspaces.external_id,
			updated.environment_id, updated.environment_external_id
		from updated
		join organizations on organizations.id = updated.organization_id
		join workspaces on workspaces.id = updated.workspace_id
	`, keyHash).Scan(
		&key.ID, &key.ExternalID, &key.OrganizationID, &key.OrganizationExternalID,
		&key.WorkspaceID, &key.WorkspaceUUID, &key.WorkspaceExternalID,
		&key.EnvironmentID, &key.EnvironmentExternalID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentKey{}, ErrNotFound
	}
	return key, err
}

func (d *DB) CreateEnvironmentWork(ctx context.Context, work EnvironmentWork) (EnvironmentWork, error) {
	return scanEnvironmentWork(d.Pool.QueryRow(ctx, `
		insert into environment_work (
			uuid, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, created_at, updated_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7::jsonb, $8::jsonb, $9, $10, $11, $11
		)
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
	`, work.UUID, work.ExternalID, work.OrganizationID, work.WorkspaceID, work.EnvironmentID,
		work.EnvironmentExternalID, jsonArg(work.Data), jsonArg(work.Metadata), work.Secret, coalesceWorkState(work.State), work.CreatedAt))
}

func (d *DB) GetEnvironmentWork(ctx context.Context, workspaceID int64, environmentExternalID, workExternalID string) (EnvironmentWork, error) {
	return scanEnvironmentWork(d.Pool.QueryRow(ctx, environmentWorkSelectSQL()+`
		where workspace_id = $1 and environment_external_id = $2 and external_id = $3 and deleted_at is null
	`, workspaceID, environmentExternalID, workExternalID))
}

func (d *DB) GetLatestEnvironmentWorkByData(ctx context.Context, workspaceID int64, environmentExternalID, dataType, dataID string) (EnvironmentWork, error) {
	return scanEnvironmentWork(d.Pool.QueryRow(ctx, environmentWorkSelectSQL()+`
		where workspace_id = $1
			and environment_external_id = $2
			and data->>'type' = $3
			and data->>'id' = $4
			and deleted_at is null
		order by created_at desc, id desc
		limit 1
	`, workspaceID, environmentExternalID, dataType, dataID))
}

func (d *DB) ListEnvironmentWorkPage(ctx context.Context, params ListEnvironmentWorkPageParams) ([]EnvironmentWork, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := environmentWorkSelectSQL() + `
		where workspace_id = $1 and environment_external_id = $2 and deleted_at is null
	`
	args := []any{params.WorkspaceID, params.EnvironmentExternalID}
	nextArg := 3
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	work, err := scanEnvironmentWorkRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(work) > params.Limit
	if hasMore {
		work = work[:params.Limit]
	}
	return work, hasMore, nil
}

func (d *DB) PollEnvironmentWork(ctx context.Context, workspaceID int64, environmentExternalID, workerID string, claimFor time.Duration) (*EnvironmentWork, error) {
	if claimFor <= 0 {
		claimFor = 5 * time.Second
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if workerID != "" {
		if _, err := tx.Exec(ctx, `
			insert into environment_worker_polls (
				organization_id, workspace_id, environment_id, environment_external_id, worker_id, last_poll_at
			)
			select organization_id, workspace_id, id, external_id, $3, now()
			from environments
			where workspace_id = $1 and external_id = $2 and deleted_at is null
			on conflict (environment_id, worker_id) do update set last_poll_at = excluded.last_poll_at
		`, workspaceID, environmentExternalID, workerID); err != nil {
			return nil, err
		}
	}

	work, err := scanEnvironmentWork(tx.QueryRow(ctx, `
		update environment_work
		set claimed_by_worker_id = $3,
			claim_expires_at = $4,
			updated_at = now()
		where id = (
			select id
			from environment_work
			where workspace_id = $1
				and environment_external_id = $2
				and deleted_at is null
				and state = 'queued'
				and (claim_expires_at is null or claim_expires_at <= now())
			order by created_at asc, id asc
			limit 1
			for update skip locked
		)
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
	`, workspaceID, environmentExternalID, nullableWorkerID(workerID), time.Now().UTC().Add(claimFor)))
	if errors.Is(err, ErrNotFound) {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
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
	work, err := scanEnvironmentWork(d.Pool.QueryRow(ctx, fmt.Sprintf(`
		update environment_work
		set claimed_by_worker_id = $1,
			claim_expires_at = $2,
			updated_at = now()
		where id = (
			select id
			from environment_work
			where deleted_at is null
				and state = 'queued'
				and (claim_expires_at is null or claim_expires_at <= now())
				%s
			order by created_at asc, id asc
			limit 1
			for update skip locked
		)
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
	`, filter), nullableWorkerID(workerID), time.Now().UTC().Add(claimFor)))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &work, nil
}

func (d *DB) GetEnvironmentByInternalID(ctx context.Context, workspaceID, environmentID int64) (Environment, error) {
	return scanEnvironment(d.Pool.QueryRow(ctx, environmentSelectSQL()+`
		where workspace_id = $1 and id = $2 and deleted_at is null
	`, workspaceID, environmentID))
}

func (d *DB) AckEnvironmentWork(ctx context.Context, workspaceID int64, environmentExternalID, workExternalID string) (EnvironmentWork, error) {
	return scanEnvironmentWork(d.Pool.QueryRow(ctx, `
		update environment_work
		set state = case when state = 'queued' then 'starting' else state end,
			acknowledged_at = coalesce(acknowledged_at, now()),
			started_at = coalesce(started_at, now()),
			claim_expires_at = null,
			updated_at = now()
		where workspace_id = $1
			and environment_external_id = $2
			and external_id = $3
			and deleted_at is null
			and state in ('queued', 'starting', 'active')
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
	`, workspaceID, environmentExternalID, workExternalID))
}

func (d *DB) UpdateEnvironmentWorkMetadata(ctx context.Context, workspaceID int64, environmentExternalID, workExternalID string, metadata json.RawMessage) (EnvironmentWork, error) {
	return updateEnvironmentWorkMetadata(ctx, d.Pool, workspaceID, environmentExternalID, workExternalID, metadata)
}

func updateEnvironmentWorkMetadata(ctx context.Context, querier queryRower, workspaceID int64, environmentExternalID, workExternalID string, metadata json.RawMessage) (EnvironmentWork, error) {
	return scanEnvironmentWork(querier.QueryRow(ctx, `
		update environment_work
		set metadata = $4::jsonb,
			updated_at = now()
		where workspace_id = $1 and environment_external_id = $2 and external_id = $3 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
	`, workspaceID, environmentExternalID, workExternalID, jsonArg(metadata)))
}

func (d *DB) HeartbeatEnvironmentWork(ctx context.Context, workspaceID int64, environmentExternalID, workExternalID, expectedLastHeartbeat string, ttlSeconds int, format func(time.Time) string) (WorkHeartbeatResult, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	if ttlSeconds < 5 {
		ttlSeconds = 5
	}
	if ttlSeconds > 300 {
		ttlSeconds = 300
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return WorkHeartbeatResult{}, err
	}
	defer tx.Rollback(ctx)

	current, err := scanEnvironmentWork(tx.QueryRow(ctx, environmentWorkSelectSQL()+`
		where workspace_id = $1 and environment_external_id = $2 and external_id = $3 and deleted_at is null
		for update
	`, workspaceID, environmentExternalID, workExternalID))
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
	updated, err := scanEnvironmentWork(tx.QueryRow(ctx, `
		update environment_work
		set state = $4,
			latest_heartbeat_at = now(),
			heartbeat_ttl_seconds = $5,
			updated_at = now()
		where id = $1 and workspace_id = $2 and environment_external_id = $3
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
	`, current.ID, workspaceID, environmentExternalID, nextState, ttlSeconds))
	if err != nil {
		return WorkHeartbeatResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkHeartbeatResult{}, err
	}
	lastHeartbeat := ""
	if updated.LatestHeartbeatAt != nil {
		lastHeartbeat = format(*updated.LatestHeartbeatAt)
	}
	return WorkHeartbeatResult{Work: updated, TTLSeconds: ttlSeconds, LeaseExtended: leaseExtended, LastHeartbeat: lastHeartbeat}, nil
}

func (d *DB) StopEnvironmentWork(ctx context.Context, workspaceID int64, environmentExternalID, workExternalID string, force bool) (EnvironmentWork, error) {
	nextState := "stopped"
	if !force {
		nextState = "stopping"
	}
	return scanEnvironmentWork(d.Pool.QueryRow(ctx, `
		update environment_work
		set state = $4,
			stop_requested_at = coalesce(stop_requested_at, now()),
			stopped_at = case when $4 = 'stopped' then coalesce(stopped_at, now()) else stopped_at end,
			updated_at = now()
		where workspace_id = $1
			and environment_external_id = $2
			and external_id = $3
			and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
	`, workspaceID, environmentExternalID, workExternalID, nextState))
}

func (d *DB) EnvironmentWorkStats(ctx context.Context, workspaceID int64, environmentExternalID string) (EnvironmentWorkStats, error) {
	var stats EnvironmentWorkStats
	var workers int
	err := d.Pool.QueryRow(ctx, `
		select
			count(*) filter (
				where state = 'queued'
					and (claim_expires_at is null or claim_expires_at <= now())
			)::int as depth,
			count(*) filter (where state <> 'stopped')::int as pending,
			min(created_at) filter (where state = 'queued') as oldest_queued_at,
			coalesce((
				select count(distinct worker_id)::int
				from environment_worker_polls p
				where p.workspace_id = $1
					and p.environment_external_id = $2
					and p.last_poll_at > now() - interval '30 seconds'
			), 0)::int as workers_polling
		from environment_work
		where workspace_id = $1
			and environment_external_id = $2
			and deleted_at is null
	`, workspaceID, environmentExternalID).Scan(&stats.Depth, &stats.Pending, &stats.OldestQueuedAt, &workers)
	if err != nil {
		return EnvironmentWorkStats{}, err
	}
	if workers > 0 {
		stats.WorkersPolling = &workers
	}
	return stats, nil
}

func (d *DB) CreateEnvironmentSandbox(ctx context.Context, sandbox EnvironmentSandbox) (EnvironmentSandbox, error) {
	return scanEnvironmentSandbox(d.Pool.QueryRow(ctx, `
		insert into environment_sandboxes (
			uuid, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, work_id, work_external_id, provider, template,
			provider_sandbox_id, state, metadata, last_error, created_at, updated_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13::jsonb, $14, $15, $15
		)
		returning id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, work_id, work_external_id, provider, template,
			provider_sandbox_id, state, metadata, last_error, created_at, updated_at, stopped_at
	`, sandbox.UUID, sandbox.ExternalID, sandbox.OrganizationID, sandbox.WorkspaceID, sandbox.EnvironmentID,
		sandbox.EnvironmentExternalID, sandbox.WorkID, sandbox.WorkExternalID, sandbox.Provider, sandbox.Template,
		sandbox.ProviderSandboxID, sandbox.State, jsonArg(sandbox.Metadata), sandbox.LastError, sandbox.CreatedAt))
}

func (d *DB) UpdateEnvironmentSandboxState(ctx context.Context, workspaceID int64, externalID, state string, providerSandboxID *string, lastError *string, stoppedAt *time.Time) error {
	_, err := d.Pool.Exec(ctx, `
		update environment_sandboxes
		set state = $3,
			provider_sandbox_id = coalesce($4, provider_sandbox_id),
			last_error = $5,
			stopped_at = coalesce($6, stopped_at),
			updated_at = now()
		where workspace_id = $1 and external_id = $2
	`, workspaceID, externalID, state, providerSandboxID, lastError, stoppedAt)
	return err
}

func (d *DB) GetActiveEnvironmentSandboxForWork(ctx context.Context, workspaceID int64, environmentExternalID, workExternalID string) (EnvironmentSandbox, error) {
	return scanEnvironmentSandbox(d.Pool.QueryRow(ctx, `
		select id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, work_id, work_external_id, provider, template,
			provider_sandbox_id, state, metadata, last_error, created_at, updated_at, stopped_at
		from environment_sandboxes
		where workspace_id = $1
			and environment_external_id = $2
			and work_external_id = $3
			and provider_sandbox_id is not null
			and state in ('creating', 'running', 'stopping')
		order by created_at desc, id desc
		limit 1
	`, workspaceID, environmentExternalID, workExternalID))
}

func environmentSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id, created_by_api_key_id,
			name, description, config, metadata, scope, provider, resolved_template,
			created_at, updated_at, archived_at, deleted_at
		from environments
	`
}

func environmentWorkSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, claimed_by_worker_id,
			claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
			heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at
		from environment_work
	`
}

type environmentScanner interface {
	Scan(dest ...any) error
}

type environmentRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanEnvironment(row environmentScanner) (Environment, error) {
	var env Environment
	var config, metadata []byte
	err := row.Scan(&env.ID, &env.UUID, &env.ExternalID, &env.OrganizationID, &env.WorkspaceID, &env.CreatedByAPIKeyID,
		&env.Name, &env.Description, &config, &metadata, &env.Scope, &env.Provider, &env.ResolvedTemplate,
		&env.CreatedAt, &env.UpdatedAt, &env.ArchivedAt, &env.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	if err != nil {
		return Environment{}, err
	}
	env.Config = copyRaw(config)
	env.Metadata = copyRaw(metadata)
	return env, nil
}

func scanEnvironmentRows(rows environmentRows) ([]Environment, error) {
	var environments []Environment
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		environments = append(environments, env)
	}
	return environments, rows.Err()
}

func scanEnvironmentWork(row environmentScanner) (EnvironmentWork, error) {
	var work EnvironmentWork
	var data, metadata []byte
	err := row.Scan(&work.ID, &work.UUID, &work.ExternalID, &work.OrganizationID, &work.WorkspaceID, &work.EnvironmentID,
		&work.EnvironmentExternalID, &data, &metadata, &work.Secret, &work.State, &work.ClaimedByWorkerID,
		&work.ClaimExpiresAt, &work.AcknowledgedAt, &work.StartedAt, &work.LatestHeartbeatAt,
		&work.HeartbeatTTLSeconds, &work.StopRequestedAt, &work.StoppedAt, &work.CreatedAt, &work.UpdatedAt, &work.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentWork{}, ErrNotFound
	}
	if err != nil {
		return EnvironmentWork{}, err
	}
	work.Data = copyRaw(data)
	work.Metadata = copyRaw(metadata)
	return work, nil
}

func scanEnvironmentWorkRows(rows environmentRows) ([]EnvironmentWork, error) {
	var work []EnvironmentWork
	for rows.Next() {
		item, err := scanEnvironmentWork(rows)
		if err != nil {
			return nil, err
		}
		work = append(work, item)
	}
	return work, rows.Err()
}

func scanEnvironmentSandbox(row environmentScanner) (EnvironmentSandbox, error) {
	var sandbox EnvironmentSandbox
	var metadata []byte
	err := row.Scan(&sandbox.ID, &sandbox.UUID, &sandbox.ExternalID, &sandbox.OrganizationID, &sandbox.WorkspaceID, &sandbox.EnvironmentID,
		&sandbox.EnvironmentExternalID, &sandbox.WorkID, &sandbox.WorkExternalID, &sandbox.Provider, &sandbox.Template,
		&sandbox.ProviderSandboxID, &sandbox.State, &metadata, &sandbox.LastError, &sandbox.CreatedAt, &sandbox.UpdatedAt, &sandbox.StoppedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentSandbox{}, ErrNotFound
	}
	if err != nil {
		return EnvironmentSandbox{}, err
	}
	sandbox.Metadata = copyRaw(metadata)
	return sandbox, nil
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
