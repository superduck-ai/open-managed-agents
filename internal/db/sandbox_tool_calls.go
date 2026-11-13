package db

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/yourbatis"
)

type SandboxToolCallSubmission struct {
	Scope        SandboxToolCallScope
	Tool         string
	Input        []byte
	QueueTimeout time.Duration
	Timeout      time.Duration
	Validate     func(context.Context) (SandboxToolCallValidation, error)
}

func (d *DB) SubmitSandboxToolCall(ctx context.Context, submission SandboxToolCallSubmission) (SandboxToolCall, error) {
	var result SandboxToolCall
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxToolCallMapper(executor)
		scope := submission.Scope
		sessionUUID, found, err := lockToolCallWorker(ctx, mapper, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrWorkerEpochMismatch
		}
		now, err := mapper.Now(ctx)
		if err != nil {
			return err
		}
		row := sandboxToolCallRow{
			UUID:             uuid.NewString(),
			OrganizationUUID: scope.OrganizationUUID,
			WorkspaceUUID:    scope.WorkspaceUUID,
			CodeSessionUUID:  sessionUUID,
			ExternalID:       scope.CallID,
			ThreadID:         scope.ThreadID,
			WorkerEpoch:      scope.WorkerEpoch,
			ToolName:         submission.Tool,
			Input:            submission.Input,
			State:            "pending",
			DeadlineAt:       now.Add(submission.QueueTimeout),
			TimeoutMS:        positiveMilliseconds(submission.Timeout),
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if submission.Validate != nil {
			existing, exists, err := mapper.Find(ctx, scope)
			if err != nil {
				return err
			}
			if exists {
				return toolCallSubmissionResult(existing, submission, &result)
			}
			validation, err := submission.Validate(context.WithValue(ctx, validationExecutorKey{}, validationExecutor{database: d, executor: executor}))
			if err != nil {
				return err
			}
			row.Finalizer, row.FinalizerState = validation.Finalizer, validation.FinalizerState
			if validation.Finalizer != nil {
				if validation.FinalizeAfter <= 0 {
					return ErrPreconditionFailed
				}
				row.FinalizeTimeoutMS = validation.FinalizeAfter.Milliseconds()
			}
			if len(validation.Failure) > 0 {
				row.State, row.Result, row.FinishedAt = "failed", validation.Failure, &now
			}
		}
		if err := mapper.Insert(ctx, row); err != nil {
			return err
		}
		row, found, err = mapper.Find(ctx, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		return toolCallSubmissionResult(row, submission, &result)
	})
	return result, err
}

func (d *DB) GetSandboxToolCall(ctx context.Context, scope SandboxToolCallScope) (SandboxToolCall, error) {
	var result SandboxToolCall
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxToolCallMapper(executor)
		_, found, err := lockToolCallWorker(ctx, mapper, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrWorkerEpochMismatch
		}
		row, found, err := mapper.Find(ctx, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		result = row.model()
		return nil
	})
	return result, err
}

func (d *DB) ClaimSandboxToolCalls(ctx context.Context, scope SandboxToolCallScope, limit int) ([]SandboxToolCall, error) {
	var calls []SandboxToolCall
	var unclaimable error
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxToolCallMapper(executor)
		sessionUUID, found, err := lockToolCallSandbox(ctx, mapper, scope)
		if err != nil {
			return err
		}
		if !found {
			temporary, err := markPreparingToolCallSandbox(ctx, mapper, scope)
			unclaimable = ErrToolCallSandboxRetired
			if temporary {
				unclaimable = ErrToolCallSandboxPreparing
			}
			return err
		}
		now, err := mapper.Now(ctx)
		if err != nil {
			return err
		}
		rows, err := mapper.Claim(ctx, scope, sessionUUID, now, limit)
		if err != nil {
			return err
		}
		for _, row := range rows {
			lease := now.Add(sandboxToolCallDispatchLease)
			row.State, row.SandboxUUID, row.LeaseExpiresAt = "dispatched", &scope.SandboxUUID, &lease
			row.Attempt++
			row.UpdatedAt = now
			if err := mapper.Update(ctx, row); err != nil {
				return err
			}
			calls = append(calls, row.model())
		}
		return nil
	})
	if err == nil && unclaimable != nil {
		return nil, unclaimable
	}
	return calls, err
}

func sandboxToolCallTerminal(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "expired", "lost":
		return true
	}
	return false
}

const (
	sandboxToolCallDispatchLease = 10 * time.Second
	sandboxToolCallRunLease      = 30 * time.Second
	sandboxToolCallLeaseGrace    = 30 * time.Second
	sandboxToolCallStopGrace     = 30 * time.Second
)

func stopSandboxToolCall(row *sandboxToolCallRow, reason string, now time.Time) {
	if row.State != "pending" && row.State != "dispatched" && row.State != "running" {
		return
	}
	row.CancelRequestedAt, row.CancelReason = &now, &reason
	if row.State == "running" {
		until := now.Add(sandboxToolCallStopGrace)
		row.State, row.StopGraceUntil = "cancel_requested", &until
		return
	}
	row.State, row.LeaseExpiresAt, row.FinishedAt = "cancelled", nil, &now
	if reason == "deadline" {
		row.State = "expired"
	}
}

func positiveMilliseconds(duration time.Duration) *int64 {
	if duration <= 0 {
		return nil
	}
	milliseconds := duration.Milliseconds()
	return &milliseconds
}

func requeueSandboxToolCall(row *sandboxToolCallRow) {
	row.State, row.SandboxUUID, row.LeaseExpiresAt = "pending", nil, nil
}

func expireSandboxToolCall(row *sandboxToolCallRow, now time.Time) {
	if !row.DeadlineAt.After(now) {
		stopSandboxToolCall(row, "deadline", now)
	}
	switch row.State {
	case "dispatched":
		if row.LeaseExpiresAt != nil && !row.LeaseExpiresAt.After(now) {
			requeueSandboxToolCall(row)
		}
	case "running", "cancel_requested":
		leaseLost := row.LeaseExpiresAt != nil && !row.LeaseExpiresAt.Add(sandboxToolCallLeaseGrace).After(now)
		stopUnconfirmed := row.StopGraceUntil != nil && !row.StopGraceUntil.After(now)
		if leaseLost || stopUnconfirmed {
			code := "executor_lost"
			row.State, row.FinishedAt, row.ErrorCode = "lost", &now, &code
		}
	}
}

func (d *DB) CancelSandboxToolCall(ctx context.Context, scope SandboxToolCallScope) (SandboxToolCall, error) {
	var result SandboxToolCall
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxToolCallMapper(executor)
		sessionUUID, found, err := lockToolCallWorker(ctx, mapper, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrWorkerEpochMismatch
		}
		now, err := mapper.Now(ctx)
		if err != nil {
			return err
		}
		tombstone := sandboxToolCallRow{
			UUID:              uuid.NewString(),
			OrganizationUUID:  scope.OrganizationUUID,
			WorkspaceUUID:     scope.WorkspaceUUID,
			CodeSessionUUID:   sessionUUID,
			ExternalID:        scope.CallID,
			ThreadID:          scope.ThreadID,
			WorkerEpoch:       scope.WorkerEpoch,
			ToolName:          "",
			Input:             []byte("{}"),
			State:             "cancelled",
			DeadlineAt:        now,
			CancelRequestedAt: &now,
			FinishedAt:        &now,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := mapper.Insert(ctx, tombstone); err != nil {
			return err
		}
		row, found, err := mapper.Find(ctx, scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		row, _, err = mapper.Lock(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, row.UUID)
		if err != nil {
			return err
		}
		stopSandboxToolCall(&row, "user", now)
		row.UpdatedAt = now
		if err := mapper.Update(ctx, row); err != nil {
			return err
		}
		result = row.model()
		return nil
	})
	return result, err
}

type SandboxToolCallReport struct {
	Scope           SandboxToolCallScope
	CallUUID        string
	Attempt         int
	Event           string
	Reason          string
	Result          []byte
	ResultObjectKey *string
	ResultBytes     *int64
	ResultSHA256    *string
	ResultBlobUUID  *string
}

func (d *DB) ReportSandboxToolCall(ctx context.Context, report SandboxToolCallReport) (SandboxToolCall, error) {
	var result SandboxToolCall
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxToolCallMapper(executor)
		sessionUUID, found, err := lockToolCallSandbox(ctx, mapper, report.Scope)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		row, found, err := mapper.Lock(ctx, report.Scope.OrganizationUUID, report.Scope.WorkspaceUUID, report.CallUUID)
		if err != nil {
			return err
		}
		if !found || row.CodeSessionUUID != sessionUUID {
			return ErrNotFound
		}
		if report.Event == "reject" && report.Attempt < row.Attempt {
			result = row.model()
			return nil
		}
		if row.SandboxUUID == nil || *row.SandboxUUID != report.Scope.SandboxUUID || row.Attempt != report.Attempt {
			return ErrToolCallConflict
		}
		now, err := mapper.Now(ctx)
		if err != nil {
			return err
		}
		if err := applySandboxToolCallReport(&row, report, now); err != nil {
			return err
		}
		if report.ResultBlobUUID != nil && row.ResultObjectKey != nil && report.ResultObjectKey != nil && *row.ResultObjectKey == *report.ResultObjectKey {
			if err := attachEventPayloadBlob(ctx, executor, row.WorkspaceUUID, report.ResultBlobUUID); err != nil {
				return err
			}
		}
		if err := mapper.Update(ctx, row); err != nil {
			return err
		}
		result = row.model()
		return nil
	})
	return result, err
}

func applySandboxToolCallReport(row *sandboxToolCallRow, report SandboxToolCallReport, now time.Time) error {
	if sandboxToolCallTerminal(row.State) || row.State == "finalizing" {
		return recordCompletedToolCallReport(row, report)
	}
	expireSandboxToolCall(row, now)
	if row.State == "pending" {
		return ErrToolCallConflict
	}
	if sandboxToolCallTerminal(row.State) {
		return recordCompletedToolCallReport(row, report)
	}
	row.UpdatedAt = now
	switch report.Event {
	case "inspect":
		return nil
	case "started", "heartbeat":
		if row.State == "dispatched" && report.Event == "started" {
			row.State = "running"
			if row.TimeoutMS != nil {
				row.DeadlineAt = now.Add(time.Duration(*row.TimeoutMS) * time.Millisecond)
			}
		}
		lease := now.Add(sandboxToolCallRunLease)
		row.LeaseExpiresAt = &lease
	case "reject":
		if row.State == "dispatched" {
			requeueSandboxToolCall(row)
		}
	case "succeeded", "failed", "interrupted":
		state, err := reportedSandboxToolCallState(row, report)
		if err != nil {
			return err
		}
		row.State = state
		row.Result, row.ResultObjectKey, row.ResultBytes, row.ResultSHA256 = report.Result, report.ResultObjectKey, report.ResultBytes, report.ResultSHA256
		row.FinishedAt = &now
		if row.State == "succeeded" && row.Finalizer != nil {
			until := now.Add(time.Duration(row.FinalizeTimeoutMS) * time.Millisecond)
			row.State, row.FinalizeUntil, row.FinishedAt = "finalizing", &until, nil
		}
	default:
		return ErrToolCallConflict
	}
	return nil
}

func reportedSandboxToolCallState(row *sandboxToolCallRow, report SandboxToolCallReport) (string, error) {
	if report.Event != "interrupted" {
		return report.Event, nil
	}
	switch report.Reason {
	case "stop_requested", "deadline":
		if row.CancelReason == nil {
			row.CancelReason = &report.Reason
		}
		return "cancelled", nil
	case "executor_shutdown":
		if row.State == "cancel_requested" {
			return "cancelled", nil
		}
		code := "executor_interrupted"
		row.ErrorCode = &code
		return "failed", nil
	}
	return "", ErrToolCallConflict
}

const (
	sandboxToolCallSweepBatch      = 64
	sandboxToolCallSweepMaxBatches = 16
	sandboxToolCallFinalizeLease   = 35 * time.Second
)

func (d *DB) SweepSandboxToolCalls(ctx context.Context) ([]SandboxToolCall, error) {
	var changes []SandboxToolCall
	for range sandboxToolCallSweepMaxBatches {
		claimed := 0
		var batch []SandboxToolCall
		err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
			mapper := NewSandboxToolCallMapper(executor)
			now, err := mapper.Now(ctx)
			if err != nil {
				return err
			}
			rows, err := mapper.ClaimDue(ctx, now, sandboxToolCallSweepBatch)
			if err != nil {
				return err
			}
			claimed = len(rows)
			for _, row := range rows {
				prior := row.State
				expireSandboxToolCall(&row, now)
				if row.State == prior {
					continue
				}
				row.UpdatedAt = now
				if err := mapper.Update(ctx, row); err != nil {
					return err
				}
				batch = append(batch, row.model())
			}
			return nil
		})
		if err != nil {
			return changes, err
		}
		changes = append(changes, batch...)
		if claimed < sandboxToolCallSweepBatch {
			break
		}
	}
	return changes, nil
}

func (d *DB) ClaimDueSandboxToolCallFinalizations(ctx context.Context) ([]SandboxToolCall, error) {
	var calls []SandboxToolCall
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxToolCallMapper(executor)
		now, err := mapper.Now(ctx)
		if err != nil {
			return err
		}
		rows, err := mapper.ClaimDueFinalizations(ctx, now, sandboxToolCallSweepBatch)
		if err != nil {
			return err
		}
		for _, row := range rows {
			until := now.Add(sandboxToolCallFinalizeLease)
			row.FinalizeUntil, row.UpdatedAt = &until, now
			if err := mapper.Update(ctx, row); err != nil {
				return err
			}
			calls = append(calls, row.model())
		}
		return nil
	})
	return calls, err
}

func (d *DB) FinalizeSandboxToolCall(ctx context.Context, call SandboxToolCall, failure []byte) (SandboxToolCall, error) {
	var result SandboxToolCall
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSandboxToolCallMapper(executor)
		row, found, err := mapper.Lock(ctx, call.OrganizationUUID, call.WorkspaceUUID, call.UUID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if row.State == "finalizing" {
			row.State = "succeeded"
			if failure != nil {
				row.State, row.Result, row.ResultObjectKey = "failed", failure, nil
			}
			now, err := mapper.Now(ctx)
			if err != nil {
				return err
			}
			row.FinishedAt, row.UpdatedAt = &now, now
			if err := mapper.Update(ctx, row); err != nil {
				return err
			}
		}
		result = row.model()
		return nil
	})
	return result, err
}

func markPreparingToolCallSandbox(ctx context.Context, mapper SandboxToolCallMapper, scope SandboxToolCallScope) (bool, error) {
	state, found, err := mapper.SandboxState(ctx, scope)
	if err != nil || !found {
		return false, err
	}
	switch state {
	case "stopping", "stopped", "failed":
		return false, nil
	case "creating":
		_, err = mapper.MarkReady(ctx, scope)
		return err == nil, err
	}
	return true, nil
}

func (d *DB) IsSandboxToolCallReady(ctx context.Context, organizationUUID, workspaceUUID, sandboxUUID string) (bool, error) {
	return NewSandboxToolCallMapper(d.mapperDB).IsReady(ctx, organizationUUID, workspaceUUID, sandboxUUID)
}

type SandboxToolCallValidation struct {
	Failure        []byte
	Finalizer      *string
	FinalizerState []byte
	FinalizeAfter  time.Duration
}

func toolCallSubmissionResult(row sandboxToolCallRow, submission SandboxToolCallSubmission, result *SandboxToolCall) error {
	a, err := canonicalToolCallInput(row.Input)
	if err != nil {
		return ErrToolCallConflict
	}
	b, err := canonicalToolCallInput(submission.Input)
	if err != nil {
		return ErrToolCallConflict
	}
	if row.ToolName != "" && (row.ToolName != submission.Tool || string(a) != string(b)) {
		return ErrToolCallConflict
	}
	*result = row.model()
	return nil
}

func sameToolCallResult(row *sandboxToolCallRow, report SandboxToolCallReport) bool {
	return row.ResultSHA256 != nil && report.ResultSHA256 != nil && *row.ResultSHA256 == *report.ResultSHA256 && row.ResultBytes != nil && report.ResultBytes != nil && *row.ResultBytes == *report.ResultBytes
}

func recordCompletedToolCallReport(row *sandboxToolCallRow, report SandboxToolCallReport) error {
	switch report.Event {
	case "reject", "heartbeat", "started", "inspect":
		return nil
	}
	if row.State == "lost" {
		row.LateResult = report.Result
		if report.ResultObjectKey != nil {
			row.LateResult, _ = json.Marshal(struct {
				ObjectKey string  `json:"object_key"`
				Bytes     *int64  `json:"bytes"`
				SHA256    *string `json:"sha256"`
			}{*report.ResultObjectKey, report.ResultBytes, report.ResultSHA256})
		}
		return nil
	}
	if sameToolCallResult(row, report) {
		return nil
	}
	return ErrToolCallConflict
}

func lockToolCallWorker(ctx context.Context, mapper SandboxToolCallMapper, scope SandboxToolCallScope) (string, bool, error) {
	_, found, err := mapper.LockParent(ctx, scope)
	if err != nil || !found {
		return "", found, err
	}
	return mapper.LockWorker(ctx, scope)
}
func lockToolCallSandbox(ctx context.Context, mapper SandboxToolCallMapper, scope SandboxToolCallScope) (string, bool, error) {
	_, found, err := mapper.LockParent(ctx, scope)
	if err != nil || !found {
		return "", found, err
	}
	_, found, err = mapper.LockCode(ctx, scope)
	if err != nil || !found {
		return "", found, err
	}
	return mapper.LockSandbox(ctx, scope)
}

func canonicalToolCallInput(input []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
