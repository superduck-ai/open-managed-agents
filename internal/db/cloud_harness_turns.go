package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/yourbatis"
)

const HarnessModelAttempts = 3

type HarnessTurnRef struct {
	UUID             string `json:"turn_uuid"`
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	SessionUUID      string `json:"session_uuid"`
}

type HarnessTurnLease struct {
	Ref                HarnessTurnRef
	CodeSessionUUID    string
	Epoch              int64
	DelegationToolUUID string
}

type HarnessTurnDelivery struct {
	InputUUID   string
	TaskUUID    string
	ToolUUID    string
	Kind        string
	LegacyInput bool
}

type HarnessTurnClaim struct {
	Lease          HarnessTurnLease
	CodeSession    CodeSession
	State          string
	LegacyInput    json.RawMessage
	InputEventUUID string
	Subagent       *SessionThread
	Task           HarnessTask
	Superseded     bool
}

type HarnessModelPhase uint8

const (
	HarnessModelStart HarnessModelPhase = iota + 1
	HarnessModelEnd
	HarnessModelError
)

type HarnessModelCompletion struct {
	Message  harness.AssistantMessage
	UserText string
}

func (r harnessTurnRow) reference() HarnessTurnRef {
	return HarnessTurnRef{UUID: r.UUID, OrganizationUUID: r.OrganizationUUID, WorkspaceUUID: r.WorkspaceUUID, SessionUUID: r.SessionUUID}
}

func (d *DB) HarnessTurnState(ctx context.Context, ref HarnessTurnRef) (string, error) {
	turn, found, err := NewCloudHarnessMapper(d.mapperDB).Find(ctx, ref)
	if err == nil && !found {
		err = ErrNotFound
	}
	return turn.State, err
}

func (d *DB) ClaimHarnessTurn(ctx context.Context, ref HarnessTurnRef, delivery HarnessTurnDelivery, tokenHash string, leaseUntil time.Time) (HarnessTurnClaim, error) {
	session, found, err := d.GetSessionByUUID(ctx, ref.WorkspaceUUID, ref.SessionUUID)
	if err != nil {
		return HarnessTurnClaim{}, err
	}
	if !found {
		return HarnessTurnClaim{}, ErrNotFound
	}
	var claim HarnessTurnClaim
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		var txErr error
		claim, txErr = claimHarnessTurnTx(ctx, executor, ref, delivery, session.ExternalID, tokenHash, leaseUntil)
		return txErr
	})
	return claim, err
}

func claimHarnessTurnTx(ctx context.Context, executor yourbatis.Executor, ref HarnessTurnRef, delivery HarnessTurnDelivery, sessionID, tokenHash string, leaseUntil time.Time) (HarnessTurnClaim, error) {
	session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), ref.WorkspaceUUID, sessionID)
	if err != nil {
		return HarnessTurnClaim{}, err
	}
	if !harnessSessionMatches(session, ref) {
		return HarnessTurnClaim{}, ErrInvalidState
	}
	if _, _, err := NewCodeSessionMapper(executor).LockLatestInputState(ctx, session.WorkspaceUUID, session.UUID); err != nil {
		return HarnessTurnClaim{}, err
	}
	mapper := NewCloudHarnessMapper(executor)
	earlier, err := mapper.HasEarlierPendingInput(ctx, ref, ref.UUID)
	if err != nil {
		return HarnessTurnClaim{}, err
	}
	if earlier {
		return HarnessTurnClaim{}, ErrWorkerEpochMismatch
	}
	row, found, err := lockHarnessDelivery(ctx, mapper, ref, delivery)
	if err != nil || !found {
		if err == nil {
			err = ErrNotFound
		}
		return HarnessTurnClaim{}, err
	}
	if row.State == "completed" || row.State == "failed" || row.State == HarnessWaitingChoice || row.State == HarnessInterrupted {
		return HarnessTurnClaim{State: row.State}, nil
	}
	if row.ResumeInputUUID != nil && delivery.InputUUID != *row.ResumeInputUUID {
		return HarnessTurnClaim{}, ErrInvalidState
	}
	task := row.task()
	if task.UUID != delivery.TaskUUID {
		return HarnessTurnClaim{Task: task, Superseded: true}, nil
	}
	if (delivery.Kind != "" && task.Kind != delivery.Kind) || (delivery.ToolUUID != "" && task.ToolUUID != delivery.ToolUUID) {
		return HarnessTurnClaim{}, ErrInvalidState
	}
	ref = task.Ref
	code, err := NewCodeSessionMapper(executor).FindLatestBySessionExternalID(ctx, ref.WorkspaceUUID, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return HarnessTurnClaim{}, ErrWorkerEpochMismatch
	}
	if err != nil {
		return HarnessTurnClaim{}, err
	}
	var legacyInput json.RawMessage
	if delivery.LegacyInput {
		input, err := NewSessionEventMapper(executor).FindByUUID(ctx, ref.WorkspaceUUID, ref.SessionUUID, row.InputEventUUID)
		if err != nil {
			return HarnessTurnClaim{}, mapNoRows(err)
		}
		legacyInput = input.Payload
	}
	epoch, err := mapper.GrantModelLease(ctx, harnessClaimParams{
		Ref: ref, CodeSessionUUID: code.UUID, TokenHash: tokenHash, LeaseUntil: leaseUntil,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return HarnessTurnClaim{}, ErrWorkerEpochMismatch
	}
	if err != nil {
		return HarnessTurnClaim{}, err
	}
	lease := HarnessTurnLease{Ref: ref, CodeSessionUUID: code.UUID, Epoch: epoch}
	if row.DelegationToolUUID != nil {
		lease.DelegationToolUUID = *row.DelegationToolUUID
	}
	subagent, err := harnessSubagentThread(ctx, executor, row)
	if err != nil {
		return HarnessTurnClaim{}, err
	}
	count, err := mapper.Claim(ctx, harnessLeaseParams{Lease: lease, LeaseUntil: leaseUntil})
	if err := requireHarnessLease(count, err); err != nil {
		return HarnessTurnClaim{}, err
	}
	if _, err := NewHarnessConversationMapper(executor).EnsureRoot(ctx, harnessRootKey(ref)); err != nil {
		return HarnessTurnClaim{}, err
	}
	return HarnessTurnClaim{Lease: lease, CodeSession: code.session(), State: "running", LegacyInput: legacyInput, InputEventUUID: row.historyInputID(), Subagent: subagent, Task: task}, nil
}

func lockHarnessDelivery(ctx context.Context, mapper CloudHarnessMapper, ref HarnessTurnRef, delivery HarnessTurnDelivery) (harnessTurnRow, bool, error) {
	if delivery.TaskUUID == "" {
		if err := mapper.CreateForInput(ctx, ref); err != nil {
			return harnessTurnRow{}, false, err
		}
	}
	return mapper.Lock(ctx, ref)
}

func harnessSessionMatches(session Session, ref HarnessTurnRef) bool {
	return session.UUID == ref.SessionUUID && session.OrganizationUUID == ref.OrganizationUUID &&
		session.ExecutionRuntime == sessioncontract.CloudHarnessRuntime && session.ArchivedAt == nil && session.Status != "terminated"
}

func (d *DB) RenewHarnessTurn(ctx context.Context, lease HarnessTurnLease, leaseUntil time.Time) error {
	params := harnessLeaseParams{Lease: lease, LeaseUntil: leaseUntil}
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewCloudHarnessMapper(executor)
		if err := requireHarnessLease(mapper.RenewModelLease(ctx, params)); err != nil {
			return err
		}
		return requireHarnessLease(mapper.RenewTurn(ctx, params))
	})
}

func (d *DB) ReleaseHarnessTurn(ctx context.Context, lease HarnessTurnLease) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewCloudHarnessMapper(executor)
		if err := mapper.RevokeModelLease(ctx, lease); err != nil {
			return err
		}
		return requireHarnessLease(mapper.ReleaseTurn(ctx, lease))
	})
}

func requireHarnessLease(count int64, err error) error {
	if err == nil && count != 1 {
		return ErrWorkerEpochMismatch
	}
	return err
}

func (d *DB) RecordHarnessModelEvents(ctx context.Context, lease HarnessTurnLease, codeSession CodeSession, phase HarnessModelPhase, events []SessionEvent, completion *HarnessModelCompletion) ([]SessionEvent, error) {
	var stored []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		var txErr error
		stored, txErr = recordHarnessEventsTx(ctx, executor, lease, codeSession, phase, events, completion)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return stored, nil
}

func recordHarnessEventsTx(ctx context.Context, executor yourbatis.Executor, lease HarnessTurnLease, codeSession CodeSession, phase HarnessModelPhase, events []SessionEvent, completion *HarnessModelCompletion) ([]SessionEvent, error) {
	ref := lease.Ref
	session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), ref.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !harnessSessionMatches(session, ref) || codeSession.UUID != lease.CodeSessionUUID {
		return nil, ErrWorkerEpochMismatch
	}
	mapper := NewCloudHarnessMapper(executor)
	if _, err := mapper.LockModelLease(ctx, lease); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrWorkerEpochMismatch
		}
		return nil, err
	}
	if phase == HarnessModelStart {
		if err := requireHarnessLease(mapper.StartModel(ctx, lease)); err != nil {
			return nil, err
		}
	}
	row, found, err := mapper.Lock(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !found || row.WorkerEpoch != lease.Epoch || row.State != "running" {
		return nil, ErrWorkerEpochMismatch
	}

	batch, err := insertSessionHistoryTx(ctx, executor, session, events)
	if err != nil || phase == HarnessModelStart {
		return batch.Events, err
	}
	if phase == HarnessModelEnd {
		if completion == nil {
			return nil, ErrInvalidState
		}
		tool, err := completeHarnessConversation(ctx, executor, session.ExternalID, row, *completion)
		if err != nil {
			return nil, err
		}
		if tool.UUID != "" {
			if tool.Name == sessioncontract.HarnessAskUserQuestion && row.SubagentThreadUUID == nil {
				waiting, err := parkHarnessChoiceTx(ctx, executor, session, lease, row, tool, batch.Events)
				return append(batch.Events, waiting...), err
			}
			return batch.Events, checkpointHarnessTool(ctx, mapper, lease, row, tool, "pending")
		}
	}
	if row.SubagentThreadUUID != nil && (phase == HarnessModelEnd || row.Attempts >= HarnessModelAttempts) {
		extra, err := completeHarnessSubagentTx(ctx, executor, session, codeSession, lease, row, completion)
		return append(batch.Events, extra...), err
	}
	return finishHarnessEventsTx(ctx, executor, session, lease, row, phase, batch.Events)
}

func finishHarnessEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, lease HarnessTurnLease, row harnessTurnRow, phase HarnessModelPhase, stored []SessionEvent) ([]SessionEvent, error) {
	mapper := NewCloudHarnessMapper(executor)
	result := harnessFinishParams{Lease: lease, State: "pending"}
	stopReason := ""
	if phase == HarnessModelEnd {
		index := slices.IndexFunc(stored, func(event SessionEvent) bool { return event.EventType == "agent.message" })
		if index < 0 {
			return nil, ErrInvalidState
		}
		result.State, stopReason = "completed", "end_turn"
	} else if row.Attempts >= HarnessModelAttempts {
		if err := settleFailedHarnessHistory(ctx, executor, row); err != nil {
			return nil, err
		}
		result.State, stopReason = "failed", "retries_exhausted"
	}
	if err := requireHarnessLease(mapper.Finish(ctx, result)); err != nil {
		return nil, err
	}
	if err := mapper.RevokeModelLease(ctx, lease); err != nil {
		return nil, err
	}
	if stopReason == "" {
		return stored, nil
	}
	settled, err := settleHarnessInputTx(ctx, executor, session, row, stopReason)
	return append(stored, settled...), err
}

func settleHarnessInputTx(ctx context.Context, executor yourbatis.Executor, session Session, row harnessTurnRow, stopReason string) ([]SessionEvent, error) {
	mapper := NewSessionEventMapper(executor)
	input, err := mapper.FindByUUID(ctx, session.WorkspaceUUID, session.UUID, row.InputEventUUID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	processed, applied, err := mapper.MarkInputProcessed(ctx, session.WorkspaceUUID, session.ExternalID, input.ExternalID, now)
	if err != nil {
		return nil, err
	}
	var result []SessionEvent
	if applied {
		result = append(result, processed.event())
	}
	payload, err := json.Marshal(sessionStatusPayload{StopReason: &sessionStopReason{Type: stopReason}})
	if err != nil {
		return nil, err
	}
	batch, err := insertSessionHistoryTx(ctx, executor, session, []SessionEvent{{
		UUID: uuid.NewString(), ExternalID: derivedStatusEventID(input.ExternalID, "session.status_idle"),
		EventType: "session.status_idle", Payload: payload, CreatedAt: now, ProcessedAt: now,
	}})
	return append(result, batch.Events...), err
}
