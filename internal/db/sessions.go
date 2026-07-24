package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Session struct {
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	CreatedByAPIKeyID     int64
	EnvironmentID         int64
	EnvironmentExternalID string
	AgentID               int64
	AgentExternalID       string
	AgentVersion          int
	AgentSnapshot         json.RawMessage
	DeploymentID          *string
	Title                 *string
	Metadata              json.RawMessage
	VaultIDs              json.RawMessage
	Status                string
	Usage                 json.RawMessage
	Stats                 json.RawMessage
	OutcomeEvaluations    json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ArchivedAt            *time.Time
	DeletedAt             *time.Time
}

type SessionThread struct {
	ID                     int64
	UUID                   string
	ExternalID             string
	OrganizationID         int64
	WorkspaceID            int64
	SessionID              int64
	SessionExternalID      string
	ParentThreadID         *int64
	ParentThreadExternalID *string
	AgentSnapshot          json.RawMessage
	Status                 string
	Usage                  json.RawMessage
	Stats                  json.RawMessage
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ArchivedAt             *time.Time
	DeletedAt              *time.Time
}

type SessionResource struct {
	ID                int64
	UUID              string
	ExternalID        string
	OrganizationID    int64
	WorkspaceID       int64
	SessionID         int64
	SessionExternalID string
	ResourceType      string
	Payload           json.RawMessage
	SecretPayload     json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type SessionEvent struct {
	ID                int64
	UUID              string
	ExternalID        string
	OrganizationID    int64
	WorkspaceID       int64
	SessionID         int64
	SessionExternalID string
	ThreadID          *int64
	ThreadExternalID  *string
	EventType         string
	Payload           json.RawMessage
	ProcessedAt       time.Time
	CreatedAt         time.Time
	DeletedAt         *time.Time
}

type SessionPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type SessionEventPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type SessionThreadPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type ListSessionsPageParams struct {
	WorkspaceID     int64
	Limit           int
	Cursor          *SessionPageCursor
	Order           string
	IncludeArchived bool
	AgentExternalID string
	AgentVersion    *int
	DeploymentID    string
	MemoryStoreID   string
	Statuses        []string
	CreatedAtGT     *time.Time
	CreatedAtGTE    *time.Time
	CreatedAtLT     *time.Time
	CreatedAtLTE    *time.Time
}

type ListSessionEventsPageParams struct {
	WorkspaceID       int64
	SessionExternalID string
	ThreadExternalID  string
	PrimaryOnly       bool
	Limit             int
	Cursor            *SessionEventPageCursor
	Order             string
	Types             []string
	CreatedAtGT       *time.Time
	CreatedAtGTE      *time.Time
	CreatedAtLT       *time.Time
	CreatedAtLTE      *time.Time
}

type ListSessionThreadsPageParams struct {
	WorkspaceID       int64
	SessionExternalID string
	Limit             int
	Cursor            *SessionThreadPageCursor
}

type CreateSessionInput struct {
	Session    Session
	Thread     SessionThread
	Resources  []SessionResource
	FileMounts []SessionFileMount
	Work       EnvironmentWork
}

// SessionFileMount is the already-normalized database binding for one file
// resource. Path is the full path inside the Session Filestore namespace.
type SessionFileMount struct {
	ResourceExternalID string
	FileExternalID     string
	Path               string
}

func (d *DB) CreateSession(ctx context.Context, input CreateSessionInput) (Session, SessionThread, []SessionResource, EnvironmentWork, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	defer tx.Rollback()

	session, thread, resources, work, err := insertSessionSQLXTx(ctx, tx, input)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	return session, thread, resources, work, nil
}

func (d *DB) GetSession(ctx context.Context, workspaceID int64, externalID string) (Session, error) {
	return getSessionSQLX(ctx, d.sql, getSessionQuery, sessionLookupArguments(workspaceID, externalID))
}

func (d *DB) UpdateSession(ctx context.Context, workspaceID int64, externalID string, next Session) (Session, error) {
	return scanSession(d.Pool.QueryRow(ctx, `
		update sessions
		set agent_snapshot = $3::jsonb,
			title = $4,
			metadata = $5::jsonb,
			updated_at = $6
		where workspace_id = $1
			and external_id = $2
			and deleted_at is null
			and archived_at is null
			and status = 'idle'
		returning `+sessionColumns()+`
	`, workspaceID, externalID, jsonArg(next.AgentSnapshot), next.Title, jsonArg(next.Metadata), next.UpdatedAt))
}

func (d *DB) PatchSessionMetadata(ctx context.Context, workspaceID int64, externalID string, patch json.RawMessage) (Session, error) {
	return scanSession(d.Pool.QueryRow(ctx, `
		update sessions
		set metadata = coalesce(metadata, '{}'::jsonb) || $3::jsonb,
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning `+sessionColumns()+`
	`, workspaceID, externalID, jsonArg(patch)))
}

func (d *DB) SetSessionOutcomeEvaluations(ctx context.Context, workspaceID int64, externalID string, evaluations json.RawMessage) (Session, error) {
	return scanSession(d.Pool.QueryRow(ctx, `
		update sessions
		set outcome_evaluations = $3::jsonb,
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning `+sessionColumns()+`
	`, workspaceID, externalID, jsonArg(evaluations)))
}

func (d *DB) SetSessionStatus(ctx context.Context, workspaceID int64, externalID, status string) error {
	tag, err := d.Pool.Exec(ctx, `
		update sessions
		set status = $3,
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) SetSessionThreadStatus(ctx context.Context, workspaceID int64, sessionExternalID, threadExternalID, status string) error {
	tag, err := d.Pool.Exec(ctx, `
		update session_threads
		set status = $4,
			updated_at = now()
		where workspace_id = $1 and session_external_id = $2 and external_id = $3 and deleted_at is null
	`, workspaceID, sessionExternalID, threadExternalID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CreateSessionThreadIfAbsent(ctx context.Context, thread SessionThread) (SessionThread, error) {
	inserted, err := scanSessionThread(d.Pool.QueryRow(ctx, `
		insert into session_threads (
			uuid, external_id, organization_id, workspace_id, session_id, session_external_id,
			parent_thread_id, parent_thread_external_id, agent_snapshot, status, usage, stats,
			created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11::jsonb, $12::jsonb, $13, $13)
		on conflict (workspace_id, external_id) do nothing
		returning `+sessionThreadColumns()+`
	`, thread.UUID, thread.ExternalID, thread.OrganizationID, thread.WorkspaceID, thread.SessionID,
		thread.SessionExternalID, thread.ParentThreadID, thread.ParentThreadExternalID, jsonArg(thread.AgentSnapshot),
		thread.Status, jsonArg(thread.Usage), jsonArg(thread.Stats), thread.CreatedAt))
	if err == nil {
		return inserted, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SessionThread{}, err
	}
	return d.GetSessionThread(ctx, thread.WorkspaceID, thread.SessionExternalID, thread.ExternalID)
}

func (d *DB) ArchiveSession(ctx context.Context, workspaceID int64, externalID string) (Session, error) {
	return scanSession(d.Pool.QueryRow(ctx, `
		update sessions
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_id = $1
			and external_id = $2
			and deleted_at is null
			and status not in ('running', 'rescheduling')
		returning `+sessionColumns()+`
	`, workspaceID, externalID))
}

func (d *DB) DeleteSession(ctx context.Context, workspaceID int64, externalID string) (Session, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)

	session, err := scanSession(tx.QueryRow(ctx, `
		update sessions
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where workspace_id = $1
			and external_id = $2
			and deleted_at is null
			and status not in ('running', 'rescheduling')
		returning `+sessionColumns()+`
	`, workspaceID, externalID))
	if err != nil {
		return Session{}, err
	}
	if err := retireSessionFilesystemTx(ctx, tx, session); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `
		update session_threads set deleted_at = coalesce(deleted_at, now()), updated_at = now()
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
	`, workspaceID, externalID); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `
		update session_resources set deleted_at = coalesce(deleted_at, now()), updated_at = now()
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
	`, workspaceID, externalID); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `
		update session_events set deleted_at = coalesce(deleted_at, now())
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
	`, workspaceID, externalID); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `
		update environment_work
		set state = case when state in ('stopped') then state else 'stopping' end,
			stop_requested_at = coalesce(stop_requested_at, now()),
			updated_at = now()
		where workspace_id = $1
			and environment_external_id = $2
			and data->>'id' = $3
			and deleted_at is null
			and state not in ('stopped')
	`, workspaceID, session.EnvironmentExternalID, externalID); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (d *DB) ListSessionsPage(ctx context.Context, params ListSessionsPageParams) ([]Session, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	order := "desc"
	if params.Order == "asc" {
		order = "asc"
	}
	comparison := "<"
	if order == "asc" {
		comparison = ">"
	}
	query := `
		select ` + sessionColumns() + `
		from sessions s
		where s.workspace_id = $1 and s.deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if !params.IncludeArchived {
		query += " and s.archived_at is null"
	}
	if params.AgentExternalID != "" {
		query += fmt.Sprintf(" and s.agent_external_id = $%d", nextArg)
		args = append(args, params.AgentExternalID)
		nextArg++
	}
	if params.AgentVersion != nil {
		query += fmt.Sprintf(" and s.agent_version = $%d", nextArg)
		args = append(args, *params.AgentVersion)
		nextArg++
	}
	if params.DeploymentID != "" {
		query += fmt.Sprintf(" and s.deployment_id = $%d", nextArg)
		args = append(args, params.DeploymentID)
		nextArg++
	}
	if params.MemoryStoreID != "" {
		query += fmt.Sprintf(` and exists (
			select 1 from session_resources sr
			where sr.workspace_id = s.workspace_id
				and sr.session_external_id = s.external_id
				and sr.deleted_at is null
				and sr.resource_type = 'memory_store'
				and (sr.payload->>'memory_store_id' = $%d or sr.payload->>'id' = $%d)
		)`, nextArg, nextArg)
		args = append(args, params.MemoryStoreID)
		nextArg++
	}
	if len(params.Statuses) > 0 {
		query += fmt.Sprintf(" and s.status = any($%d::text[])", nextArg)
		args = append(args, params.Statuses)
		nextArg++
	}
	if params.CreatedAtGT != nil {
		query += fmt.Sprintf(" and s.created_at > $%d", nextArg)
		args = append(args, *params.CreatedAtGT)
		nextArg++
	}
	if params.CreatedAtGTE != nil {
		query += fmt.Sprintf(" and s.created_at >= $%d", nextArg)
		args = append(args, *params.CreatedAtGTE)
		nextArg++
	}
	if params.CreatedAtLT != nil {
		query += fmt.Sprintf(" and s.created_at < $%d", nextArg)
		args = append(args, *params.CreatedAtLT)
		nextArg++
	}
	if params.CreatedAtLTE != nil {
		query += fmt.Sprintf(" and s.created_at <= $%d", nextArg)
		args = append(args, *params.CreatedAtLTE)
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (s.created_at %s $%d or (s.created_at = $%d and s.id %s $%d))", comparison, nextArg, nextArg, comparison, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by s.created_at %s, s.id %s limit $%d", order, order, nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	sessions, err := scanSessionRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(sessions) > params.Limit
	if hasMore {
		sessions = sessions[:params.Limit]
	}
	return sessions, hasMore, nil
}

func (d *DB) GetPrimarySessionThread(ctx context.Context, workspaceID int64, sessionExternalID string) (SessionThread, error) {
	return scanSessionThread(d.Pool.QueryRow(ctx, `
		select `+sessionThreadColumns()+`
		from session_threads
		where workspace_id = $1 and session_external_id = $2 and parent_thread_id is null and deleted_at is null
		order by created_at asc, id asc
		limit 1
	`, workspaceID, sessionExternalID))
}

func (d *DB) GetSessionThread(ctx context.Context, workspaceID int64, sessionExternalID, threadExternalID string) (SessionThread, error) {
	return scanSessionThread(d.Pool.QueryRow(ctx, `
		select `+sessionThreadColumns()+`
		from session_threads
		where workspace_id = $1 and session_external_id = $2 and external_id = $3 and deleted_at is null
	`, workspaceID, sessionExternalID, threadExternalID))
}

func (d *DB) ListSessionThreadsPage(ctx context.Context, params ListSessionThreadsPageParams) ([]SessionThread, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := `
		select ` + sessionThreadColumns() + `
		from session_threads
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
	`
	args := []any{params.WorkspaceID, params.SessionExternalID}
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
	threads, err := scanSessionThreadRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(threads) > params.Limit
	if hasMore {
		threads = threads[:params.Limit]
	}
	return threads, hasMore, nil
}

func (d *DB) ListSessionThreads(ctx context.Context, workspaceID int64, sessionExternalID string) ([]SessionThread, error) {
	rows, err := d.Pool.Query(ctx, `
		select `+sessionThreadColumns()+`
		from session_threads
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
		order by created_at asc, id asc
	`, workspaceID, sessionExternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionThreadRows(rows)
}

func (d *DB) ArchiveSessionThread(ctx context.Context, workspaceID int64, sessionExternalID, threadExternalID string) (SessionThread, error) {
	return scanSessionThread(d.Pool.QueryRow(ctx, `
		update session_threads
		set archived_at = coalesce(archived_at, now()),
			status = 'terminated',
			updated_at = now()
		where workspace_id = $1
			and session_external_id = $2
			and external_id = $3
			and deleted_at is null
			and status not in ('running', 'rescheduling')
		returning `+sessionThreadColumns()+`
	`, workspaceID, sessionExternalID, threadExternalID))
}

func (d *DB) CreateSessionResource(
	ctx context.Context,
	resource SessionResource,
	fileMount *SessionFileMount,
	validate func([]SessionResource) error,
) (SessionResource, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return SessionResource{}, err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(
		ctx,
		tx,
		lockSessionForResourceMutationQuery,
		sessionLookupArguments(resource.WorkspaceID, resource.SessionExternalID),
	)
	if err != nil {
		return SessionResource{}, err
	}
	if session.OrganizationID != resource.OrganizationID {
		return SessionResource{}, ErrPreconditionFailed
	}
	existing, err := listSessionResourcesSQLX(
		ctx,
		tx,
		listSessionResourcesQuery,
		sessionLookupArguments(resource.WorkspaceID, resource.SessionExternalID),
	)
	if err != nil {
		return SessionResource{}, err
	}
	if validate != nil {
		if err := validate(append(existing, resource)); err != nil {
			return SessionResource{}, err
		}
	}
	created, err := createSessionResourceSQLX(ctx, tx, resource)
	if err != nil {
		return SessionResource{}, err
	}
	if err := bindSessionResourceFileTx(ctx, tx, session, created, fileMount); err != nil {
		return SessionResource{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionResource{}, err
	}
	return created, nil
}

func (d *DB) GetSessionResource(ctx context.Context, workspaceID int64, sessionExternalID, resourceExternalID string) (SessionResource, error) {
	return scanSessionResource(d.Pool.QueryRow(ctx, `
		select `+sessionResourceColumns()+`
		from session_resources
		where workspace_id = $1 and session_external_id = $2 and external_id = $3 and deleted_at is null
	`, workspaceID, sessionExternalID, resourceExternalID))
}

func (d *DB) ListSessionResources(ctx context.Context, workspaceID int64, sessionExternalID string) ([]SessionResource, error) {
	return listSessionResourcesSQLX(
		ctx,
		d.sql,
		listSessionResourcesQuery,
		sessionLookupArguments(workspaceID, sessionExternalID),
	)
}

func (d *DB) UpdateSessionResource(ctx context.Context, workspaceID int64, sessionExternalID, resourceExternalID string, payload, secretPayload json.RawMessage) (SessionResource, error) {
	return scanSessionResource(d.Pool.QueryRow(ctx, `
		update session_resources
		set payload = $4::jsonb,
			secret_payload = $5::jsonb,
			updated_at = now()
		where workspace_id = $1 and session_external_id = $2 and external_id = $3 and deleted_at is null
		returning `+sessionResourceColumns()+`
	`, workspaceID, sessionExternalID, resourceExternalID, jsonArg(payload), jsonArg(secretPayload)))
}

func (d *DB) DeleteSessionResource(ctx context.Context, workspaceID int64, sessionExternalID, resourceExternalID string) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(
		ctx,
		tx,
		lockSessionForResourceMutationQuery,
		sessionLookupArguments(workspaceID, sessionExternalID),
	)
	if err != nil {
		return err
	}
	resource, err := getSessionResourceForMutationSQLX(
		ctx,
		tx,
		workspaceID,
		sessionExternalID,
		resourceExternalID,
	)
	if err != nil {
		return err
	}
	if err := unbindSessionFileResourceTx(ctx, tx, session, resource); err != nil {
		return err
	}
	if err := softDeleteSessionResourceSQLX(
		ctx,
		tx,
		workspaceID,
		sessionExternalID,
		resourceExternalID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) AppendSessionEvents(ctx context.Context, workspaceID int64, sessionExternalID string, events []SessionEvent) ([]SessionEvent, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(ctx, tx, lockSessionForEventsQuery, sessionLookupArguments(workspaceID, sessionExternalID))
	if err != nil {
		return nil, err
	}
	if session.ArchivedAt != nil {
		return nil, ErrInvalidState
	}
	created, err := insertSessionEventsSQLXTx(ctx, tx, session, events, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (d *DB) AppendSessionEventsIfAbsent(ctx context.Context, workspaceID int64, sessionExternalID string, events []SessionEvent) ([]SessionEvent, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(ctx, tx, lockSessionForEventsQuery, sessionLookupArguments(workspaceID, sessionExternalID))
	if err != nil {
		return nil, err
	}
	if session.ArchivedAt != nil {
		return nil, ErrInvalidState
	}
	created, err := insertSessionEventsSQLXTx(ctx, tx, session, events, true)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (d *DB) GetSessionEvent(ctx context.Context, workspaceID int64, sessionExternalID string, eventExternalID string) (SessionEvent, error) {
	eventExternalID = strings.TrimSpace(eventExternalID)
	if eventExternalID == "" {
		return SessionEvent{}, ErrNotFound
	}
	return scanSessionEvent(d.Pool.QueryRow(ctx, `
		select `+sessionEventColumns()+`
		from session_events
		where workspace_id = $1
			and session_external_id = $2
			and external_id = $3
			and deleted_at is null
	`, workspaceID, sessionExternalID, eventExternalID))
}

func (d *DB) ListSessionEventsPage(ctx context.Context, params ListSessionEventsPageParams) ([]SessionEvent, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	order := "asc"
	if params.Order == "desc" {
		order = "desc"
	}
	comparison := ">"
	if order == "desc" {
		comparison = "<"
	}
	query := `
		select ` + sessionEventColumns() + `
		from session_events
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
	`
	args := []any{params.WorkspaceID, params.SessionExternalID}
	nextArg := 3
	if params.ThreadExternalID != "" {
		query += fmt.Sprintf(" and thread_external_id = $%d", nextArg)
		args = append(args, params.ThreadExternalID)
		nextArg++
	} else if params.PrimaryOnly {
		query += ` and thread_external_id = (
			select external_id
			from session_threads
			where workspace_id = $1 and session_external_id = $2 and parent_thread_id is null and deleted_at is null
			order by created_at asc, id asc
			limit 1
		)`
	}
	if len(params.Types) > 0 {
		query += fmt.Sprintf(" and event_type = any($%d::text[])", nextArg)
		args = append(args, params.Types)
		nextArg++
	}
	if params.CreatedAtGT != nil {
		query += fmt.Sprintf(" and created_at > $%d", nextArg)
		args = append(args, *params.CreatedAtGT)
		nextArg++
	}
	if params.CreatedAtGTE != nil {
		query += fmt.Sprintf(" and created_at >= $%d", nextArg)
		args = append(args, *params.CreatedAtGTE)
		nextArg++
	}
	if params.CreatedAtLT != nil {
		query += fmt.Sprintf(" and created_at < $%d", nextArg)
		args = append(args, *params.CreatedAtLT)
		nextArg++
	}
	if params.CreatedAtLTE != nil {
		query += fmt.Sprintf(" and created_at <= $%d", nextArg)
		args = append(args, *params.CreatedAtLTE)
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at %s $%d or (created_at = $%d and id %s $%d))", comparison, nextArg, nextArg, comparison, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at %s, id %s limit $%d", order, order, nextArg)
	args = append(args, params.Limit+1)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	events, err := scanSessionEventRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(events) > params.Limit
	if hasMore {
		events = events[:params.Limit]
	}
	return events, hasMore, nil
}

func (d *DB) ChildSessionToolUseIDs(ctx context.Context, workspaceID int64, sessionExternalID string, toolUseIDs []string) (map[string]struct{}, error) {
	if len(toolUseIDs) == 0 {
		return map[string]struct{}{}, nil
	}
	rows, err := d.Pool.Query(ctx, `
		select distinct coalesce(
			e.payload->>'tool_use_id',
			e.payload->>'mcp_tool_use_id',
			e.payload->>'custom_tool_use_id',
			e.payload->>'id'
		) as tool_use_id
		from session_events e
		join session_threads t
			on t.workspace_id = e.workspace_id
			and t.session_external_id = e.session_external_id
			and t.external_id = e.thread_external_id
			and t.deleted_at is null
		where e.workspace_id = $1
			and e.session_external_id = $2
			and e.deleted_at is null
			and t.parent_thread_id is not null
			and e.event_type = any($3::text[])
			and coalesce(
				e.payload->>'tool_use_id',
				e.payload->>'mcp_tool_use_id',
				e.payload->>'custom_tool_use_id',
				e.payload->>'id'
			) = any($4::text[])
	`, workspaceID, sessionExternalID, []string{"agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use"}, toolUseIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[string]struct{})
	for rows.Next() {
		var toolUseID string
		if err := rows.Scan(&toolUseID); err != nil {
			return nil, err
		}
		toolUseID = strings.TrimSpace(toolUseID)
		if toolUseID != "" {
			found[toolUseID] = struct{}{}
		}
	}
	return found, rows.Err()
}

func sessionColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, created_by_api_key_id,
		environment_id, environment_external_id, agent_id, agent_external_id, agent_version,
		agent_snapshot, deployment_id, title, metadata, vault_ids, status, usage, stats,
		outcome_evaluations, created_at, updated_at, archived_at, deleted_at`
}

func sessionThreadColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, session_id, session_external_id,
		parent_thread_id, parent_thread_external_id, agent_snapshot, status, usage, stats,
		created_at, updated_at, archived_at, deleted_at`
}

func sessionResourceColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, session_id, session_external_id,
		resource_type, payload, secret_payload, created_at, updated_at, deleted_at`
}

func sessionEventColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, session_id, session_external_id,
		thread_id, thread_external_id, event_type, payload, processed_at, created_at, deleted_at`
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanSession(row rowScanner) (Session, error) {
	var session Session
	var agentSnapshot, metadata, vaultIDs, usage, stats, outcomes []byte
	err := row.Scan(&session.ID, &session.UUID, &session.ExternalID, &session.OrganizationID, &session.WorkspaceID,
		&session.CreatedByAPIKeyID, &session.EnvironmentID, &session.EnvironmentExternalID, &session.AgentID,
		&session.AgentExternalID, &session.AgentVersion, &agentSnapshot, &session.DeploymentID, &session.Title,
		&metadata, &vaultIDs, &session.Status, &usage, &stats, &outcomes, &session.CreatedAt, &session.UpdatedAt,
		&session.ArchivedAt, &session.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	session.AgentSnapshot = copyRaw(agentSnapshot)
	session.Metadata = copyRaw(metadata)
	session.VaultIDs = copyRaw(vaultIDs)
	session.Usage = copyRaw(usage)
	session.Stats = copyRaw(stats)
	session.OutcomeEvaluations = copyRaw(outcomes)
	return session, nil
}

func scanSessionRows(rows rowsScanner) ([]Session, error) {
	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func scanSessionThread(row rowScanner) (SessionThread, error) {
	var thread SessionThread
	var agentSnapshot, usage, stats []byte
	err := row.Scan(&thread.ID, &thread.UUID, &thread.ExternalID, &thread.OrganizationID, &thread.WorkspaceID,
		&thread.SessionID, &thread.SessionExternalID, &thread.ParentThreadID, &thread.ParentThreadExternalID,
		&agentSnapshot, &thread.Status, &usage, &stats, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.ArchivedAt, &thread.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionThread{}, ErrNotFound
	}
	if err != nil {
		return SessionThread{}, err
	}
	thread.AgentSnapshot = copyRaw(agentSnapshot)
	thread.Usage = copyRaw(usage)
	thread.Stats = copyRaw(stats)
	return thread, nil
}

func scanSessionThreadRows(rows rowsScanner) ([]SessionThread, error) {
	var threads []SessionThread
	for rows.Next() {
		thread, err := scanSessionThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func scanSessionResource(row rowScanner) (SessionResource, error) {
	var resource SessionResource
	var payload, secretPayload []byte
	err := row.Scan(&resource.ID, &resource.UUID, &resource.ExternalID, &resource.OrganizationID, &resource.WorkspaceID,
		&resource.SessionID, &resource.SessionExternalID, &resource.ResourceType, &payload, &secretPayload,
		&resource.CreatedAt, &resource.UpdatedAt, &resource.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionResource{}, ErrNotFound
	}
	if err != nil {
		return SessionResource{}, err
	}
	resource.Payload = copyRaw(payload)
	resource.SecretPayload = copyRaw(secretPayload)
	return resource, nil
}

func scanSessionEvent(row rowScanner) (SessionEvent, error) {
	var event SessionEvent
	var payload []byte
	err := row.Scan(&event.ID, &event.UUID, &event.ExternalID, &event.OrganizationID, &event.WorkspaceID,
		&event.SessionID, &event.SessionExternalID, &event.ThreadID, &event.ThreadExternalID,
		&event.EventType, &payload, &event.ProcessedAt, &event.CreatedAt, &event.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionEvent{}, ErrNotFound
	}
	if err != nil {
		return SessionEvent{}, err
	}
	event.Payload = copyRaw(payload)
	return event, nil
}

func scanSessionEventRows(rows rowsScanner) ([]SessionEvent, error) {
	var events []SessionEvent
	for rows.Next() {
		event, err := scanSessionEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
