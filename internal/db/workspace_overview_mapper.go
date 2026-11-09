package db

import (
	"context"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkspaceOverviewMapper -sql ./workspace_overview_mapper.xml -out ./workspace_overview_mapper.sqlmap.gen.go -dialect postgres

type WorkspaceOverviewMapper interface {
	Aggregate(ctx context.Context, query WorkspaceOverviewQuery) ([]WorkspaceOverviewBucket, error)
	CountAgents(ctx context.Context, organizationUUID, workspaceUUID string) (int64, error)
}
