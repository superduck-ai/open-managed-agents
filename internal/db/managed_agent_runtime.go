package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jmoiron/sqlx"
)

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

func createManagedAgentRuntimeTx(
	ctx context.Context,
	tx *sqlx.Tx,
	input CreateManagedAgentRuntimeInput,
	buildInitialInboundEvents func([]SessionEvent) ([]AppendCodeSessionEventInput, error),
) (CreateManagedAgentRuntimeResult, error) {
	workArguments := map[string]any{
		"workspace_id":            input.CodeSession.WorkspaceID,
		"environment_external_id": input.EnvironmentExternalID,
		"work_external_id":        input.WorkExternalID,
	}
	work, err := getEnvironmentWorkSQLX(ctx, tx, lockManagedAgentEnvironmentWorkQuery, workArguments)
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
	workArguments["runtime_patch"] = jsonArg(input.EnvironmentWorkRuntimePatch)
	work, err = getEnvironmentWorkSQLX(ctx, tx, patchManagedAgentWorkMetadataQuery, workArguments)
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
	return listSessionEventsSQLX(ctx, tx, listManagedAgentSessionEventsQuery, arguments)
}
