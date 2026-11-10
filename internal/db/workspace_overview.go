package db

import (
	"context"
	"time"
)

type WorkspaceOverviewQuery struct {
	OrganizationUUID string
	WorkspaceUUID    string
	Start            time.Time
	End              time.Time
	Timezone         string
}

type WorkspaceOverviewBucket struct {
	Date               string    `db:"date"`
	AgentID            string    `db:"agent_id"`
	AgentName          string    `db:"agent_name"`
	SessionCount       int64     `db:"session_count"`
	FailedSessionCount int64     `db:"failed_session_count"`
	LastRunAt          time.Time `db:"last_run_at"`
}

func (d *DB) AggregateWorkspaceOverview(ctx context.Context, query WorkspaceOverviewQuery) ([]WorkspaceOverviewBucket, error) {
	return NewWorkspaceOverviewMapper(d.mapperDB).Aggregate(ctx, query)
}

func (d *DB) CountWorkspaceOverviewAgents(ctx context.Context, organizationUUID, workspaceUUID string) (int64, error) {
	return NewWorkspaceOverviewMapper(d.mapperDB).CountAgents(ctx, organizationUUID, workspaceUUID)
}
