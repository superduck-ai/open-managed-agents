package db

import (
	"context"
	"time"
)

// WorkspaceMCPServer 是工作区内可复用的远程 MCP 配置。
// 凭据、Agent 工具权限和工具目录快照不属于该实体。
type WorkspaceMCPServer struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	Name             string
	TransportType    string
	EndpointURL      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ArchivedAt       *time.Time
	DeletedAt        *time.Time
}

type WorkspaceMCPServerPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListWorkspaceMCPServersPageParams struct {
	WorkspaceUUID   string
	Search          string
	Limit           int
	Cursor          *WorkspaceMCPServerPageCursor
	IncludeArchived bool
}

func (d *DB) CreateWorkspaceMCPServer(ctx context.Context, server WorkspaceMCPServer) (WorkspaceMCPServer, error) {
	row, err := NewMCPServerMapper(d.mapperDB).Insert(ctx, mcpServerWriteParamsFrom(server))
	if isUniqueViolation(err) {
		return WorkspaceMCPServer{}, ErrDuplicate
	}
	if err != nil {
		return WorkspaceMCPServer{}, err
	}
	return row.workspaceMCPServer(), nil
}

func (d *DB) ListWorkspaceMCPServersPage(ctx context.Context, params ListWorkspaceMCPServersPageParams) ([]WorkspaceMCPServer, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	rows, err := NewMCPServerMapper(d.mapperDB).ListPage(ctx, mcpServerPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, Search: params.Search, FetchLimit: params.Limit + 1,
		Cursor: params.Cursor, IncludeArchived: params.IncludeArchived,
	})
	if err != nil {
		return nil, false, err
	}
	servers := make([]WorkspaceMCPServer, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, row.workspaceMCPServer())
	}
	hasMore := len(servers) > params.Limit
	if hasMore {
		servers = servers[:params.Limit]
	}
	return servers, hasMore, nil
}

func (d *DB) GetWorkspaceMCPServer(ctx context.Context, workspaceUUID, externalID string) (WorkspaceMCPServer, error) {
	row, err := NewMCPServerMapper(d.mapperDB).FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return WorkspaceMCPServer{}, mapNoRows(err)
	}
	return row.workspaceMCPServer(), nil
}

func (d *DB) UpdateWorkspaceMCPServer(ctx context.Context, workspaceUUID, externalID string, next WorkspaceMCPServer) (WorkspaceMCPServer, error) {
	params := mcpServerWriteParamsFrom(next)
	params.WorkspaceUUID = workspaceUUID
	params.ExternalID = externalID
	row, err := NewMCPServerMapper(d.mapperDB).UpdateByExternalID(ctx, params)
	if isUniqueViolation(err) {
		return WorkspaceMCPServer{}, ErrDuplicate
	}
	if err != nil {
		return WorkspaceMCPServer{}, mapNoRows(err)
	}
	return row.workspaceMCPServer(), nil
}

func (d *DB) ArchiveWorkspaceMCPServer(ctx context.Context, workspaceUUID, externalID string) (WorkspaceMCPServer, error) {
	row, err := NewMCPServerMapper(d.mapperDB).ArchiveByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return WorkspaceMCPServer{}, mapNoRows(err)
	}
	return row.workspaceMCPServer(), nil
}

func (d *DB) DeleteWorkspaceMCPServer(ctx context.Context, workspaceUUID, externalID string) error {
	_, err := NewMCPServerMapper(d.mapperDB).SoftDeleteByExternalID(ctx, workspaceUUID, externalID)
	return mapNoRows(err)
}

func mcpServerWriteParamsFrom(server WorkspaceMCPServer) mcpServerWriteParams {
	return mcpServerWriteParams{
		UUID: server.UUID, ExternalID: server.ExternalID,
		OrganizationUUID: server.OrganizationUUID, WorkspaceUUID: server.WorkspaceUUID,
		Name: server.Name, TransportType: server.TransportType, EndpointURL: server.EndpointURL,
		CreatedAt: server.CreatedAt, UpdatedAt: server.UpdatedAt,
	}
}

func (r mcpServerMapperRow) workspaceMCPServer() WorkspaceMCPServer {
	return WorkspaceMCPServer{
		UUID: r.UUID, ExternalID: r.ExternalID,
		OrganizationUUID: r.OrganizationUUID, WorkspaceUUID: r.WorkspaceUUID,
		Name: r.Name, TransportType: r.TransportType, EndpointURL: r.EndpointURL,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt, DeletedAt: r.DeletedAt,
	}
}
