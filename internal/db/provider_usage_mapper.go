package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ProviderUsageMapper -sql ./provider_usage_mapper.xml -out ./provider_usage_mapper.sqlmap.gen.go -dialect postgres

type upsertProviderUsageParams struct {
	OrganizationUUID   string
	WorkspaceUUID      string
	RuntimeProfileUUID string
	ProviderKind       string
	SessionExternalID  string
	RemoteSessionID    string
	MetricUnit         string
	CumulativeAmount   float64
	DeltaAmount        float64
	RawUsage           string
	SampledAt          time.Time
}

type sumWorkspaceProviderUsageQuery struct {
	OrganizationUUID   string
	WorkspaceUUID      string
	SessionExternalIDs []string
	Start              *time.Time
	End                *time.Time
}

type ProviderUsageMapper interface {
	Upsert(ctx context.Context, params upsertProviderUsageParams) (ProviderUsageRecord, error)
	SumByWorkspace(ctx context.Context, query sumWorkspaceProviderUsageQuery) ([]ProviderUsageSummary, error)
}
