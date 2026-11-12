package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DSHExecutionMapper -sql ./dsh_execution_mapper.xml -out ./dsh_execution_mapper.sqlmap.gen.go -dialect postgres

type dshCodeRow struct {
	UUID       string `db:"uuid"`
	ExternalID string `db:"external_id"`
	SessionID  string `db:"session_external_id"`
}

type dshInputExecutionRow struct {
	CancelRequestedAt *time.Time `db:"cancel_requested_at"`
	StartedAt         *time.Time `db:"started_at"`
	CompletedAt       *time.Time `db:"completed_at"`
	InputID           string     `db:"input_id"`
	ThreadID          string     `db:"session_thread_id"`
	Status            string     `db:"status"`
	Result            *string    `db:"result"`
	FailureCode       *string    `db:"failure_code"`
	AcceptedAt        time.Time  `db:"accepted_at"`
}

type dshExecutionSummary struct {
	ActiveInputID       *string `db:"active_input_id"`
	HasEligibleAccepted bool    `db:"has_eligible_accepted"`
	LastResult          *string `db:"last_result"`
}

type dshPermissionRow struct {
	RequestID     string  `db:"request_id"`
	InputID       *string `db:"input_id"`
	DSHSessionID  string  `db:"dsh_session_id"`
	ThreadID      string  `db:"session_thread_id"`
	ToolUseID     string  `db:"tool_use_id"`
	Turn          *string `db:"turn"`
	Tool          []byte  `db:"tool"`
	Input         []byte  `db:"input"`
	Available     *bool   `db:"available"`
	PublicEventID string  `db:"public_event_id"`
	State         string  `db:"state"`
	Decision      []byte  `db:"decision"`
}

type dshPermissionInsert struct {
	Scope         DSHScope
	RequestID     string
	InputID       *string
	DSHSessionID  string
	ThreadID      string
	ToolUseID     string
	Turn          *string
	Tool          []byte
	Input         []byte
	Available     *bool
	PublicEventID string
	State         string
	Decision      []byte
}

type DSHExecutionMapper interface {
	HasSession(ctx context.Context, organizationUUID, workspaceUUID, sessionUUID string) (bool, error)
	NextExecution(ctx context.Context, scope DSHScope) (dshInputExecutionRow, bool, error)
	InterruptExecution(ctx context.Context, scope DSHScope, threadID string) (int64, error)
	FindCode(ctx context.Context, organizationUUID, workspaceUUID, sessionUUID string) (dshCodeRow, bool, error)
	InsertExecution(ctx context.Context, scope DSHScope, inputID, threadID string, acceptedAt time.Time) (int64, error)
	Execution(ctx context.Context, scope DSHScope, inputID string) (dshInputExecutionRow, bool, error)
	Summary(ctx context.Context, scope DSHScope) (dshExecutionSummary, error)
	StartExecution(ctx context.Context, scope DSHScope, inputID string) (int64, error)
	FinishExecution(ctx context.Context, scope DSHScope, inputID, result string, failureCode *string) (int64, error)
	Permission(ctx context.Context, scope DSHScope, requestID string) (dshPermissionRow, bool, error)
	Permissions(ctx context.Context, scope DSHScope) ([]dshPermissionRow, error)
	InsertPermission(ctx context.Context, input dshPermissionInsert) (int64, error)
	SetPermission(ctx context.Context, scope DSHScope, requestID, state string, decision []byte) (int64, error)
	ClosePermissions(ctx context.Context, scope DSHScope, inputID, threadID string) (int64, error)
	Receipt(ctx context.Context, scope DSHScope, eventID string) (string, bool, error)
	InsertReceipt(ctx context.Context, scope DSHScope, eventID, payloadDigest string) (int64, error)
	BindTranscript(ctx context.Context, scope DSHScope, dshSessionID, threadID string) (int64, error)
}
