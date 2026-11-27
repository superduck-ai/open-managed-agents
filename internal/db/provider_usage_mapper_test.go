package db

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestProviderUsageMapperBuildersAndScan(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	orgUUID := "11111111-1111-4111-8111-111111111111"
	wsUUID := "22222222-2222-4222-8222-222222222222"
	profUUID := "33333333-3333-4333-8333-333333333333"

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: providerUsageMapperUpsertStatement,
		bound: buildProviderUsageMapperUpsert(yourbatis.DialectPostgres, upsertProviderUsageParams{
			OrganizationUUID:   orgUUID,
			WorkspaceUUID:      wsUUID,
			RuntimeProfileUUID: profUUID,
			ProviderKind:       "qoder_cloud",
			SessionExternalID:  "ses_qdr_1",
			RemoteSessionID:    "ses_remote_1",
			MetricUnit:         "credits",
			CumulativeAmount:   12.5,
			DeltaAmount:        2.5,
			RawUsage:           `{"total_credits":12.5}`,
			SampledAt:          now,
		}),
		wantID:   "ProviderUsageMapper.Upsert",
		wantKind: yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.OrganizationUUID", "params.WorkspaceUUID", "params.RuntimeProfileUUID",
			"params.ProviderKind", "params.SessionExternalID", "params.RemoteSessionID",
			"params.MetricUnit", "params.CumulativeAmount", "params.DeltaAmount",
			"params.RawUsage", "params.SampledAt",
		},
		wantSQLFragments: []string{"INSERT INTO provider_usage_records", "ON CONFLICT (workspace_uuid, session_external_id, metric_unit, sampled_at)", "RETURNING"},
	})

	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: providerUsageMapperSumByWorkspaceStatement,
		bound: buildProviderUsageMapperSumByWorkspace(yourbatis.DialectPostgres, sumWorkspaceProviderUsageQuery{
			OrganizationUUID:   orgUUID,
			WorkspaceUUID:      wsUUID,
			SessionExternalIDs: []string{"ses_1", "ses_2"},
			Start:              &start,
			End:                &end,
		}),
		wantID:            "ProviderUsageMapper.SumByWorkspace",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"query.OrganizationUUID", "query.WorkspaceUUID", "sessionID", "sessionID", "query.Start", "query.End"},
		wantSQLFragments:  []string{"FROM provider_usage_records", "session_external_id IN", "$3", "$4", "sampled_at >= $5", "sampled_at < $6", "GROUP BY metric_unit"},
	})

	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{"metric_unit", "total_delta", "session_count"},
		rows:    [][]driver.Value{{"credits", float64(42.25), int64(3)}},
	})
	rows, err := NewProviderUsageMapper(executor).SumByWorkspace(context.Background(), sumWorkspaceProviderUsageQuery{
		OrganizationUUID: orgUUID,
		WorkspaceUUID:    wsUUID,
	})
	if err != nil || len(rows) != 1 || rows[0].MetricUnit != "credits" || rows[0].TotalDelta != 42.25 || rows[0].SessionCount != 3 {
		t.Fatalf("SumByWorkspace() = (%+v, %v)", rows, err)
	}

	var _ func(*DB, context.Context, ProviderUsageRecord) (ProviderUsageRecord, error) = (*DB).RecordProviderUsage
	var _ func(*DB, context.Context, SumWorkspaceProviderUsageParams) ([]ProviderUsageSummary, error) = (*DB).SumWorkspaceProviderUsage
	_ = json.RawMessage(`{}`)
}
