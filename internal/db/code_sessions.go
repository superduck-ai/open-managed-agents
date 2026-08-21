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

	"github.com/samber/lo"
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
	LastInboundSequenceNum      int64
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
	ExternalID     string
	EventType      string
	EventSubtype   string
	PayloadUUID    *string
	RequestID      *string
	Payload        json.RawMessage
	PayloadHash    string
	IdempotencyKey string
	DeliveryStatus string
	Source         string
	CreatedAt      time.Time
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

const managedAgentActivationInboundBatchSize = 500

func (tx ManagedAgentActivationTx) AppendCodeSessionInboundEvents(
	ctx context.Context,
	codeSession CodeSession,
	inputs []AppendCodeSessionEventInput,
) error {
	if len(inputs) == 0 {
		return nil
	}
	existing, err := listExistingActivationInboundEvents(
		ctx,
		tx.codeSessionInboundEventMapper,
		codeSession,
		inputs,
	)
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
		inserted, err := tx.codeSessionInboundEventMapper.InsertCodeSessionInboundEvents(
			ctx,
			rows[start:end],
		)
		if err != nil {
			return err
		}
		if inserted != int64(end-start) {
			return ErrInvalidState
		}
	}
	updated, err := tx.codeSessionMapper.UpdateCodeSessionInboundSequence(
		ctx,
		codeSession.UUID,
		lastSequence,
		time.Now().UTC(),
	)
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
	codeSessionInboundEventMapper CodeSessionInboundEventMapper,
	codeSession CodeSession,
	inputs []AppendCodeSessionEventInput,
) (map[string]struct{}, error) {
	idempotencyKeys := lo.Uniq(lo.FilterMap(
		inputs,
		func(input AppendCodeSessionEventInput, _ int) (string, bool) {
			return input.IdempotencyKey, input.IdempotencyKey != ""
		},
	))
	if len(idempotencyKeys) == 0 {
		return map[string]struct{}{}, nil
	}

	existing := make(map[string]struct{}, len(idempotencyKeys))
	for start := 0; start < len(idempotencyKeys); start += managedAgentActivationInboundBatchSize {
		end := min(start+managedAgentActivationInboundBatchSize, len(idempotencyKeys))
		rows, err := codeSessionInboundEventMapper.ListExistingActivationInboundEvents(
			ctx,
			codeSession.OrganizationUUID,
			codeSession.WorkspaceUUID,
			idempotencyKeys[start:end],
		)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.CodeSessionExternalID != codeSession.ExternalID {
				return nil, ErrInvalidState
			}
			existing[row.IdempotencyKey] = struct{}{}
		}
	}
	return existing, nil
}

func activationInboundEventInsertRows(
	codeSession CodeSession,
	inputs []AppendCodeSessionEventInput,
	existing map[string]struct{},
) ([]codeSessionInboundEventInsertRow, int64, error) {
	now := time.Now().UTC()
	sequence := codeSession.LastInboundSequenceNum
	rows := make([]codeSessionInboundEventInsertRow, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
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
			OrganizationUUID:      codeSession.OrganizationUUID,
			WorkspaceUUID:         codeSession.WorkspaceUUID,
			CodeSessionUUID:       codeSession.UUID,
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

func (d *DB) AppendCodeSessionInboundEvent(ctx context.Context, codeSessionExternalID string, input AppendCodeSessionEventInput) (CodeSessionEvent, bool, error) {
	return d.appendCodeSessionInboundEvent(ctx, codeSessionExternalID, input)
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

func (d *DB) appendCodeSessionInboundEvent(ctx context.Context, codeSessionExternalID string, input AppendCodeSessionEventInput) (CodeSessionEvent, bool, error) {
	var event CodeSessionEvent
	var duplicate bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		codeSessionMapper := NewCodeSessionMapper(executor)
		inboundMapper := NewCodeSessionInboundEventMapper(executor)

		codeSession, found, err := codeSessionMapper.LockCodeSessionByExternalID(ctx, codeSessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		// 有幂等键时先按 workspace 查是否已写入；命中则返回已有事件并标记 duplicate，避免重复插入。
		if input.IdempotencyKey != "" {
			existing, found, err := inboundMapper.GetCodeSessionInboundEventByIdempotencyKey(
				ctx, codeSession.WorkspaceUUID, input.IdempotencyKey,
			)
			if err != nil {
				return err
			}
			if found {
				event = existing.event()
				duplicate = true
				return nil
			}
		}

		now := input.CreatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		// 写入投递给 worker 的事件（默认 delivery_status=queued），并推进 last_inbound_sequence_num。
		deliveryStatus := input.DeliveryStatus
		if deliveryStatus == "" {
			deliveryStatus = "queued"
		}
		sequence := codeSession.LastInboundSequenceNum + 1
		inserted, err := inboundMapper.InsertCodeSessionInboundEvent(ctx, codeSessionInboundEventInsertRow{
			ExternalID:            input.ExternalID,
			OrganizationUUID:      codeSession.OrganizationUUID,
			WorkspaceUUID:         codeSession.WorkspaceUUID,
			CodeSessionUUID:       codeSession.UUID,
			CodeSessionExternalID: codeSession.ExternalID,
			SequenceNum:           sequence,
			EventType:             input.EventType,
			EventSubtype:          input.EventSubtype,
			PayloadUUID:           input.PayloadUUID,
			RequestID:             input.RequestID,
			Payload:               input.Payload,
			PayloadHash:           input.PayloadHash,
			IdempotencyKey:        input.IdempotencyKey,
			DeliveryStatus:        deliveryStatus,
			Source:                input.Source,
			CreatedAt:             now,
		})
		if err != nil {
			return err
		}
		updated, err := codeSessionMapper.UpdateCodeSessionInboundSequence(ctx, codeSession.UUID, sequence, now)
		if err != nil {
			return err
		}
		if updated != 1 {
			return ErrInvalidState
		}
		event = inserted.event()
		return nil
	})
	if err != nil {
		return CodeSessionEvent{}, false, err
	}
	return event, duplicate, nil
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

func (d *DB) ListQueuedCodeSessionInboundEvents(ctx context.Context, codeSessionExternalID string) ([]CodeSessionEvent, error) {
	mapper := NewCodeSessionInboundEventMapper(d.mapperDB)
	rows, err := mapper.ListQueued(ctx, codeSessionExternalID)
	return codeSessionEvents(rows), err
}

func (d *DB) ListQueuedCodeSessionInboundEventsForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) ([]CodeSessionEvent, error) {
	if epoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	mapper := NewCodeSessionInboundEventMapper(d.mapperDB)
	rows, err := mapper.ListQueuedForEpoch(ctx, codeSessionExternalID, epoch)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return codeSessionEvents(rows), nil
	}
	if err := d.ValidateCodeSessionWorkerEpoch(ctx, codeSessionExternalID, epoch); err != nil {
		return nil, err
	}
	return []CodeSessionEvent{}, nil
}

func (d *DB) ListCodeSessionInboundEventsForWorkerStream(ctx context.Context, codeSessionExternalID string, epoch int64, afterSequence int64) ([]CodeSessionEvent, error) {
	if epoch <= 0 {
		return nil, ErrWorkerEpochMismatch
	}
	if afterSequence < 0 {
		afterSequence = 0
	}
	mapper := NewCodeSessionInboundEventMapper(d.mapperDB)
	rows, err := mapper.ListForWorkerStream(ctx, codeSessionExternalID, epoch, afterSequence)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return codeSessionEvents(rows), nil
	}
	if err := d.ValidateCodeSessionWorkerEpoch(ctx, codeSessionExternalID, epoch); err != nil {
		return nil, err
	}
	return []CodeSessionEvent{}, nil
}

func (d *DB) MarkCodeSessionInboundEventSent(ctx context.Context, eventExternalID string) error {
	mapper := NewCodeSessionInboundEventMapper(d.mapperDB)
	rowsAffected, err := mapper.MarkSent(ctx, eventExternalID)
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
	mapper := NewCodeSessionInboundEventMapper(d.mapperDB)
	rowsAffected, err := mapper.MarkSentForEpoch(ctx, codeSessionExternalID, eventExternalID, epoch)
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
	var result CodeSessionWorkerDeliveryResult
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		codeSessionMapper := NewCodeSessionMapper(executor)
		inboundEventMapper := NewCodeSessionInboundEventMapper(executor)
		session, found, err := codeSessionMapper.LockCodeSessionByExternalID(ctx, codeSessionExternalID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if session.CurrentWorkerEpoch != epoch {
			return ErrWorkerEpochMismatch
		}

		now := time.Now().UTC()
		for _, update := range updates {
			eventID := strings.TrimSpace(update.EventID)
			status := strings.TrimSpace(update.Status)
			rank := codeSessionDeliveryStatusRank(status)
			if eventID == "" || rank < codeSessionDeliveryStatusRank("received") {
				return ErrInvalidState
			}

			event, err := getCodeSessionInboundDeliveryEvent(ctx, inboundEventMapper, session.UUID, eventID)
			if errors.Is(err, ErrNotFound) {
				result.Ignored++
				continue
			}
			if err != nil {
				return err
			}
			if event.DeliveryWorkerEpoch == nil || *event.DeliveryWorkerEpoch != epoch || codeSessionDeliveryStatusRank(event.DeliveryStatus) < codeSessionDeliveryStatusRank("sent") {
				result.Ignored++
				continue
			}

			targetStatus := event.DeliveryStatus
			if rank > codeSessionDeliveryStatusRank(event.DeliveryStatus) {
				targetStatus = status
			}
			if err := inboundEventMapper.UpdateDelivery(ctx, updateCodeSessionInboundDeliveryParams{
				UUID:           event.UUID,
				TargetStatus:   targetStatus,
				MarkReceived:   rank >= codeSessionDeliveryStatusRank("received"),
				MarkProcessing: rank >= codeSessionDeliveryStatusRank("processing"),
				MarkProcessed:  rank >= codeSessionDeliveryStatusRank("processed"),
				Epoch:          epoch,
				Now:            now,
			}); err != nil {
				return err
			}
			result.Applied++
		}
		if result.Applied == 0 {
			return nil
		}
		return codeSessionMapper.TouchWorkerActivityByUUID(ctx, session.UUID, now)
	})
	return result, err
}

func getCodeSessionInboundDeliveryEvent(ctx context.Context, mapper CodeSessionInboundEventMapper, codeSessionUUID string, eventID string) (CodeSessionEvent, error) {
	row, err := mapper.LockDeliveryByPayloadUUID(ctx, codeSessionUUID, eventID)
	if err == nil {
		return row.event(), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CodeSessionEvent{}, err
	}
	row, err = mapper.LockDeliveryByExternalID(ctx, codeSessionUUID, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return CodeSessionEvent{}, ErrNotFound
	}
	if err != nil {
		return CodeSessionEvent{}, err
	}
	return row.event(), nil
}

func codeSessionEvents(rows []codeSessionEventRow) []CodeSessionEvent {
	events := make([]CodeSessionEvent, len(rows))
	for index := range rows {
		events[index] = rows[index].event()
	}
	return events
}

func codeSessionInternalEvents(rows []codeSessionInternalEventRow) []CodeSessionInternalEvent {
	events := make([]CodeSessionInternalEvent, len(rows))
	for index := range rows {
		events[index] = rows[index].event()
	}
	return events
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
	mapper := NewCodeSessionMapper(d.mapperDB)
	rowsAffected, err := mapper.TouchWorkerActivityForActiveLease(ctx, codeSessionExternalID, epoch, now)
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}
	// 条件更新未命中后再读取当前状态，以便把 takeover 与 lease 过期映射为不同 HTTP 错误。
	record, found, err := d.GetCodeSession(ctx, codeSessionExternalID)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
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
