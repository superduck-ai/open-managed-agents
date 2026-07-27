package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// createCodeSessionQuery / insertCodeSessionInboundEventQuery 同时服务
// CreateCodeSession、Managed Agent 原子启动，以及命名参数绑定测试。
var createCodeSessionQuery = `
		insert into code_sessions (
			external_id, organization_id, workspace_id, session_id, session_external_id,
			environment_id, environment_external_id, work_dir, permission_mode, model,
			status, metadata, oauth_access_token_hash, created_at, updated_at
		)
		values (
			:external_id, :organization_id, :workspace_id, :session_id,
			:session_external_id, :environment_id, :environment_external_id,
			:work_dir, :permission_mode, :model, :status, CAST(:metadata AS jsonb),
			:oauth_access_token_hash, :created_at, :created_at
		)
		returning ` + codeSessionColumns()

// insertCodeSessionInboundEventQuery 复用 AppendCodeSessionInboundEvent 的
// workspace 幂等键：同一 workspace 内重复的 idempotency_key 不再入队。
const insertCodeSessionInboundEventQuery = `
		insert into code_session_inbound_events (
			external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
			sequence_num, event_type, event_subtype, payload_uuid, request_id, payload,
			payload_hash, idempotency_key, delivery_status, source, created_at, updated_at
		)
		values (
			:external_id, :organization_id, :workspace_id, :code_session_id, :code_session_external_id,
			:sequence_num, :event_type, :event_subtype, :payload_uuid, :request_id, CAST(:payload AS jsonb),
			:payload_hash, :idempotency_key, :delivery_status, :source, :created_at, :created_at
		)
		on conflict (workspace_id, idempotency_key)
			where deleted_at is null and idempotency_key <> ''
			do nothing
	`

const updateCodeSessionInboundSequenceQuery = `
		update code_sessions
		set last_inbound_sequence_num = :sequence_num, updated_at = now()
		where id = :code_session_id
	`

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

type codeSessionEventRow struct {
	ID                    int64      `db:"id"`
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationID        int64      `db:"organization_id"`
	WorkspaceID           int64      `db:"workspace_id"`
	CodeSessionID         int64      `db:"code_session_id"`
	CodeSessionExternalID string     `db:"code_session_external_id"`
	SequenceNum           int64      `db:"sequence_num"`
	EventType             string     `db:"event_type"`
	EventSubtype          string     `db:"event_subtype"`
	PayloadUUID           *string    `db:"payload_uuid"`
	RequestID             *string    `db:"request_id"`
	Payload               []byte     `db:"payload"`
	PayloadHash           string     `db:"payload_hash"`
	IdempotencyKey        string     `db:"idempotency_key"`
	DeliveryStatus        string     `db:"delivery_status"`
	Source                string     `db:"source"`
	SentAt                *time.Time `db:"sent_at"`
	DeliveryWorkerEpoch   *int64     `db:"delivery_worker_epoch"`
	ReceivedAt            *time.Time `db:"received_at"`
	ProcessingAt          *time.Time `db:"processing_at"`
	ProcessedAt           *time.Time `db:"processed_at"`
	LastDeliveryAttemptAt *time.Time `db:"last_delivery_attempt_at"`
	LastDeliveryUpdateAt  *time.Time `db:"last_delivery_update_at"`
	DeliveryAttempts      int        `db:"delivery_attempts"`
	Ephemeral             bool       `db:"ephemeral"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

type codeSessionInternalEventRow struct {
	ID                    int64      `db:"id"`
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationID        int64      `db:"organization_id"`
	WorkspaceID           int64      `db:"workspace_id"`
	CodeSessionID         int64      `db:"code_session_id"`
	CodeSessionExternalID string     `db:"code_session_external_id"`
	SequenceNum           int64      `db:"sequence_num"`
	EventType             string     `db:"event_type"`
	PayloadUUID           string     `db:"payload_uuid"`
	AgentID               *string    `db:"agent_id"`
	IsCompaction          bool       `db:"is_compaction"`
	Payload               []byte     `db:"payload"`
	PayloadHash           string     `db:"payload_hash"`
	IdempotencyKey        string     `db:"idempotency_key"`
	EventMetadata         []byte     `db:"event_metadata"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

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

type codeSessionNetworkPolicyContextRow struct {
	OrganizationID        int64  `db:"organization_id"`
	WorkspaceID           int64  `db:"workspace_id"`
	EnvironmentExternalID string `db:"environment_external_id"`
	EnvironmentConfig     []byte `db:"environment_config"`
	AgentSnapshot         []byte `db:"agent_snapshot"`
}

type codeSessionWorkerLeaseRow struct {
	ID                   int64        `db:"id"`
	CurrentWorkerEpoch   int64        `db:"current_worker_epoch"`
	WorkerLeaseExpiresAt sql.NullTime `db:"worker_lease_expires_at"`
}

func getCodeSessionSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (CodeSession, error) {
	var row codeSessionRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CodeSession{}, ErrNotFound
		}
		return CodeSession{}, err
	}
	return row.session(), nil
}

func insertCodeSessionSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	input CreateCodeSessionInput,
) (CodeSession, error) {
	now := input.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := input.Status
	if status == "" {
		status = "active"
	}
	return getCodeSessionSQLX(ctx, database, createCodeSessionQuery, map[string]any{
		"external_id":             input.ExternalID,
		"organization_id":         input.OrganizationID,
		"workspace_id":            input.WorkspaceID,
		"session_id":              input.SessionID,
		"session_external_id":     input.SessionExternalID,
		"environment_id":          input.EnvironmentID,
		"environment_external_id": input.EnvironmentExternalID,
		"work_dir":                input.WorkDir,
		"permission_mode":         input.PermissionMode,
		"model":                   input.Model,
		"status":                  status,
		"metadata":                jsonArg(input.Metadata),
		"oauth_access_token_hash": nullableString(strings.TrimSpace(input.OAuthAccessTokenHash)),
		"created_at":              now,
	})
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
		deliveryStatus := input.DeliveryStatus
		if deliveryStatus == "" {
			deliveryStatus = "queued"
		}
		inserted, err := namedExecRowsAffected(ctx, database, insertCodeSessionInboundEventQuery, map[string]any{
			"external_id":              input.ExternalID,
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
			"delivery_status":          deliveryStatus,
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

func getCodeSessionEventSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (CodeSessionEvent, error) {
	var row codeSessionEventRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CodeSessionEvent{}, ErrNotFound
		}
		return CodeSessionEvent{}, err
	}
	return row.event(), nil
}

func selectCodeSessionEventsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]CodeSessionEvent, error) {
	var rows []codeSessionEventRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	events := make([]CodeSessionEvent, len(rows))
	for index := range rows {
		events[index] = rows[index].event()
	}
	return events, nil
}

func getCodeSessionInternalEventSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (CodeSessionInternalEvent, error) {
	var row codeSessionInternalEventRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CodeSessionInternalEvent{}, ErrNotFound
		}
		return CodeSessionInternalEvent{}, err
	}
	return row.event(), nil
}

func selectCodeSessionInternalEventsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]CodeSessionInternalEvent, error) {
	var rows []codeSessionInternalEventRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	events := make([]CodeSessionInternalEvent, len(rows))
	for index := range rows {
		events[index] = rows[index].event()
	}
	return events, nil
}

func getCodeSessionCredentialContextSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (CodeSessionCredentialContext, error) {
	var row codeSessionCredentialContextRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CodeSessionCredentialContext{}, ErrNotFound
		}
		return CodeSessionCredentialContext{}, err
	}
	return row.context(), nil
}

func (r codeSessionRow) session() CodeSession {
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

func (r codeSessionEventRow) event() CodeSessionEvent {
	return CodeSessionEvent{
		ID:                    r.ID,
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationID:        r.OrganizationID,
		WorkspaceID:           r.WorkspaceID,
		CodeSessionID:         r.CodeSessionID,
		CodeSessionExternalID: r.CodeSessionExternalID,
		SequenceNum:           r.SequenceNum,
		EventType:             r.EventType,
		EventSubtype:          r.EventSubtype,
		PayloadUUID:           r.PayloadUUID,
		RequestID:             r.RequestID,
		Payload:               copyRaw(r.Payload),
		PayloadHash:           r.PayloadHash,
		IdempotencyKey:        r.IdempotencyKey,
		DeliveryStatus:        r.DeliveryStatus,
		Source:                r.Source,
		SentAt:                r.SentAt,
		DeliveryWorkerEpoch:   r.DeliveryWorkerEpoch,
		ReceivedAt:            r.ReceivedAt,
		ProcessingAt:          r.ProcessingAt,
		ProcessedAt:           r.ProcessedAt,
		LastDeliveryAttemptAt: r.LastDeliveryAttemptAt,
		LastDeliveryUpdateAt:  r.LastDeliveryUpdateAt,
		DeliveryAttempts:      r.DeliveryAttempts,
		Ephemeral:             r.Ephemeral,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		DeletedAt:             r.DeletedAt,
	}
}

func (r codeSessionInternalEventRow) event() CodeSessionInternalEvent {
	return CodeSessionInternalEvent{
		ID:                    r.ID,
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationID:        r.OrganizationID,
		WorkspaceID:           r.WorkspaceID,
		CodeSessionID:         r.CodeSessionID,
		CodeSessionExternalID: r.CodeSessionExternalID,
		SequenceNum:           r.SequenceNum,
		EventType:             r.EventType,
		PayloadUUID:           r.PayloadUUID,
		AgentID:               r.AgentID,
		IsCompaction:          r.IsCompaction,
		Payload:               copyRaw(r.Payload),
		PayloadHash:           r.PayloadHash,
		IdempotencyKey:        r.IdempotencyKey,
		EventMetadata:         copyRaw(r.EventMetadata),
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		DeletedAt:             r.DeletedAt,
	}
}

func (r codeSessionCredentialContextRow) context() CodeSessionCredentialContext {
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
