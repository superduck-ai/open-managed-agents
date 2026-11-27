package db

import (
	"context"
	"encoding/json"
	"time"
)

type ProviderUsageRecord struct {
	ID                 int64           `db:"id"`
	OrganizationUUID   string          `db:"organization_uuid"`
	WorkspaceUUID      string          `db:"workspace_uuid"`
	RuntimeProfileUUID string          `db:"runtime_profile_uuid"`
	ProviderKind       string          `db:"provider_kind"`
	SessionExternalID  string          `db:"session_external_id"`
	RemoteSessionID    string          `db:"remote_session_id"`
	MetricUnit         string          `db:"metric_unit"`
	CumulativeAmount   float64         `db:"cumulative_amount"`
	DeltaAmount        float64         `db:"delta_amount"`
	RawUsage           json.RawMessage `db:"raw_usage"`
	SampledAt          time.Time       `db:"sampled_at"`
}

type ProviderUsageSummary struct {
	MetricUnit   string  `db:"metric_unit"`
	TotalDelta   float64 `db:"total_delta"`
	SessionCount int64   `db:"session_count"`
}

type SumWorkspaceProviderUsageParams struct {
	OrganizationUUID   string
	WorkspaceUUID      string
	SessionExternalIDs []string
	Start              *time.Time
	End                *time.Time
}

func (d *DB) RecordProviderUsage(ctx context.Context, record ProviderUsageRecord) (ProviderUsageRecord, error) {
	rawUsage := record.RawUsage
	if len(rawUsage) == 0 {
		rawUsage = json.RawMessage(`{}`)
	}
	sampledAt := record.SampledAt
	if sampledAt.IsZero() {
		sampledAt = time.Now().UTC()
	}
	return NewProviderUsageMapper(d.mapperDB).Upsert(ctx, upsertProviderUsageParams{
		OrganizationUUID:   record.OrganizationUUID,
		WorkspaceUUID:      record.WorkspaceUUID,
		RuntimeProfileUUID: record.RuntimeProfileUUID,
		ProviderKind:       record.ProviderKind,
		SessionExternalID:  record.SessionExternalID,
		RemoteSessionID:    record.RemoteSessionID,
		MetricUnit:         record.MetricUnit,
		CumulativeAmount:   record.CumulativeAmount,
		DeltaAmount:        record.DeltaAmount,
		RawUsage:           string(rawUsage),
		SampledAt:          sampledAt,
	})
}

func (d *DB) SumWorkspaceProviderUsage(ctx context.Context, params SumWorkspaceProviderUsageParams) ([]ProviderUsageSummary, error) {
	return NewProviderUsageMapper(d.mapperDB).SumByWorkspace(ctx, sumWorkspaceProviderUsageQuery{
		OrganizationUUID:   params.OrganizationUUID,
		WorkspaceUUID:      params.WorkspaceUUID,
		SessionExternalIDs: params.SessionExternalIDs,
		Start:              params.Start,
		End:                params.End,
	})
}
