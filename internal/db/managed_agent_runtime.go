package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jmoiron/sqlx"
)

// Managed Agent 启动事务编排：锁序固定为 Environment Work → Session，
// SQL 常量与 Terminate 实现见 managed_agent_runtime_sqlx.go。

type CreateManagedAgentRuntimeInput struct {
	CodeSession                 CreateCodeSessionInput
	SessionMetadataPatch        json.RawMessage
	EnvironmentWorkRuntimePatch json.RawMessage
	EnvironmentExternalID       string
	WorkExternalID              string
}

type CreateManagedAgentRuntimeResult struct {
	CodeSession     CodeSession
	EnvironmentWork EnvironmentWork
	Credentials     CodeSessionCredentialContext
}

// CreateManagedAgentRuntime atomically creates the code-session identity and initial
// queue, then publishes the matching runtime metadata on the public Session and Work.
// buildInitialInboundEvents runs while the public Session row is locked and must stay
// side-effect free; it converts the locked event snapshot without doing more DB work.
// beforeCommit performs non-persistent credential preparation while rollback is still
// possible, so a signing error cannot expose a partially committed runtime.
func (d *DB) CreateManagedAgentRuntime(
	ctx context.Context,
	input CreateManagedAgentRuntimeInput,
	buildInitialInboundEvents func([]SessionEvent) ([]AppendCodeSessionEventInput, error),
	beforeCommit func(CodeSessionCredentialContext) error,
) (CreateManagedAgentRuntimeResult, error) {
	if buildInitialInboundEvents == nil {
		return CreateManagedAgentRuntimeResult{}, errors.New("managed agent initial inbound event builder is required")
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	defer tx.Rollback()

	result, err := createManagedAgentRuntimeTx(ctx, tx, input, buildInitialInboundEvents)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	if beforeCommit != nil {
		if err := beforeCommit(result.Credentials); err != nil {
			return CreateManagedAgentRuntimeResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	return result, nil
}

// createManagedAgentRuntimeTx 在同一只 sqlx.Tx 中完成 Managed Agent 启动提交：
//  1. 锁定 active Environment Work（锁序第一步）
//  2. 锁定 idle Session 并读取事件快照（锁序第二步）
//  3. 回调拼装 initial inbound（禁止再访问数据库）
//  4. 插入 Code Session，并写入 inbound 事件、推进 sequence
//  5. 发布 Session / Work runtime metadata
//  6. 加载签发 session-ingress JWT 所需的凭证上下文
func createManagedAgentRuntimeTx(
	ctx context.Context,
	tx *sqlx.Tx,
	input CreateManagedAgentRuntimeInput,
	buildInitialInboundEvents func([]SessionEvent) ([]AppendCodeSessionEventInput, error),
) (CreateManagedAgentRuntimeResult, error) {
	work, err := lockManagedAgentEnvironmentWork(
		ctx,
		tx,
		input.CodeSession.WorkspaceID,
		input.EnvironmentExternalID,
		input.WorkExternalID,
	)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	if work.State != "active" {
		return CreateManagedAgentRuntimeResult{}, ErrInvalidState
	}

	publicEvents, err := lockSessionAndListEventsTx(ctx, tx, input.CodeSession.WorkspaceID, input.CodeSession.SessionExternalID)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}

	inboundEvents, err := buildInitialInboundEvents(publicEvents)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}

	codeSession, err := insertCodeSessionSQLX(ctx, tx, input.CodeSession)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	lastInboundSequence, err := appendCodeSessionInboundEventsSQLX(ctx, tx, codeSession, inboundEvents)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	codeSession.LastInboundSequenceNum = lastInboundSequence

	if _, err := patchSessionMetadataSQLX(
		ctx,
		tx,
		codeSession.WorkspaceID,
		codeSession.SessionExternalID,
		input.SessionMetadataPatch,
	); err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	work, err = patchManagedAgentWorkMetadata(
		ctx,
		tx,
		codeSession.WorkspaceID,
		input.EnvironmentExternalID,
		input.WorkExternalID,
		input.EnvironmentWorkRuntimePatch,
	)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}

	credentials, err := getCodeSessionCredentialContextForIssueSQLX(
		ctx,
		tx,
		codeSession.OrganizationID,
		codeSession.WorkspaceID,
		codeSession.ExternalID,
	)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	return CreateManagedAgentRuntimeResult{
		CodeSession:     codeSession,
		EnvironmentWork: work,
		Credentials:     credentials,
	}, nil
}

// lockSessionAndListEventsTx 与 AppendSessionEvents 使用同一条 Session 行锁。
// 锁内读取的最终快照会随 Code Session 一起提交；锁后写入的事件则会在
// Runtime 提交后通过实时转发路径进入 inbound queue。
func lockSessionAndListEventsTx(ctx context.Context, tx *sqlx.Tx, workspaceID int64, sessionExternalID string) ([]SessionEvent, error) {
	arguments := sessionLookupArguments(workspaceID, sessionExternalID)
	session, err := getSessionSQLX(ctx, tx, lockSessionForEventsQuery, arguments)
	if err != nil {
		return nil, err
	}
	if session.ArchivedAt != nil || session.Status != "idle" {
		return nil, ErrInvalidState
	}
	return listManagedAgentSessionEvents(ctx, tx, workspaceID, sessionExternalID)
}
