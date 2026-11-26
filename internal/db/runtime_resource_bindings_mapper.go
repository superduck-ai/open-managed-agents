package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper RuntimeResourceBindingMapper -sql ./runtime_resource_bindings_mapper.xml -out ./runtime_resource_bindings_mapper.sqlmap.gen.go -dialect postgres

type upsertRuntimeResourceBindingParams struct {
	OrganizationUUID    string
	WorkspaceUUID       string
	RuntimeProfileUUID  string
	ProviderKind        string
	ResourceType        string
	OMAExternalID       string
	RemoteID            string
	ParentOMAExternalID string
	ResourceRole        string
	State               string
	Summary             string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type listRuntimeResourceBindingsQuery struct {
	OrganizationUUID    string
	WorkspaceUUID       string
	ResourceType        string
	ParentOMAExternalID string
	ResourceRole        string
	IncludeArchived     bool
	Limit               int
	Offset              int
}

type searchRuntimeResourceBindingsQuery struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ResourceType     string
	Pattern          string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

type updateRuntimeResourceBindingParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ResourceType     string
	OMAExternalID    string
	State            string
	Summary          string
}

type RuntimeResourceBindingMapper interface {
	Upsert(ctx context.Context, params upsertRuntimeResourceBindingParams) (RuntimeResourceBinding, error)
	InsertIfAbsent(ctx context.Context, params upsertRuntimeResourceBindingParams) (RuntimeResourceBinding, error)
	FindByOMAID(ctx context.Context, organizationUUID string, workspaceUUID string, resourceType string, omaExternalID string) (RuntimeResourceBinding, error)
	FindByRemoteID(ctx context.Context, runtimeProfileUUID string, resourceType string, remoteID string) (RuntimeResourceBinding, error)
	ListByWorkspaceAndType(ctx context.Context, query listRuntimeResourceBindingsQuery) ([]RuntimeResourceBinding, error)
	SearchByWorkspaceAndType(ctx context.Context, query searchRuntimeResourceBindingsQuery) ([]RuntimeResourceBinding, error)
	UpdateSummaryAndState(ctx context.Context, params updateRuntimeResourceBindingParams) (RuntimeResourceBinding, error)
	SoftDelete(ctx context.Context, organizationUUID string, workspaceUUID string, resourceType string, omaExternalID string) (int64, error)
	LockAgentInTx(ctx context.Context, organizationUUID string, workspaceUUID string, omaExternalID string) (RuntimeResourceBinding, error)
	AggregateExternalOverview(ctx context.Context, query WorkspaceOverviewQuery) ([]WorkspaceOverviewBucket, error)
}
