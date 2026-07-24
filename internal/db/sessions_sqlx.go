package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	sessionSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id, workspace_id,
		created_by_api_key_id, environment_id, environment_external_id, agent_id, agent_external_id,
		agent_version, agent_snapshot, deployment_id, title, metadata, vault_ids, status, usage, stats,
		outcome_evaluations, created_at, updated_at, archived_at, deleted_at`
	sessionResourceSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id, workspace_id,
		session_id, session_external_id, resource_type, payload, secret_payload,
		created_at, updated_at, deleted_at`
	getSessionQuery = `
		select ` + sessionSQLXColumns + `
		from sessions
		where workspace_id = :workspace_id
			and external_id = :session_external_id
			and deleted_at is null
	`
	listSessionResourcesQuery = `
		select ` + sessionResourceSQLXColumns + `
		from session_resources
		where workspace_id = :workspace_id
			and session_external_id = :session_external_id
			and deleted_at is null
		order by created_at desc, id desc
	`
	lockSessionForResourceMutationQuery = `
		select ` + sessionSQLXColumns + `
		from sessions
		where workspace_id = :workspace_id
			and external_id = :session_external_id
			and deleted_at is null
		for update
	`
	createSessionResourceQuery = `
		insert into session_resources (
			uuid, external_id, organization_id, workspace_id, session_id, session_external_id,
			resource_type, payload, secret_payload, created_at, updated_at
		)
		select
			:resource_uuid, :resource_external_id, :organization_id, :workspace_id,
			s.id, :session_external_id, :resource_type,
			CAST(:payload AS jsonb), CAST(:secret_payload AS jsonb), :created_at, :created_at
		from sessions s
		where s.workspace_id = :workspace_id
			and s.external_id = :session_external_id
			and s.deleted_at is null
			and s.archived_at is null
		returning ` + sessionResourceSQLXColumns + `
	`
	createSessionQuery = `
		insert into sessions (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			environment_id, environment_external_id, agent_id, agent_external_id,
			agent_version, agent_snapshot, deployment_id, title, metadata, vault_ids,
			status, usage, stats, outcome_evaluations, created_at, updated_at
		)
		values (
			:session_uuid, :session_external_id, :organization_id, :workspace_id, :created_by_api_key_id,
			:environment_id, :environment_external_id, :agent_id, :agent_external_id,
			:agent_version, CAST(:agent_snapshot AS jsonb), :deployment_id, :title,
			CAST(:metadata AS jsonb), CAST(:vault_ids AS jsonb), :status,
			CAST(:usage AS jsonb), CAST(:stats AS jsonb), CAST(:outcome_evaluations AS jsonb),
			:created_at, :created_at
		)
		returning ` + sessionSQLXColumns + `
	`
	createSessionThreadQuery = `
		insert into session_threads (
			uuid, external_id, organization_id, workspace_id, session_id, session_external_id,
			parent_thread_id, parent_thread_external_id, agent_snapshot, status, usage, stats,
			created_at, updated_at
		)
		values (
			:thread_uuid, :thread_external_id, :organization_id, :workspace_id,
			:session_id, :session_external_id, :parent_thread_id, :parent_thread_external_id,
			CAST(:agent_snapshot AS jsonb), :status, CAST(:usage AS jsonb),
			CAST(:stats AS jsonb), :created_at, :created_at
		)
		returning ` + sessionThreadSQLXColumns + `
	`
	createEnvironmentWorkQuery = `
		insert into environment_work (
			uuid, external_id, organization_id, workspace_id, environment_id,
			environment_external_id, data, metadata, secret, state, created_at, updated_at
		)
		values (
			:work_uuid, :work_external_id, :organization_id, :workspace_id, :environment_id,
			:environment_external_id, CAST(:data AS jsonb), CAST(:metadata AS jsonb),
			:secret, :state, :created_at, :created_at
		)
		returning ` + environmentWorkSQLXColumns + `
	`
	sessionThreadSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id,
		workspace_id, session_id, session_external_id, parent_thread_id, parent_thread_external_id,
		agent_snapshot, status, usage, stats, created_at, updated_at, archived_at, deleted_at`
	environmentWorkSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id,
		workspace_id, environment_id, environment_external_id, data, metadata, secret, state,
		claimed_by_worker_id, claim_expires_at, acknowledged_at, started_at, latest_heartbeat_at,
		heartbeat_ttl_seconds, stop_requested_at, stopped_at, created_at, updated_at, deleted_at`
)

// 这些 *Row 结构体是 sessions 相关表的 sqlx 扫描边界；领域层拿到的仍是
// Session / SessionResource 等业务模型，而不是直接暴露 JSONB 原始字段。
type sessionRow struct {
	ID                    int64      `db:"id"`
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationID        int64      `db:"organization_id"`
	WorkspaceID           int64      `db:"workspace_id"`
	CreatedByAPIKeyID     int64      `db:"created_by_api_key_id"`
	EnvironmentID         int64      `db:"environment_id"`
	EnvironmentExternalID string     `db:"environment_external_id"`
	AgentID               int64      `db:"agent_id"`
	AgentExternalID       string     `db:"agent_external_id"`
	AgentVersion          int        `db:"agent_version"`
	AgentSnapshot         []byte     `db:"agent_snapshot"`
	DeploymentID          *string    `db:"deployment_id"`
	Title                 *string    `db:"title"`
	Metadata              []byte     `db:"metadata"`
	VaultIDs              []byte     `db:"vault_ids"`
	Status                string     `db:"status"`
	Usage                 []byte     `db:"usage"`
	Stats                 []byte     `db:"stats"`
	OutcomeEvaluations    []byte     `db:"outcome_evaluations"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	ArchivedAt            *time.Time `db:"archived_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

type sessionResourceRow struct {
	ID                int64      `db:"id"`
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationID    int64      `db:"organization_id"`
	WorkspaceID       int64      `db:"workspace_id"`
	SessionID         int64      `db:"session_id"`
	SessionExternalID string     `db:"session_external_id"`
	ResourceType      string     `db:"resource_type"`
	Payload           []byte     `db:"payload"`
	SecretPayload     []byte     `db:"secret_payload"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	DeletedAt         *time.Time `db:"deleted_at"`
}

type sessionThreadRow struct {
	ID                     int64      `db:"id"`
	UUID                   string     `db:"uuid"`
	ExternalID             string     `db:"external_id"`
	OrganizationID         int64      `db:"organization_id"`
	WorkspaceID            int64      `db:"workspace_id"`
	SessionID              int64      `db:"session_id"`
	SessionExternalID      string     `db:"session_external_id"`
	ParentThreadID         *int64     `db:"parent_thread_id"`
	ParentThreadExternalID *string    `db:"parent_thread_external_id"`
	AgentSnapshot          []byte     `db:"agent_snapshot"`
	Status                 string     `db:"status"`
	Usage                  []byte     `db:"usage"`
	Stats                  []byte     `db:"stats"`
	CreatedAt              time.Time  `db:"created_at"`
	UpdatedAt              time.Time  `db:"updated_at"`
	ArchivedAt             *time.Time `db:"archived_at"`
	DeletedAt              *time.Time `db:"deleted_at"`
}

type environmentWorkRow struct {
	ID                    int64      `db:"id"`
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationID        int64      `db:"organization_id"`
	WorkspaceID           int64      `db:"workspace_id"`
	EnvironmentID         int64      `db:"environment_id"`
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

func sessionLookupArguments(workspaceID int64, sessionExternalID string) map[string]any {
	return map[string]any{
		"workspace_id":        workspaceID,
		"session_external_id": sessionExternalID,
	}
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
		"resource_uuid":        resource.UUID,
		"resource_external_id": resource.ExternalID,
		"organization_id":      resource.OrganizationID,
		"workspace_id":         resource.WorkspaceID,
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
	filesystem, err := insertSessionFilesystemSQLXTx(ctx, tx, session)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if err := ensureFilestoreFixedRootsTx(
		ctx,
		tx,
		session.WorkspaceID,
		filesystem,
		session.CreatedAt,
	); err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}

	input.Thread.SessionID = session.ID
	input.Thread.SessionExternalID = session.ExternalID
	thread, err := insertSessionThreadSQLX(ctx, tx, input.Thread)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}

	resources := make([]SessionResource, 0, len(input.Resources))
	fileMounts, err := sessionFileMountsByResource(input.FileMounts)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if err := enforceSessionFileResourceCapacityTx(
		ctx,
		tx,
		session.WorkspaceID,
		session.ExternalID,
		sessionFileResourceCount(input.Resources),
	); err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if len(fileMounts) > 0 {
		lockedFilesystem, err := lockSessionFilestoreMutationTx(ctx, tx, session)
		if err != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
		}
		if lockedFilesystem.ID != filesystem.ID {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, ErrPreconditionFailed
		}
		filesystem = lockedFilesystem
	}
	for _, resource := range input.Resources {
		resource.SessionExternalID = session.ExternalID
		created, err := createSessionResourceSQLX(ctx, tx, resource)
		if err != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
		}
		fileMount, hasFileMount := fileMounts[created.ExternalID]
		var fileMountPointer *SessionFileMount
		if hasFileMount {
			fileMountPointer = &fileMount
		}
		if err := bindSessionFileResourceWithLockedFilesystemTx(
			ctx,
			tx,
			session,
			filesystem,
			created,
			fileMountPointer,
		); err != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
		}
		delete(fileMounts, created.ExternalID)
		resources = append(resources, created)
	}
	if len(fileMounts) != 0 {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, ErrPreconditionFailed
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
	var row sessionThreadRow
	err := namedGetContext(ctx, database, &row, createSessionThreadQuery, createSessionThreadArguments(thread))
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
		"session_uuid":            session.UUID,
		"session_external_id":     session.ExternalID,
		"organization_id":         session.OrganizationID,
		"workspace_id":            session.WorkspaceID,
		"created_by_api_key_id":   session.CreatedByAPIKeyID,
		"environment_id":          session.EnvironmentID,
		"environment_external_id": session.EnvironmentExternalID,
		"agent_id":                session.AgentID,
		"agent_external_id":       session.AgentExternalID,
		"agent_version":           session.AgentVersion,
		"agent_snapshot":          jsonArg(session.AgentSnapshot),
		"deployment_id":           session.DeploymentID,
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
		"thread_uuid":               thread.UUID,
		"thread_external_id":        thread.ExternalID,
		"organization_id":           thread.OrganizationID,
		"workspace_id":              thread.WorkspaceID,
		"session_id":                thread.SessionID,
		"session_external_id":       thread.SessionExternalID,
		"parent_thread_id":          thread.ParentThreadID,
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
		"work_uuid":               work.UUID,
		"work_external_id":        work.ExternalID,
		"organization_id":         work.OrganizationID,
		"workspace_id":            work.WorkspaceID,
		"environment_id":          work.EnvironmentID,
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
		ID:                    r.ID,
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationID:        r.OrganizationID,
		WorkspaceID:           r.WorkspaceID,
		CreatedByAPIKeyID:     r.CreatedByAPIKeyID,
		EnvironmentID:         r.EnvironmentID,
		EnvironmentExternalID: r.EnvironmentExternalID,
		AgentID:               r.AgentID,
		AgentExternalID:       r.AgentExternalID,
		AgentVersion:          r.AgentVersion,
		AgentSnapshot:         copyRaw(r.AgentSnapshot),
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
		ID:                     r.ID,
		UUID:                   r.UUID,
		ExternalID:             r.ExternalID,
		OrganizationID:         r.OrganizationID,
		WorkspaceID:            r.WorkspaceID,
		SessionID:              r.SessionID,
		SessionExternalID:      r.SessionExternalID,
		ParentThreadID:         r.ParentThreadID,
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
		ID:                r.ID,
		UUID:              r.UUID,
		ExternalID:        r.ExternalID,
		OrganizationID:    r.OrganizationID,
		WorkspaceID:       r.WorkspaceID,
		SessionID:         r.SessionID,
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
		ID:                    r.ID,
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationID:        r.OrganizationID,
		WorkspaceID:           r.WorkspaceID,
		EnvironmentID:         r.EnvironmentID,
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
