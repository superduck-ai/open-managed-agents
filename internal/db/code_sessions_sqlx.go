package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type codeSessionRow struct {
	UUID                        string     `db:"uuid"`
	ExternalID                  string     `db:"external_id"`
	OrganizationUUID            string     `db:"organization_uuid"`
	WorkspaceUUID               string     `db:"workspace_uuid"`
	SessionUUID                 string     `db:"session_uuid"`
	SessionExternalID           string     `db:"session_external_id"`
	EnvironmentUUID             string     `db:"environment_uuid"`
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
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	CodeSessionUUID       string     `db:"code_session_uuid"`
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
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	CodeSessionUUID       string     `db:"code_session_uuid"`
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
	CodeSessionUUID         string `db:"code_session_uuid"`
	CodeSessionExternalID   string `db:"code_session_external_id"`
	OrganizationUUID        string `db:"organization_uuid"`
	WorkspaceUUID           string `db:"workspace_uuid"`
	WorkspaceExternalID     string `db:"workspace_external_id"`
	PublicSessionUUID       string `db:"public_session_uuid"`
	PublicSessionExternalID string `db:"public_session_external_id"`
	AgentUUID               string `db:"agent_uuid"`
	AgentExternalID         string `db:"agent_external_id"`
	AgentVersion            int    `db:"agent_version"`
	AccountEmail            string `db:"account_email"`
}

type codeSessionNetworkPolicyContextRow struct {
	OrganizationUUID      string `db:"organization_uuid"`
	WorkspaceUUID         string `db:"workspace_uuid"`
	EnvironmentExternalID string `db:"environment_external_id"`
	EnvironmentConfig     []byte `db:"environment_config"`
	AgentSnapshot         []byte `db:"agent_snapshot"`
}

type codeSessionWorkerLeaseRow struct {
	UUID                 string       `db:"uuid"`
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
		UUID:                        r.UUID,
		ExternalID:                  r.ExternalID,
		OrganizationUUID:            r.OrganizationUUID,
		WorkspaceUUID:               r.WorkspaceUUID,
		SessionUUID:                 r.SessionUUID,
		SessionExternalID:           r.SessionExternalID,
		EnvironmentUUID:             r.EnvironmentUUID,
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
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID,
		WorkspaceUUID:         r.WorkspaceUUID,
		CodeSessionUUID:       r.CodeSessionUUID,
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
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID,
		WorkspaceUUID:         r.WorkspaceUUID,
		CodeSessionUUID:       r.CodeSessionUUID,
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
		CodeSessionUUID:         r.CodeSessionUUID,
		CodeSessionExternalID:   r.CodeSessionExternalID,
		OrganizationUUID:        r.OrganizationUUID,
		WorkspaceUUID:           r.WorkspaceUUID,
		WorkspaceExternalID:     r.WorkspaceExternalID,
		PublicSessionUUID:       r.PublicSessionUUID,
		PublicSessionExternalID: r.PublicSessionExternalID,
		AgentUUID:               r.AgentUUID,
		AgentExternalID:         r.AgentExternalID,
		AgentVersion:            r.AgentVersion,
		AccountEmail:            r.AccountEmail,
	}
}
