package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	codeSessionSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id, workspace_id,
		session_id, session_external_id, environment_id, environment_external_id, work_dir,
		permission_mode, model, status, metadata, connection_status, last_inbound_sequence_num,
		last_outbound_sequence_num, last_internal_sequence_num, last_worker_connected_at,
		last_worker_activity_at, current_worker_epoch, worker_lease_expires_at,
		worker_registered_at, worker_last_heartbeat_at, worker_token_session_id, worker_binding,
		worker_status, worker_external_metadata, worker_requires_action_details,
		created_at, updated_at, deleted_at`
	createCodeSessionQuery = `
		insert into code_sessions (
			external_id, organization_id, workspace_id, session_id, session_external_id,
			environment_id, environment_external_id, work_dir, permission_mode, model,
			status, metadata, oauth_access_token_hash, created_at, updated_at
		)
		values (
			:code_session_external_id, :organization_id, :workspace_id, :session_id, :session_external_id,
			:environment_id, :environment_external_id, :work_dir, :permission_mode, :model,
			:status, CAST(:metadata AS jsonb), :oauth_access_token_hash, :created_at, :created_at
		)
		returning ` + codeSessionSQLXColumns + `
	`
	codeSessionCredentialContextForIssueQuery = codeSessionCredentialContextSelect + `
		where cs.external_id = :code_session_external_id
			and cs.organization_id = :organization_id
			and cs.workspace_id = :workspace_id
	` + activeCodeSessionCredentialConditions
	// insertCodeSessionInboundEventQuery 复用 AppendCodeSessionInboundEvent 的
	// workspace 幂等键：同一 workspace 内重复的 idempotency_key 不再入队。
	insertCodeSessionInboundEventQuery = `
		insert into code_session_inbound_events (
			external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
			sequence_num, event_type, event_subtype, payload_uuid, request_id, payload,
			payload_hash, idempotency_key, delivery_status, source, created_at, updated_at
		)
		values (
			:event_external_id, :organization_id, :workspace_id, :code_session_id, :code_session_external_id,
			:sequence_num, :event_type, :event_subtype, :payload_uuid, :request_id, CAST(:payload AS jsonb),
			:payload_hash, :idempotency_key, :delivery_status, :source, :created_at, :created_at
		)
		on conflict (workspace_id, idempotency_key)
			where deleted_at is null and idempotency_key <> ''
			do nothing
	`
	updateCodeSessionInboundSequenceQuery = `
		update code_sessions
		set last_inbound_sequence_num = :sequence_num, updated_at = now()
		where id = :code_session_id
	`
)

// codeSessionRow 是 code_sessions 的 sqlx 扫描边界。JSONB 列以 []byte 落地，
// 由 codeSession() 转换成领域模型持有的 json.RawMessage。
type codeSessionRow struct {
	ID                          int64      `db:"id"`
	UUID                        string     `db:"uuid"`
	ExternalID                  string     `db:"external_id"`
	OrganizationID              int64      `db:"organization_id"`
	WorkspaceID                 int64      `db:"workspace_id"`
	SessionID                   int64      `db:"session_id"`
	SessionExternalID           string     `db:"session_external_id"`
	EnvironmentID               int64      `db:"environment_id"`
	EnvironmentExternalID       string     `db:"environment_external_id"`
	WorkDir                     string     `db:"work_dir"`
	PermissionMode              string     `db:"permission_mode"`
	Model                       string     `db:"model"`
	Status                      string     `db:"status"`
	Metadata                    []byte     `db:"metadata"`
	ConnectionStatus            string     `db:"connection_status"`
	LastInboundSequenceNum      int64      `db:"last_inbound_sequence_num"`
	LastOutboundSequenceNum     int64      `db:"last_outbound_sequence_num"`
	LastInternalSequenceNum     int64      `db:"last_internal_sequence_num"`
	LastWorkerConnectedAt       *time.Time `db:"last_worker_connected_at"`
	LastWorkerActivityAt        *time.Time `db:"last_worker_activity_at"`
	CurrentWorkerEpoch          int64      `db:"current_worker_epoch"`
	WorkerLeaseExpiresAt        *time.Time `db:"worker_lease_expires_at"`
	WorkerRegisteredAt          *time.Time `db:"worker_registered_at"`
	WorkerLastHeartbeatAt       *time.Time `db:"worker_last_heartbeat_at"`
	WorkerTokenSessionID        *string    `db:"worker_token_session_id"`
	WorkerBinding               []byte     `db:"worker_binding"`
	WorkerStatus                string     `db:"worker_status"`
	WorkerExternalMetadata      []byte     `db:"worker_external_metadata"`
	WorkerRequiresActionDetails []byte     `db:"worker_requires_action_details"`
	CreatedAt                   time.Time  `db:"created_at"`
	UpdatedAt                   time.Time  `db:"updated_at"`
	DeletedAt                   *time.Time `db:"deleted_at"`
}

// codeSessionCredentialContextRow 与 codeSessionCredentialContextSelect 的列别名
// 一一对应，使 OAuth 鉴权与 JWT 签发共用同一份身份投影。
type codeSessionCredentialContextRow struct {
	CodeSessionID           int64  `db:"code_session_id"`
	CodeSessionExternalID   string `db:"code_session_external_id"`
	OrganizationID          int64  `db:"organization_id"`
	OrganizationUUID        string `db:"organization_uuid"`
	OrganizationExternalID  string `db:"organization_external_id"`
	WorkspaceID             int64  `db:"workspace_id"`
	WorkspaceUUID           string `db:"workspace_uuid"`
	WorkspaceExternalID     string `db:"workspace_external_id"`
	PublicSessionID         int64  `db:"public_session_id"`
	PublicSessionExternalID string `db:"public_session_external_id"`
	AgentID                 int64  `db:"agent_id"`
	AgentExternalID         string `db:"agent_external_id"`
	AgentVersion            int    `db:"agent_version"`
	AccountEmail            string `db:"account_email"`
}

func insertCodeSessionSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	input CreateCodeSessionInput,
) (CodeSession, error) {
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}
	if input.Status == "" {
		input.Status = "active"
	}
	var row codeSessionRow
	if err := namedGetContext(ctx, database, &row, createCodeSessionQuery, map[string]any{
		"code_session_external_id": input.ExternalID,
		"organization_id":          input.OrganizationID,
		"workspace_id":             input.WorkspaceID,
		"session_id":               input.SessionID,
		"session_external_id":      input.SessionExternalID,
		"environment_id":           input.EnvironmentID,
		"environment_external_id":  input.EnvironmentExternalID,
		"work_dir":                 input.WorkDir,
		"permission_mode":          input.PermissionMode,
		"model":                    input.Model,
		"status":                   input.Status,
		"metadata":                 jsonArg(input.Metadata),
		"oauth_access_token_hash":  nullableString(strings.TrimSpace(input.OAuthAccessTokenHash)),
		"created_at":               input.CreatedAt,
	}); err != nil {
		return CodeSession{}, err
	}
	return row.codeSession(), nil
}

func getCodeSessionCredentialContextForIssueSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	organizationID int64,
	workspaceID int64,
	codeSessionExternalID string,
) (CodeSessionCredentialContext, error) {
	var row codeSessionCredentialContextRow
	if err := namedGetContext(ctx, database, &row, codeSessionCredentialContextForIssueQuery, map[string]any{
		"code_session_external_id": strings.TrimSpace(codeSessionExternalID),
		"organization_id":          organizationID,
		"workspace_id":             workspaceID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CodeSessionCredentialContext{}, ErrNotFound
		}
		return CodeSessionCredentialContext{}, err
	}
	return row.credentialContext(), nil
}

// appendCodeSessionInboundEventsSQLX 按顺序为 inputs 分配 inbound sequence，并在
// 至少写入一条时推进 code session 的 last_inbound_sequence_num。命中 workspace
// 幂等键的输入被跳过且不消耗 sequence，因此返回的序号始终连续。
func appendCodeSessionInboundEventsSQLX(
	ctx context.Context,
	database sqlxNamedExecer,
	session CodeSession,
	inputs []AppendCodeSessionEventInput,
) (int64, error) {
	sequence := session.LastInboundSequenceNum
	for _, input := range inputs {
		if input.RequiredWorkerEpoch != nil {
			return sequence, ErrWorkerEpochMismatch
		}
		createdAt := input.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		inserted, err := namedExecRowsAffected(ctx, database, insertCodeSessionInboundEventQuery, map[string]any{
			"event_external_id":        input.ExternalID,
			"organization_id":          session.OrganizationID,
			"workspace_id":             session.WorkspaceID,
			"code_session_id":          session.ID,
			"code_session_external_id": session.ExternalID,
			"sequence_num":             sequence + 1,
			"event_type":               input.EventType,
			"event_subtype":            input.EventSubtype,
			"payload_uuid":             input.PayloadUUID,
			"request_id":               input.RequestID,
			"payload":                  jsonArg(input.Payload),
			"payload_hash":             input.PayloadHash,
			"idempotency_key":          input.IdempotencyKey,
			"delivery_status":          input.DeliveryStatus,
			"source":                   input.Source,
			"created_at":               createdAt,
		})
		if err != nil {
			return sequence, err
		}
		if inserted == 0 {
			continue
		}
		sequence++
	}
	if sequence == session.LastInboundSequenceNum {
		return sequence, nil
	}
	updated, err := namedExecRowsAffected(ctx, database, updateCodeSessionInboundSequenceQuery, map[string]any{
		"sequence_num":    sequence,
		"code_session_id": session.ID,
	})
	if err != nil {
		return sequence, err
	}
	if updated != 1 {
		return sequence, errors.New("update managed agent code session sequence")
	}
	return sequence, nil
}

func (r codeSessionRow) codeSession() CodeSession {
	workerExternalMetadata := copyRaw(r.WorkerExternalMetadata)
	if len(workerExternalMetadata) == 0 {
		workerExternalMetadata = json.RawMessage(`{}`)
	}
	return CodeSession{
		ID:                          r.ID,
		UUID:                        r.UUID,
		ExternalID:                  r.ExternalID,
		OrganizationID:              r.OrganizationID,
		WorkspaceID:                 r.WorkspaceID,
		SessionID:                   r.SessionID,
		SessionExternalID:           r.SessionExternalID,
		EnvironmentID:               r.EnvironmentID,
		EnvironmentExternalID:       r.EnvironmentExternalID,
		WorkDir:                     r.WorkDir,
		PermissionMode:              r.PermissionMode,
		Model:                       r.Model,
		Status:                      r.Status,
		Metadata:                    copyRaw(r.Metadata),
		ConnectionStatus:            r.ConnectionStatus,
		LastInboundSequenceNum:      r.LastInboundSequenceNum,
		LastOutboundSequenceNum:     r.LastOutboundSequenceNum,
		LastInternalSequenceNum:     r.LastInternalSequenceNum,
		LastWorkerConnectedAt:       r.LastWorkerConnectedAt,
		LastWorkerActivityAt:        r.LastWorkerActivityAt,
		CurrentWorkerEpoch:          r.CurrentWorkerEpoch,
		WorkerLeaseExpiresAt:        r.WorkerLeaseExpiresAt,
		WorkerRegisteredAt:          r.WorkerRegisteredAt,
		WorkerLastHeartbeatAt:       r.WorkerLastHeartbeatAt,
		WorkerTokenSessionID:        r.WorkerTokenSessionID,
		WorkerBinding:               copyRaw(r.WorkerBinding),
		WorkerStatus:                r.WorkerStatus,
		WorkerExternalMetadata:      workerExternalMetadata,
		WorkerRequiresActionDetails: copyRaw(r.WorkerRequiresActionDetails),
		CreatedAt:                   r.CreatedAt,
		UpdatedAt:                   r.UpdatedAt,
		DeletedAt:                   r.DeletedAt,
	}
}

func (r codeSessionCredentialContextRow) credentialContext() CodeSessionCredentialContext {
	return CodeSessionCredentialContext{
		CodeSessionID:           r.CodeSessionID,
		CodeSessionExternalID:   r.CodeSessionExternalID,
		OrganizationID:          r.OrganizationID,
		OrganizationUUID:        r.OrganizationUUID,
		OrganizationExternalID:  r.OrganizationExternalID,
		WorkspaceID:             r.WorkspaceID,
		WorkspaceUUID:           r.WorkspaceUUID,
		WorkspaceExternalID:     r.WorkspaceExternalID,
		PublicSessionID:         r.PublicSessionID,
		PublicSessionExternalID: r.PublicSessionExternalID,
		AgentID:                 r.AgentID,
		AgentExternalID:         r.AgentExternalID,
		AgentVersion:            r.AgentVersion,
		AccountEmail:            r.AccountEmail,
	}
}
