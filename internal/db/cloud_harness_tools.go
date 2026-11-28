package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/yourbatis"
)

const (
	HarnessToolTask      = "tool"
	HarnessModelTask     = "model"
	HarnessSubagentTask  = "subagent"
	HarnessMaxToolCalls  = 16
	HarnessWaitingChoice = "waiting_choice"
	HarnessWaitingTool   = "waiting_tool"
	HarnessInterrupted   = "interrupted"
)

type HarnessTask struct {
	Ref      HarnessTurnRef
	UUID     string
	ToolUUID string
	Kind     string
}

func (d *DB) ParkHarnessTool(ctx context.Context, lease HarnessTurnLease, toolUUID string) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewCloudHarnessMapper(executor)
		if _, err := mapper.LockModelLease(ctx, lease); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrWorkerEpochMismatch
			}
			return err
		}
		if err := requireHarnessLease(mapper.ParkTool(ctx, harnessLeaseParams{Lease: lease}, toolUUID)); err != nil {
			return err
		}
		return mapper.RevokeModelLease(ctx, lease)
	})
}

type HarnessTool struct {
	UUID               string
	CallID             string
	Name               string
	Arguments          json.RawMessage
	Result             *string
	Failed             bool
	DelegationToolUUID *string
}

func (r harnessTurnRow) task() HarnessTask {
	task := HarnessTask{Ref: r.reference(), Kind: r.TaskKind}
	if r.TaskUUID != nil && r.ToolUUID != nil {
		task.UUID, task.ToolUUID = *r.TaskUUID, *r.ToolUUID
	}
	return task
}

func (r harnessToolRow) tool() HarnessTool {
	return HarnessTool{UUID: r.UUID, CallID: r.CallID, Name: r.Name, Arguments: r.Arguments, Result: r.Result, Failed: r.Failed, DelegationToolUUID: r.DelegationToolUUID}
}

func (d *DB) HarnessTurnTask(ctx context.Context, ref HarnessTurnRef) (HarnessTask, string, error) {
	row, found, err := NewCloudHarnessMapper(d.mapperDB).Find(ctx, ref)
	if err == nil && !found {
		err = ErrNotFound
	}
	return row.task(), row.State, err
}

func (d *DB) ActiveHarnessTool(ctx context.Context, ref HarnessTurnRef, taskUUID string, epoch int64) (HarnessTool, error) {
	mapper := NewCloudHarnessMapper(d.mapperDB)
	row, found, err := mapper.Find(ctx, ref)
	if err != nil {
		return HarnessTool{}, err
	}
	task := row.task()
	if !found || row.WorkerEpoch != epoch || row.State != "running" || task.UUID != taskUUID || task.Kind != HarnessToolTask {
		return HarnessTool{}, ErrNotFound
	}
	tool, found, err := mapper.FindTool(ctx, ref, task.ToolUUID)
	if err == nil && !found {
		err = ErrNotFound
	}
	return tool.tool(), err
}

func (d *DB) ListHarnessTools(ctx context.Context, ref HarnessTurnRef) ([]HarnessTool, error) {
	rows, err := NewCloudHarnessMapper(d.mapperDB).ListTools(ctx, ref)
	if err != nil {
		return nil, err
	}
	result := make([]HarnessTool, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.tool())
	}
	return result, nil
}

func checkpointHarnessTool(ctx context.Context, mapper CloudHarnessMapper, lease HarnessTurnLease, row harnessTurnRow, tool HarnessTool, state string) error {
	return advanceHarnessTask(ctx, mapper, harnessTaskParams{
		Lease: lease, TaskUUID: tool.UUID, ToolUUID: tool.UUID, Kind: HarnessToolTask, State: state,
		SubagentThreadUUID: row.SubagentThreadUUID, DelegationToolUUID: row.DelegationToolUUID,
	})
}

func advanceHarnessTask(ctx context.Context, mapper CloudHarnessMapper, params harnessTaskParams) error {
	if err := requireHarnessLease(mapper.AdvanceTask(ctx, params)); err != nil {
		return err
	}
	return mapper.RevokeModelLease(ctx, params.Lease)
}

func (d *DB) FinishHarnessTool(ctx context.Context, sessionID string, lease HarnessTurnLease, taskUUID string, result sessioncontract.HarnessToolResult, events []SessionEvent) ([]SessionEvent, error) {
	var stored []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), lease.Ref.WorkspaceUUID, sessionID)
		if err != nil {
			return err
		}
		if !harnessSessionMatches(session, lease.Ref) {
			return ErrInvalidState
		}
		if _, _, err := NewCodeSessionMapper(executor).LockLatestInputState(ctx, session.WorkspaceUUID, session.UUID); err != nil {
			return err
		}
		mapper := NewCloudHarnessMapper(executor)
		if err := finishHarnessToolTx(ctx, mapper, lease, taskUUID, result); err != nil {
			return err
		}
		batch, err := insertSessionHistoryTx(ctx, executor, session, events)
		stored = batch.Events
		return err
	})
	return stored, err
}

func finishHarnessToolTx(ctx context.Context, mapper CloudHarnessMapper, lease HarnessTurnLease, taskUUID string, result sessioncontract.HarnessToolResult) error {
	row, found, err := mapper.Lock(ctx, lease.Ref)
	if err != nil {
		return err
	}
	task := row.task()
	if !found || task.UUID != taskUUID || task.Kind != HarnessToolTask || row.WorkerEpoch != lease.Epoch {
		return ErrWorkerEpochMismatch
	}
	if err := requireHarnessLease(mapper.FinishTool(ctx, harnessToolResultParams{
		Ref: lease.Ref, ToolUUID: task.ToolUUID, Text: result.Text, Failed: result.Failed,
	})); err != nil {
		return err
	}
	return advanceHarnessTask(ctx, mapper, harnessTaskParams{
		Lease: lease, TaskUUID: uuid.NewString(), ToolUUID: task.ToolUUID, Kind: HarnessModelTask, State: "pending",
		SubagentThreadUUID: row.SubagentThreadUUID, DelegationToolUUID: row.DelegationToolUUID,
	})
}

func (d *DB) HarnessExecutionToolCount(ctx context.Context, lease HarnessTurnLease) (int, error) {
	mapper := NewCloudHarnessMapper(d.mapperDB)
	row, found, err := mapper.Find(ctx, lease.Ref)
	if err != nil {
		return 0, err
	}
	if !found || row.WorkerEpoch != lease.Epoch || row.State != "running" {
		return 0, ErrWorkerEpochMismatch
	}
	return mapper.CountExecutionTools(ctx, lease.Ref, row.DelegationToolUUID)
}
