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
	"github.com/superduck-ai/yourbatis"
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
	// MaxSessionOutputFileResources caps the output files ListSessionResources
	// returns. Output files bypass the write-time limit, so without this cap the
	// resources array would grow unbounded. files.list(scope_id) stays complete.
	MaxSessionOutputFileResources = sessioncontract.MaxFileResources
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
	// Path 是 Filestore namespace 中的公开路径；FileExternalID 是 Owned File 的
	// `file_...` ID，只在读取路径同批 JOIN 出 File 行时才有值。
	Path           string
	FileExternalID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
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
	Session       Session
	Thread        SessionThread
	Resources     []CreateSessionResourceInput
	InitialEvents []SessionEvent
	Work          EnvironmentWork
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
	var session Session
	var thread SessionThread
	var resources []SessionResource
	var work EnvironmentWork
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		var txErr error
		session, thread, resources, work, txErr = insertSessionTx(ctx, executor, input)
		return txErr
	})
	return session, thread, resources, work, err
}

func (d *DB) GetSession(ctx context.Context, workspaceUUID string, externalID string) (Session, bool, error) {
	mapper := NewSessionMapper(d.mapperDB)
	row, found, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil || !found {
		return Session{}, found, err
	}
	return row.session(), true, nil
}

func (d *DB) GetSessionByUUID(ctx context.Context, workspaceUUID string, sessionUUID string) (Session, bool, error) {
	mapper := NewSessionMapper(d.mapperDB)
	row, found, err := mapper.FindByUUID(ctx, workspaceUUID, sessionUUID)
	if err != nil || !found {
		return Session{}, found, err
	}
	return row.session(), true, nil
}

func (d *DB) UpdateSession(ctx context.Context, workspaceUUID string, externalID string, next Session) (Session, error) {
	mapper := NewSessionMapper(d.mapperDB)
	row, err := mapper.UpdateByExternalID(ctx, sessionUpdateParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    externalID,
		AgentSnapshot: agentJSONArg(next.AgentSnapshot),
		Title:         next.Title,
		Metadata:      agentJSONArg(next.Metadata),
		UpdatedAt:     next.UpdatedAt,
	})
	return row.session(), mapNoRows(err)
}

func (d *DB) PatchSessionMetadata(ctx context.Context, workspaceUUID string, externalID string, patch json.RawMessage) (Session, error) {
	mapper := NewSessionMapper(d.mapperDB)
	row, err := mapper.PatchMetadata(ctx, workspaceUUID, externalID, agentJSONArg(patch))
	return row.session(), mapNoRows(err)
}

func (d *DB) SetSessionOutcomeEvaluations(ctx context.Context, workspaceUUID string, externalID string, evaluations json.RawMessage) (Session, error) {
	mapper := NewSessionMapper(d.mapperDB)
	row, err := mapper.SetOutcomeEvaluations(ctx, workspaceUUID, externalID, agentJSONArg(evaluations))
	return row.session(), mapNoRows(err)
}

func (d *DB) SetSessionStatus(ctx context.Context, workspaceUUID string, externalID, status string) error {
	mapper := NewSessionMapper(d.mapperDB)
	rowsAffected, err := mapper.SetStatus(ctx, workspaceUUID, externalID, status)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) SetSessionThreadStatus(ctx context.Context, workspaceUUID string, sessionExternalID, threadExternalID, status string) error {
	mapper := NewSessionThreadMapper(d.mapperDB)
	rowsAffected, err := mapper.SetStatus(ctx, workspaceUUID, sessionExternalID, threadExternalID, status)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CreateSessionThreadIfAbsent(ctx context.Context, thread SessionThread) (SessionThread, error) {
	mapper := NewSessionThreadMapper(d.mapperDB)
	row, found, err := mapper.InsertIfAbsent(ctx, sessionThreadWriteParameters(thread))
	if err != nil {
		return SessionThread{}, err
	}
	if found {
		return row.thread(), nil
	}
	return d.GetSessionThread(ctx, thread.WorkspaceUUID, thread.SessionExternalID, thread.ExternalID)
}

func (d *DB) ArchiveSession(ctx context.Context, workspaceUUID string, externalID string) (Session, error) {
	mapper := NewSessionMapper(d.mapperDB)
	row, err := mapper.Archive(ctx, workspaceUUID, externalID)
	return row.session(), mapNoRows(err)
}

func (d *DB) DeleteSession(ctx context.Context, workspaceUUID string, externalID string) (Session, error) {
	var session Session
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		sessionMapper := NewSessionMapper(executor)
		threadMapper := NewSessionThreadMapper(executor)
		resourceMapper := NewSessionResourceMapper(executor)
		eventMapper := NewSessionEventMapper(executor)
		workMapper := NewEnvironmentWorkMapper(executor)

		row, txErr := sessionMapper.SoftDelete(ctx, workspaceUUID, externalID)
		if txErr != nil {
			return mapNoRows(txErr)
		}
		session = row.session()
		if txErr = retireSessionFilesystemTx(ctx, executor, session); txErr != nil {
			return txErr
		}
		if _, txErr = threadMapper.SoftDeleteBySession(ctx, workspaceUUID, externalID); txErr != nil {
			return txErr
		}
		if _, txErr = resourceMapper.SoftDeleteBySession(ctx, workspaceUUID, externalID); txErr != nil {
			return txErr
		}
		if _, txErr = eventMapper.SoftDeleteBySession(ctx, workspaceUUID, externalID); txErr != nil {
			return txErr
		}
		_, txErr = workMapper.StopForDeletedSession(ctx, workspaceUUID, session.EnvironmentExternalID, session.UUID)
		return txErr
	})
	return session, err
}

func (d *DB) ListSessionsPage(ctx context.Context, params ListSessionsPageParams) ([]Session, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewSessionMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, sessionPageParameters(params))
	if err != nil {
		return nil, false, err
	}
	sessions := sessionsFromRows(rows)
	hasMore := len(sessions) > params.Limit
	if hasMore {
		sessions = sessions[:params.Limit]
	}
	return sessions, hasMore, nil
}

func (d *DB) GetPrimarySessionThread(ctx context.Context, workspaceUUID string, sessionExternalID string) (SessionThread, bool, error) {
	mapper := NewSessionThreadMapper(d.mapperDB)
	row, err := mapper.FindPrimary(ctx, workspaceUUID, sessionExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionThread{}, false, nil
	}
	if err != nil {
		return SessionThread{}, false, err
	}
	return row.thread(), true, nil
}

func (d *DB) GetSessionThread(ctx context.Context, workspaceUUID string, sessionExternalID, threadExternalID string) (SessionThread, error) {
	mapper := NewSessionThreadMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, sessionExternalID, threadExternalID)
	return row.thread(), mapNoRows(err)
}

func (d *DB) ListSessionThreadsPage(ctx context.Context, params ListSessionThreadsPageParams) ([]SessionThread, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewSessionThreadMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, sessionThreadPageMapperParams{
		WorkspaceUUID:     params.WorkspaceUUID,
		SessionExternalID: params.SessionExternalID,
		FetchLimit:        params.Limit + 1,
		Cursor:            params.Cursor,
	})
	if err != nil {
		return nil, false, err
	}
	threads := sessionThreadsFromRows(rows)
	hasMore := len(threads) > params.Limit
	if hasMore {
		threads = threads[:params.Limit]
	}
	return threads, hasMore, nil
}

func (d *DB) ListSessionThreads(ctx context.Context, workspaceUUID string, sessionExternalID string) ([]SessionThread, error) {
	mapper := NewSessionThreadMapper(d.mapperDB)
	rows, err := mapper.List(ctx, workspaceUUID, sessionExternalID)
	return sessionThreadsFromRows(rows), err
}

func (d *DB) ArchiveSessionThread(ctx context.Context, workspaceUUID string, sessionExternalID, threadExternalID string) (SessionThread, error) {
	mapper := NewSessionThreadMapper(d.mapperDB)
	row, err := mapper.Archive(ctx, workspaceUUID, sessionExternalID, threadExternalID)
	return row.thread(), mapNoRows(err)
}

func (d *DB) CreateSessionResource(
	ctx context.Context,
	input CreateSessionResourceInput,
) (SessionResource, error) {
	resource := input.Resource
	var created SessionResource
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		sessionMapper := NewSessionMapper(executor)
		row, txErr := sessionMapper.LockForMutation(ctx, resource.WorkspaceUUID, resource.SessionExternalID)
		if txErr != nil {
			return mapNoRows(txErr)
		}
		session := row.session()
		if session.ArchivedAt != nil {
			return ErrInvalidState
		}
		if session.OrganizationUUID != resource.OrganizationUUID {
			return ErrPreconditionFailed
		}
		if resource.ResourceType == SessionResourceTypeFile {
			if txErr = enforceSessionFileResourceCapacityTx(ctx, executor, resource.WorkspaceUUID, resource.SessionExternalID, 1); txErr != nil {
				return txErr
			}
		}
		created, txErr = createSessionResource(ctx, executor, resource)
		if txErr != nil {
			return txErr
		}
		if created.ResourceType != SessionResourceTypeFile {
			if input.FileMount != nil {
				return ErrPreconditionFailed
			}
			return nil
		}
		filesystem, txErr := lockSessionFilestoreMutationTx(ctx, executor, session)
		if txErr != nil {
			return txErr
		}
		_, txErr = bindSessionFileResourceWithLockedFilesystemTx(ctx, executor, session, filesystem, created, input.FileMount)
		return txErr
	})
	return created, err
}

func (d *DB) GetSessionResource(ctx context.Context, workspaceUUID string, sessionExternalID, resourceExternalID string) (SessionResource, error) {
	mapper := NewSessionResourceMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, sessionExternalID, resourceExternalID)
	return row.resource(), mapNoRows(err)
}

func (d *DB) ListSessionResources(ctx context.Context, workspaceUUID string, sessionExternalID string) ([]SessionResource, error) {
	mapper := NewSessionResourceMapper(d.mapperDB)
	rows, err := mapper.List(ctx, workspaceUUID, sessionExternalID, MaxSessionOutputFileResources)
	return sessionResourcesFromRows(rows), err
}

func (d *DB) UpdateSessionResource(ctx context.Context, workspaceUUID string, sessionExternalID, resourceExternalID string, payload, secretPayload json.RawMessage) (SessionResource, error) {
	mapper := NewSessionResourceMapper(d.mapperDB)
	row, err := mapper.Update(ctx, sessionResourceUpdateParams{
		WorkspaceUUID:      workspaceUUID,
		SessionExternalID:  sessionExternalID,
		ResourceExternalID: resourceExternalID,
		Payload:            agentJSONArg(payload),
		SecretPayload:      agentJSONArg(secretPayload),
	})
	return row.resource(), mapNoRows(err)
}

func (d *DB) DeleteSessionResource(ctx context.Context, workspaceUUID string, sessionExternalID, resourceExternalID string) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		sessionMapper := NewSessionMapper(executor)
		row, err := sessionMapper.LockForMutation(ctx, workspaceUUID, sessionExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		session := row.session()
		if session.ArchivedAt != nil {
			return ErrInvalidState
		}
		resource, err := getSessionResourceForMutation(ctx, executor, workspaceUUID, sessionExternalID, resourceExternalID)
		if err != nil {
			return err
		}
		referenced, err := NewSessionEventMapper(executor).HasFileReferenceForResource(
			ctx,
			workspaceUUID,
			sessionExternalID,
			resourceExternalID,
		)
		if err != nil {
			return err
		}
		if referenced {
			return ErrFileInUse
		}
		if err = unbindSessionFileResourceTx(ctx, executor, session, resource); err != nil {
			return err
		}
		return softDeleteSessionResource(ctx, executor, workspaceUUID, sessionExternalID, resourceExternalID)
	})
}

func (d *DB) AppendSessionEvents(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
	events []SessionEvent,
	outcomeEvaluations json.RawMessage,
) ([]SessionEvent, error) {
	var created []SessionEvent
	err := d.WithSessionEventWriteTx(ctx, workspaceUUID, sessionExternalID, func(tx SessionEventWriteTx, session Session) error {
		var txErr error
		created, txErr = tx.AppendEvents(ctx, session, events, outcomeEvaluations)
		return txErr
	})
	return created, err
}

func (d *DB) AppendSessionEventsIfAbsent(ctx context.Context, workspaceUUID string, sessionExternalID string, events []SessionEvent) ([]SessionEvent, error) {
	var created []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		sessionMapper := NewSessionMapper(executor)
		row, txErr := sessionMapper.LockForMutation(ctx, workspaceUUID, sessionExternalID)
		if txErr != nil {
			return mapNoRows(txErr)
		}
		session := row.session()
		if session.ArchivedAt != nil {
			return ErrInvalidState
		}
		created, txErr = insertSessionEventsTx(ctx, executor, session, events, true)
		return txErr
	})
	return created, err
}

func (d *DB) GetSessionEvent(ctx context.Context, workspaceUUID string, sessionExternalID string, eventExternalID string) (SessionEvent, error) {
	eventExternalID = strings.TrimSpace(eventExternalID)
	if eventExternalID == "" {
		return SessionEvent{}, ErrNotFound
	}
	mapper := NewSessionEventMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, sessionExternalID, eventExternalID)
	return row.event(), mapNoRows(err)
}

func (d *DB) GetSessionToolPermissionRequest(ctx context.Context, workspaceUUID string, sessionExternalID string, toolUseID string) (SessionEvent, error) {
	toolUseID = strings.TrimSpace(toolUseID)
	if toolUseID == "" {
		return SessionEvent{}, ErrNotFound
	}
	mapper := NewSessionEventMapper(d.mapperDB)
	row, err := mapper.FindLatestToolPermissionRequest(ctx, workspaceUUID, sessionExternalID, toolUseID)
	return row.event(), mapNoRows(err)
}

func (d *DB) ListSessionEventsPage(ctx context.Context, params ListSessionEventsPageParams) ([]SessionEvent, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewSessionEventMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, sessionEventPageParameters(params))
	if err != nil {
		return nil, false, err
	}
	events := sessionEventsFromRows(rows)
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
	mapper := NewSessionEventMapper(d.mapperDB)
	toolUseIDRows, err := mapper.ChildSessionToolUseIDs(
		ctx,
		workspaceUUID,
		sessionExternalID,
		[]string{"agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use"},
		toolUseIDs,
	)
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
