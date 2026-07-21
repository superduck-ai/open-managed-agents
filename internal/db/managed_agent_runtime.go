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
	InboundEvents                   []AppendCodeSessionEventInput
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
// beforeCommit performs non-persistent credential preparation while rollback is still
// possible, so a signing error cannot expose a partially committed runtime.
func (d *DB) CreateManagedAgentRuntime(
	ctx context.Context,
	input CreateManagedAgentRuntimeInput,
	beforeCommit func(CodeSessionCredentialContext) error,
) (CreateManagedAgentRuntimeResult, error) {
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
	codeSession, err := createCodeSessionTx(ctx, tx, input.CodeSession)
	if err != nil {
		return CreateManagedAgentRuntimeResult{}, err
	}
	lastInboundSequence, err := appendInitialCodeSessionEvents(ctx, tx, codeSession, input.InboundEvents)
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
