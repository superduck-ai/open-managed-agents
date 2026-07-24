package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type CreateManagedAgentRuntimeInput struct {
	CodeSession                     CreateCodeSessionInput
	SessionMetadataPatch            json.RawMessage
	EnvironmentWorkPreparationPatch json.RawMessage
	EnvironmentWorkRuntimePatch     json.RawMessage
	EnvironmentExternalID           string
	WorkExternalID                  string
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
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	defer tx.Rollback(ctx)

	work, err := scanEnvironmentWork(tx.QueryRow(ctx, environmentWorkSelectSQL()+`
		where workspace_id = $1
			and environment_external_id = $2
			and external_id = $3
			and deleted_at is null
		for update
	`, input.CodeSession.WorkspaceID, input.EnvironmentExternalID, input.WorkExternalID))
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
	codeSession, err := createCodeSessionTx(ctx, tx, input.CodeSession)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	lastInboundSequence, err := appendInitialCodeSessionEvents(ctx, tx, codeSession, inboundEvents)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	codeSession.LastInboundSequenceNum = lastInboundSequence
	if _, err := patchSessionMetadataTx(ctx, tx, codeSession.WorkspaceID, codeSession.SessionExternalID, input.SessionMetadataPatch); err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	work, err = patchEnvironmentWorkMetadataTx(
		ctx,
		tx,
		codeSession.WorkspaceID,
		input.EnvironmentExternalID,
		input.WorkExternalID,
		input.EnvironmentWorkPreparationPatch,
		input.EnvironmentWorkRuntimePatch,
	)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	credentials, err := getCodeSessionCredentialContextForIssueTx(
		ctx,
		tx,
		codeSession.OrganizationID,
		codeSession.WorkspaceID,
		codeSession.ExternalID,
	)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	if beforeCommit != nil {
		if err := beforeCommit(credentials); err != nil {
			return CreateManagedAgentRuntimeResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
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
func lockSessionAndListEventsTx(ctx context.Context, tx pgx.Tx, workspaceID int64, sessionExternalID string) ([]SessionEvent, error) {
	if _, err := scanSession(tx.QueryRow(ctx, `
		select `+sessionColumns()+`
		from sessions
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, sessionExternalID)); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		select `+sessionEventColumns()+`
		from session_events
		where workspace_id = $1 and session_external_id = $2 and deleted_at is null
		order by created_at asc, id asc
	`, workspaceID, sessionExternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionEventRows(rows)
}

func appendInitialCodeSessionEvents(ctx context.Context, tx pgx.Tx, session CodeSession, inputs []AppendCodeSessionEventInput) (int64, error) {
	sequence := session.LastInboundSequenceNum
	for _, input := range inputs {
		if input.RequiredWorkerEpoch != nil {
			return sequence, ErrWorkerEpochMismatch
		}
		sequence++
		createdAt := input.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		_, err := tx.Exec(ctx, `
			insert into code_session_inbound_events (
				external_id, organization_id, workspace_id, code_session_id, code_session_external_id,
				sequence_num, event_type, event_subtype, payload_uuid, request_id, payload,
				payload_hash, idempotency_key, delivery_status, source, created_at, updated_at
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15, $16, $16)
		`, input.ExternalID, session.OrganizationID, session.WorkspaceID, session.ID, session.ExternalID,
			sequence, input.EventType, input.EventSubtype, input.PayloadUUID, input.RequestID, jsonArg(input.Payload),
			input.PayloadHash, input.IdempotencyKey, input.DeliveryStatus, input.Source, createdAt)
		if err != nil {
			return sequence, err
		}
	}
	if sequence == session.LastInboundSequenceNum {
		return sequence, nil
	}
	commandTag, err := tx.Exec(ctx, `
		update code_sessions
		set last_inbound_sequence_num = $1, updated_at = now()
		where id = $2
	`, sequence, session.ID)
	if err != nil {
		return sequence, err
	}
	if commandTag.RowsAffected() != 1 {
		return sequence, errors.New("update managed agent code session sequence")
	}
	return sequence, nil
}
