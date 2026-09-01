package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper CodeSessionMapper -sql ./code_session_mapper.xml -out ./code_session_mapper.sqlmap.gen.go -dialect postgres

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

type createCodeSessionParams struct {
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
	Metadata              []byte
	OAuthAccessTokenHash  *string
	InitialWorkerEpoch    int64
	CreatedAt             time.Time
}

type registerCodeSessionWorkerParams struct {
	UUID                 string
	Epoch                int64
	ExpiresAt            time.Time
	Now                  time.Time
	WorkerTokenSessionID *string
	WorkerBinding        []byte
}

type heartbeatCodeSessionWorkerParams struct {
	ExternalID string
	UUID       string
	Epoch      int64
	Now        time.Time
	ExpiresAt  time.Time
}

type updateCodeSessionWorkerStateParams struct {
	UUID                  string
	WorkerStatus          string
	RequiresActionDetails []byte
	ExternalMetadata      []byte
	Now                   time.Time
}

type updateCodeSessionConnectionParams struct {
	ExternalID    string
	Status        string
	Connected     bool
	RequiredEpoch *int64
	Now           time.Time
}

type rotateCodeSessionCredentialsParams struct {
	OrganizationUUID      string
	WorkspaceUUID         string
	SessionExternalID     string
	CodeSessionExternalID string
	OAuthAccessTokenHash  string
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

type codeSessionVaultIDsRow struct {
	VaultIDs []byte `db:"vault_ids"`
}

type codeSessionWorkerLeaseRow struct {
	UUID                 string       `db:"uuid"`
	CurrentWorkerEpoch   int64        `db:"current_worker_epoch"`
	WorkerLeaseExpiresAt sql.NullTime `db:"worker_lease_expires_at"`
}

type codeSessionWorkerExpiryRow struct {
	WorkerLeaseExpiresAt time.Time `db:"worker_lease_expires_at"`
}

type resumeCodeSessionWorkerLeaseParams struct {
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionExternalID string
	ProviderSandboxID     string
	ExpiresAt             time.Time
	Now                   time.Time
}

// CodeSessionMapper contains queries whose primary table is code_sessions.
type CodeSessionMapper interface {
	ResetIdleSinceForSession(ctx context.Context, organizationUUID, workspaceUUID, sessionUUID string) error
	Insert(ctx context.Context, params createCodeSessionParams) (codeSessionRow, error)
	FindCredentialByOAuthAccessTokenHash(ctx context.Context, tokenHash string) (codeSessionCredentialContextRow, error)
	FindCredentialForIssue(ctx context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string) (codeSessionCredentialContextRow, error)
	FindNetworkPolicyContext(ctx context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string) (codeSessionNetworkPolicyContextRow, error)
	FindVaultIDs(ctx context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string) (codeSessionVaultIDsRow, bool, error)
	FindByExternalID(ctx context.Context, codeSessionExternalID string) (codeSessionRow, bool, error)
	FindActiveForEnvironmentWork(ctx context.Context, organizationUUID, workspaceUUID, environmentUUID, sessionUUID string) ([]codeSessionRow, error)
	FindLatestBySessionExternalID(ctx context.Context, workspaceUUID, sessionExternalID string) (codeSessionRow, error)
	LockCodeSessionByExternalID(ctx context.Context, codeSessionExternalID string) (codeSessionRow, bool, error)
	LockInitializingCodeSession(ctx context.Context, workspaceUUID, codeSessionUUID string) (codeSessionRow, bool, error)
	LockWorkerLeaseByExternalID(ctx context.Context, codeSessionExternalID string) (codeSessionWorkerLeaseRow, error)
	FindCurrentWorkerEpochByExternalID(ctx context.Context, codeSessionExternalID string) (int64, error)
	RegisterWorker(ctx context.Context, params registerCodeSessionWorkerParams) (int64, error)
	HeartbeatWorkerByExternalID(ctx context.Context, params heartbeatCodeSessionWorkerParams) (codeSessionWorkerExpiryRow, error)
	HeartbeatWorkerByUUID(ctx context.Context, params heartbeatCodeSessionWorkerParams) (codeSessionWorkerExpiryRow, error)
	ResumeWorkerLeaseForSandbox(ctx context.Context, params resumeCodeSessionWorkerLeaseParams) (int64, error)
	UpdateWorkerState(ctx context.Context, params updateCodeSessionWorkerStateParams) (codeSessionRow, error)
	UpdateCodeSessionInboundSequence(ctx context.Context, codeSessionUUID string, sequenceNum int64, now time.Time) (int64, error)
	UpdateCodeSessionInternalSequence(ctx context.Context, codeSessionUUID string, sequenceNum int64, now time.Time) error
	ActivateCodeSession(ctx context.Context, codeSessionUUID string, now time.Time) (int64, error)
	TouchWorkerActivityByUUID(ctx context.Context, codeSessionUUID string, now time.Time) error
	TouchWorkerActivityForActiveLease(ctx context.Context, codeSessionExternalID string, epoch int64, now time.Time) (int64, error)
	TouchWorkerActivity(ctx context.Context, codeSessionExternalID string, requiredEpoch *int64, now time.Time) (int64, error)
	UpdateConnection(ctx context.Context, params updateCodeSessionConnectionParams) (int64, error)
	CountActiveIngressWorkerEpoch(ctx context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string, workerEpoch int64) (int64, error)
	RotateCredentials(ctx context.Context, params rotateCodeSessionCredentialsParams) (int64, error)
	TerminateByExternalID(ctx context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string) (int64, error)
}

func (r codeSessionRow) session() CodeSession {
	workerExternalMetadata := bytes.Clone(r.WorkerExternalMetadata)
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
		Metadata:                    bytes.Clone(r.Metadata),
		ConnectionStatus:            r.ConnectionStatus,
		LastInboundSequenceNum:      r.LastInboundSequenceNum,
		LastInternalSequenceNum:     r.LastInternalSequenceNum,
		LastWorkerConnectedAt:       r.LastWorkerConnectedAt,
		LastWorkerActivityAt:        r.LastWorkerActivityAt,
		CurrentWorkerEpoch:          r.CurrentWorkerEpoch,
		WorkerLeaseExpiresAt:        r.WorkerLeaseExpiresAt,
		WorkerRegisteredAt:          r.WorkerRegisteredAt,
		WorkerLastHeartbeatAt:       r.WorkerLastHeartbeatAt,
		WorkerTokenSessionID:        r.WorkerTokenSessionID,
		WorkerBinding:               bytes.Clone(r.WorkerBinding),
		WorkerStatus:                r.WorkerStatus,
		WorkerExternalMetadata:      workerExternalMetadata,
		WorkerRequiresActionDetails: bytes.Clone(r.WorkerRequiresActionDetails),
		CreatedAt:                   r.CreatedAt,
		UpdatedAt:                   r.UpdatedAt,
		DeletedAt:                   r.DeletedAt,
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
