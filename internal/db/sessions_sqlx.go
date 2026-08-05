package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

const (
	sessionSQLXColumns = `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		created_by_api_key_uuid,
		environment_uuid, environment_external_id,
		agent_uuid, agent_external_id,
		agent_version, agent_snapshot, deployment_uuid,
		deployment_external_id, title, metadata, vault_ids, status, usage, stats,
		outcome_evaluations, created_at, updated_at, archived_at, deleted_at`
	sessionResourceSQLXColumns = `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		session_uuid,
		session_external_id, resource_type, payload, secret_payload,
		created_at, updated_at, deleted_at`
	getSessionQuery = `
		select ` + sessionSQLXColumns + `
		from sessions
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
	`
	updateSessionQuery = `
		update sessions
		set agent_snapshot = CAST(:agent_snapshot AS jsonb),
			title = :title,
			metadata = CAST(:metadata AS jsonb),
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
			and archived_at is null
			and status = 'idle'
		returning ` + sessionSQLXColumns + `
	`
	patchSessionMetadataQuery = `
		update sessions
		set metadata = coalesce(metadata, CAST('{}' AS jsonb)) || CAST(:metadata_patch AS jsonb),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
		returning ` + sessionSQLXColumns + `
	`
	setSessionOutcomeEvaluationsQuery = `
		update sessions
		set outcome_evaluations = CAST(:outcome_evaluations AS jsonb),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
		returning ` + sessionSQLXColumns + `
	`
	setSessionStatusQuery = `
		update sessions
		set status = :status, updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
	`
	setSessionThreadStatusQuery = `
		update session_threads
		set status = :status, updated_at = now()
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :thread_external_id
			and deleted_at is null
	`
	createSessionThreadIfAbsentQuery = `
		insert into session_threads (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id,
			parent_thread_uuid, parent_thread_external_id, agent_snapshot, status, usage, stats,
			created_at, updated_at
		)
		values (
			:thread_uuid, :thread_external_id, :organization_uuid, :workspace_uuid,
			:session_uuid, :session_external_id, :parent_thread_uuid, :parent_thread_external_id,
			CAST(:agent_snapshot AS jsonb), :status, CAST(:usage AS jsonb),
			CAST(:stats AS jsonb), :created_at, :created_at
		)
		on conflict (workspace_uuid, external_id) do nothing
		returning ` + sessionThreadSQLXColumns + `
	`
	archiveSessionQuery = `
		update sessions
		set archived_at = coalesce(archived_at, now()), updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
			and status not in ('running', 'rescheduling')
		returning ` + sessionSQLXColumns + `
	`
	deleteSessionQuery = `
		update sessions
		set deleted_at = coalesce(deleted_at, now()), updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
			and status not in ('running', 'rescheduling')
		returning ` + sessionSQLXColumns + `
	`
	deleteSessionThreadsQuery = `
		update session_threads
		set deleted_at = coalesce(deleted_at, now()), updated_at = now()
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and deleted_at is null
	`
	deleteSessionResourcesQuery = `
		update session_resources
		set deleted_at = coalesce(deleted_at, now()), updated_at = now()
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and payload is not null
			and deleted_at is null
	`
	deleteSessionEventsQuery = `
		update session_events
		set deleted_at = coalesce(deleted_at, now())
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and deleted_at is null
	`
	stopDeletedSessionEnvironmentWorkQuery = `
		update environment_work
		set state = case when state in ('stopped') then state else 'stopping' end,
			stop_requested_at = coalesce(stop_requested_at, now()),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and environment_external_id = :environment_external_id
			and data->>'id' = :session_external_id
			and deleted_at is null
			and state not in ('stopped')
	`
	listSessionResourcesQuery = `
		select ` + sessionResourceSQLXColumns + `
		from session_resources
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and payload is not null
			and deleted_at is null
		order by created_at desc, uuid desc
	`
	lockSessionForResourceMutationQuery = `
		select ` + sessionSQLXColumns + `
		from sessions
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
		for update
	`
	createSessionResourceQuery = `
		insert into session_resources (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id,
			resource_type, payload, secret_payload, created_at, updated_at
		)
		select
			:resource_uuid, :resource_external_id, :organization_uuid, :workspace_uuid,
			s.uuid, :session_external_id, :resource_type,
			CAST(:payload AS jsonb), CAST(:secret_payload AS jsonb), :created_at, :created_at
		from sessions s
		where s.workspace_uuid = :workspace_uuid
			and s.external_id = :session_external_id
			and s.deleted_at is null
			and s.archived_at is null
		returning ` + sessionResourceSQLXColumns + `
	`
	createSessionQuery = `
		insert into sessions (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, deployment_uuid, deployment_external_id,
			title, metadata, vault_ids,
			status, usage, stats, outcome_evaluations, created_at, updated_at
		)
		values (
			:session_uuid, :session_external_id, :organization_uuid, :workspace_uuid, :created_by_api_key_uuid,
			:environment_uuid, :environment_external_id, :agent_uuid, :agent_external_id,
			:agent_version, CAST(:agent_snapshot AS jsonb),
			:deployment_uuid, :deployment_external_id, :title,
			CAST(:metadata AS jsonb), CAST(:vault_ids AS jsonb), :status,
			CAST(:usage AS jsonb), CAST(:stats AS jsonb), CAST(:outcome_evaluations AS jsonb),
			:created_at, :created_at
		)
		returning ` + sessionSQLXColumns + `
	`
	createSessionThreadQuery = `
		insert into session_threads (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id,
			parent_thread_uuid, parent_thread_external_id, agent_snapshot, status, usage, stats,
			created_at, updated_at
		)
		values (
			:thread_uuid, :thread_external_id, :organization_uuid, :workspace_uuid,
			:session_uuid, :session_external_id, :parent_thread_uuid, :parent_thread_external_id,
			CAST(:agent_snapshot AS jsonb), :status, CAST(:usage AS jsonb),
			CAST(:stats AS jsonb), :created_at, :created_at
		)
		returning ` + sessionThreadSQLXColumns + `
	`
	getSessionResourceQuery = `
		select ` + sessionResourceSQLXColumns + `
		from session_resources
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :resource_external_id
			and payload is not null
			and deleted_at is null
	`
	updateSessionResourceQuery = `
		update session_resources
		set payload = CAST(:payload AS jsonb),
			secret_payload = CAST(:secret_payload AS jsonb),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :resource_external_id
			and payload is not null
			and deleted_at is null
		returning ` + sessionResourceSQLXColumns + `
	`
	createEnvironmentWorkQuery = `
		insert into environment_work (
			uuid, external_id, organization_uuid, workspace_uuid, environment_uuid,
			environment_external_id, data, metadata, secret, state, created_at, updated_at
		)
		values (
			:work_uuid, :work_external_id, :organization_uuid, :workspace_uuid, :environment_uuid,
			:environment_external_id, CAST(:data AS jsonb), CAST(:metadata AS jsonb),
			:secret, :state, :created_at, :created_at
		)
		returning ` + environmentWorkSQLXColumns + `
	`
	sessionThreadSQLXColumns = `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		session_uuid, session_external_id,
		parent_thread_uuid, parent_thread_external_id,
		agent_snapshot, status, usage, stats, created_at, updated_at, archived_at, deleted_at`
	environmentWorkSQLXColumns = `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		environment_uuid,
		environment_external_id, data, metadata, secret, state,
		claimed_by_worker_id, claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
		heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at`
)

// 这些 *Row 结构体是 sessions 相关表的 sqlx 扫描边界；领域层拿到的仍是
// Session / SessionResource 等业务模型，而不是直接暴露 JSONB 原始字段。
type sessionRow struct {
	UUID                  uuid.UUID     `db:"uuid"`
	ExternalID            string        `db:"external_id"`
	OrganizationUUID      uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID         uuid.UUID     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID   uuid.UUID     `db:"created_by_api_key_uuid"`
	EnvironmentUUID       uuid.UUID     `db:"environment_uuid"`
	EnvironmentExternalID string        `db:"environment_external_id"`
	AgentUUID             uuid.UUID     `db:"agent_uuid"`
	AgentExternalID       string        `db:"agent_external_id"`
	AgentVersion          int           `db:"agent_version"`
	AgentSnapshot         []byte        `db:"agent_snapshot"`
	DeploymentUUID        uuid.NullUUID `db:"deployment_uuid"`
	DeploymentID          *string       `db:"deployment_external_id"`
	Title                 *string       `db:"title"`
	Metadata              []byte        `db:"metadata"`
	VaultIDs              []byte        `db:"vault_ids"`
	Status                string        `db:"status"`
	Usage                 []byte        `db:"usage"`
	Stats                 []byte        `db:"stats"`
	OutcomeEvaluations    []byte        `db:"outcome_evaluations"`
	CreatedAt             time.Time     `db:"created_at"`
	UpdatedAt             time.Time     `db:"updated_at"`
	ArchivedAt            *time.Time    `db:"archived_at"`
	DeletedAt             *time.Time    `db:"deleted_at"`
}

type sessionResourceRow struct {
	UUID              uuid.UUID  `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  uuid.UUID  `db:"organization_uuid"`
	WorkspaceUUID     uuid.UUID  `db:"workspace_uuid"`
	SessionUUID       uuid.UUID  `db:"session_uuid"`
	SessionExternalID string     `db:"session_external_id"`
	ResourceType      string     `db:"resource_type"`
	Payload           []byte     `db:"payload"`
	SecretPayload     []byte     `db:"secret_payload"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	DeletedAt         *time.Time `db:"deleted_at"`
}

type sessionThreadRow struct {
	UUID                   uuid.UUID     `db:"uuid"`
	ExternalID             string        `db:"external_id"`
	OrganizationUUID       uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID          uuid.UUID     `db:"workspace_uuid"`
	SessionUUID            uuid.UUID     `db:"session_uuid"`
	SessionExternalID      string        `db:"session_external_id"`
	ParentThreadUUID       uuid.NullUUID `db:"parent_thread_uuid"`
	ParentThreadExternalID *string       `db:"parent_thread_external_id"`
	AgentSnapshot          []byte        `db:"agent_snapshot"`
	Status                 string        `db:"status"`
	Usage                  []byte        `db:"usage"`
	Stats                  []byte        `db:"stats"`
	CreatedAt              time.Time     `db:"created_at"`
	UpdatedAt              time.Time     `db:"updated_at"`
	ArchivedAt             *time.Time    `db:"archived_at"`
	DeletedAt              *time.Time    `db:"deleted_at"`
}

type environmentWorkRow struct {
	UUID                  uuid.UUID  `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      uuid.UUID  `db:"organization_uuid"`
	WorkspaceUUID         uuid.UUID  `db:"workspace_uuid"`
	EnvironmentUUID       uuid.UUID  `db:"environment_uuid"`
	EnvironmentExternalID string     `db:"environment_external_id"`
	Data                  []byte     `db:"data"`
	Metadata              []byte     `db:"metadata"`
	Secret                *string    `db:"secret"`
	State                 string     `db:"state"`
	ClaimedByWorkerID     *string    `db:"claimed_by_worker_id"`
	ClaimExpiresAt        *time.Time `db:"claim_expires_at"`
	AcknowledgedAt        *time.Time `db:"acknowledged_at"`
	StartedAt             *time.Time `db:"started_at"`
	LatestHeartbeatAt     *time.Time `db:"latest_heartbeat_at"`
	HeartbeatTTLSeconds   *int       `db:"heartbeat_ttl_seconds"`
	StopRequestedAt       *time.Time `db:"stop_requested_at"`
	StoppedAt             *time.Time `db:"stopped_at"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

func sessionLookupArguments(workspaceUUID string, sessionExternalID string) map[string]any {
	return map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": sessionExternalID,
	}
}

func (tx ManagedAgentActivationTx) LockSessionForEvents(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
) (Session, error) {
	row, found, err := tx.sessionMapper.LockSessionForEvents(
		ctx,
		workspaceUUID,
		sessionExternalID,
	)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, ErrNotFound
	}
	return row.session(), nil
}

func getSessionSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (Session, error) {
	var row sessionRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	return row.session(), nil
}

func listSessionsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]Session, error) {
	var rows []sessionRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	sessions := make([]Session, len(rows))
	for index := range rows {
		sessions[index] = rows[index].session()
	}
	return sessions, nil
}

func listSessionThreadsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]SessionThread, error) {
	var rows []sessionThreadRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	threads := make([]SessionThread, len(rows))
	for index := range rows {
		threads[index] = rows[index].thread()
	}
	return threads, nil
}

func getSessionResourceSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (SessionResource, error) {
	var row sessionResourceRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionResource{}, ErrNotFound
		}
		return SessionResource{}, err
	}
	return row.resource(), nil
}

func listSessionResourcesSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]SessionResource, error) {
	var rows []sessionResourceRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	resources := make([]SessionResource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, row.resource())
	}
	return resources, nil
}

func createSessionResourceSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	resource SessionResource,
) (SessionResource, error) {
	var row sessionResourceRow
	err := namedGetContext(ctx, database, &row, createSessionResourceQuery, map[string]any{
		"resource_uuid":        dbUUID(resource.UUID),
		"resource_external_id": resource.ExternalID,
		"organization_uuid":    dbUUID(resource.OrganizationUUID),
		"workspace_uuid":       dbUUID(resource.WorkspaceUUID),
		"session_external_id":  resource.SessionExternalID,
		"resource_type":        resource.ResourceType,
		"payload":              jsonArg(resource.Payload),
		"secret_payload":       jsonArg(resource.SecretPayload),
		"created_at":           resource.CreatedAt,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return SessionResource{}, ErrNotFound
	}
	if err != nil {
		return SessionResource{}, err
	}
	return row.resource(), nil
}

func insertSessionSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	input CreateSessionInput,
) (Session, SessionThread, []SessionResource, EnvironmentWork, error) {
	session, err := insertSessionRecordSQLX(ctx, tx, input.Session)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	filesystem, err := insertSessionFilesystemTx(ctx, tx, session)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if err := ensureFilestoreFixedRootsTx(
		ctx,
		newSQLXTxExecutor(tx),
		filesystem,
		session.CreatedAt,
	); err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}

	input.Thread.SessionUUID = session.UUID
	input.Thread.SessionExternalID = session.ExternalID
	thread, err := insertSessionThreadSQLX(ctx, tx, input.Thread)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}

	resources := make([]SessionResource, 0, len(input.Resources))
	if err := enforceSessionFileResourceCapacityTx(
		ctx,
		tx,
		session.WorkspaceUUID,
		session.ExternalID,
		sessionFileResourceCount(input.Resources),
	); err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if sessionHasFileMount(input.Resources) {
		lockedFilesystem, err := lockSessionFilestoreMutationTx(ctx, tx, session)
		if err != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
		}
		if lockedFilesystem.UUID != filesystem.UUID {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, ErrPreconditionFailed
		}
		filesystem = lockedFilesystem
	}
	for _, resourceInput := range input.Resources {
		resource := resourceInput.Resource
		resource.SessionExternalID = session.ExternalID
		created, err := createSessionResourceSQLX(ctx, tx, resource)
		if err != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
		}
		if _, err := bindSessionFileResourceWithLockedFilesystemTx(
			ctx,
			tx,
			session,
			filesystem,
			created,
			resourceInput.FileMount,
		); err != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
		}
		resources = append(resources, created)
	}

	work, err := insertEnvironmentWorkSQLX(ctx, tx, input.Work)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	return session, thread, resources, work, nil
}

func insertSessionRecordSQLX(ctx context.Context, database sqlxNamedQueryer, session Session) (Session, error) {
	var row sessionRow
	err := namedGetContext(ctx, database, &row, createSessionQuery, createSessionArguments(session))
	if err != nil {
		return Session{}, err
	}
	return row.session(), nil
}

func insertSessionThreadSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	thread SessionThread,
) (SessionThread, error) {
	return insertSessionThreadWithQuerySQLX(
		ctx,
		database,
		createSessionThreadQuery,
		createSessionThreadArguments(thread),
	)
}

func insertSessionThreadWithQuerySQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (SessionThread, error) {
	var row sessionThreadRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if err != nil {
		return SessionThread{}, err
	}
	return row.thread(), nil
}

func insertEnvironmentWorkSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	work EnvironmentWork,
) (EnvironmentWork, error) {
	var row environmentWorkRow
	err := namedGetContext(ctx, database, &row, createEnvironmentWorkQuery, createEnvironmentWorkArguments(work))
	if err != nil {
		return EnvironmentWork{}, err
	}
	return row.work(), nil
}

func createSessionArguments(session Session) map[string]any {
	return map[string]any{
		"session_uuid":            dbUUID(session.UUID),
		"session_external_id":     session.ExternalID,
		"organization_uuid":       dbUUID(session.OrganizationUUID),
		"workspace_uuid":          dbUUID(session.WorkspaceUUID),
		"created_by_api_key_uuid": dbUUID(session.CreatedByAPIKeyUUID),
		"environment_uuid":        dbUUID(session.EnvironmentUUID),
		"environment_external_id": session.EnvironmentExternalID,
		"agent_uuid":              dbUUID(session.AgentUUID),
		"agent_external_id":       session.AgentExternalID,
		"agent_version":           session.AgentVersion,
		"agent_snapshot":          jsonArg(session.AgentSnapshot),
		"deployment_uuid":         dbNullableUUID(session.DeploymentUUID),
		"deployment_external_id":  session.DeploymentID,
		"title":                   session.Title,
		"metadata":                jsonArg(session.Metadata),
		"vault_ids":               jsonArg(session.VaultIDs),
		"status":                  session.Status,
		"usage":                   jsonArg(session.Usage),
		"stats":                   jsonArg(session.Stats),
		"outcome_evaluations":     jsonArg(session.OutcomeEvaluations),
		"created_at":              session.CreatedAt,
	}
}

func createSessionThreadArguments(thread SessionThread) map[string]any {
	return map[string]any{
		"thread_uuid":               dbUUID(thread.UUID),
		"thread_external_id":        thread.ExternalID,
		"organization_uuid":         dbUUID(thread.OrganizationUUID),
		"workspace_uuid":            dbUUID(thread.WorkspaceUUID),
		"session_uuid":              dbUUID(thread.SessionUUID),
		"session_external_id":       thread.SessionExternalID,
		"parent_thread_uuid":        dbNullableUUID(thread.ParentThreadUUID),
		"parent_thread_external_id": thread.ParentThreadExternalID,
		"agent_snapshot":            jsonArg(thread.AgentSnapshot),
		"status":                    thread.Status,
		"usage":                     jsonArg(thread.Usage),
		"stats":                     jsonArg(thread.Stats),
		"created_at":                thread.CreatedAt,
	}
}

func createEnvironmentWorkArguments(work EnvironmentWork) map[string]any {
	return map[string]any{
		"work_uuid":               dbUUID(work.UUID),
		"work_external_id":        work.ExternalID,
		"organization_uuid":       dbUUID(work.OrganizationUUID),
		"workspace_uuid":          dbUUID(work.WorkspaceUUID),
		"environment_uuid":        dbUUID(work.EnvironmentUUID),
		"environment_external_id": work.EnvironmentExternalID,
		"data":                    jsonArg(work.Data),
		"metadata":                jsonArg(work.Metadata),
		"secret":                  work.Secret,
		"state":                   work.State,
		"created_at":              work.CreatedAt,
	}
}

func (r sessionRow) session() Session {
	return Session{
		UUID:                  r.UUID.String(),
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID.String(),
		WorkspaceUUID:         r.WorkspaceUUID.String(),
		CreatedByAPIKeyUUID:   r.CreatedByAPIKeyUUID.String(),
		EnvironmentUUID:       r.EnvironmentUUID.String(),
		EnvironmentExternalID: r.EnvironmentExternalID,
		AgentUUID:             r.AgentUUID.String(),
		AgentExternalID:       r.AgentExternalID,
		AgentVersion:          r.AgentVersion,
		AgentSnapshot:         copyRaw(r.AgentSnapshot),
		DeploymentUUID:        nullableUUIDString(r.DeploymentUUID),
		DeploymentID:          r.DeploymentID,
		Title:                 r.Title,
		Metadata:              copyRaw(r.Metadata),
		VaultIDs:              copyRaw(r.VaultIDs),
		Status:                r.Status,
		Usage:                 copyRaw(r.Usage),
		Stats:                 copyRaw(r.Stats),
		OutcomeEvaluations:    copyRaw(r.OutcomeEvaluations),
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		ArchivedAt:            r.ArchivedAt,
		DeletedAt:             r.DeletedAt,
	}
}

func (r sessionThreadRow) thread() SessionThread {
	return SessionThread{
		UUID:                   r.UUID.String(),
		ExternalID:             r.ExternalID,
		OrganizationUUID:       r.OrganizationUUID.String(),
		WorkspaceUUID:          r.WorkspaceUUID.String(),
		SessionUUID:            r.SessionUUID.String(),
		SessionExternalID:      r.SessionExternalID,
		ParentThreadUUID:       nullableUUIDString(r.ParentThreadUUID),
		ParentThreadExternalID: r.ParentThreadExternalID,
		AgentSnapshot:          copyRaw(r.AgentSnapshot),
		Status:                 r.Status,
		Usage:                  copyRaw(r.Usage),
		Stats:                  copyRaw(r.Stats),
		CreatedAt:              r.CreatedAt,
		UpdatedAt:              r.UpdatedAt,
		ArchivedAt:             r.ArchivedAt,
		DeletedAt:              r.DeletedAt,
	}
}

func (r sessionResourceRow) resource() SessionResource {
	return SessionResource{
		UUID:              r.UUID.String(),
		ExternalID:        r.ExternalID,
		OrganizationUUID:  r.OrganizationUUID.String(),
		WorkspaceUUID:     r.WorkspaceUUID.String(),
		SessionUUID:       r.SessionUUID.String(),
		SessionExternalID: r.SessionExternalID,
		ResourceType:      r.ResourceType,
		Payload:           copyRaw(r.Payload),
		SecretPayload:     copyRaw(r.SecretPayload),
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
		DeletedAt:         r.DeletedAt,
	}
}

func (r environmentWorkRow) work() EnvironmentWork {
	return EnvironmentWork{
		UUID:                  r.UUID.String(),
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID.String(),
		WorkspaceUUID:         r.WorkspaceUUID.String(),
		EnvironmentUUID:       r.EnvironmentUUID.String(),
		EnvironmentExternalID: r.EnvironmentExternalID,
		Data:                  copyRaw(r.Data),
		Metadata:              copyRaw(r.Metadata),
		Secret:                r.Secret,
		State:                 r.State,
		ClaimedByWorkerID:     r.ClaimedByWorkerID,
		ClaimExpiresAt:        r.ClaimExpiresAt,
		AcknowledgedAt:        r.AcknowledgedAt,
		StartedAt:             r.StartedAt,
		LatestHeartbeatAt:     r.LatestHeartbeatAt,
		HeartbeatTTLSeconds:   r.HeartbeatTTLSeconds,
		StopRequestedAt:       r.StopRequestedAt,
		StoppedAt:             r.StoppedAt,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		DeletedAt:             r.DeletedAt,
	}
}
