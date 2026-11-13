package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SandboxToolCallMapper -sql ./sandbox_tool_call_mapper.xml -out ./sandbox_tool_call_mapper.sqlmap.gen.go -dialect postgres

type sandboxToolCallRow struct {
	UUID              string     `db:"uuid"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	CodeSessionUUID   string     `db:"code_session_uuid"`
	ExternalID        string     `db:"external_id"`
	ThreadID          string     `db:"thread_id"`
	WorkerEpoch       int64      `db:"worker_epoch"`
	SandboxUUID       *string    `db:"sandbox_uuid"`
	Attempt           int        `db:"attempt"`
	ToolName          string     `db:"tool_name"`
	Input             []byte     `db:"input"`
	State             string     `db:"state"`
	DeadlineAt        time.Time  `db:"deadline_at"`
	TimeoutMS         *int64     `db:"timeout_ms"`
	LeaseExpiresAt    *time.Time `db:"lease_expires_at"`
	CancelRequestedAt *time.Time `db:"cancel_requested_at"`
	CancelReason      *string    `db:"cancel_reason"`
	StopGraceUntil    *time.Time `db:"stop_grace_until"`
	Result            []byte     `db:"result"`
	ResultObjectKey   *string    `db:"result_object_key"`
	ResultBytes       *int64     `db:"result_bytes"`
	ResultSHA256      *string    `db:"result_sha256"`
	Finalizer         *string    `db:"finalizer"`
	FinalizerState    []byte     `db:"finalizer_state"`
	FinalizeTimeoutMS int64      `db:"finalize_timeout_ms"`
	FinalizeUntil     *time.Time `db:"finalize_until"`
	ErrorCode         *string    `db:"error_code"`
	LateResult        []byte     `db:"late_result"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	FinishedAt        *time.Time `db:"finished_at"`
}

type SandboxToolCallMapper interface {
	LockParent(ctx context.Context, scope SandboxToolCallScope) (string, bool, error)
	LockCode(ctx context.Context, scope SandboxToolCallScope) (string, bool, error)
	MarkReady(ctx context.Context, scope SandboxToolCallScope) (int64, error)
	SandboxState(ctx context.Context, scope SandboxToolCallScope) (string, bool, error)
	IsReady(ctx context.Context, organizationUUID, workspaceUUID, sandboxUUID string) (bool, error)
	LockWorker(ctx context.Context, scope SandboxToolCallScope) (string, bool, error)
	LockSandbox(ctx context.Context, scope SandboxToolCallScope) (string, bool, error)
	Find(ctx context.Context, scope SandboxToolCallScope) (sandboxToolCallRow, bool, error)
	Lock(ctx context.Context, organizationUUID, workspaceUUID, callUUID string) (sandboxToolCallRow, bool, error)
	Insert(ctx context.Context, row sandboxToolCallRow) error
	Update(ctx context.Context, row sandboxToolCallRow) error
	Claim(ctx context.Context, scope SandboxToolCallScope, codeSessionUUID string, limit int) ([]sandboxToolCallRow, error)
	Due(ctx context.Context, limit int) ([]sandboxToolCallRow, error)
}
