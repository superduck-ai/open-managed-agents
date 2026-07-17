package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type CodeSession struct {
	ID                          int64
	UUID                        string
	ExternalID                  string
	OrganizationID              int64
	WorkspaceID                 int64
	SessionID                   int64
	SessionExternalID           string
	EnvironmentID               int64
	EnvironmentExternalID       string
	WorkDir                     string
	PermissionMode              string
	Model                       string
	Status                      string
	Metadata                    json.RawMessage
	ConnectionStatus            string
	LastInboundSequenceNum      int64
	LastOutboundSequenceNum     int64
	LastInternalSequenceNum     int64
	LastWorkerConnectedAt       *time.Time
	LastWorkerActivityAt        *time.Time
	CurrentWorkerEpoch          int64
	WorkerLeaseExpiresAt        *time.Time
	WorkerRegisteredAt          *time.Time
	WorkerLastHeartbeatAt       *time.Time
	WorkerTokenSessionID        *string
	WorkerBinding               json.RawMessage
	WorkerStatus                string
	WorkerExternalMetadata      json.RawMessage
	WorkerRequiresActionDetails json.RawMessage
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	DeletedAt                   *time.Time
}

// CreateCodeSessionInput 同时写入 code session 与仅保存 hash 的 OAuth-compatible token。
type CreateCodeSessionInput struct {
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	SessionID             int64
	SessionExternalID     string
	EnvironmentID         int64
	EnvironmentExternalID string
	WorkDir               string
	PermissionMode        string
	Model                 string
	Status                string
	Metadata              json.RawMessage
	OAuthAccessTokenHash  string
	CreatedAt             time.Time
}

// CodeSessionCredentialContext 是凭证校验所需的数据库投影，同时绑定 code session、
// public session、agent、organization 与 workspace，避免只按 external ID 做全局授权。
type CodeSessionCredentialContext struct {
	CodeSessionID           int64
	CodeSessionExternalID   string
	OrganizationID          int64
	OrganizationUUID        string
	OrganizationExternalID  string
	WorkspaceID             int64
	WorkspaceUUID           string
	WorkspaceExternalID     string
	PublicSessionID         int64
	PublicSessionExternalID string
	AgentID                 int64
	AgentExternalID         string
	AgentVersion            int
	AccountEmail            string
}

type CodeSessionWorkerBinding struct {
	TokenSessionID string          `json:"token_session_id,omitempty"`
	AuthMode       string          `json:"auth_mode,omitempty"`
	Subject        string          `json:"subject,omitempty"`
	Issuer         string          `json:"issuer,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

func nullableWorkerTokenSessionID(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

type CodeSessionEvent struct {
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	CodeSessionID         int64
	CodeSessionExternalID string
	SequenceNum           int64
	EventType             string
	EventSubtype          string
	PayloadUUID           *string
	RequestID             *string
	Payload               json.RawMessage
	PayloadHash           string
	IdempotencyKey        string
	DeliveryStatus        string
	Source                string
	SentAt                *time.Time
	DeliveryWorkerEpoch   *int64
	ReceivedAt            *time.Time
	ProcessingAt          *time.Time
	ProcessedAt           *time.Time
	LastDeliveryAttemptAt *time.Time
	LastDeliveryUpdateAt  *time.Time
	DeliveryAttempts      int
	Ephemeral             bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

type CodeSessionInternalEvent struct {
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	CodeSessionID         int64
	CodeSessionExternalID string
	SequenceNum           int64
	EventType             string
	PayloadUUID           string
	AgentID               *string
	IsCompaction          bool
	Payload               json.RawMessage
	PayloadHash           string
	IdempotencyKey        string
	EventMetadata         json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

type AppendCodeSessionEventInput struct {
	ExternalID          string
	EventType           string
	EventSubtype        string
	PayloadUUID         *string
	RequestID           *string
	Payload             json.RawMessage
	PayloadHash         string
	IdempotencyKey      string
	DeliveryStatus      string
	Source              string
	CreatedAt           time.Time
	RequiredWorkerEpoch *int64
	Ephemeral           bool
}

type AppendCodeSessionInternalEventInput struct {
	ExternalID     string
	EventType      string
	PayloadUUID    string
	AgentID        *string
	IsCompaction   bool
	Payload        json.RawMessage
	PayloadHash    string
	IdempotencyKey string
	EventMetadata  json.RawMessage
	CreatedAt      time.Time
}

type ListCodeSessionInternalEventsPageParams struct {
	WorkspaceID           int64
	CodeSessionExternalID string
	Subagents             bool
	AfterSequence         int64
	Limit                 int
}

type CodeSessionWorkerDeliveryUpdate struct {
	EventID string
	Status  string
}

type CodeSessionWorkerDeliveryResult struct {
	Applied int
	Ignored int
}

type UpdateCodeSessionWorkerStateInput struct {
	WorkerEpoch              int64
	WorkerStatus             *string
	RequiresActionDetailsSet bool
	RequiresActionDetails    json.RawMessage
	ExternalMetadataSet      bool
	ExternalMetadata         json.RawMessage
}

type CodeSessionWorkerHeartbeatError struct {
	Err                  error
	ProvidedEpoch        int64
	CurrentEpoch         int64
	WorkerLeaseExpiresAt *time.Time
}

func (e *CodeSessionWorkerHeartbeatError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *CodeSessionWorkerHeartbeatError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CreateCodeSession 在同一次 INSERT 中保存 code session 与 OAuth token hash。
func (d *DB) CreateCodeSession(ctx context.Context, input CreateCodeSessionInput) (CodeSession, error) {
	now := input.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := input.Status
	if status == "" {
		status = "active"
	}
	oauthAccessTokenHash := nullableString(strings.TrimSpace(input.OAuthAccessTokenHash))
	return scanCodeSession(d.Pool.QueryRow(ctx, `
		insert into code_sessions (
			external_id, organization_id, workspace_id, session_id, session_external_id,
			environment_id, environment_external_id, work_dir, permission_mode, model,
			status, metadata, oauth_access_token_hash, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $14)
		returning `+codeSessionColumns()+`
	`, input.ExternalID, input.OrganizationID, input.WorkspaceID, input.SessionID, input.SessionExternalID,
		input.EnvironmentID, input.EnvironmentExternalID, input.WorkDir, input.PermissionMode, input.Model,
		status, jsonArg(input.Metadata), oauthAccessTokenHash, now))
}

// codeSessionCredentialContextSelect 查询 code session 的鉴权身份信息。
// OAuth token 鉴权和 session-ingress JWT 签发都会使用这些信息。
// JOIN 中同时校验 organization、workspace 和 session 的归属，防止跨租户查询。
// worker lease 不在这里校验：OAuth 鉴权要求有效 lease，首次签发 JWT 时还没有 lease。
const codeSessionCredentialContextSelect = `
	select cs.id, cs.external_id,
		cs.organization_id, o.uuid::text, o.external_id,
		cs.workspace_id, w.uuid::text, w.external_id,
		s.id, s.external_id, s.agent_id, s.agent_external_id, s.agent_version,
		coalesce(u.email, '')
	from code_sessions cs
	join organizations o on o.id = cs.organization_id
	join workspaces w on w.id = cs.workspace_id and w.organization_id = cs.organization_id
	join sessions s on s.id = cs.session_id
		and s.workspace_id = cs.workspace_id
		and s.organization_id = cs.organization_id
		and s.deleted_at is null
	left join api_keys ak on ak.id = s.created_by_api_key_id and ak.workspace_id = s.workspace_id
	left join users u on u.id = ak.created_by_user_id
		and u.organization_id = cs.organization_id
		and u.deleted_at is null
`

// activeCodeSessionCredentialConditions 保证凭证只关联仍可运行的 code session 和 public session。
const activeCodeSessionCredentialConditions = `
	and cs.status = 'active'
	and cs.deleted_at is null
	and s.status <> 'terminated'
`

// GetCodeSessionByOAuthAccessTokenHash 只返回 session 与 CCR worker lease 仍存活的凭证上下文。
func (d *DB) GetCodeSessionByOAuthAccessTokenHash(ctx context.Context, tokenHash string) (CodeSessionCredentialContext, error) {
	// 调用方只传 SHA-256 hash，明文 OAuth-compatible token 不进入数据库边界。
	row := d.Pool.QueryRow(ctx, codeSessionCredentialContextSelect+`
		where cs.oauth_access_token_hash = $1
	`+activeCodeSessionCredentialConditions+`
		and cs.worker_lease_expires_at > now()
	`, strings.TrimSpace(tokenHash))
	return scanCodeSessionCredentialContext(row)
}

// GetCodeSessionCredentialContextForIssue 用于初始 session-ingress JWT 签发，并将查询绑定到预期租户。
func (d *DB) GetCodeSessionCredentialContextForIssue(ctx context.Context, organizationID, workspaceID int64, codeSessionExternalID string) (CodeSessionCredentialContext, error) {
	row := d.Pool.QueryRow(ctx, codeSessionCredentialContextSelect+`
		where cs.external_id = $1
			and cs.organization_id = $2
			and cs.workspace_id = $3
	`+activeCodeSessionCredentialConditions, strings.TrimSpace(codeSessionExternalID), organizationID, workspaceID)
	return scanCodeSessionCredentialContext(row)
}

func scanCodeSessionCredentialContext(row rowScanner) (CodeSessionCredentialContext, error) {
	// 所有鉴权查询共用同一列顺序，避免 hash 查询与 session 查询产生身份字段漂移。
	var value CodeSessionCredentialContext
	err := row.Scan(
		&value.CodeSessionID, &value.CodeSessionExternalID,
		&value.OrganizationID, &value.OrganizationUUID, &value.OrganizationExternalID,
		&value.WorkspaceID, &value.WorkspaceUUID, &value.WorkspaceExternalID,
		&value.PublicSessionID, &value.PublicSessionExternalID,
		&value.AgentID, &value.AgentExternalID, &value.AgentVersion,
		&value.AccountEmail,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CodeSessionCredentialContext{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionCredentialContext{}, err
	}
	return value, nil
}

func (d *DB) GetCodeSession(ctx context.Context, externalID string) (CodeSession, error) {
	return scanCodeSession(d.Pool.QueryRow(ctx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = $1 and deleted_at is null
	`, externalID))
}

func (d *DB) GetCodeSessionBySessionExternalID(ctx context.Context, workspaceID int64, sessionExternalID string) (CodeSession, error) {
	return scanCodeSession(d.Pool.QueryRow(ctx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
		order by created_at desc, id desc
		limit 1
	`, workspaceID, sessionExternalID))
}

func (d *DB) RegisterCodeSessionWorker(ctx context.Context, codeSessionExternalID string, binding CodeSessionWorkerBinding, leaseTTL time.Duration) (int64, time.Time, error) {
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer tx.Rollback(ctx)

	session, err := scanCodeSession(tx.QueryRow(ctx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = $1 and deleted_at is null
		for update
	`, codeSessionExternalID))
	if err != nil {
		return 0, time.Time{}, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(leaseTTL)
	nextEpoch := session.CurrentWorkerEpoch + 1
	bindingJSON, err := json.Marshal(binding)
	if err != nil {
		return 0, time.Time{}, err
	}

	var epoch int64
	if err := tx.QueryRow(ctx, `
		update code_sessions
		set current_worker_epoch = $1,
			worker_lease_expires_at = $2,
			worker_registered_at = $3,
			worker_last_heartbeat_at = null,
			worker_token_session_id = $4,
			worker_binding = coalesce($5::jsonb, '{}'::jsonb),
			connection_status = 'connected',
			last_worker_connected_at = $3,
			last_worker_activity_at = $3,
			updated_at = $3
		where id = $6
		returning current_worker_epoch
	`, nextEpoch, expiresAt, now, nullableWorkerTokenSessionID(binding.TokenSessionID), jsonArg(json.RawMessage(bindingJSON)), session.ID).Scan(&epoch); err != nil {
		return 0, time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, time.Time{}, err
	}
	return epoch, expiresAt, nil
}

func (d *DB) ValidateCodeSessionWorkerEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	if epoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	var current int64
	err := d.Pool.QueryRow(ctx, `
		select current_worker_epoch
		from code_sessions
		where external_id = $1 and deleted_at is null
	`, codeSessionExternalID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if current != epoch {
		return ErrWorkerEpochMismatch
	}
	return nil
}

func (d *DB) HeartbeatCodeSessionWorker(ctx context.Context, codeSessionExternalID string, epoch int64, leaseTTL time.Duration) (time.Time, error) {
	if epoch <= 0 {
		return time.Time{}, ErrWorkerEpochMismatch
	}
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	now := time.Now().UTC()
	expiresAt := now.Add(leaseTTL)
	err := d.Pool.QueryRow(ctx, `
		update code_sessions
		set worker_last_heartbeat_at = $3,
			worker_lease_expires_at = $4,
			last_worker_activity_at = $3,
			connection_status = 'connected',
			updated_at = $3
		where external_id = $1 and current_worker_epoch = $2 and deleted_at is null
		returning worker_lease_expires_at
	`, codeSessionExternalID, epoch, now, expiresAt).Scan(&expiresAt)
	if err == nil {
		return expiresAt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, err
	}

	var current int64
	err = d.Pool.QueryRow(ctx, `
		select current_worker_epoch
		from code_sessions
		where external_id = $1 and deleted_at is null
	`, codeSessionExternalID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Time{}, ErrWorkerEpochMismatch
}

func (d *DB) RecordCodeSessionWorkerHeartbeat(ctx context.Context, codeSessionExternalID string, epoch int64, leaseTTL time.Duration, grace time.Duration) (time.Time, error) {
	if epoch <= 0 {
		return time.Time{}, ErrWorkerEpochMismatch
	}
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	if grace < 0 {
		grace = 0
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)

	var sessionID int64
	var currentEpoch int64
	var lease sql.NullTime
	err = tx.QueryRow(ctx, `
		select id, current_worker_epoch, worker_lease_expires_at
		from code_sessions
		where external_id = $1 and deleted_at is null
		for update
	`, codeSessionExternalID).Scan(&sessionID, &currentEpoch, &lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}

	var leaseExpiresAt *time.Time
	if lease.Valid {
		value := lease.Time.UTC()
		leaseExpiresAt = &value
	}
	if currentEpoch <= 0 || leaseExpiresAt == nil {
		return time.Time{}, &CodeSessionWorkerHeartbeatError{
			Err:                  ErrWorkerNotRegistered,
			ProvidedEpoch:        epoch,
			CurrentEpoch:         currentEpoch,
			WorkerLeaseExpiresAt: leaseExpiresAt,
		}
	}
	if currentEpoch != epoch {
		return time.Time{}, &CodeSessionWorkerHeartbeatError{
			Err:                  ErrWorkerEpochMismatch,
			ProvidedEpoch:        epoch,
			CurrentEpoch:         currentEpoch,
			WorkerLeaseExpiresAt: leaseExpiresAt,
		}
	}

	now := time.Now().UTC()
	if now.After(leaseExpiresAt.Add(grace)) {
		return time.Time{}, &CodeSessionWorkerHeartbeatError{
			Err:                  ErrWorkerLeaseExpired,
			ProvidedEpoch:        epoch,
			CurrentEpoch:         currentEpoch,
			WorkerLeaseExpiresAt: leaseExpiresAt,
		}
	}

	expiresAt := now.Add(leaseTTL)
	if err := tx.QueryRow(ctx, `
		update code_sessions
		set worker_last_heartbeat_at = $1,
			worker_lease_expires_at = $2,
			last_worker_activity_at = $1,
			connection_status = 'connected',
			updated_at = $1
		where id = $3
		returning worker_lease_expires_at
	`, now, expiresAt, sessionID).Scan(&expiresAt); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return expiresAt, nil
}

func (d *DB) UpdateCodeSessionWorkerState(ctx context.Context, codeSessionExternalID string, input UpdateCodeSessionWorkerStateInput) (CodeSession, error) {
	if input.WorkerEpoch <= 0 {
		return CodeSession{}, ErrWorkerEpochMismatch
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return CodeSession{}, err
	}
	defer tx.Rollback(ctx)

	current, err := scanCodeSession(tx.QueryRow(ctx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = $1 and deleted_at is null
		for update
	`, codeSessionExternalID))
	if err != nil {
		return CodeSession{}, err
	}
	if input.WorkerEpoch != current.CurrentWorkerEpoch {
		return CodeSession{}, ErrWorkerEpochMismatch
	}

	workerStatus := current.WorkerStatus
	if input.WorkerStatus != nil {
		workerStatus = *input.WorkerStatus
	}
	requiresActionDetails := current.WorkerRequiresActionDetails
	if input.RequiresActionDetailsSet {
		requiresActionDetails = nil
		if !rawIsJSONNull(input.RequiresActionDetails) {
			requiresActionDetails = copyRaw(input.RequiresActionDetails)
		}
	}
	if workerStatus != "requires_action" {
		requiresActionDetails = nil
	}
	externalMetadata := current.WorkerExternalMetadata
	if input.ExternalMetadataSet {
		externalMetadata, err = mergeCodeSessionWorkerExternalMetadata(externalMetadata, input.ExternalMetadata)
		if err != nil {
			return CodeSession{}, err
		}
	}
	if len(externalMetadata) == 0 {
		externalMetadata = json.RawMessage(`{}`)
	}

	now := time.Now().UTC()
	updated, err := scanCodeSession(tx.QueryRow(ctx, `
		update code_sessions
		set worker_status = $2,
			worker_requires_action_details = $3::jsonb,
			worker_external_metadata = $4::jsonb,
			connection_status = 'connected',
			last_worker_connected_at = $5,
			last_worker_activity_at = $5,
			updated_at = $5
		where id = $1
		returning `+codeSessionColumns()+`
	`, current.ID, workerStatus, jsonArg(requiresActionDetails), jsonArg(externalMetadata), now))
	if err != nil {
		return CodeSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CodeSession{}, err
	}
	return updated, nil
}

func (d *DB) AppendCodeSessionInboundEvent(ctx context.Context, codeSessionExternalID string, input AppendCodeSessionEventInput) (CodeSessionEvent, bool, error) {
	return d.appendCodeSessionEvent(ctx, "inbound", codeSessionExternalID, input)
}

func (d *DB) AppendCodeSessionOutboundEvent(ctx context.Context, codeSessionExternalID string, input AppendCodeSessionEventInput) (CodeSessionEvent, bool, error) {
	return d.appendCodeSessionEvent(ctx, "outbound", codeSessionExternalID, input)
}

func (d *DB) AppendCodeSessionInternalEvents(ctx context.Context, codeSessionExternalID string, workerEpoch int64, inputs []AppendCodeSessionInternalEventInput) ([]CodeSessionInternalEvent, error) {
	if workerEpoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	session, err := scanCodeSession(tx.QueryRow(ctx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = $1 and deleted_at is null
		for update
	`, codeSessionExternalID))
	if err != nil {
		return nil, err
	}
	if session.CurrentWorkerEpoch != workerEpoch {
		return nil, ErrWorkerEpochMismatch
	}

	created := make([]CodeSessionInternalEvent, 0, len(inputs))
	sequence := session.LastInternalSequenceNum
	now := time.Now().UTC()
	for _, input := range inputs {
		nextSequence := sequence + 1
		createdAt := input.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		event, err := scanCodeSessionInternalEvent(tx.QueryRow(ctx, `
			insert into code_session_internal_events (
				external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
				sequence_num, event_type, payload_uuid, agent_id, is_compaction, payload,
				payload_hash, idempotency_key, event_metadata, created_at, updated_at
				)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14::jsonb, $15, $15)
				on conflict (workspace_id, idempotency_key) where deleted_at is null and idempotency_key <> '' do nothing
				returning `+codeSessionInternalEventColumns()+`
			`, input.ExternalID, session.OrganizationID, session.WorkspaceID, session.ID, session.ExternalID,
			nextSequence, input.EventType, input.PayloadUUID, input.AgentID, input.IsCompaction, jsonArg(input.Payload),
			input.PayloadHash, input.IdempotencyKey, jsonArg(input.EventMetadata), createdAt))
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sequence = nextSequence
		created = append(created, event)
	}

	if sequence != session.LastInternalSequenceNum {
		if _, err := tx.Exec(ctx, `
			update code_sessions
			set last_internal_sequence_num = $1, updated_at = $2
			where id = $3
		`, sequence, now, session.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (d *DB) appendCodeSessionEvent(ctx context.Context, direction string, codeSessionExternalID string, input AppendCodeSessionEventInput) (CodeSessionEvent, bool, error) {
	if input.RequiredWorkerEpoch != nil && *input.RequiredWorkerEpoch <= 0 {
		return CodeSessionEvent{}, false, ErrWorkerEpochMismatch
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	defer tx.Rollback(ctx)

	session, err := scanCodeSession(tx.QueryRow(ctx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = $1 and deleted_at is null
		for update
	`, codeSessionExternalID))
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	if input.RequiredWorkerEpoch != nil && session.CurrentWorkerEpoch != *input.RequiredWorkerEpoch {
		return CodeSessionEvent{}, false, ErrWorkerEpochMismatch
	}
	if input.IdempotencyKey != "" {
		existing, err := d.getCodeSessionEventTx(ctx, tx, direction, session.WorkspaceID, input.IdempotencyKey)
		if err == nil {
			return existing, true, tx.Commit(ctx)
		}
		if !errors.Is(err, ErrNotFound) {
			return CodeSessionEvent{}, false, err
		}
	}

	now := input.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	sequence := session.LastInboundSequenceNum + 1
	sequenceColumn := "last_inbound_sequence_num"
	if direction == "outbound" {
		sequence = session.LastOutboundSequenceNum + 1
		sequenceColumn = "last_outbound_sequence_num"
	}
	deliveryStatus := input.DeliveryStatus
	if deliveryStatus == "" && direction == "inbound" {
		deliveryStatus = "queued"
	}

	var event CodeSessionEvent
	if direction == "inbound" {
		event, err = scanCodeSessionEvent(tx.QueryRow(ctx, `
			insert into code_session_inbound_events (
				external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
				sequence_num, event_type, event_subtype, payload_uuid, request_id, payload,
				payload_hash, idempotency_key, delivery_status, source, created_at, updated_at
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15, $16, $16)
			returning `+codeSessionInboundEventColumns()+`
		`, input.ExternalID, session.OrganizationID, session.WorkspaceID, session.ID, session.ExternalID,
			sequence, input.EventType, input.EventSubtype, input.PayloadUUID, input.RequestID, jsonArg(input.Payload),
			input.PayloadHash, input.IdempotencyKey, deliveryStatus, input.Source, now))
	} else {
		event, err = scanCodeSessionEvent(tx.QueryRow(ctx, `
			insert into code_session_outbound_events (
				external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
				sequence_num, event_type, event_subtype, payload_uuid, request_id, payload,
				payload_hash, idempotency_key, source, ephemeral, created_at, updated_at
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15, $16, $16)
			returning `+codeSessionOutboundEventColumns()+`
		`, input.ExternalID, session.OrganizationID, session.WorkspaceID, session.ID, session.ExternalID,
			sequence, input.EventType, input.EventSubtype, input.PayloadUUID, input.RequestID, jsonArg(input.Payload),
			input.PayloadHash, input.IdempotencyKey, input.Source, input.Ephemeral, now))
	}
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	if _, err := tx.Exec(ctx, `update code_sessions set `+sequenceColumn+` = $1, updated_at = $2 where id = $3`, sequence, now, session.ID); err != nil {
		return CodeSessionEvent{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CodeSessionEvent{}, false, err
	}
	return event, false, nil
}

func (d *DB) getCodeSessionEventTx(ctx context.Context, tx pgx.Tx, direction string, workspaceID int64, idempotencyKey string) (CodeSessionEvent, error) {
	if direction == "outbound" {
		return scanCodeSessionEvent(tx.QueryRow(ctx, `
			select `+codeSessionOutboundEventColumns()+`
			from code_session_outbound_events
			where workspace_id = $1 and idempotency_key = $2 and deleted_at is null
			limit 1
		`, workspaceID, idempotencyKey))
	}
	return scanCodeSessionEvent(tx.QueryRow(ctx, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where workspace_id = $1 and idempotency_key = $2 and deleted_at is null
		limit 1
	`, workspaceID, idempotencyKey))
}

func (d *DB) ListCodeSessionInternalEventsPage(ctx context.Context, params ListCodeSessionInternalEventsPageParams) ([]CodeSessionInternalEvent, bool, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 500
	}
	if params.AfterSequence < 0 {
		params.AfterSequence = 0
	}
	var rows pgx.Rows
	var err error
	if params.Subagents {
		rows, err = d.Pool.Query(ctx, `
			with boundaries as (
				select distinct on (e.agent_id) e.agent_id, e.sequence_num
				from code_session_internal_events e
				where e.workspace_id = $1
					and e.code_session_external_id = $2
					and e.deleted_at is null
					and e.agent_id is not null
					and e.is_compaction
				order by e.agent_id, e.sequence_num desc
			)
			select `+codeSessionInternalEventColumnsWithAlias("e")+`
			from code_session_internal_events e
			left join boundaries b on b.agent_id = e.agent_id
			where e.workspace_id = $1
				and e.code_session_external_id = $2
				and e.deleted_at is null
				and e.agent_id is not null
				and e.sequence_num > greatest($3::bigint, coalesce(b.sequence_num - 1, 0))
			order by e.sequence_num asc
			limit $4
		`, params.WorkspaceID, params.CodeSessionExternalID, params.AfterSequence, limit+1)
	} else {
		rows, err = d.Pool.Query(ctx, `
			with boundary as (
				select e.sequence_num
				from code_session_internal_events e
				where e.workspace_id = $1
					and e.code_session_external_id = $2
					and e.deleted_at is null
					and e.agent_id is null
					and e.is_compaction
				order by e.sequence_num desc
				limit 1
			)
			select `+codeSessionInternalEventColumnsWithAlias("e")+`
			from code_session_internal_events e
			left join boundary b on true
			where e.workspace_id = $1
				and e.code_session_external_id = $2
				and e.deleted_at is null
				and e.agent_id is null
				and e.sequence_num > greatest($3::bigint, coalesce(b.sequence_num - 1, 0))
			order by e.sequence_num asc
			limit $4
		`, params.WorkspaceID, params.CodeSessionExternalID, params.AfterSequence, limit+1)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	events, err := scanCodeSessionInternalEventRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	return events, hasMore, nil
}

func (d *DB) ListQueuedCodeSessionInboundEvents(ctx context.Context, codeSessionExternalID string) ([]CodeSessionEvent, error) {
	rows, err := d.Pool.Query(ctx, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where code_session_external_id = $1 and delivery_status = 'queued' and deleted_at is null
		order by sequence_num asc
	`, codeSessionExternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCodeSessionEventRows(rows)
}

func (d *DB) ListQueuedCodeSessionInboundEventsForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) ([]CodeSessionEvent, error) {
	if epoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	rows, err := d.Pool.Query(ctx, `
		select `+codeSessionInboundEventColumnsWithAlias("e")+`
		from code_session_inbound_events e
		join code_sessions cs on cs.id = e.code_session_id
		where e.code_session_external_id = $1
			and e.delivery_status = 'queued'
			and e.deleted_at is null
			and cs.deleted_at is null
			and cs.current_worker_epoch = $2
			and cs.current_worker_epoch > 0
		order by e.sequence_num asc
	`, codeSessionExternalID, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := scanCodeSessionEventRows(rows)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		return events, nil
	}
	if err := d.ValidateCodeSessionWorkerEpoch(ctx, codeSessionExternalID, epoch); err != nil {
		return nil, err
	}
	return events, nil
}

func (d *DB) ListCodeSessionInboundEventsForWorkerStream(ctx context.Context, codeSessionExternalID string, epoch int64, afterSequence int64) ([]CodeSessionEvent, error) {
	if epoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	if afterSequence < 0 {
		afterSequence = 0
	}
	rows, err := d.Pool.Query(ctx, `
		select `+codeSessionInboundEventColumnsWithAlias("e")+`
		from code_session_inbound_events e
		join code_sessions cs on cs.id = e.code_session_id
		where e.code_session_external_id = $1
			and e.sequence_num > $3
			and e.delivery_status <> 'processed'
			and not (
				e.delivery_status = 'sent'
				and e.delivery_worker_epoch is null
				and e.received_at is null
				and e.processing_at is null
				and e.processed_at is null
			)
			and e.deleted_at is null
			and cs.deleted_at is null
			and cs.current_worker_epoch = $2
			and cs.current_worker_epoch > 0
		order by e.sequence_num asc
	`, codeSessionExternalID, epoch, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := scanCodeSessionEventRows(rows)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		return events, nil
	}
	if err := d.ValidateCodeSessionWorkerEpoch(ctx, codeSessionExternalID, epoch); err != nil {
		return nil, err
	}
	return events, nil
}

func (d *DB) ListCodeSessionOutboundEventsAfter(ctx context.Context, codeSessionExternalID string, afterSequence int64, limit int) ([]CodeSessionEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.Pool.Query(ctx, `
		select `+codeSessionOutboundEventColumns()+`
		from code_session_outbound_events
		where code_session_external_id = $1 and sequence_num > $2 and deleted_at is null
		order by sequence_num asc
		limit $3
	`, codeSessionExternalID, afterSequence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCodeSessionEventRows(rows)
}

func (d *DB) GetLatestCodeSessionToolPermissionRequest(ctx context.Context, codeSessionExternalID string, toolUseID string) (CodeSessionEvent, error) {
	toolUseID = strings.TrimSpace(toolUseID)
	if toolUseID == "" {
		return CodeSessionEvent{}, ErrNotFound
	}
	return scanCodeSessionEvent(d.Pool.QueryRow(ctx, `
		select `+codeSessionOutboundEventColumns()+`
		from code_session_outbound_events
		where code_session_external_id = $1
			and event_type = 'control_request'
			and event_subtype = 'can_use_tool'
			and payload->'request'->>'tool_use_id' = $2
			and deleted_at is null
		order by sequence_num desc
		limit 1
	`, codeSessionExternalID, toolUseID))
}

func (d *DB) MarkCodeSessionInboundEventSent(ctx context.Context, eventExternalID string) error {
	tag, err := d.Pool.Exec(ctx, `
		update code_session_inbound_events
		set delivery_status = case when delivery_status = 'queued' then 'sent' else delivery_status end,
			sent_at = coalesce(sent_at, now()),
			last_delivery_attempt_at = now(),
			delivery_attempts = delivery_attempts + 1,
			updated_at = now()
		where external_id = $1 and deleted_at is null
	`, eventExternalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) MarkCodeSessionInboundEventSentForEpoch(ctx context.Context, codeSessionExternalID string, eventExternalID string, epoch int64) error {
	if epoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	tag, err := d.Pool.Exec(ctx, `
		update code_session_inbound_events e
		set delivery_status = case when e.delivery_status = 'queued' then 'sent' else e.delivery_status end,
			sent_at = coalesce(e.sent_at, now()),
			delivery_worker_epoch = $3,
			last_delivery_attempt_at = now(),
			delivery_attempts = e.delivery_attempts + 1,
			updated_at = now()
		from code_sessions cs
		where e.external_id = $1
			and e.code_session_external_id = $2
			and e.deleted_at is null
			and cs.id = e.code_session_id
			and cs.deleted_at is null
			and cs.current_worker_epoch = $3
			and cs.current_worker_epoch > 0
	`, eventExternalID, codeSessionExternalID, epoch)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if err := d.ValidateCodeSessionWorkerEpoch(ctx, codeSessionExternalID, epoch); err != nil {
		return err
	}
	return ErrNotFound
}

func (d *DB) ApplyCodeSessionWorkerDeliveryUpdates(ctx context.Context, codeSessionExternalID string, epoch int64, updates []CodeSessionWorkerDeliveryUpdate) (CodeSessionWorkerDeliveryResult, error) {
	if epoch <= 0 {
		return CodeSessionWorkerDeliveryResult{}, ErrWorkerEpochMismatch
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return CodeSessionWorkerDeliveryResult{}, err
	}
	defer tx.Rollback(ctx)

	session, err := scanCodeSession(tx.QueryRow(ctx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = $1 and deleted_at is null
		for update
	`, codeSessionExternalID))
	if err != nil {
		return CodeSessionWorkerDeliveryResult{}, err
	}
	if session.CurrentWorkerEpoch != epoch {
		return CodeSessionWorkerDeliveryResult{}, ErrWorkerEpochMismatch
	}

	now := time.Now().UTC()
	var result CodeSessionWorkerDeliveryResult
	for _, update := range updates {
		eventID := strings.TrimSpace(update.EventID)
		status := strings.TrimSpace(update.Status)
		rank := codeSessionDeliveryStatusRank(status)
		if eventID == "" || rank < codeSessionDeliveryStatusRank("received") {
			return CodeSessionWorkerDeliveryResult{}, ErrInvalidState
		}

		event, err := getCodeSessionInboundDeliveryEventTx(ctx, tx, session.ID, eventID)
		if errors.Is(err, ErrNotFound) {
			result.Ignored++
			continue
		}
		if err != nil {
			return CodeSessionWorkerDeliveryResult{}, err
		}
		if event.DeliveryWorkerEpoch == nil || *event.DeliveryWorkerEpoch != epoch || codeSessionDeliveryStatusRank(event.DeliveryStatus) < codeSessionDeliveryStatusRank("sent") {
			result.Ignored++
			continue
		}

		targetStatus := event.DeliveryStatus
		if rank > codeSessionDeliveryStatusRank(event.DeliveryStatus) {
			targetStatus = status
		}
		if _, err := tx.Exec(ctx, `
			update code_session_inbound_events
			set delivery_status = $2,
				received_at = case when $3 then coalesce(received_at, $7) else received_at end,
				processing_at = case when $4 then coalesce(processing_at, $7) else processing_at end,
				processed_at = case when $5 then coalesce(processed_at, $7) else processed_at end,
				delivery_worker_epoch = $6,
				last_delivery_update_at = $7,
				updated_at = $7
			where id = $1 and deleted_at is null
		`, event.ID, targetStatus, rank >= codeSessionDeliveryStatusRank("received"), rank >= codeSessionDeliveryStatusRank("processing"), rank >= codeSessionDeliveryStatusRank("processed"), epoch, now); err != nil {
			return CodeSessionWorkerDeliveryResult{}, err
		}
		result.Applied++
	}
	if result.Applied > 0 {
		if _, err := tx.Exec(ctx, `
			update code_sessions
			set last_worker_activity_at = $1, updated_at = $1
			where id = $2 and deleted_at is null
		`, now, session.ID); err != nil {
			return CodeSessionWorkerDeliveryResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CodeSessionWorkerDeliveryResult{}, err
	}
	return result, nil
}

func getCodeSessionInboundDeliveryEventTx(ctx context.Context, tx pgx.Tx, codeSessionID int64, eventID string) (CodeSessionEvent, error) {
	event, err := scanCodeSessionEvent(tx.QueryRow(ctx, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where code_session_id = $1
			and payload_uuid = $2
			and deleted_at is null
		order by sequence_num asc
		limit 1
		for update
	`, codeSessionID, eventID))
	if err == nil {
		return event, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return CodeSessionEvent{}, err
	}

	return scanCodeSessionEvent(tx.QueryRow(ctx, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where code_session_id = $1
			and external_id = $2
			and deleted_at is null
		limit 1
		for update
	`, codeSessionID, eventID))
}

func codeSessionDeliveryStatusRank(status string) int {
	switch strings.TrimSpace(status) {
	case "queued":
		return 0
	case "sent":
		return 1
	case "received":
		return 2
	case "processing":
		return 3
	case "processed":
		return 4
	default:
		return -1
	}
}

func (d *DB) MarkCodeSessionWorkerConnected(ctx context.Context, codeSessionExternalID string) error {
	return d.updateCodeSessionConnection(ctx, codeSessionExternalID, "connected", true, nil)
}

func (d *DB) MarkCodeSessionWorkerConnectedForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	return d.updateCodeSessionConnection(ctx, codeSessionExternalID, "connected", true, &epoch)
}

func (d *DB) MarkCodeSessionWorkerDisconnected(ctx context.Context, codeSessionExternalID string) error {
	return d.updateCodeSessionConnection(ctx, codeSessionExternalID, "disconnected", false, nil)
}

func (d *DB) MarkCodeSessionWorkerDisconnectedForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	return d.updateCodeSessionConnection(ctx, codeSessionExternalID, "disconnected", false, &epoch)
}

func (d *DB) TouchCodeSessionWorkerActivity(ctx context.Context, codeSessionExternalID string) error {
	return d.touchCodeSessionWorkerActivity(ctx, codeSessionExternalID, nil)
}

func (d *DB) TouchCodeSessionWorkerActivityForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	return d.touchCodeSessionWorkerActivity(ctx, codeSessionExternalID, &epoch)
}

// TouchCodeSessionWorkerActivityForActiveLease 只允许 OTLP 刷新当前 epoch 且 lease 尚未过期的 worker，
// 不能借遥测请求复活已经被接管或租约过期的 worker。
func (d *DB) TouchCodeSessionWorkerActivityForActiveLease(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	if epoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	now := time.Now().UTC()
	tag, err := d.Pool.Exec(ctx, `
		update code_sessions
		set last_worker_activity_at = $1, updated_at = $1
		where external_id = $2
			and current_worker_epoch = $3
			and worker_lease_expires_at > $1
			and deleted_at is null
	`, now, codeSessionExternalID, epoch)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	// 条件更新未命中后再读取当前状态，以便把 takeover 与 lease 过期映射为不同 HTTP 错误。
	record, err := d.GetCodeSession(ctx, codeSessionExternalID)
	if err != nil {
		return err
	}
	if record.CurrentWorkerEpoch != epoch {
		return ErrWorkerEpochMismatch
	}
	return ErrWorkerLeaseExpired
}

func (d *DB) touchCodeSessionWorkerActivity(ctx context.Context, codeSessionExternalID string, requiredEpoch *int64) error {
	if requiredEpoch != nil && *requiredEpoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	now := time.Now().UTC()
	query := `
		update code_sessions
		set last_worker_activity_at = $1, updated_at = $1
		where external_id = $2 and deleted_at is null
	`
	args := []any{now, codeSessionExternalID}
	if requiredEpoch != nil {
		query = `
			update code_sessions
			set last_worker_activity_at = $1, updated_at = $1
			where external_id = $2 and current_worker_epoch = $3 and deleted_at is null
		`
		args = append(args, *requiredEpoch)
	}
	tag, err := d.Pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	return d.codeSessionWorkerEpochUpdateError(ctx, codeSessionExternalID, requiredEpoch)
}

func (d *DB) updateCodeSessionConnection(ctx context.Context, codeSessionExternalID string, status string, connected bool, requiredEpoch *int64) error {
	if requiredEpoch != nil && *requiredEpoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	now := time.Now().UTC()
	query := `
		update code_sessions
		set connection_status = $1, last_worker_activity_at = $2, updated_at = $2
		where external_id = $3 and deleted_at is null
	`
	args := []any{status, now, codeSessionExternalID}
	if connected {
		query = `
			update code_sessions
			set connection_status = $1, last_worker_connected_at = $2, last_worker_activity_at = $2, updated_at = $2
			where external_id = $3 and deleted_at is null
		`
	}
	if requiredEpoch != nil {
		if connected {
			query = `
				update code_sessions
				set connection_status = $1, last_worker_connected_at = $2, last_worker_activity_at = $2, updated_at = $2
				where external_id = $3 and current_worker_epoch = $4 and deleted_at is null
			`
		} else {
			query = `
				update code_sessions
				set connection_status = $1, last_worker_activity_at = $2, updated_at = $2
				where external_id = $3 and current_worker_epoch = $4 and deleted_at is null
			`
		}
		args = append(args, *requiredEpoch)
	}
	tag, err := d.Pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return d.codeSessionWorkerEpochUpdateError(ctx, codeSessionExternalID, requiredEpoch)
	}
	return nil
}

func (d *DB) codeSessionWorkerEpochUpdateError(ctx context.Context, codeSessionExternalID string, requiredEpoch *int64) error {
	if requiredEpoch == nil {
		return ErrNotFound
	}

	var current int64
	err := d.Pool.QueryRow(ctx, `
		select current_worker_epoch
		from code_sessions
		where external_id = $1 and deleted_at is null
	`, codeSessionExternalID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrWorkerEpochMismatch
}

func codeSessionColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, session_id, session_external_id,
		environment_id, environment_external_id, work_dir, permission_mode, model, status, metadata,
		connection_status, last_inbound_sequence_num, last_outbound_sequence_num, last_internal_sequence_num,
		last_worker_connected_at, last_worker_activity_at, current_worker_epoch, worker_lease_expires_at,
		worker_registered_at, worker_last_heartbeat_at, worker_token_session_id, worker_binding,
		worker_status, worker_external_metadata, worker_requires_action_details,
		created_at, updated_at, deleted_at`
}

func codeSessionInboundEventColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
		sequence_num, event_type, event_subtype, payload_uuid, request_id, payload, payload_hash,
		idempotency_key, delivery_status, source, sent_at, delivery_worker_epoch, received_at, processing_at,
		processed_at, last_delivery_attempt_at, last_delivery_update_at, delivery_attempts,
		false as ephemeral, created_at, updated_at, deleted_at`
}

func codeSessionInboundEventColumnsWithAlias(alias string) string {
	prefix := strings.TrimSpace(alias)
	if prefix != "" {
		prefix += "."
	}
	return prefix + `id, ` + prefix + `uuid::text, ` + prefix + `external_id, ` + prefix + `organization_id, ` + prefix + `workspace_id, ` + prefix + `code_session_id, ` + prefix + `code_session_external_id,
		` + prefix + `sequence_num, ` + prefix + `event_type, ` + prefix + `event_subtype, ` + prefix + `payload_uuid, ` + prefix + `request_id, ` + prefix + `payload, ` + prefix + `payload_hash,
		` + prefix + `idempotency_key, ` + prefix + `delivery_status, ` + prefix + `source, ` + prefix + `sent_at, ` + prefix + `delivery_worker_epoch, ` + prefix + `received_at, ` + prefix + `processing_at,
		` + prefix + `processed_at, ` + prefix + `last_delivery_attempt_at, ` + prefix + `last_delivery_update_at, ` + prefix + `delivery_attempts,
		false as ephemeral, ` + prefix + `created_at, ` + prefix + `updated_at, ` + prefix + `deleted_at`
}

func codeSessionOutboundEventColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
		sequence_num, event_type, event_subtype, payload_uuid, request_id, payload, payload_hash,
		idempotency_key, ''::text as delivery_status, source, null::timestamptz as sent_at,
		null::bigint as delivery_worker_epoch, null::timestamptz as received_at, null::timestamptz as processing_at,
		null::timestamptz as processed_at, null::timestamptz as last_delivery_attempt_at,
		null::timestamptz as last_delivery_update_at, 0::integer as delivery_attempts,
		ephemeral, created_at, updated_at, deleted_at`
}

func codeSessionInternalEventColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
		sequence_num, event_type, payload_uuid, agent_id, is_compaction, payload, payload_hash,
		idempotency_key, event_metadata, created_at, updated_at, deleted_at`
}

func codeSessionInternalEventColumnsWithAlias(alias string) string {
	prefix := strings.TrimSpace(alias)
	if prefix != "" {
		prefix += "."
	}
	return prefix + `id, ` + prefix + `uuid::text, ` + prefix + `external_id, ` + prefix + `organization_id, ` + prefix + `workspace_id, ` + prefix + `code_session_id, ` + prefix + `code_session_external_id,
		` + prefix + `sequence_num, ` + prefix + `event_type, ` + prefix + `payload_uuid, ` + prefix + `agent_id, ` + prefix + `is_compaction, ` + prefix + `payload, ` + prefix + `payload_hash,
		` + prefix + `idempotency_key, ` + prefix + `event_metadata, ` + prefix + `created_at, ` + prefix + `updated_at, ` + prefix + `deleted_at`
}

func scanCodeSession(row rowScanner) (CodeSession, error) {
	var session CodeSession
	var metadata []byte
	var workerBinding []byte
	var workerExternalMetadata []byte
	var workerRequiresActionDetails []byte
	err := row.Scan(&session.ID, &session.UUID, &session.ExternalID, &session.OrganizationID, &session.WorkspaceID,
		&session.SessionID, &session.SessionExternalID, &session.EnvironmentID, &session.EnvironmentExternalID,
		&session.WorkDir, &session.PermissionMode, &session.Model, &session.Status, &metadata,
		&session.ConnectionStatus, &session.LastInboundSequenceNum, &session.LastOutboundSequenceNum,
		&session.LastInternalSequenceNum, &session.LastWorkerConnectedAt, &session.LastWorkerActivityAt, &session.CurrentWorkerEpoch,
		&session.WorkerLeaseExpiresAt, &session.WorkerRegisteredAt, &session.WorkerLastHeartbeatAt,
		&session.WorkerTokenSessionID, &workerBinding, &session.WorkerStatus, &workerExternalMetadata,
		&workerRequiresActionDetails, &session.CreatedAt, &session.UpdatedAt, &session.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CodeSession{}, ErrNotFound
	}
	if err != nil {
		return CodeSession{}, err
	}
	session.Metadata = copyRaw(metadata)
	session.WorkerBinding = copyRaw(workerBinding)
	session.WorkerExternalMetadata = copyRaw(workerExternalMetadata)
	if len(session.WorkerExternalMetadata) == 0 {
		session.WorkerExternalMetadata = json.RawMessage(`{}`)
	}
	session.WorkerRequiresActionDetails = copyRaw(workerRequiresActionDetails)
	return session, nil
}

func mergeCodeSessionWorkerExternalMetadata(base json.RawMessage, patch json.RawMessage) (json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if len(base) > 0 && !rawIsJSONNull(base) {
		if err := json.Unmarshal(base, &merged); err != nil {
			return nil, err
		}
	}
	var overlay map[string]json.RawMessage
	if err := json.Unmarshal(patch, &overlay); err != nil {
		return nil, err
	}
	for key, value := range overlay {
		if rawIsJSONNull(value) {
			delete(merged, key)
			continue
		}
		merged[key] = copyRaw(value)
	}
	if len(merged) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.Marshal(merged)
}

func rawIsJSONNull(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) == 0 || bytes.Equal(raw, []byte("null"))
}

func scanCodeSessionEvent(row rowScanner) (CodeSessionEvent, error) {
	var event CodeSessionEvent
	var payload []byte
	err := row.Scan(&event.ID, &event.UUID, &event.ExternalID, &event.OrganizationID, &event.WorkspaceID,
		&event.CodeSessionID, &event.CodeSessionExternalID, &event.SequenceNum, &event.EventType, &event.EventSubtype,
		&event.PayloadUUID, &event.RequestID, &payload, &event.PayloadHash, &event.IdempotencyKey,
		&event.DeliveryStatus, &event.Source, &event.SentAt, &event.DeliveryWorkerEpoch, &event.ReceivedAt,
		&event.ProcessingAt, &event.ProcessedAt, &event.LastDeliveryAttemptAt, &event.LastDeliveryUpdateAt,
		&event.DeliveryAttempts, &event.Ephemeral, &event.CreatedAt, &event.UpdatedAt, &event.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CodeSessionEvent{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionEvent{}, err
	}
	event.Payload = copyRaw(payload)
	return event, nil
}

func scanCodeSessionInternalEvent(row rowScanner) (CodeSessionInternalEvent, error) {
	var event CodeSessionInternalEvent
	var payload, eventMetadata []byte
	err := row.Scan(&event.ID, &event.UUID, &event.ExternalID, &event.OrganizationID, &event.WorkspaceID,
		&event.CodeSessionID, &event.CodeSessionExternalID, &event.SequenceNum, &event.EventType,
		&event.PayloadUUID, &event.AgentID, &event.IsCompaction, &payload, &event.PayloadHash,
		&event.IdempotencyKey, &eventMetadata, &event.CreatedAt, &event.UpdatedAt, &event.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CodeSessionInternalEvent{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionInternalEvent{}, err
	}
	event.Payload = copyRaw(payload)
	event.EventMetadata = copyRaw(eventMetadata)
	return event, nil
}

func scanCodeSessionEventRows(rows rowsScanner) ([]CodeSessionEvent, error) {
	var events []CodeSessionEvent
	for rows.Next() {
		event, err := scanCodeSessionEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanCodeSessionInternalEventRows(rows rowsScanner) ([]CodeSessionInternalEvent, error) {
	var events []CodeSessionInternalEvent
	for rows.Next() {
		event, err := scanCodeSessionInternalEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
