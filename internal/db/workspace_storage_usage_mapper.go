package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkspaceStorageUsageMapper -sql ./workspace_storage_usage_mapper.xml -out ./workspace_storage_usage_mapper.sqlmap.gen.go -dialect postgres

type WorkspaceStorageUsageMapper interface {
	LockWorkspace(ctx context.Context, workspaceUUID string) error
	GetWorkspaceStorageBytes(ctx context.Context, workspaceUUID string) (int64, error)
	ReconcileWorkspaceStorageUsage(ctx context.Context, workspaceUUID string) (workspaceStorageUsage, error)
	UpsertWorkspaceStorageUsage(ctx context.Context, params workspaceStorageUsageParams) error
	EnsureWorkspaceStorageUsage(ctx context.Context, workspaceUUID string) error
	GetWorkspaceStorageUsageForUpdate(ctx context.Context, workspaceUUID string) (workspaceStorageUsage, error)
	UpdateWorkspaceStorageUsage(ctx context.Context, params workspaceStorageUsageParams) error
}

type workspaceStorageUsageParams struct {
	WorkspaceUUID  string
	FilesBytes     int64
	FilestoreBytes int64
}
