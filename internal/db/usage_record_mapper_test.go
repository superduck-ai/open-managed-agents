package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestUsageMapperBoundScope(t *testing.T) {
	now := time.Now().UTC()
	query := UsageQuery{OrganizationUUID: "org", WorkspaceUUIDs: []string{"first", "second"}, Timezone: "UTC", Start: now, End: now.Add(time.Hour), AsOf: now, Model: "model' OR TRUE --", Source: "api", SessionID: "session", APIKeyUUID: "key"}
	bound := buildUsageRecordMapperAggregate(yourbatis.DialectPostgres, query)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: usageRecordMapperAggregateStatement, bound: bound,
		wantID: "UsageRecordMapper.Aggregate", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"query.Timezone", "query.OrganizationUUID", "workspace", "workspace", "query.Start", "query.End", "query.AsOf", "query.Model", "query.Source", "query.SessionID", "query.APIKeyUUID"},
		wantSQLFragments:  []string{"organization_uuid = $2", "workspace_uuid IN ( $3 , $4 )", "ended_at >= $5", "ended_at < $6", "recorded_at <= $7", "model = $8", "source = $9", "session_external_id = $10", "api_key_uuid = $11"},
	})
	if strings.Contains(bound.SQL, query.Model) || !reflect.DeepEqual(bound.Values(), []any{"UTC", "org", "first", "second", now, now.Add(time.Hour), now, query.Model, "api", "session", "key"}) {
		t.Fatalf("筛选未正确绑定：%+v", bound)
	}
	dimensions := buildUsageRecordMapperDimensions(yourbatis.DialectPostgres, query)
	assertMapperBuilderContract(t, mapperBuilderContract{statement: usageRecordMapperDimensionsStatement, bound: dimensions, wantID: "UsageRecordMapper.Dimensions", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"query.OrganizationUUID", "workspace", "workspace"}, wantSQLFragments: []string{"organization_uuid = $1", "workspace_uuid IN ( $2 , $3 )"}})
}
