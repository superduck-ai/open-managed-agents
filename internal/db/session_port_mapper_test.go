package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionPortMapperFindTargetResolvesLatestActiveRuntime(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionPortMapperFindTargetStatement,
		bound:     buildSessionPortMapperFindTarget(yourbatis.DialectPostgres, "session-uuid"),
		wantID:    "SessionPortMapper.FindTarget", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"sessionUUID"},
		wantSQLFragments: []string{"WHERE s.uuid = $1", "s.archived_at IS NOT NULL OR s.deleted_at IS NOT NULL",
			"code.organization_uuid = s.organization_uuid", "code.status = 'active'", "work.state = 'active'",
			"sb.provider_sandbox_id IS NOT NULL", "ORDER BY sb.created_at DESC"},
	})
}

func TestSessionPortMapperTouchIsTenantScopedAndThrottled(t *testing.T) {
	now := time.Date(2026, time.November, 12, 0, 0, 0, 0, time.UTC)
	params := sessionPortAccessParams{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code", Now: now, Threshold: now.Add(-time.Minute)}
	bound := buildSessionPortMapperTouchPortLastAccess(yourbatis.DialectPostgres, params)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionPortMapperTouchPortLastAccessStatement, bound: bound,
		wantID: "SessionPortMapper.TouchPortLastAccess", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.Now", "params.OrganizationUUID", "params.WorkspaceUUID", "params.CodeSessionUUID", "params.Threshold"},
		wantSQLFragments:  []string{"port_last_access_at = $1", "organization_uuid = $2", "workspace_uuid = $3", "uuid = $4", "port_last_access_at < $5"},
	})
	values := make([]any, len(bound.Args))
	for i, arg := range bound.Args {
		values[i] = arg.Value
	}
	if !reflect.DeepEqual(values, []any{now, "org", "workspace", "code", now.Add(-time.Minute)}) {
		t.Fatalf("bound values = %#v", values)
	}
}
