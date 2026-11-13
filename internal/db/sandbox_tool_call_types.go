package db

import "time"

type SandboxToolCall struct {
	UUID              string
	OrganizationUUID  string
	WorkspaceUUID     string
	CodeSessionUUID   string
	ExternalID        string
	ThreadID          string
	WorkerEpoch       int64
	SandboxUUID       *string
	Attempt           int
	ToolName          string
	Input             []byte
	State             string
	DeadlineAt        time.Time
	TimeoutMS         *int64
	LeaseExpiresAt    *time.Time
	CancelRequestedAt *time.Time
	CancelReason      *string
	StopGraceUntil    *time.Time
	Result            []byte
	ResultObjectKey   *string
	ResultBytes       *int64
	ResultSHA256      *string
	Finalizer         *string
	FinalizerState    []byte
	FinalizeTimeoutMS int64
	FinalizeUntil     *time.Time
	ErrorCode         *string
	LateResult        []byte
	CreatedAt         time.Time
	UpdatedAt         time.Time
	FinishedAt        *time.Time
}

func (r sandboxToolCallRow) model() SandboxToolCall {
	return SandboxToolCall{
		UUID:              r.UUID,
		OrganizationUUID:  r.OrganizationUUID,
		WorkspaceUUID:     r.WorkspaceUUID,
		CodeSessionUUID:   r.CodeSessionUUID,
		ExternalID:        r.ExternalID,
		ThreadID:          r.ThreadID,
		WorkerEpoch:       r.WorkerEpoch,
		SandboxUUID:       r.SandboxUUID,
		Attempt:           r.Attempt,
		ToolName:          r.ToolName,
		Input:             r.Input,
		State:             r.State,
		DeadlineAt:        r.DeadlineAt,
		TimeoutMS:         r.TimeoutMS,
		LeaseExpiresAt:    r.LeaseExpiresAt,
		CancelRequestedAt: r.CancelRequestedAt,
		CancelReason:      r.CancelReason,
		StopGraceUntil:    r.StopGraceUntil,
		Result:            r.Result,
		ResultObjectKey:   r.ResultObjectKey,
		ResultBytes:       r.ResultBytes,
		ResultSHA256:      r.ResultSHA256,
		Finalizer:         r.Finalizer,
		FinalizerState:    r.FinalizerState,
		FinalizeTimeoutMS: r.FinalizeTimeoutMS,
		FinalizeUntil:     r.FinalizeUntil,
		ErrorCode:         r.ErrorCode,
		LateResult:        r.LateResult,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
		FinishedAt:        r.FinishedAt,
	}
}

type SandboxToolCallScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	CodeSessionID    string
	ThreadID         string
	CallID           string
	SandboxUUID      string
	WorkerEpoch      int64
}
