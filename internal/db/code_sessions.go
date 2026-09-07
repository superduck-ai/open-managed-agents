package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/yourbatis"
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
	InitialWorkerEpoch    int64
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

func optionalCodeSessionString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

type CodeSessionEvent struct {
	ExternalID            string
	CodeSessionExternalID string
	SequenceNum           int64
	EventType             string
	EventSubtype          string
	PayloadUUID           *string
	RequestID             *string
	Payload               json.RawMessage
	PayloadHash           string
	IdempotencyKey        string
	Source                string
	CreatedAt             time.Time
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
	oauthAccessTokenHash := optionalCodeSessionString(input.OAuthAccessTokenHash)
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, createCodeSessionParams{
		ExternalID:            input.ExternalID,
		OrganizationUUID:      input.OrganizationUUID,
		WorkspaceUUID:         input.WorkspaceUUID,
		SessionUUID:           input.SessionUUID,
		SessionExternalID:     input.SessionExternalID,
		EnvironmentUUID:       input.EnvironmentUUID,
		EnvironmentExternalID: input.EnvironmentExternalID,
		WorkDir:               input.WorkDir,
		PermissionMode:        input.PermissionMode,
		Model:                 input.Model,
		Status:                status,
		Metadata:              input.Metadata,
		OAuthAccessTokenHash:  oauthAccessTokenHash,
		InitialWorkerEpoch:    input.InitialWorkerEpoch,
		CreatedAt:             now,
	})
	if err != nil {
		return CodeSession{}, err
	}
	return row.session(), nil
}

func (tx ManagedAgentActivationTx) LockInitializingCodeSession(
	ctx context.Context,
	workspaceUUID string,
	codeSessionUUID string,
) (CodeSession, error) {
	row, found, err := tx.codeSessionMapper.LockInitializingCodeSession(ctx, workspaceUUID, codeSessionUUID)
	if err != nil {
		return CodeSession{}, err
	}
	if !found {
		return CodeSession{}, ErrNotFound
	}
	return row.session(), nil
}

func (tx ManagedAgentActivationTx) ActivateCodeSession(
	ctx context.Context,
	codeSessionUUID string,
	now time.Time,
) (bool, error) {
	updated, err := tx.codeSessionMapper.ActivateCodeSession(ctx, codeSessionUUID, now)
	if err != nil {
		return false, err
	}
	return updated == 1, nil
}

// GetCodeSessionByOAuthAccessTokenHash 只返回 session 与 CCR worker lease 仍存活的凭证上下文。
func (d *DB) GetCodeSessionByOAuthAccessTokenHash(ctx context.Context, tokenHash string) (CodeSessionCredentialContext, error) {
	// 调用方只传 SHA-256 hash，明文 OAuth-compatible token 不进入数据库边界。
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, err := mapper.FindCredentialByOAuthAccessTokenHash(ctx, strings.TrimSpace(tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return CodeSessionCredentialContext{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionCredentialContext{}, err
	}
	return row.context(), nil
}

// GetCodeSessionCredentialContextForIssue 用于初始 session-ingress JWT 签发，并将查询绑定到预期租户。
func (d *DB) GetCodeSessionCredentialContextForIssue(ctx context.Context, organizationUUID, workspaceUUID string, codeSessionExternalID string) (CodeSessionCredentialContext, error) {
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, err := mapper.FindCredentialForIssue(
		ctx,
		strings.TrimSpace(organizationUUID),
		strings.TrimSpace(workspaceUUID),
		strings.TrimSpace(codeSessionExternalID),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CodeSessionCredentialContext{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionCredentialContext{}, err
	}
	return row.context(), nil
}

func (d *DB) ValidateCodeSessionIngressWorkerEpoch(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	codeSessionExternalID string,
	workerEpoch int64,
) error {
	mapper := NewCodeSessionMapper(d.mapperDB)
	count, err := mapper.CountActiveIngressWorkerEpoch(
		ctx,
		organizationUUID,
		workspaceUUID,
		codeSessionExternalID,
		workerEpoch,
	)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
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
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, err := mapper.FindNetworkPolicyContext(
		ctx,
		strings.TrimSpace(organizationUUID),
		strings.TrimSpace(workspaceUUID),
		strings.TrimSpace(codeSessionExternalID),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CodeSessionNetworkPolicyContext{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionNetworkPolicyContext{}, err
	}
	return CodeSessionNetworkPolicyContext{
		OrganizationUUID:      row.OrganizationUUID,
		WorkspaceUUID:         row.WorkspaceUUID,
		EnvironmentExternalID: row.EnvironmentExternalID,
		EnvironmentConfig:     bytes.Clone(row.EnvironmentConfig),
		AgentSnapshot:         bytes.Clone(row.AgentSnapshot),
	}, nil
}

// GetCodeSessionVaultIDs loads parent-session vault_ids for an active code session
// scoped to the authenticated organization/workspace.
func (d *DB) GetCodeSessionVaultIDs(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
) ([]string, error) {
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, found, err := mapper.FindVaultIDs(
		ctx,
		organizationUUID,
		workspaceUUID,
		codeSessionExternalID,
	)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	return decodeVaultIDList(row.VaultIDs)
}

func decodeVaultIDList(raw []byte) ([]string, error) {
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("decode vault_ids: %w", err)
	}
	if ids == nil {
		return nil, errors.New("decode vault_ids: expected an array")
	}
	for _, id := range ids {
		if id == "" || strings.TrimSpace(id) != id {
			return nil, fmt.Errorf("decode vault_ids: invalid ID %q", id)
		}
	}
	return ids, nil
}

func (d *DB) GetCodeSession(ctx context.Context, externalID string) (CodeSession, bool, error) {
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, found, err := mapper.FindByExternalID(ctx, externalID)
	if err != nil || !found {
		return CodeSession{}, found, err
	}
	return row.session(), true, nil
}

func (d *DB) GetCodeSessionBySessionExternalID(ctx context.Context, workspaceUUID string, sessionExternalID string) (CodeSession, error) {
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, err := mapper.FindLatestBySessionExternalID(ctx, workspaceUUID, sessionExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return CodeSession{}, ErrNotFound
	}
	if err != nil {
		return CodeSession{}, err
	}
	return row.session(), nil
}

func (d *DB) GetActiveCodeSessionForEnvironmentWork(ctx context.Context, work EnvironmentWork, sessionUUID string) (CodeSession, error) {
	mapper := NewCodeSessionMapper(d.mapperDB)
	rows, err := mapper.FindActiveForEnvironmentWork(
		ctx,
		work.OrganizationUUID,
		work.WorkspaceUUID,
		work.EnvironmentUUID,
		sessionUUID,
	)
	if err != nil {
		return CodeSession{}, err
	}
	if len(rows) == 0 {
		return CodeSession{}, ErrNotFound
	}
	if len(rows) != 1 {
		return CodeSession{}, ErrInvalidState
	}
	return rows[0].session(), nil
}

func (d *DB) RegisterCodeSessionWorker(ctx context.Context, codeSessionExternalID string, binding CodeSessionWorkerBinding, leaseTTL time.Duration) (int64, time.Time, error) {
	return d.registerCodeSessionWorker(ctx, codeSessionExternalID, binding, leaseTTL, nil)
}

func (d *DB) RegisterCodeSessionWorkerAtEpoch(
	ctx context.Context,
	codeSessionExternalID string,
	workerEpoch int64,
	binding CodeSessionWorkerBinding,
	leaseTTL time.Duration,
) (int64, time.Time, error) {
	if workerEpoch <= 0 {
		return 0, time.Time{}, ErrWorkerEpochMismatch
	}
	return d.registerCodeSessionWorker(
		ctx,
		codeSessionExternalID,
		binding,
		leaseTTL,
		&workerEpoch,
	)
}

func (d *DB) registerCodeSessionWorker(
	ctx context.Context,
	codeSessionExternalID string,
	binding CodeSessionWorkerBinding,
	leaseTTL time.Duration,
	expectedEpoch *int64,
) (int64, time.Time, error) {
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	now := time.Now().UTC()
	expiresAt := now.Add(leaseTTL)
	bindingJSON, err := json.Marshal(binding)
	if err != nil {
		return 0, time.Time{}, err
	}
	var epoch int64
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewCodeSessionMapper(executor)
		row, found, err := mapper.LockCodeSessionByExternalID(ctx, codeSessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		// Credential rotation reserves a positive epoch and clears the lease. A
		// legacy JWT without worker_epoch must not advance past that fence.
		if expectedEpoch == nil && row.CurrentWorkerEpoch > 0 && row.WorkerLeaseExpiresAt == nil {
			return ErrWorkerEpochMismatch
		}
		nextEpoch := row.CurrentWorkerEpoch + 1
		if expectedEpoch != nil {
			if row.CurrentWorkerEpoch != *expectedEpoch {
				return ErrWorkerEpochMismatch
			}
			nextEpoch = *expectedEpoch
		}
		epoch, err = mapper.RegisterWorker(ctx, registerCodeSessionWorkerParams{
			UUID:                 row.UUID,
			Epoch:                nextEpoch,
			ExpiresAt:            expiresAt,
			Now:                  now,
			WorkerTokenSessionID: optionalCodeSessionString(binding.TokenSessionID),
			WorkerBinding:        bindingJSON,
		})
		return err
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	return epoch, expiresAt, nil
}

func (d *DB) ValidateCodeSessionWorkerEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	if epoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	mapper := NewCodeSessionMapper(d.mapperDB)
	current, err := mapper.FindCurrentWorkerEpochByExternalID(ctx, codeSessionExternalID)
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

// WithLockedCodeSessionWorkerEpoch 在同一个 Yourbatis 事务中锁定 Code Session、
// 校验 epoch 并执行投递确认。fn 返回是否需要更新 worker 活跃时间；该更新也使用
// 当前事务，不能在回调中通过其他 DB 方法重新获取连接或更新同一行。
//
// 例如 epoch=7 的 processed 已获得锁时，凭证轮换必须等 ACK 完成才能推进到 8；
// 若轮换先完成，旧请求会在调用 fn（包括 Redis 查询和 JetStream ACK）之前被拒绝。
func (d *DB) WithLockedCodeSessionWorkerEpoch(
	ctx context.Context,
	codeSessionExternalID string,
	epoch int64,
	fn func() (recordActivity bool, err error),
) error {
	if epoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	return d.withLockedCodeSession(ctx, codeSessionExternalID, func(executor yourbatis.Executor, row codeSessionRow) error {
		if row.CurrentWorkerEpoch != epoch {
			return ErrWorkerEpochMismatch
		}
		recordActivity, err := fn()
		if err != nil || !recordActivity {
			return err
		}
		return NewCodeSessionMapper(executor).TouchWorkerActivityByUUID(ctx, row.UUID, time.Now().UTC())
	})
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
	mapper := NewCodeSessionMapper(d.mapperDB)
	row, err := mapper.HeartbeatWorkerByExternalID(ctx, heartbeatCodeSessionWorkerParams{
		ExternalID: codeSessionExternalID,
		Epoch:      epoch,
		Now:        now,
		ExpiresAt:  expiresAt,
	})
	if err == nil {
		return row.WorkerLeaseExpiresAt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}

	_, err = mapper.FindCurrentWorkerEpochByExternalID(ctx, codeSessionExternalID)
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
	var expiresAt time.Time
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewCodeSessionMapper(executor)
		leaseRow, err := mapper.LockWorkerLeaseByExternalID(ctx, codeSessionExternalID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		currentEpoch := leaseRow.CurrentWorkerEpoch
		var leaseExpiresAt *time.Time
		if leaseRow.WorkerLeaseExpiresAt.Valid {
			value := leaseRow.WorkerLeaseExpiresAt.Time.UTC()
			leaseExpiresAt = &value
		}
		if currentEpoch <= 0 || leaseExpiresAt == nil {
			return &CodeSessionWorkerHeartbeatError{
				Err:                  ErrWorkerNotRegistered,
				ProvidedEpoch:        epoch,
				CurrentEpoch:         currentEpoch,
				WorkerLeaseExpiresAt: leaseExpiresAt,
			}
		}
		if currentEpoch != epoch {
			return &CodeSessionWorkerHeartbeatError{
				Err:                  ErrWorkerEpochMismatch,
				ProvidedEpoch:        epoch,
				CurrentEpoch:         currentEpoch,
				WorkerLeaseExpiresAt: leaseExpiresAt,
			}
		}

		now := time.Now().UTC()
		if now.After(leaseExpiresAt.Add(grace)) {
			return &CodeSessionWorkerHeartbeatError{
				Err:                  ErrWorkerLeaseExpired,
				ProvidedEpoch:        epoch,
				CurrentEpoch:         currentEpoch,
				WorkerLeaseExpiresAt: leaseExpiresAt,
			}
		}

		expiresAt = now.Add(leaseTTL)
		row, err := mapper.HeartbeatWorkerByUUID(ctx, heartbeatCodeSessionWorkerParams{
			UUID:      leaseRow.UUID,
			Epoch:     epoch,
			Now:       now,
			ExpiresAt: expiresAt,
		})
		if err == nil {
			expiresAt = row.WorkerLeaseExpiresAt
		}
		return err
	})
	return expiresAt, err
}

// ResumeCodeSessionWorkerLeaseForSandbox re-arms the existing worker lease
// after the provider has successfully resumed the same sandbox. It preserves
// the current epoch and only updates a registered worker still associated with
// the exact active sandbox, so a retired sandbox cannot revive a fenced worker.
func (d *DB) ResumeCodeSessionWorkerLeaseForSandbox(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	codeSessionExternalID string,
	providerSandboxID string,
	leaseTTL time.Duration,
) (bool, error) {
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	now := time.Now().UTC()
	mapper := NewCodeSessionMapper(d.mapperDB)
	rowsAffected, err := mapper.ResumeWorkerLeaseForSandbox(ctx, resumeCodeSessionWorkerLeaseParams{
		OrganizationUUID:      organizationUUID,
		WorkspaceUUID:         workspaceUUID,
		CodeSessionExternalID: codeSessionExternalID,
		ProviderSandboxID:     providerSandboxID,
		ExpiresAt:             now.Add(leaseTTL),
		Now:                   now,
	})
	if err != nil {
		return false, err
	}
	return rowsAffected == 1, nil
}

func (d *DB) UpdateCodeSessionWorkerState(ctx context.Context, codeSessionExternalID string, input UpdateCodeSessionWorkerStateInput) (CodeSession, error) {
	if input.WorkerEpoch <= 0 {
		return CodeSession{}, ErrWorkerEpochMismatch
	}
	var updated CodeSession
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewCodeSessionMapper(executor)
		current, found, err := mapper.LockCodeSessionByExternalID(ctx, codeSessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if input.WorkerEpoch != current.CurrentWorkerEpoch {
			return ErrWorkerEpochMismatch
		}

		workerStatus := current.WorkerStatus
		if input.WorkerStatus != nil {
			workerStatus = *input.WorkerStatus
		}
		requiresActionDetails := json.RawMessage(current.WorkerRequiresActionDetails)
		if input.RequiresActionDetailsSet {
			requiresActionDetails = nil
			if !rawIsJSONNull(input.RequiresActionDetails) {
				requiresActionDetails = bytes.Clone(input.RequiresActionDetails)
			}
		}
		if workerStatus != "requires_action" {
			requiresActionDetails = nil
		}
		externalMetadata := json.RawMessage(current.WorkerExternalMetadata)
		if input.ExternalMetadataSet {
			externalMetadata, err = mergeCodeSessionWorkerExternalMetadata(externalMetadata, input.ExternalMetadata)
			if err != nil {
				return err
			}
		}
		if len(externalMetadata) == 0 {
			externalMetadata = json.RawMessage(`{}`)
		}

		row, err := mapper.UpdateWorkerState(ctx, updateCodeSessionWorkerStateParams{
			UUID:                  current.UUID,
			WorkerStatus:          workerStatus,
			RequiresActionDetails: requiresActionDetails,
			ExternalMetadata:      externalMetadata,
			Now:                   time.Now().UTC(),
		})
		if err == nil {
			updated = row.session()
		}
		return err
	})
	return updated, err
}

func (d *DB) AppendCodeSessionInternalEvents(ctx context.Context, codeSessionExternalID string, workerEpoch int64, inputs []AppendCodeSessionInternalEventInput) ([]CodeSessionInternalEvent, error) {
	if workerEpoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	created := make([]CodeSessionInternalEvent, 0, len(inputs))
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		codeSessionMapper := NewCodeSessionMapper(executor)
		internalEventMapper := NewCodeSessionInternalEventMapper(executor)
		session, found, err := codeSessionMapper.LockCodeSessionByExternalID(ctx, codeSessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if session.CurrentWorkerEpoch != workerEpoch {
			return ErrWorkerEpochMismatch
		}

		sequence := session.LastInternalSequenceNum
		now := time.Now().UTC()
		for _, input := range inputs {
			nextSequence := sequence + 1
			createdAt := input.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			row, err := internalEventMapper.Insert(ctx, codeSessionInternalEventInsertParams{
				ExternalID:            input.ExternalID,
				OrganizationUUID:      session.OrganizationUUID,
				WorkspaceUUID:         session.WorkspaceUUID,
				CodeSessionUUID:       session.UUID,
				CodeSessionExternalID: session.ExternalID,
				SequenceNum:           nextSequence,
				EventType:             input.EventType,
				PayloadUUID:           input.PayloadUUID,
				AgentID:               input.AgentID,
				IsCompaction:          input.IsCompaction,
				Payload:               input.Payload,
				PayloadHash:           input.PayloadHash,
				IdempotencyKey:        input.IdempotencyKey,
				EventMetadata:         input.EventMetadata,
				CreatedAt:             createdAt,
			})
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			sequence = nextSequence
			created = append(created, row.event())
		}

		if sequence == session.LastInternalSequenceNum {
			return nil
		}
		return codeSessionMapper.UpdateCodeSessionInternalSequence(ctx, session.UUID, sequence, now)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (d *DB) ListCodeSessionInternalEventsPage(ctx context.Context, params ListCodeSessionInternalEventsPageParams) ([]CodeSessionInternalEvent, bool, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 500
	}
	if params.AfterSequence < 0 {
		params.AfterSequence = 0
	}
	mapper := NewCodeSessionInternalEventMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, listCodeSessionInternalEventsParams{
		WorkspaceUUID:         params.WorkspaceUUID,
		CodeSessionExternalID: params.CodeSessionExternalID,
		Subagents:             params.Subagents,
		AfterSequence:         params.AfterSequence,
		Limit:                 limit + 1,
	})
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return codeSessionInternalEvents(rows), hasMore, nil
}

func codeSessionInternalEvents(rows []codeSessionInternalEventRow) []CodeSessionInternalEvent {
	events := make([]CodeSessionInternalEvent, len(rows))
	for index := range rows {
		events[index] = rows[index].event()
	}
	return events
}

// withLockedCodeSession 在同一个 Yourbatis 事务中按 external ID 锁定 Code Session
// 行并把事务执行器与锁定行交给回调；回调自行完成状态或 epoch 校验等 gate 判断，
// 新的行锁变体不必重复事务与锁定的骨架。
func (d *DB) withLockedCodeSession(
	ctx context.Context,
	codeSessionExternalID string,
	fn func(executor yourbatis.Executor, row codeSessionRow) error,
) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, found, err := NewCodeSessionMapper(executor).LockCodeSessionByExternalID(ctx, codeSessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		return fn(executor, row)
	})
}

func (d *DB) MarkCodeSessionWorkerConnectedForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) error {
	return d.updateCodeSessionConnection(ctx, codeSessionExternalID, "connected", true, &epoch)
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

// ValidateCodeSessionWorkerActiveLease checks that a worker is currently
// registered without writing any state. OTLP ingress calls it before reading
// the body so expired workers are rejected without doing decode work.
func (d *DB) ValidateCodeSessionWorkerActiveLease(ctx context.Context, codeSessionExternalID string) error {
	record, found, err := d.GetCodeSession(ctx, codeSessionExternalID)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	if record.WorkerLeaseExpiresAt == nil || !record.WorkerLeaseExpiresAt.After(time.Now().UTC()) {
		return ErrWorkerLeaseExpired
	}
	return nil
}

func (d *DB) touchCodeSessionWorkerActivity(ctx context.Context, codeSessionExternalID string, requiredEpoch *int64) error {
	if requiredEpoch != nil && *requiredEpoch <= 0 {
		return ErrWorkerEpochMismatch
	}
	mapper := NewCodeSessionMapper(d.mapperDB)
	rowsAffected, err := mapper.TouchWorkerActivity(ctx, codeSessionExternalID, requiredEpoch, time.Now().UTC())
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
	mapper := NewCodeSessionMapper(d.mapperDB)
	rowsAffected, err := mapper.UpdateConnection(ctx, updateCodeSessionConnectionParams{
		ExternalID:    codeSessionExternalID,
		Status:        status,
		Connected:     connected,
		RequiredEpoch: requiredEpoch,
		Now:           time.Now().UTC(),
	})
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

	mapper := NewCodeSessionMapper(d.mapperDB)
	_, err := mapper.FindCurrentWorkerEpochByExternalID(ctx, codeSessionExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrWorkerEpochMismatch
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
		merged[key] = bytes.Clone(value)
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
