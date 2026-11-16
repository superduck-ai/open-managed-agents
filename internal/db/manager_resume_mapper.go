package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper ManagerResumeMapper -sql ./manager_resume_mapper.xml -out ./manager_resume_mapper.sqlmap.gen.go -dialect postgres

type managerResumeRow struct {
	OrganizationUUID string `db:"organization_uuid"`
	WorkspaceUUID    string `db:"workspace_uuid"`
	SandboxUUID      string `db:"sandbox_uuid"`
	SessionID        string `db:"session_id"`
	CodeSessionID    string `db:"code_session_id"`
	WorkDir          string `db:"work_dir"`
	Transport        string `db:"transport"`
	Version          string `db:"version"`
	Phase            string `db:"phase"`
}

type ManagerResumeMapper interface {
	RecordVersion(ctx context.Context, target ManagerResumeTarget, version string) (int64, error)
	Find(ctx context.Context, workspaceUUID, providerID string) (managerResumeRow, bool, error)
	TryLock(ctx context.Context, sandboxUUID string) (bool, error)
	SetPhase(ctx context.Context, target ManagerResumeTarget, phase string) (int64, error)
	IsIdle(ctx context.Context, target ManagerResumeTarget) (bool, error)
}
