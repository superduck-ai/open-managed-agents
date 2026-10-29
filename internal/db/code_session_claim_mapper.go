package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper CodeSessionClaimMapper -sql ./code_session_claim_mapper.xml -out ./code_session_claim_mapper.sqlmap.gen.go -dialect postgres

type codeSessionClaimCandidateRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	SessionExternalID   string     `db:"session_external_id"`
	CurrentWorkerEpoch  int64      `db:"current_worker_epoch"`
	WorkerClaimAttempts int        `db:"worker_claim_attempts"`
	RequestedAt         *time.Time `db:"worker_claim_requested_at"`
	TraceParent         *string    `db:"worker_claim_trace_parent"`
}

type claimCodeSessionParams struct {
	UUID                 string
	WorkerID             string
	ClaimExpiresAt       time.Time
	OAuthAccessTokenHash string
}

type releaseCodeSessionClaimParams struct {
	ExternalID string
	WorkerID   string
	Epoch      int64
	Kind       CodeSessionClaimReleaseKind
}

type failedCodeSessionClaimRow struct {
	UUID               string `db:"uuid"`
	CurrentWorkerEpoch int64  `db:"current_worker_epoch"`
}

type failCodeSessionClaimParams struct {
	ExternalID  string
	WorkerID    string
	Epoch       int64
	MaxAttempts int
	HoldUntil   time.Time
}

type codeSessionWorkerClaimStateRow struct {
	CurrentWorkerEpoch int64 `db:"current_worker_epoch"`
	ClaimReleased      bool  `db:"claim_released"`
}

type CodeSessionClaimMapper interface {
	LockNextClaimable(ctx context.Context, leaseGraceMicros int64) (codeSessionClaimCandidateRow, bool, error)
	Claim(ctx context.Context, params claimCodeSessionParams) (int64, error)
	HoldExhausted(ctx context.Context, uuid string, holdUntil time.Time) (int64, error)
	ResetExhausted(ctx context.Context, uuid string, epoch int64) (int64, error)
	FailPermanently(ctx context.Context, params failCodeSessionClaimParams) (failedCodeSessionClaimRow, error)
	Release(ctx context.Context, params releaseCodeSessionClaimParams) (int64, error)
	FindWorkerTransport(ctx context.Context, externalID string) (string, error)
	FindWorkerClaimState(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (codeSessionWorkerClaimStateRow, bool, error)
	Request(ctx context.Context, organizationUUID, workspaceUUID, externalID string, traceParent *string) (int64, error)
}
