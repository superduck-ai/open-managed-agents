package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type CodeSession struct {
	UUID                        string
	ExternalID                  string
	OrganizationUUID            string
	WorkspaceUUID               string
	SessionUUID                 string
	SessionExternalID           string
	EnvironmentUUID             string
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
	OrganizationUUID      string
	WorkspaceUUID         string
	SessionUUID           string
	SessionExternalID     string
	EnvironmentUUID       string
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
	CodeSessionUUID         string
	CodeSessionExternalID   string
	OrganizationUUID        string
	WorkspaceUUID           string
	WorkspaceExternalID     string
	PublicSessionUUID       string
	PublicSessionExternalID string
	AgentUUID               string
	AgentExternalID         string
	AgentVersion            int
	AccountEmail            string
}

// CodeSessionNetworkPolicyContext 是 upstream proxy 每次 CONNECT 授权所需的
// 数据库投影。查询必须同时绑定已验签 JWT 中的 organization/workspace UUID，
// 并校验 Code Session 与 Environment、Session 的内部租户关系。
type CodeSessionNetworkPolicyContext struct {
	OrganizationUUID      string
	WorkspaceUUID         string
	EnvironmentExternalID string
	EnvironmentConfig     json.RawMessage
	AgentSnapshot         json.RawMessage
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
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionUUID       string
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
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionUUID       string
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
	WorkspaceUUID         string
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
	return getCodeSessionSQLX(ctx, d.sql, `
		insert into code_sessions (
			external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id,
			environment_uuid, environment_external_id, work_dir, permission_mode, model,
			status, metadata, oauth_access_token_hash, created_at, updated_at
		)
		values (
			:external_id, :organization_uuid, :workspace_uuid, :session_uuid,
			:session_external_id, :environment_uuid, :environment_external_id,
			:work_dir, :permission_mode, :model, :status, CAST(:metadata AS jsonb),
			:oauth_access_token_hash, :created_at, :created_at
		)
		returning `+codeSessionColumns()+`
	`, map[string]any{
		"external_id":             input.ExternalID,
		"organization_uuid":       dbUUID(input.OrganizationUUID),
		"workspace_uuid":          dbUUID(input.WorkspaceUUID),
		"session_uuid":            dbUUID(input.SessionUUID),
		"session_external_id":     input.SessionExternalID,
		"environment_uuid":        dbUUID(input.EnvironmentUUID),
		"environment_external_id": input.EnvironmentExternalID,
		"work_dir":                input.WorkDir,
		"permission_mode":         input.PermissionMode,
		"model":                   input.Model,
		"status":                  status,
		"metadata":                jsonArg(input.Metadata),
		"oauth_access_token_hash": oauthAccessTokenHash,
		"created_at":              now,
	})
}

func (tx ManagedAgentActivationTx) LockInitializingCodeSession(
	ctx context.Context,
	codeSessionUUID string,
) (CodeSession, error) {
	return getCodeSessionSQLX(ctx, tx.tx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where uuid = :uuid
			and status = 'initializing'
			and deleted_at is null
		for update
	`, map[string]any{
		"uuid": dbUUID(codeSessionUUID),
	})
}

const managedAgentActivationInboundBatchSize = 500

func (tx ManagedAgentActivationTx) AppendCodeSessionInboundEvents(
	ctx context.Context,
	codeSession CodeSession,
	inputs []AppendCodeSessionEventInput,
) error {
	if len(inputs) == 0 {
		return nil
	}
	existing, err := listExistingActivationInboundEvents(ctx, tx.tx, codeSession, inputs)
	if err != nil {
		return err
	}
	rows, lastSequence, err := activationInboundEventInsertRows(codeSession, inputs, existing)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for start := 0; start < len(rows); start += managedAgentActivationInboundBatchSize {
		end := min(start+managedAgentActivationInboundBatchSize, len(rows))
		result, err := tx.tx.NamedExecContext(ctx, `
			insert into code_session_inbound_events (
				external_id, organization_uuid, workspace_uuid, code_session_uuid,
				code_session_external_id, sequence_num, event_type, event_subtype,
				payload_uuid, request_id, payload, payload_hash, idempotency_key,
				delivery_status, source, created_at, updated_at
			)
			values (
				:external_id, :organization_uuid, :workspace_uuid, :code_session_uuid,
				:code_session_external_id, :sequence_num, :event_type, :event_subtype,
				:payload_uuid, :request_id, CAST(:payload AS jsonb), :payload_hash,
				:idempotency_key, :delivery_status, :source, :created_at, :created_at
			)
		`, rows[start:end])
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted != int64(end-start) {
			return ErrInvalidState
		}
	}
	updated, err := namedExecRowsAffected(ctx, tx.tx, `
		update code_sessions
		set last_inbound_sequence_num = :sequence_num, updated_at = :now
		where uuid = :uuid
	`, map[string]any{
		"sequence_num": lastSequence,
		"now":          time.Now().UTC(),
		"uuid":         dbUUID(codeSession.UUID),
	})
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrInvalidState
	}
	return nil
}

func listExistingActivationInboundEvents(
	ctx context.Context,
	database sqlxNamedQueryer,
	codeSession CodeSession,
	inputs []AppendCodeSessionEventInput,
) (map[string]struct{}, error) {
	idempotencyKeys := make([]string, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input.IdempotencyKey == "" {
			continue
		}
		if _, ok := seen[input.IdempotencyKey]; ok {
			continue
		}
		seen[input.IdempotencyKey] = struct{}{}
		idempotencyKeys = append(idempotencyKeys, input.IdempotencyKey)
	}
	if len(idempotencyKeys) == 0 {
		return map[string]struct{}{}, nil
	}

	var rows []codeSessionInboundEventIdentityRow
	if err := namedSelectContext(ctx, database, &rows, `
		select code_session_external_id, idempotency_key
		from code_session_inbound_events
		where organization_uuid = :organization_uuid
			and workspace_uuid = :workspace_uuid
			and idempotency_key = any(:idempotency_keys)
			and deleted_at is null
	`, map[string]any{
		"organization_uuid": dbUUID(codeSession.OrganizationUUID),
		"workspace_uuid":    dbUUID(codeSession.WorkspaceUUID),
		"idempotency_keys":  idempotencyKeys,
	}); err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.CodeSessionExternalID != codeSession.ExternalID {
			return nil, ErrInvalidState
		}
		existing[row.IdempotencyKey] = struct{}{}
	}
	return existing, nil
}

func activationInboundEventInsertRows(
	codeSession CodeSession,
	inputs []AppendCodeSessionEventInput,
	existing map[string]struct{},
) ([]codeSessionInboundEventInsertRow, int64, error) {
	organizationUUID, err := parseDBUUID("organization_uuid", codeSession.OrganizationUUID)
	if err != nil {
		return nil, 0, err
	}
	workspaceUUID, err := parseDBUUID("workspace_uuid", codeSession.WorkspaceUUID)
	if err != nil {
		return nil, 0, err
	}
	codeSessionUUID, err := parseDBUUID("code_session_uuid", codeSession.UUID)
	if err != nil {
		return nil, 0, err
	}

	now := time.Now().UTC()
	sequence := codeSession.LastInboundSequenceNum
	rows := make([]codeSessionInboundEventInsertRow, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input.RequiredWorkerEpoch != nil && codeSession.CurrentWorkerEpoch != *input.RequiredWorkerEpoch {
			return nil, 0, ErrWorkerEpochMismatch
		}
		if input.IdempotencyKey != "" {
			if _, ok := existing[input.IdempotencyKey]; ok {
				continue
			}
			if _, ok := seen[input.IdempotencyKey]; ok {
				continue
			}
			seen[input.IdempotencyKey] = struct{}{}
		}
		createdAt := input.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		deliveryStatus := input.DeliveryStatus
		if deliveryStatus == "" {
			deliveryStatus = "queued"
		}
		sequence++
		rows = append(rows, codeSessionInboundEventInsertRow{
			ExternalID:            input.ExternalID,
			OrganizationUUID:      organizationUUID,
			WorkspaceUUID:         workspaceUUID,
			CodeSessionUUID:       codeSessionUUID,
			CodeSessionExternalID: codeSession.ExternalID,
			SequenceNum:           sequence,
			EventType:             input.EventType,
			EventSubtype:          input.EventSubtype,
			PayloadUUID:           input.PayloadUUID,
			RequestID:             input.RequestID,
			Payload:               []byte(input.Payload),
			PayloadHash:           input.PayloadHash,
			IdempotencyKey:        input.IdempotencyKey,
			DeliveryStatus:        deliveryStatus,
			Source:                input.Source,
			CreatedAt:             createdAt,
		})
	}
	return rows, sequence, nil
}

func (tx ManagedAgentActivationTx) ActivateCodeSession(
	ctx context.Context,
	codeSessionUUID string,
	now time.Time,
) (bool, error) {
	updated, err := namedExecRowsAffected(ctx, tx.tx, `
		update code_sessions
		set status = 'active', updated_at = :now
		where uuid = :uuid
			and status = 'initializing'
			and deleted_at is null
	`, map[string]any{
		"uuid": dbUUID(codeSessionUUID),
		"now":  now,
	})
	if err != nil {
		return false, err
	}
	return updated == 1, nil
}

// codeSessionCredentialContextSelect 查询 code session 的鉴权身份信息。
// OAuth token 鉴权和 session-ingress JWT 签发都会使用这些信息。
// JOIN 中同时校验 organization、workspace 和 session 的归属，防止跨租户查询。
// worker lease 不在这里校验：OAuth 鉴权要求有效 lease，首次签发 JWT 时还没有 lease。
const codeSessionCredentialContextSelect = `
	select cs.uuid AS code_session_uuid, cs.external_id AS code_session_external_id,
		cs.organization_uuid AS organization_uuid,
		cs.workspace_uuid AS workspace_uuid,
		w.external_id AS workspace_external_id,
		s.uuid AS public_session_uuid, s.external_id AS public_session_external_id,
		s.agent_uuid AS agent_uuid, s.agent_external_id, s.agent_version,
		coalesce(u.email, '') AS account_email
	from code_sessions cs
	join workspaces w on w.uuid = cs.workspace_uuid
		and w.organization_uuid = cs.organization_uuid
	join sessions s on s.uuid = cs.session_uuid
		and s.workspace_uuid = cs.workspace_uuid
		and s.organization_uuid = cs.organization_uuid
		and s.deleted_at is null
	left join api_keys ak on ak.uuid = s.created_by_api_key_uuid and ak.workspace_uuid = w.uuid
	left join users u on u.uuid = ak.created_by_user_uuid
		and u.organization_uuid = cs.organization_uuid
		and u.deleted_at is null
`

// activeCodeSessionCredentialConditions 保证凭证只关联仍可运行的 code session 和 public session。
const activeCodeSessionCredentialConditions = `
	and cs.status = 'active'
	and cs.deleted_at is null
	and s.status <> 'terminated'
`

const codeSessionByOAuthAccessTokenHashQuery = codeSessionCredentialContextSelect + `
	where cs.oauth_access_token_hash = :token_hash
` + activeCodeSessionCredentialConditions + `
	and cs.worker_lease_expires_at > now()
`

const codeSessionCredentialContextForIssueQuery = codeSessionCredentialContextSelect + `
	where cs.external_id = :code_session_external_id
		and cs.organization_uuid = :organization_uuid
		and cs.workspace_uuid = :workspace_uuid
` + activeCodeSessionCredentialConditions

// GetCodeSessionByOAuthAccessTokenHash 只返回 session 与 CCR worker lease 仍存活的凭证上下文。
func (d *DB) GetCodeSessionByOAuthAccessTokenHash(ctx context.Context, tokenHash string) (CodeSessionCredentialContext, error) {
	// 调用方只传 SHA-256 hash，明文 OAuth-compatible token 不进入数据库边界。
	return getCodeSessionCredentialContextSQLX(ctx, d.sql, codeSessionByOAuthAccessTokenHashQuery, map[string]any{
		"token_hash": strings.TrimSpace(tokenHash),
	})
}

// GetCodeSessionCredentialContextForIssue 用于初始 session-ingress JWT 签发，并将查询绑定到预期租户。
func (d *DB) GetCodeSessionCredentialContextForIssue(ctx context.Context, organizationUUID, workspaceUUID string, codeSessionExternalID string) (CodeSessionCredentialContext, error) {
	return getCodeSessionCredentialContextSQLX(ctx, d.sql, codeSessionCredentialContextForIssueQuery, map[string]any{
		"code_session_external_id": strings.TrimSpace(codeSessionExternalID),
		"organization_uuid":        dbUUID(organizationUUID),
		"workspace_uuid":           dbUUID(workspaceUUID),
	})
}

// GetCodeSessionNetworkPolicyContext 从已验签的租户身份出发，一次性加载并校验
// Code Session、Environment 与 Session 的策略关系。项目不使用数据库外键，因此
// 每个 join 都显式约束 organization/workspace 与稳定 UUID；任一关系缺失都 fail closed。
func (d *DB) GetCodeSessionNetworkPolicyContext(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
) (CodeSessionNetworkPolicyContext, error) {
	var row codeSessionNetworkPolicyContextRow
	err := namedGetContext(ctx, d.sql, &row, `
		select cs.organization_uuid AS organization_uuid,
			cs.workspace_uuid AS workspace_uuid,
			e.external_id AS environment_external_id,
			e.config AS environment_config, s.agent_snapshot
		from code_sessions cs
		join environments e
			on e.uuid = cs.environment_uuid
			and e.external_id = cs.environment_external_id
			and e.organization_uuid = cs.organization_uuid
			and e.workspace_uuid = cs.workspace_uuid
			and e.deleted_at is null
		join sessions s
			on s.uuid = cs.session_uuid
			and s.external_id = cs.session_external_id
			and s.organization_uuid = cs.organization_uuid
			and s.workspace_uuid = cs.workspace_uuid
			and s.environment_uuid = cs.environment_uuid
			and s.environment_external_id = cs.environment_external_id
			and s.deleted_at is null
		where cs.external_id = :code_session_external_id
			and cs.organization_uuid = :organization_uuid
			and cs.workspace_uuid = :workspace_uuid
	`+activeCodeSessionCredentialConditions,
		map[string]any{
			"code_session_external_id": strings.TrimSpace(codeSessionExternalID),
			"organization_uuid":        dbUUID(strings.TrimSpace(organizationUUID)),
			"workspace_uuid":           dbUUID(strings.TrimSpace(workspaceUUID)),
		})
	if errors.Is(err, sql.ErrNoRows) {
		return CodeSessionNetworkPolicyContext{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionNetworkPolicyContext{}, err
	}
	return CodeSessionNetworkPolicyContext{
		OrganizationUUID:      row.OrganizationUUID.String(),
		WorkspaceUUID:         row.WorkspaceUUID.String(),
		EnvironmentExternalID: row.EnvironmentExternalID,
		EnvironmentConfig:     copyRaw(row.EnvironmentConfig),
		AgentSnapshot:         copyRaw(row.AgentSnapshot),
	}, nil
}

func (d *DB) GetCodeSession(ctx context.Context, externalID string) (CodeSession, error) {
	return getCodeSessionSQLX(ctx, d.sql, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = :external_id and deleted_at is null
	`, map[string]any{"external_id": externalID})
}

func (d *DB) GetCodeSessionBySessionExternalID(ctx context.Context, workspaceUUID string, sessionExternalID string) (CodeSession, error) {
	return getCodeSessionSQLX(ctx, d.sql, `
		select `+codeSessionColumns()+`
		from code_sessions
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and deleted_at is null
		order by created_at desc, uuid desc
		limit 1
	`, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": sessionExternalID,
	})
}

func (d *DB) RegisterCodeSessionWorker(ctx context.Context, codeSessionExternalID string, binding CodeSessionWorkerBinding, leaseTTL time.Duration) (int64, time.Time, error) {
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer tx.Rollback()

	session, err := getCodeSessionSQLX(ctx, tx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"external_id": codeSessionExternalID})
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
	if err := namedGetContext(ctx, tx, &epoch, `
		update code_sessions
		set current_worker_epoch = :epoch,
			worker_lease_expires_at = :expires_at,
			worker_registered_at = :now,
			worker_last_heartbeat_at = null,
			worker_token_session_id = :worker_token_session_id,
			worker_binding = coalesce(CAST(:worker_binding AS jsonb), CAST('{}' AS jsonb)),
			connection_status = 'connected',
			last_worker_connected_at = :now,
			last_worker_activity_at = :now,
			updated_at = :now
		where uuid = :uuid
		returning current_worker_epoch
	`, map[string]any{
		"epoch":                   nextEpoch,
		"expires_at":              expiresAt,
		"now":                     now,
		"worker_token_session_id": nullableWorkerTokenSessionID(binding.TokenSessionID),
		"worker_binding":          jsonArg(json.RawMessage(bindingJSON)),
		"uuid":                    dbUUID(session.UUID),
	}); err != nil {
		return 0, time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return 0, time.Time{}, err
	}
	return epoch, expiresAt, nil
}

func (d *DB) ValidateCodeSessionWorkerEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	if epoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	var current int64
	err := namedGetContext(ctx, d.sql, &current, `
		select current_worker_epoch
		from code_sessions
		where external_id = :external_id and deleted_at is null
	`, map[string]any{"external_id": codeSessionExternalID})
	if errors.Is(err, sql.ErrNoRows) {
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
	err := namedGetContext(ctx, d.sql, &expiresAt, `
		update code_sessions
		set worker_last_heartbeat_at = :now,
			worker_lease_expires_at = :expires_at,
			last_worker_activity_at = :now,
			connection_status = 'connected',
			updated_at = :now
		where external_id = :external_id
			and current_worker_epoch = :epoch
			and deleted_at is null
		returning worker_lease_expires_at
	`, map[string]any{
		"external_id": codeSessionExternalID,
		"epoch":       epoch,
		"now":         now,
		"expires_at":  expiresAt,
	})
	if err == nil {
		return expiresAt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}

	var current int64
	err = namedGetContext(ctx, d.sql, &current, `
		select current_worker_epoch
		from code_sessions
		where external_id = :external_id and deleted_at is null
	`, map[string]any{"external_id": codeSessionExternalID})
	if errors.Is(err, sql.ErrNoRows) {
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
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()

	var leaseRow codeSessionWorkerLeaseRow
	err = namedGetContext(ctx, tx, &leaseRow, `
		select uuid, current_worker_epoch, worker_lease_expires_at
		from code_sessions
		where external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"external_id": codeSessionExternalID})
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, err
	}

	sessionUUID := leaseRow.UUID.String()
	currentEpoch := leaseRow.CurrentWorkerEpoch
	lease := leaseRow.WorkerLeaseExpiresAt
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
	if err := namedGetContext(ctx, tx, &expiresAt, `
		update code_sessions
		set worker_last_heartbeat_at = :now,
			worker_lease_expires_at = :expires_at,
			last_worker_activity_at = :now,
			connection_status = 'connected',
			updated_at = :now
		where uuid = :uuid
		returning worker_lease_expires_at
	`, map[string]any{
		"now":        now,
		"expires_at": expiresAt,
		"uuid":       dbUUID(sessionUUID),
	}); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return expiresAt, nil
}

func (d *DB) UpdateCodeSessionWorkerState(ctx context.Context, codeSessionExternalID string, input UpdateCodeSessionWorkerStateInput) (CodeSession, error) {
	if input.WorkerEpoch <= 0 {
		return CodeSession{}, ErrWorkerEpochMismatch
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return CodeSession{}, err
	}
	defer tx.Rollback()

	current, err := getCodeSessionSQLX(ctx, tx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"external_id": codeSessionExternalID})
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
	updated, err := getCodeSessionSQLX(ctx, tx, `
		update code_sessions
		set worker_status = :worker_status,
			worker_requires_action_details = CAST(:requires_action_details AS jsonb),
			worker_external_metadata = CAST(:external_metadata AS jsonb),
			connection_status = 'connected',
			last_worker_connected_at = :now,
			last_worker_activity_at = :now,
			updated_at = :now
		where uuid = :uuid
		returning `+codeSessionColumns()+`
	`, map[string]any{
		"uuid":                    dbUUID(current.UUID),
		"worker_status":           workerStatus,
		"requires_action_details": jsonArg(requiresActionDetails),
		"external_metadata":       jsonArg(externalMetadata),
		"now":                     now,
	})
	if err != nil {
		return CodeSession{}, err
	}
	if err := tx.Commit(); err != nil {
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
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	session, err := getCodeSessionSQLX(ctx, tx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"external_id": codeSessionExternalID})
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
		event, err := getCodeSessionInternalEventSQLX(ctx, tx, `
			insert into code_session_internal_events (
				external_id, organization_uuid, workspace_uuid, code_session_uuid, code_session_external_id,
				sequence_num, event_type, payload_uuid, agent_id, is_compaction, payload,
				payload_hash, idempotency_key, event_metadata, created_at, updated_at
				)
				values (
					:external_id, :organization_uuid, :workspace_uuid, :code_session_uuid,
					:code_session_external_id, :sequence_num, :event_type, :payload_uuid,
					:agent_id, :is_compaction, CAST(:payload AS jsonb), :payload_hash,
					:idempotency_key, CAST(:event_metadata AS jsonb), :created_at, :created_at
				)
				on conflict (workspace_uuid, idempotency_key) where deleted_at is null and idempotency_key <> '' do nothing
				returning `+codeSessionInternalEventColumns()+`
			`, map[string]any{
			"external_id":              input.ExternalID,
			"organization_uuid":        dbUUID(session.OrganizationUUID),
			"workspace_uuid":           dbUUID(session.WorkspaceUUID),
			"code_session_uuid":        dbUUID(session.UUID),
			"code_session_external_id": session.ExternalID,
			"sequence_num":             nextSequence,
			"event_type":               input.EventType,
			"payload_uuid":             input.PayloadUUID,
			"agent_id":                 input.AgentID,
			"is_compaction":            input.IsCompaction,
			"payload":                  jsonArg(input.Payload),
			"payload_hash":             input.PayloadHash,
			"idempotency_key":          input.IdempotencyKey,
			"event_metadata":           jsonArg(input.EventMetadata),
			"created_at":               createdAt,
		})
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
		if _, err := namedExecContext(ctx, tx, `
			update code_sessions
			set last_internal_sequence_num = :sequence_num, updated_at = :now
			where uuid = :uuid
		`, map[string]any{
			"sequence_num": sequence,
			"now":          now,
			"uuid":         dbUUID(session.UUID),
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (d *DB) appendCodeSessionEvent(ctx context.Context, direction string, codeSessionExternalID string, input AppendCodeSessionEventInput) (CodeSessionEvent, bool, error) {
	if input.RequiredWorkerEpoch != nil && *input.RequiredWorkerEpoch <= 0 {
		return CodeSessionEvent{}, false, ErrWorkerEpochMismatch
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	defer tx.Rollback()

	session, err := getCodeSessionSQLX(ctx, tx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"external_id": codeSessionExternalID})
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	event, duplicate, err := d.appendCodeSessionEventSQLXTx(ctx, tx, session, direction, input)
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return CodeSessionEvent{}, false, err
	}
	return event, duplicate, nil
}

func (d *DB) appendCodeSessionEventSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session CodeSession,
	direction string,
	input AppendCodeSessionEventInput,
) (CodeSessionEvent, bool, error) {
	if input.RequiredWorkerEpoch != nil && session.CurrentWorkerEpoch != *input.RequiredWorkerEpoch {
		return CodeSessionEvent{}, false, ErrWorkerEpochMismatch
	}
	if input.IdempotencyKey != "" {
		existing, err := d.getCodeSessionEventTx(ctx, tx, direction, session.WorkspaceUUID, input.IdempotencyKey)
		if err == nil {
			return existing, true, nil
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

	var (
		event CodeSessionEvent
		err   error
	)
	eventArguments := map[string]any{
		"external_id":              input.ExternalID,
		"organization_uuid":        dbUUID(session.OrganizationUUID),
		"workspace_uuid":           dbUUID(session.WorkspaceUUID),
		"code_session_uuid":        dbUUID(session.UUID),
		"code_session_external_id": session.ExternalID,
		"sequence_num":             sequence,
		"event_type":               input.EventType,
		"event_subtype":            input.EventSubtype,
		"payload_uuid":             input.PayloadUUID,
		"request_id":               input.RequestID,
		"payload":                  jsonArg(input.Payload),
		"payload_hash":             input.PayloadHash,
		"idempotency_key":          input.IdempotencyKey,
		"source":                   input.Source,
		"created_at":               now,
	}
	if direction == "inbound" {
		eventArguments["delivery_status"] = deliveryStatus
		event, err = getCodeSessionEventSQLX(ctx, tx, `
			insert into code_session_inbound_events (
				external_id, organization_uuid, workspace_uuid, code_session_uuid, code_session_external_id,
				sequence_num, event_type, event_subtype, payload_uuid, request_id, payload,
				payload_hash, idempotency_key, delivery_status, source, created_at, updated_at
			)
			values (
				:external_id, :organization_uuid, :workspace_uuid, :code_session_uuid,
				:code_session_external_id, :sequence_num, :event_type, :event_subtype,
				:payload_uuid, :request_id, CAST(:payload AS jsonb), :payload_hash,
				:idempotency_key, :delivery_status, :source, :created_at, :created_at
			)
			returning `+codeSessionInboundEventColumns()+`
		`, eventArguments)
	} else {
		eventArguments["ephemeral"] = input.Ephemeral
		event, err = getCodeSessionEventSQLX(ctx, tx, `
			insert into code_session_outbound_events (
				external_id, organization_uuid, workspace_uuid, code_session_uuid, code_session_external_id,
				sequence_num, event_type, event_subtype, payload_uuid, request_id, payload,
				payload_hash, idempotency_key, source, ephemeral, created_at, updated_at
			)
			values (
				:external_id, :organization_uuid, :workspace_uuid, :code_session_uuid,
				:code_session_external_id, :sequence_num, :event_type, :event_subtype,
				:payload_uuid, :request_id, CAST(:payload AS jsonb), :payload_hash,
				:idempotency_key, :source, :ephemeral, :created_at, :created_at
			)
			returning `+codeSessionOutboundEventColumns()+`
		`, eventArguments)
	}
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	if _, err := namedExecContext(ctx, tx, `update code_sessions set `+sequenceColumn+` = :sequence_num, updated_at = :now where uuid = :uuid`, map[string]any{
		"sequence_num": sequence,
		"now":          now,
		"uuid":         dbUUID(session.UUID),
	}); err != nil {
		return CodeSessionEvent{}, false, err
	}
	return event, false, nil
}

func (d *DB) getCodeSessionEventTx(ctx context.Context, tx sqlxNamedQueryer, direction string, workspaceUUID string, idempotencyKey string) (CodeSessionEvent, error) {
	arguments := map[string]any{
		"workspace_uuid":  dbUUID(workspaceUUID),
		"idempotency_key": idempotencyKey,
	}
	if direction == "outbound" {
		return getCodeSessionEventSQLX(ctx, tx, `
			select `+codeSessionOutboundEventColumns()+`
			from code_session_outbound_events
			where workspace_uuid = :workspace_uuid and idempotency_key = :idempotency_key and deleted_at is null
			limit 1
		`, arguments)
	}
	return getCodeSessionEventSQLX(ctx, tx, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where workspace_uuid = :workspace_uuid and idempotency_key = :idempotency_key and deleted_at is null
		limit 1
	`, arguments)
}

func (d *DB) ListCodeSessionInternalEventsPage(ctx context.Context, params ListCodeSessionInternalEventsPageParams) ([]CodeSessionInternalEvent, bool, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 500
	}
	if params.AfterSequence < 0 {
		params.AfterSequence = 0
	}
	arguments := map[string]any{
		"workspace_uuid":           dbUUID(params.WorkspaceUUID),
		"code_session_external_id": params.CodeSessionExternalID,
		"after_sequence":           params.AfterSequence,
		"limit":                    limit + 1,
	}
	var query string
	if params.Subagents {
		query = `
			with boundaries as (
				select distinct on (e.agent_id) e.agent_id, e.sequence_num
				from code_session_internal_events e
				where e.workspace_uuid = :workspace_uuid
					and e.code_session_external_id = :code_session_external_id
					and e.deleted_at is null
					and e.agent_id is not null
					and e.is_compaction
				order by e.agent_id, e.sequence_num desc
			)
			select ` + codeSessionInternalEventColumnsWithAlias("e") + `
			from code_session_internal_events e
			left join boundaries b on b.agent_id = e.agent_id
			where e.workspace_uuid = :workspace_uuid
				and e.code_session_external_id = :code_session_external_id
				and e.deleted_at is null
				and e.agent_id is not null
				and e.sequence_num > greatest(CAST(:after_sequence AS bigint), coalesce(b.sequence_num - 1, 0))
			order by e.sequence_num asc
			limit :limit
		`
	} else {
		query = `
			with boundary as (
				select e.sequence_num
				from code_session_internal_events e
				where e.workspace_uuid = :workspace_uuid
					and e.code_session_external_id = :code_session_external_id
					and e.deleted_at is null
					and e.agent_id is null
					and e.is_compaction
				order by e.sequence_num desc
				limit 1
			)
			select ` + codeSessionInternalEventColumnsWithAlias("e") + `
			from code_session_internal_events e
			left join boundary b on true
			where e.workspace_uuid = :workspace_uuid
				and e.code_session_external_id = :code_session_external_id
				and e.deleted_at is null
				and e.agent_id is null
				and e.sequence_num > greatest(CAST(:after_sequence AS bigint), coalesce(b.sequence_num - 1, 0))
			order by e.sequence_num asc
			limit :limit
		`
	}
	events, err := selectCodeSessionInternalEventsSQLX(ctx, d.sql, query, arguments)
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
	return selectCodeSessionEventsSQLX(ctx, d.sql, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where code_session_external_id = :external_id
			and delivery_status = 'queued'
			and deleted_at is null
		order by sequence_num asc
	`, map[string]any{"external_id": codeSessionExternalID})
}

func (d *DB) ListQueuedCodeSessionInboundEventsForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) ([]CodeSessionEvent, error) {
	if epoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	events, err := selectCodeSessionEventsSQLX(ctx, d.sql, `
		select `+codeSessionInboundEventColumnsWithAlias("e")+`
		from code_session_inbound_events e
			join code_sessions cs on cs.uuid = e.code_session_uuid
		where e.code_session_external_id = :external_id
			and e.delivery_status = 'queued'
			and e.deleted_at is null
			and cs.deleted_at is null
			and cs.current_worker_epoch = :epoch
			and cs.current_worker_epoch > 0
		order by e.sequence_num asc
	`, map[string]any{"external_id": codeSessionExternalID, "epoch": epoch})
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
	events, err := selectCodeSessionEventsSQLX(ctx, d.sql, `
		select `+codeSessionInboundEventColumnsWithAlias("e")+`
		from code_session_inbound_events e
			join code_sessions cs on cs.uuid = e.code_session_uuid
		where e.code_session_external_id = :external_id
			and e.sequence_num > :after_sequence
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
			and cs.current_worker_epoch = :epoch
			and cs.current_worker_epoch > 0
		order by e.sequence_num asc
	`, map[string]any{
		"external_id":    codeSessionExternalID,
		"epoch":          epoch,
		"after_sequence": afterSequence,
	})
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
	return selectCodeSessionEventsSQLX(ctx, d.sql, `
		select `+codeSessionOutboundEventColumns()+`
		from code_session_outbound_events
		where code_session_external_id = :external_id
			and sequence_num > :after_sequence
			and deleted_at is null
		order by sequence_num asc
		limit :limit
	`, map[string]any{
		"external_id":    codeSessionExternalID,
		"after_sequence": afterSequence,
		"limit":          limit,
	})
}

func (d *DB) GetLatestCodeSessionToolPermissionRequest(ctx context.Context, codeSessionExternalID string, toolUseID string) (CodeSessionEvent, error) {
	toolUseID = strings.TrimSpace(toolUseID)
	if toolUseID == "" {
		return CodeSessionEvent{}, ErrNotFound
	}
	return getCodeSessionEventSQLX(ctx, d.sql, `
		select `+codeSessionOutboundEventColumns()+`
		from code_session_outbound_events
		where code_session_external_id = :external_id
			and event_type = 'control_request'
			and event_subtype = 'can_use_tool'
			and payload->'request'->>'tool_use_id' = :tool_use_id
			and deleted_at is null
		order by sequence_num desc
		limit 1
	`, map[string]any{"external_id": codeSessionExternalID, "tool_use_id": toolUseID})
}

func (d *DB) MarkCodeSessionInboundEventSent(ctx context.Context, eventExternalID string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update code_session_inbound_events
		set delivery_status = case when delivery_status = 'queued' then 'sent' else delivery_status end,
			sent_at = coalesce(sent_at, now()),
			last_delivery_attempt_at = now(),
			delivery_attempts = delivery_attempts + 1,
			updated_at = now()
		where external_id = :event_external_id and deleted_at is null
	`, map[string]any{"event_external_id": eventExternalID})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) MarkCodeSessionInboundEventSentForEpoch(ctx context.Context, codeSessionExternalID string, eventExternalID string, epoch int64) error {
	if epoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update code_session_inbound_events e
		set delivery_status = case when e.delivery_status = 'queued' then 'sent' else e.delivery_status end,
			sent_at = coalesce(e.sent_at, now()),
			delivery_worker_epoch = :epoch,
			last_delivery_attempt_at = now(),
			delivery_attempts = e.delivery_attempts + 1,
			updated_at = now()
		from code_sessions cs
		where e.external_id = :event_external_id
			and e.code_session_external_id = :code_session_external_id
			and e.deleted_at is null
			and cs.uuid = e.code_session_uuid
			and cs.deleted_at is null
			and cs.current_worker_epoch = :epoch
			and cs.current_worker_epoch > 0
	`, map[string]any{
		"event_external_id":        eventExternalID,
		"code_session_external_id": codeSessionExternalID,
		"epoch":                    epoch,
	})
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
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
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return CodeSessionWorkerDeliveryResult{}, err
	}
	defer tx.Rollback()

	session, err := getCodeSessionSQLX(ctx, tx, `
		select `+codeSessionColumns()+`
		from code_sessions
		where external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"external_id": codeSessionExternalID})
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

		event, err := getCodeSessionInboundDeliveryEventTx(ctx, tx, session.UUID, eventID)
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
		if _, err := namedExecContext(ctx, tx, `
			update code_session_inbound_events
			set delivery_status = :target_status,
				received_at = case when :mark_received then coalesce(received_at, :now) else received_at end,
				processing_at = case when :mark_processing then coalesce(processing_at, :now) else processing_at end,
				processed_at = case when :mark_processed then coalesce(processed_at, :now) else processed_at end,
				delivery_worker_epoch = :epoch,
				last_delivery_update_at = :now,
				updated_at = :now
				where uuid = :uuid and deleted_at is null
			`, map[string]any{
			"uuid":            dbUUID(event.UUID),
			"target_status":   targetStatus,
			"mark_received":   rank >= codeSessionDeliveryStatusRank("received"),
			"mark_processing": rank >= codeSessionDeliveryStatusRank("processing"),
			"mark_processed":  rank >= codeSessionDeliveryStatusRank("processed"),
			"epoch":           epoch,
			"now":             now,
		}); err != nil {
			return CodeSessionWorkerDeliveryResult{}, err
		}
		result.Applied++
	}
	if result.Applied > 0 {
		if _, err := namedExecContext(ctx, tx, `
			update code_sessions
			set last_worker_activity_at = :now, updated_at = :now
				where uuid = :uuid and deleted_at is null
			`, map[string]any{
			"now":  now,
			"uuid": dbUUID(session.UUID),
		}); err != nil {
			return CodeSessionWorkerDeliveryResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CodeSessionWorkerDeliveryResult{}, err
	}
	return result, nil
}

func getCodeSessionInboundDeliveryEventTx(ctx context.Context, tx sqlxNamedQueryer, codeSessionUUID string, eventID string) (CodeSessionEvent, error) {
	arguments := map[string]any{
		"code_session_uuid": dbUUID(codeSessionUUID),
		"event_id":          eventID,
	}
	event, err := getCodeSessionEventSQLX(ctx, tx, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where code_session_uuid = :code_session_uuid
			and payload_uuid = :event_id
			and deleted_at is null
		order by sequence_num asc
		limit 1
		for update
	`, arguments)
	if err == nil {
		return event, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return CodeSessionEvent{}, err
	}

	return getCodeSessionEventSQLX(ctx, tx, `
		select `+codeSessionInboundEventColumns()+`
		from code_session_inbound_events
		where code_session_uuid = :code_session_uuid
			and external_id = :event_id
			and deleted_at is null
		limit 1
		for update
	`, arguments)
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
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update code_sessions
		set last_worker_activity_at = :now, updated_at = :now
		where external_id = :external_id
			and current_worker_epoch = :epoch
			and worker_lease_expires_at > :now
			and deleted_at is null
	`, map[string]any{"now": now, "external_id": codeSessionExternalID, "epoch": epoch})
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
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
		set last_worker_activity_at = :now, updated_at = :now
		where external_id = :external_id and deleted_at is null
	`
	arguments := map[string]any{"now": now, "external_id": codeSessionExternalID}
	if requiredEpoch != nil {
		query = `
			update code_sessions
			set last_worker_activity_at = :now, updated_at = :now
			where external_id = :external_id and current_worker_epoch = :epoch and deleted_at is null
		`
		arguments["epoch"] = *requiredEpoch
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, query, arguments)
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
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
		set connection_status = :status, last_worker_activity_at = :now, updated_at = :now
		where external_id = :external_id and deleted_at is null
	`
	arguments := map[string]any{"status": status, "now": now, "external_id": codeSessionExternalID}
	if connected {
		query = `
			update code_sessions
			set connection_status = :status, last_worker_connected_at = :now, last_worker_activity_at = :now, updated_at = :now
			where external_id = :external_id and deleted_at is null
		`
	}
	if requiredEpoch != nil {
		if connected {
			query = `
				update code_sessions
				set connection_status = :status, last_worker_connected_at = :now, last_worker_activity_at = :now, updated_at = :now
				where external_id = :external_id and current_worker_epoch = :epoch and deleted_at is null
			`
		} else {
			query = `
				update code_sessions
				set connection_status = :status, last_worker_activity_at = :now, updated_at = :now
				where external_id = :external_id and current_worker_epoch = :epoch and deleted_at is null
			`
		}
		arguments["epoch"] = *requiredEpoch
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, query, arguments)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return d.codeSessionWorkerEpochUpdateError(ctx, codeSessionExternalID, requiredEpoch)
	}
	return nil
}

func (d *DB) codeSessionWorkerEpochUpdateError(ctx context.Context, codeSessionExternalID string, requiredEpoch *int64) error {
	if requiredEpoch == nil {
		return ErrNotFound
	}

	var current int64
	err := namedGetContext(ctx, d.sql, &current, `
		select current_worker_epoch
		from code_sessions
		where external_id = :external_id and deleted_at is null
	`, map[string]any{"external_id": codeSessionExternalID})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrWorkerEpochMismatch
}

func codeSessionColumns() string {
	return `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		session_uuid, session_external_id,
		environment_uuid,
		environment_external_id, work_dir, permission_mode, model, status, metadata,
		connection_status, last_inbound_sequence_num, last_outbound_sequence_num, last_internal_sequence_num,
		last_worker_connected_at, last_worker_activity_at, current_worker_epoch, worker_lease_expires_at,
		worker_registered_at, worker_last_heartbeat_at, worker_token_session_id, worker_binding,
		worker_status, worker_external_metadata, worker_requires_action_details,
		created_at, updated_at, deleted_at`
}

func codeSessionInboundEventColumns() string {
	return `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		code_session_uuid, code_session_external_id,
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
	return prefix + `uuid AS uuid, ` + prefix + `external_id, ` + prefix + `organization_uuid AS organization_uuid, ` + prefix + `workspace_uuid AS workspace_uuid, ` + prefix + `code_session_uuid AS code_session_uuid, ` + prefix + `code_session_external_id,
		` + prefix + `sequence_num, ` + prefix + `event_type, ` + prefix + `event_subtype, ` + prefix + `payload_uuid, ` + prefix + `request_id, ` + prefix + `payload, ` + prefix + `payload_hash,
		` + prefix + `idempotency_key, ` + prefix + `delivery_status, ` + prefix + `source, ` + prefix + `sent_at, ` + prefix + `delivery_worker_epoch, ` + prefix + `received_at, ` + prefix + `processing_at,
		` + prefix + `processed_at, ` + prefix + `last_delivery_attempt_at, ` + prefix + `last_delivery_update_at, ` + prefix + `delivery_attempts,
		false as ephemeral, ` + prefix + `created_at, ` + prefix + `updated_at, ` + prefix + `deleted_at`
}

func codeSessionOutboundEventColumns() string {
	return `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		code_session_uuid, code_session_external_id,
		sequence_num, event_type, event_subtype, payload_uuid, request_id, payload, payload_hash,
		idempotency_key, CAST('' AS text) AS delivery_status, source, CAST(null AS timestamptz) AS sent_at,
		CAST(null AS bigint) AS delivery_worker_epoch, CAST(null AS timestamptz) AS received_at, CAST(null AS timestamptz) AS processing_at,
		CAST(null AS timestamptz) AS processed_at, CAST(null AS timestamptz) AS last_delivery_attempt_at,
		CAST(null AS timestamptz) AS last_delivery_update_at, CAST(0 AS integer) AS delivery_attempts,
		ephemeral, created_at, updated_at, deleted_at`
}

func codeSessionInternalEventColumns() string {
	return `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		code_session_uuid, code_session_external_id,
		sequence_num, event_type, payload_uuid, agent_id, is_compaction, payload, payload_hash,
		idempotency_key, event_metadata, created_at, updated_at, deleted_at`
}

func codeSessionInternalEventColumnsWithAlias(alias string) string {
	prefix := strings.TrimSpace(alias)
	if prefix != "" {
		prefix += "."
	}
	return prefix + `uuid AS uuid, ` + prefix + `external_id, ` + prefix + `organization_uuid AS organization_uuid, ` + prefix + `workspace_uuid AS workspace_uuid, ` + prefix + `code_session_uuid AS code_session_uuid, ` + prefix + `code_session_external_id,
		` + prefix + `sequence_num, ` + prefix + `event_type, ` + prefix + `payload_uuid, ` + prefix + `agent_id, ` + prefix + `is_compaction, ` + prefix + `payload, ` + prefix + `payload_hash,
		` + prefix + `idempotency_key, ` + prefix + `event_metadata, ` + prefix + `created_at, ` + prefix + `updated_at, ` + prefix + `deleted_at`
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
