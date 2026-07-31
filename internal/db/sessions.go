package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

type Session struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CreatedByAPIKeyUUID   string
	EnvironmentUUID       string
	EnvironmentExternalID string
	AgentUUID             string
	AgentExternalID       string
	AgentVersion          int
	AgentSnapshot         json.RawMessage
	DeploymentUUID        *string
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
	UUID                   string
	ExternalID             string
	OrganizationUUID       string
	WorkspaceUUID          string
	SessionUUID            string
	SessionExternalID      string
	ParentThreadUUID       *string
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

const (
	// SessionResourceTypeFile identifies a Files API object attached to a
	// Session. It is distinct from FilestoreEntryKindFile, which classifies
	// filesystem nodes.
	SessionResourceTypeFile = sessioncontract.FileResourceType
	// MaxSessionFileResources is the write-time limit for active File resources
	// attached to one Session.
	MaxSessionFileResources = sessioncontract.MaxFileResources
)

type SessionResource struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionUUID       string
	SessionExternalID string
	ResourceType      string
	Payload           json.RawMessage
	SecretPayload     json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type SessionEvent struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionUUID       string
	SessionExternalID string
	ThreadUUID        *string
	ThreadExternalID  *string
	EventType         string
	Payload           json.RawMessage
	ProcessedAt       time.Time
	CreatedAt         time.Time
	DeletedAt         *time.Time
}

type SessionPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type SessionEventPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type SessionThreadPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListSessionsPageParams struct {
	WorkspaceUUID   string
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
	WorkspaceUUID     string
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
	WorkspaceUUID     string
	SessionExternalID string
	Limit             int
	Cursor            *SessionThreadPageCursor
}

type CreateSessionInput struct {
	Session   Session
	Thread    SessionThread
	Resources []CreateSessionResourceInput
	Work      EnvironmentWork
}

// CreateSessionResourceInput contains the normalized resource row and its
// optional Filestore binding. CreateSessionResource applies all write-time
// invariants while holding the owning Session row lock.
type CreateSessionResourceInput struct {
	Resource  SessionResource
	FileMount *SessionFileMount
}

// SessionFileMount is the already-normalized database binding for one file
// resource. Path is the full path inside the Session Filestore namespace.
type SessionFileMount struct {
	ResourceExternalID string
	FileExternalID     string
	Path               string
}

// SessionFileResourceLimitError reports that an atomic resource mutation would
// exceed the maximum number of active File resources for one Session.
type SessionFileResourceLimitError struct {
	Limit int
}

func (e *SessionFileResourceLimitError) Error() string {
	return fmt.Sprintf("at most %d managed-agent file resources are allowed", e.Limit)
}

// SessionFileMountConflictError reports a conflict between two active
// Session-managed File resource paths. Conflicts with ordinary Filestore
// entries continue to use ErrFilestorePathExists.
type SessionFileMountConflictError struct {
	Path            string
	ConflictingPath string
}

func (e *SessionFileMountConflictError) Error() string {
	return fmt.Sprintf(
		"file resource mount path %q conflicts with active resource path %q",
		e.Path,
		e.ConflictingPath,
	)
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

func (d *DB) GetSession(ctx context.Context, workspaceUUID string, externalID string) (Session, error) {
	return getSessionSQLX(ctx, d.sql, getSessionQuery, sessionLookupArguments(workspaceUUID, externalID))
}

func (d *DB) UpdateSession(ctx context.Context, workspaceUUID string, externalID string, next Session) (Session, error) {
	return getSessionSQLX(ctx, d.sql, updateSessionQuery, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": externalID,
		"agent_snapshot":      jsonArg(next.AgentSnapshot),
		"title":               next.Title,
		"metadata":            jsonArg(next.Metadata),
		"updated_at":          next.UpdatedAt,
	})
}

func (d *DB) PatchSessionMetadata(ctx context.Context, workspaceUUID string, externalID string, patch json.RawMessage) (Session, error) {
	return getSessionSQLX(ctx, d.sql, patchSessionMetadataQuery, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": externalID,
		"metadata_patch":      jsonArg(patch),
	})
}

func (d *DB) SetSessionOutcomeEvaluations(ctx context.Context, workspaceUUID string, externalID string, evaluations json.RawMessage) (Session, error) {
	return getSessionSQLX(ctx, d.sql, setSessionOutcomeEvaluationsQuery, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": externalID,
		"outcome_evaluations": jsonArg(evaluations),
	})
}

func (d *DB) SetSessionStatus(ctx context.Context, workspaceUUID string, externalID, status string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, setSessionStatusQuery, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": externalID,
		"status":              status,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) SetSessionThreadStatus(ctx context.Context, workspaceUUID string, sessionExternalID, threadExternalID, status string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, setSessionThreadStatusQuery, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": sessionExternalID,
		"thread_external_id":  threadExternalID,
		"status":              status,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CreateSessionThreadIfAbsent(ctx context.Context, thread SessionThread) (SessionThread, error) {
	inserted, err := insertSessionThreadWithQuerySQLX(
		ctx,
		d.sql,
		createSessionThreadIfAbsentQuery,
		createSessionThreadArguments(thread),
	)
	if err == nil {
		return inserted, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SessionThread{}, err
	}
	return d.GetSessionThread(ctx, thread.WorkspaceUUID, thread.SessionExternalID, thread.ExternalID)
}

func (d *DB) ArchiveSession(ctx context.Context, workspaceUUID string, externalID string) (Session, error) {
	return getSessionSQLX(ctx, d.sql, archiveSessionQuery, sessionLookupArguments(workspaceUUID, externalID))
}

func (d *DB) DeleteSession(ctx context.Context, workspaceUUID string, externalID string) (Session, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()

	arguments := sessionLookupArguments(workspaceUUID, externalID)
	session, err := getSessionSQLX(ctx, tx, deleteSessionQuery, arguments)
	if err != nil {
		return Session{}, err
	}
	if err := retireSessionFilesystemTx(ctx, tx, session); err != nil {
		return Session{}, err
	}
	if _, err := namedExecContext(ctx, tx, deleteSessionThreadsQuery, arguments); err != nil {
		return Session{}, err
	}
	if _, err := namedExecContext(ctx, tx, deleteSessionResourcesQuery, arguments); err != nil {
		return Session{}, err
	}
	if _, err := namedExecContext(ctx, tx, deleteSessionEventsQuery, arguments); err != nil {
		return Session{}, err
	}
	arguments["environment_external_id"] = session.EnvironmentExternalID
	if _, err := namedExecContext(ctx, tx, stopDeletedSessionEnvironmentWorkQuery, arguments); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
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
		select ` + sessionSQLXColumns + `
		from sessions s
		where s.workspace_uuid = :workspace_uuid and s.deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": dbUUID(params.WorkspaceUUID),
		"limit":          params.Limit + 1,
	}
	if !params.IncludeArchived {
		query += " and s.archived_at is null"
	}
	if params.AgentExternalID != "" {
		query += " and s.agent_external_id = :agent_external_id"
		arguments["agent_external_id"] = params.AgentExternalID
	}
	if params.AgentVersion != nil {
		query += " and s.agent_version = :agent_version"
		arguments["agent_version"] = *params.AgentVersion
	}
	if params.DeploymentID != "" {
		query += " and s.deployment_external_id = :deployment_id"
		arguments["deployment_id"] = params.DeploymentID
	}
	if params.MemoryStoreID != "" {
		query += ` and exists (
			select 1 from session_resources sr
			where sr.workspace_uuid = s.workspace_uuid
				and sr.session_external_id = s.external_id
				and sr.deleted_at is null
				and sr.resource_type = 'memory_store'
				and (
					sr.payload->>'memory_store_id' = :memory_store_id
					or sr.payload->>'id' = :memory_store_id
				)
		)`
		arguments["memory_store_id"] = params.MemoryStoreID
	}
	if len(params.Statuses) > 0 {
		query += " and s.status = any(CAST(:statuses AS text[]))"
		arguments["statuses"] = params.Statuses
	}
	if params.CreatedAtGT != nil {
		query += " and s.created_at > :created_at_gt"
		arguments["created_at_gt"] = *params.CreatedAtGT
	}
	if params.CreatedAtGTE != nil {
		query += " and s.created_at >= :created_at_gte"
		arguments["created_at_gte"] = *params.CreatedAtGTE
	}
	if params.CreatedAtLT != nil {
		query += " and s.created_at < :created_at_lt"
		arguments["created_at_lt"] = *params.CreatedAtLT
	}
	if params.CreatedAtLTE != nil {
		query += " and s.created_at <= :created_at_lte"
		arguments["created_at_lte"] = *params.CreatedAtLTE
	}
	if params.Cursor != nil {
		query += " and (s.created_at " + comparison + ` :cursor_created_at
			or (s.created_at = :cursor_created_at and s.uuid ` + comparison + ` :cursor_uuid))`
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = params.Cursor.UUID
	}
	query += " order by s.created_at " + order + ", s.uuid " + order + " limit :limit"

	sessions, err := listSessionsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(sessions) > params.Limit
	if hasMore {
		sessions = sessions[:params.Limit]
	}
	return sessions, hasMore, nil
}

func (d *DB) GetPrimarySessionThread(ctx context.Context, workspaceUUID string, sessionExternalID string) (SessionThread, error) {
	return getSessionThreadSQLX(ctx, d.sql, primarySessionThreadQuery, sessionLookupArguments(workspaceUUID, sessionExternalID))
}

func (d *DB) GetSessionThread(ctx context.Context, workspaceUUID string, sessionExternalID, threadExternalID string) (SessionThread, error) {
	arguments := sessionLookupArguments(workspaceUUID, sessionExternalID)
	arguments["thread_external_id"] = threadExternalID
	return getSessionThreadSQLX(ctx, d.sql, sessionThreadByExternalIDQuery, arguments)
}

func (d *DB) ListSessionThreadsPage(ctx context.Context, params ListSessionThreadsPageParams) ([]SessionThread, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := `
		select ` + sessionThreadSQLXColumns + `
		from session_threads
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and deleted_at is null
	`
	arguments := sessionLookupArguments(params.WorkspaceUUID, params.SessionExternalID)
	arguments["limit"] = params.Limit + 1
	if params.Cursor != nil {
		query += ` and (created_at < :cursor_created_at
			or (created_at = :cursor_created_at and uuid < :cursor_uuid))`
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = params.Cursor.UUID
	}
	query += " order by created_at desc, uuid desc limit :limit"
	threads, err := listSessionThreadsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(threads) > params.Limit
	if hasMore {
		threads = threads[:params.Limit]
	}
	return threads, hasMore, nil
}

func (d *DB) ListSessionThreads(ctx context.Context, workspaceUUID string, sessionExternalID string) ([]SessionThread, error) {
	return listSessionThreadsSQLX(ctx, d.sql, `
		select `+sessionThreadSQLXColumns+`
		from session_threads
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and deleted_at is null
		order by created_at asc, uuid asc
	`, sessionLookupArguments(workspaceUUID, sessionExternalID))
}

func (d *DB) ArchiveSessionThread(ctx context.Context, workspaceUUID string, sessionExternalID, threadExternalID string) (SessionThread, error) {
	arguments := sessionLookupArguments(workspaceUUID, sessionExternalID)
	arguments["thread_external_id"] = threadExternalID
	return getSessionThreadSQLX(ctx, d.sql, `
		update session_threads
		set archived_at = coalesce(archived_at, now()),
			status = 'terminated',
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :thread_external_id
			and deleted_at is null
			and status not in ('running', 'rescheduling')
		returning `+sessionThreadSQLXColumns+`
	`, arguments)
}

func (d *DB) CreateSessionResource(
	ctx context.Context,
	input CreateSessionResourceInput,
) (SessionResource, error) {
	resource := input.Resource
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return SessionResource{}, err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(
		ctx,
		tx,
		lockSessionForResourceMutationQuery,
		sessionLookupArguments(resource.WorkspaceUUID, resource.SessionExternalID),
	)
	if err != nil {
		return SessionResource{}, err
	}
	if session.ArchivedAt != nil {
		return SessionResource{}, ErrInvalidState
	}
	if session.OrganizationUUID != resource.OrganizationUUID {
		return SessionResource{}, ErrPreconditionFailed
	}
	if resource.ResourceType == SessionResourceTypeFile {
		if err := enforceSessionFileResourceCapacityTx(
			ctx,
			tx,
			resource.WorkspaceUUID,
			resource.SessionExternalID,
			1,
		); err != nil {
			return SessionResource{}, err
		}
	}
	created, err := createSessionResourceSQLX(ctx, tx, resource)
	if err != nil {
		return SessionResource{}, err
	}
	if created.ResourceType != SessionResourceTypeFile {
		if input.FileMount != nil {
			return SessionResource{}, ErrPreconditionFailed
		}
	} else {
		filesystem, err := lockSessionFilestoreMutationTx(ctx, tx, session)
		if err != nil {
			return SessionResource{}, err
		}
		if _, err := bindSessionFileResourceWithLockedFilesystemTx(
			ctx,
			tx,
			session,
			filesystem,
			created,
			input.FileMount,
		); err != nil {
			return SessionResource{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SessionResource{}, err
	}
	return created, nil
}

func (d *DB) GetSessionResource(ctx context.Context, workspaceUUID string, sessionExternalID, resourceExternalID string) (SessionResource, error) {
	arguments := sessionLookupArguments(workspaceUUID, sessionExternalID)
	arguments["resource_external_id"] = resourceExternalID
	return getSessionResourceSQLX(ctx, d.sql, getSessionResourceQuery, arguments)
}

func (d *DB) ListSessionResources(ctx context.Context, workspaceUUID string, sessionExternalID string) ([]SessionResource, error) {
	return listSessionResourcesSQLX(
		ctx,
		d.sql,
		listSessionResourcesQuery,
		sessionLookupArguments(workspaceUUID, sessionExternalID),
	)
}

func (d *DB) UpdateSessionResource(ctx context.Context, workspaceUUID string, sessionExternalID, resourceExternalID string, payload, secretPayload json.RawMessage) (SessionResource, error) {
	arguments := sessionLookupArguments(workspaceUUID, sessionExternalID)
	arguments["resource_external_id"] = resourceExternalID
	arguments["payload"] = jsonArg(payload)
	arguments["secret_payload"] = jsonArg(secretPayload)
	return getSessionResourceSQLX(ctx, d.sql, updateSessionResourceQuery, arguments)
}

func (d *DB) DeleteSessionResource(ctx context.Context, workspaceUUID string, sessionExternalID, resourceExternalID string) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(
		ctx,
		tx,
		lockSessionForResourceMutationQuery,
		sessionLookupArguments(workspaceUUID, sessionExternalID),
	)
	if err != nil {
		return err
	}
	if session.ArchivedAt != nil {
		return ErrInvalidState
	}
	resource, err := getSessionResourceForMutationSQLX(
		ctx,
		tx,
		workspaceUUID,
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
		workspaceUUID,
		sessionExternalID,
		resourceExternalID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) AppendSessionEvents(ctx context.Context, workspaceUUID string, sessionExternalID string, events []SessionEvent) ([]SessionEvent, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(ctx, tx, lockSessionForEventsQuery, sessionLookupArguments(workspaceUUID, sessionExternalID))
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

func (d *DB) AppendSessionEventsIfAbsent(ctx context.Context, workspaceUUID string, sessionExternalID string, events []SessionEvent) ([]SessionEvent, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(ctx, tx, lockSessionForEventsQuery, sessionLookupArguments(workspaceUUID, sessionExternalID))
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

func (d *DB) GetSessionEvent(ctx context.Context, workspaceUUID string, sessionExternalID string, eventExternalID string) (SessionEvent, error) {
	eventExternalID = strings.TrimSpace(eventExternalID)
	if eventExternalID == "" {
		return SessionEvent{}, ErrNotFound
	}
	arguments := sessionLookupArguments(workspaceUUID, sessionExternalID)
	arguments["event_external_id"] = eventExternalID
	return getSessionEventSQLX(ctx, d.sql, `
		select `+sessionEventSQLXColumns+`
		from session_events
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :event_external_id
			and deleted_at is null
	`, arguments)
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
		select ` + sessionEventSQLXColumns + `
		from session_events
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and deleted_at is null
	`
	arguments := sessionLookupArguments(params.WorkspaceUUID, params.SessionExternalID)
	arguments["limit"] = params.Limit + 1
	if params.ThreadExternalID != "" {
		query += " and thread_external_id = :thread_external_id"
		arguments["thread_external_id"] = params.ThreadExternalID
	} else if params.PrimaryOnly {
		query += ` and thread_external_id = (
			select external_id
			from session_threads
			where workspace_uuid = :workspace_uuid
				and session_external_id = :session_external_id
				and parent_thread_uuid is null
				and deleted_at is null
			order by created_at asc, uuid asc
			limit 1
		)`
	}
	if len(params.Types) > 0 {
		query += " and event_type = any(CAST(:event_types AS text[]))"
		arguments["event_types"] = params.Types
	}
	if params.CreatedAtGT != nil {
		query += " and created_at > :created_at_gt"
		arguments["created_at_gt"] = *params.CreatedAtGT
	}
	if params.CreatedAtGTE != nil {
		query += " and created_at >= :created_at_gte"
		arguments["created_at_gte"] = *params.CreatedAtGTE
	}
	if params.CreatedAtLT != nil {
		query += " and created_at < :created_at_lt"
		arguments["created_at_lt"] = *params.CreatedAtLT
	}
	if params.CreatedAtLTE != nil {
		query += " and created_at <= :created_at_lte"
		arguments["created_at_lte"] = *params.CreatedAtLTE
	}
	if params.Cursor != nil {
		query += " and (created_at " + comparison + ` :cursor_created_at
			or (created_at = :cursor_created_at and uuid ` + comparison + ` :cursor_uuid))`
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = params.Cursor.UUID
	}
	query += " order by created_at " + order + ", uuid " + order + " limit :limit"
	events, err := listSessionEventsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(events) > params.Limit
	if hasMore {
		events = events[:params.Limit]
	}
	return events, hasMore, nil
}

func (d *DB) ChildSessionToolUseIDs(ctx context.Context, workspaceUUID string, sessionExternalID string, toolUseIDs []string) (map[string]struct{}, error) {
	if len(toolUseIDs) == 0 {
		return map[string]struct{}{}, nil
	}
	var toolUseIDRows []string
	err := namedSelectContext(ctx, d.sql, &toolUseIDRows, `
		select distinct coalesce(
			e.payload->>'tool_use_id',
			e.payload->>'mcp_tool_use_id',
			e.payload->>'custom_tool_use_id',
			e.payload->>'id'
		) as tool_use_id
		from session_events e
		join session_threads t
			on t.workspace_uuid = e.workspace_uuid
			and t.session_uuid = e.session_uuid
			and t.uuid = e.thread_uuid
			and t.deleted_at is null
		where e.workspace_uuid = :workspace_uuid
			and e.session_external_id = :session_external_id
			and e.deleted_at is null
			and t.parent_thread_uuid is not null
			and e.event_type = any(CAST(:event_types AS text[]))
			and coalesce(
				e.payload->>'tool_use_id',
				e.payload->>'mcp_tool_use_id',
				e.payload->>'custom_tool_use_id',
				e.payload->>'id'
			) = any(CAST(:tool_use_ids AS text[]))
	`, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": sessionExternalID,
		"event_types":         []string{"agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use"},
		"tool_use_ids":        toolUseIDs,
	})
	if err != nil {
		return nil, err
	}
	found := make(map[string]struct{})
	for _, toolUseID := range toolUseIDRows {
		toolUseID = strings.TrimSpace(toolUseID)
		if toolUseID != "" {
			found[toolUseID] = struct{}{}
		}
	}
	return found, nil
}
