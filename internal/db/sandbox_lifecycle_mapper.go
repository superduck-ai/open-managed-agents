package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SandboxLifecycleMapper -sql ./sandbox_lifecycle_mapper.xml -out ./sandbox_lifecycle_mapper.sqlmap.gen.go -dialect postgres

type sandboxLifecycleRow struct {
	OrganizationUUID      string  `db:"organization_uuid"`
	WorkspaceUUID         string  `db:"workspace_uuid"`
	SessionExternalID     string  `db:"session_external_id"`
	CodeSessionExternalID string  `db:"code_session_external_id"`
	CodeSessionUUID       string  `db:"code_session_uuid"`
	SandboxUUID           string  `db:"sandbox_uuid"`
	ProviderSandboxID     string  `db:"provider_sandbox_id"`
	State                 string  `db:"state"`
	StopReason            *string `db:"stop_reason"`
}

type sandboxLifecycleScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	SandboxUUID      string
}

// SandboxLifecycleMapper selects and claims idle sandbox attempts for deletion.
type SandboxLifecycleMapper interface {
	ListCandidates(ctx context.Context, cutoff time.Time, after string, limit int, reclaim bool) ([]sandboxLifecycleRow, error)
	FindTarget(ctx context.Context, scope sandboxLifecycleScope) (sandboxLifecycleRow, bool, error)
	LockTarget(ctx context.Context, scope sandboxLifecycleScope) (sandboxLifecycleRow, bool, error)
	Claim(ctx context.Context, codeSessionUUID, sandboxUUID string, cutoff time.Time) (int64, error)
	BeginStop(ctx context.Context, scope sandboxLifecycleScope) error
	FinishStop(ctx context.Context, scope sandboxLifecycleScope) (int64, error)
}
