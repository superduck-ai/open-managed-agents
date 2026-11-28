package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper CloudHarnessMapper -sql ./cloud_harness_mapper.xml -out ./cloud_harness_mapper.sqlmap.gen.go -dialect postgres

type harnessTurnRow struct {
	UUID               string  `db:"uuid"`
	OrganizationUUID   string  `db:"organization_uuid"`
	WorkspaceUUID      string  `db:"workspace_uuid"`
	SessionUUID        string  `db:"session_uuid"`
	InputEventUUID     string  `db:"input_event_uuid"`
	State              string  `db:"state"`
	WorkerEpoch        int64   `db:"worker_epoch"`
	Attempts           int     `db:"attempts"`
	TaskUUID           *string `db:"task_uuid"`
	ToolUUID           *string `db:"tool_uuid"`
	TaskKind           string  `db:"task_kind"`
	SubagentThreadUUID *string `db:"subagent_thread_uuid"`
	DelegationToolUUID *string `db:"delegation_tool_uuid"`
	DenyMessage        string  `db:"deny_message"`
	Decision           string  `db:"decision"`
	ResumeInputUUID    *string `db:"resume_input_uuid"`
}

type harnessWakeRow struct {
	UUID             string `db:"uuid"`
	OrganizationUUID string `db:"organization_uuid"`
	WorkspaceUUID    string `db:"workspace_uuid"`
	SessionUUID      string `db:"session_uuid"`
	ResumeInputUUID  string `db:"resume_input_uuid"`
	Kind             string `db:"resume_input_kind"`
}

type harnessInterruptParams struct {
	Ref      HarnessTurnRef
	State    string
	TaskUUID *string
	ToolUUID *string
	Kind     string
}

type harnessClaimParams struct {
	Ref             HarnessTurnRef
	CodeSessionUUID string
	TokenHash       string
	LeaseUntil      time.Time
}

type harnessLeaseParams struct {
	Lease      HarnessTurnLease
	LeaseUntil time.Time
}

type harnessFinishParams struct {
	Lease HarnessTurnLease
	State string
}

type harnessToolRow struct {
	UUID               string  `db:"uuid"`
	CallID             string  `db:"call_id"`
	Name               string  `db:"tool_name"`
	Arguments          []byte  `db:"arguments"`
	Result             *string `db:"result_text"`
	Failed             bool    `db:"result_failed"`
	DelegationToolUUID *string `db:"delegation_tool_uuid"`
}

type harnessTaskParams struct {
	Lease              HarnessTurnLease
	TaskUUID           string
	ToolUUID           string
	Kind               string
	State              string
	SubagentThreadUUID *string
	DelegationToolUUID *string
}

type harnessToolRecord struct {
	Key       harnessThreadKey
	MessageID string
	TurnUUID  string
	ToolUUID  string
}

type harnessToolResultParams struct {
	Ref      HarnessTurnRef
	ToolUUID string
	Text     string
	Failed   bool
}

type CloudHarnessMapper interface {
	CreateForInput(ctx context.Context, ref HarnessTurnRef) (harnessTurnRow, error)
	InterruptTurn(ctx context.Context, params harnessInterruptParams) (int64, error)
	Find(ctx context.Context, ref HarnessTurnRef) (harnessTurnRow, bool, error)
	Lock(ctx context.Context, ref HarnessTurnRef) (harnessTurnRow, bool, error)
	LockCurrentTurn(ctx context.Context, ref HarnessTurnRef) (harnessTurnRow, bool, error)
	LockWaitingChoice(ctx context.Context, ref HarnessTurnRef) (harnessTurnRow, bool, error)
	FindResumeWork(ctx context.Context, ref HarnessTurnRef, kind string) (harnessTurnRow, bool, error)
	ResumeApproval(ctx context.Context, ref HarnessTurnRef, inputUUID, decision, denyMessage string) (int64, error)
	ResumeChoice(ctx context.Context, ref HarnessTurnRef, taskUUID string) (int64, error)
	ParkTool(ctx context.Context, params harnessLeaseParams, toolUUID string) (int64, error)
	ListReadyInputs(ctx context.Context, limit int) ([]harnessWakeRow, error)
	MarkInputPublished(ctx context.Context, ref HarnessTurnRef, inputUUID string) error
	GrantModelLease(ctx context.Context, params harnessClaimParams) (int64, error)
	Claim(ctx context.Context, params harnessLeaseParams) (int64, error)
	RenewTurn(ctx context.Context, params harnessLeaseParams) (int64, error)
	RenewModelLease(ctx context.Context, params harnessLeaseParams) (int64, error)
	RevokeModelLease(ctx context.Context, lease HarnessTurnLease) error
	ReleaseTurn(ctx context.Context, lease HarnessTurnLease) (int64, error)
	StartModel(ctx context.Context, lease HarnessTurnLease) (int64, error)
	LockModelLease(ctx context.Context, lease HarnessTurnLease) (int64, error)
	Finish(ctx context.Context, params harnessFinishParams) (int64, error)
	AttachTool(ctx context.Context, params harnessToolRecord) (int64, error)
	FindTool(ctx context.Context, ref HarnessTurnRef, toolUUID string) (harnessToolRow, bool, error)
	ListTools(ctx context.Context, ref HarnessTurnRef) ([]harnessToolRow, error)
	CountExecutionTools(ctx context.Context, ref HarnessTurnRef, delegationToolUUID *string) (int, error)
	FinishTool(ctx context.Context, params harnessToolResultParams) (int64, error)
	AdvanceTask(ctx context.Context, params harnessTaskParams) (int64, error)
}
