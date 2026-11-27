package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestRuntimeResourceBindingMapperBuilders(t *testing.T) {
	orgUUID := "11111111-1111-4111-8111-111111111111"
	wsUUID := "22222222-2222-4222-8222-222222222222"
	profUUID := "33333333-3333-4333-8333-333333333333"
	params := upsertRuntimeResourceBindingParams{
		OrganizationUUID:    orgUUID,
		WorkspaceUUID:       wsUUID,
		RuntimeProfileUUID:  profUUID,
		ProviderKind:        "qoder_cloud",
		ResourceType:        "agent",
		OMAExternalID:       "agent_qdr_1",
		RemoteID:            "agt_remote_1",
		ParentOMAExternalID: "",
		ResourceRole:        "",
		State:               "active",
		Summary:             `{"name":"Alpha"}`,
	}

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeResourceBindingMapperUpsertStatement,
		bound:     buildRuntimeResourceBindingMapperUpsert(yourbatis.DialectPostgres, params),
		wantID:    "RuntimeResourceBindingMapper.Upsert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.OrganizationUUID", "params.WorkspaceUUID", "params.RuntimeProfileUUID",
			"params.ProviderKind", "params.ResourceType", "params.OMAExternalID",
			"params.RemoteID", "params.ParentOMAExternalID", "params.ResourceRole",
			"params.State", "params.Summary", "params.CreatedAt", "params.UpdatedAt",
		},
		wantSQLFragments: []string{"INSERT INTO runtime_resource_bindings", "ON CONFLICT (organization_uuid, workspace_uuid, resource_type, oma_external_id)", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeResourceBindingMapperInsertIfAbsentStatement,
		bound:     buildRuntimeResourceBindingMapperInsertIfAbsent(yourbatis.DialectPostgres, params),
		wantID:    "RuntimeResourceBindingMapper.InsertIfAbsent",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.OrganizationUUID", "params.WorkspaceUUID", "params.RuntimeProfileUUID",
			"params.ProviderKind", "params.ResourceType", "params.OMAExternalID",
			"params.RemoteID", "params.ParentOMAExternalID", "params.ResourceRole",
			"params.State", "params.Summary", "params.CreatedAt", "params.UpdatedAt",
		},
		wantSQLFragments: []string{"ON CONFLICT DO NOTHING", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeResourceBindingMapperFindByOMAIDStatement,
		bound:             buildRuntimeResourceBindingMapperFindByOMAID(yourbatis.DialectPostgres, orgUUID, wsUUID, "agent", "agent_qdr_1"),
		wantID:            "RuntimeResourceBindingMapper.FindByOMAID",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "resourceType", "omaExternalID"},
		wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "resource_type = $3", "oma_external_id = $4", "state != 'deleted'"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeResourceBindingMapperFindByRemoteIDStatement,
		bound:             buildRuntimeResourceBindingMapperFindByRemoteID(yourbatis.DialectPostgres, profUUID, "agent", "agt_remote_1"),
		wantID:            "RuntimeResourceBindingMapper.FindByRemoteID",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"runtimeProfileUUID", "resourceType", "remoteID"},
		wantSQLFragments:  []string{"runtime_profile_uuid = $1", "resource_type = $2", "remote_id = $3"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeResourceBindingMapperListByWorkspaceAndTypeStatement,
		bound: buildRuntimeResourceBindingMapperListByWorkspaceAndType(yourbatis.DialectPostgres, listRuntimeResourceBindingsQuery{
			OrganizationUUID:    orgUUID,
			WorkspaceUUID:       wsUUID,
			ResourceType:        "file",
			ParentOMAExternalID: "ses_1",
			ResourceRole:        "session_output",
			IncludeArchived:     true,
			Limit:               20,
			Offset:              5,
		}),
		wantID:            "RuntimeResourceBindingMapper.ListByWorkspaceAndType",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"query.OrganizationUUID", "query.WorkspaceUUID", "query.ResourceType", "query.ParentOMAExternalID", "query.ResourceRole", "query.Limit", "query.Offset"},
		wantSQLFragments:  []string{"state IN ('active', 'archived')", "parent_oma_external_id = $4", "resource_role = $5", "LIMIT $6", "OFFSET $7"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeResourceBindingMapperSearchByWorkspaceAndTypeStatement,
		bound: buildRuntimeResourceBindingMapperSearchByWorkspaceAndType(yourbatis.DialectPostgres, searchRuntimeResourceBindingsQuery{
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    wsUUID,
			ResourceType:     "agent",
			Pattern:          "%alpha%",
			IncludeArchived:  false,
			Limit:            10,
			Offset:           0,
		}),
		wantID:            "RuntimeResourceBindingMapper.SearchByWorkspaceAndType",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"query.OrganizationUUID", "query.WorkspaceUUID", "query.ResourceType", "query.Pattern", "query.Pattern", "query.Pattern", "query.Limit", "query.Offset"},
		wantSQLFragments:  []string{"state = 'active'", "ILIKE $4 ESCAPE '\\'"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeResourceBindingMapperUpdateSummaryAndStateStatement,
		bound: buildRuntimeResourceBindingMapperUpdateSummaryAndState(yourbatis.DialectPostgres, updateRuntimeResourceBindingParams{
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    wsUUID,
			ResourceType:     "agent",
			OMAExternalID:    "agent_qdr_1",
			State:            "archived",
			Summary:          `{"name":"Alpha V2"}`,
		}),
		wantID:            "RuntimeResourceBindingMapper.UpdateSummaryAndState",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"params.Summary", "params.State", "params.OrganizationUUID", "params.WorkspaceUUID", "params.ResourceType", "params.OMAExternalID"},
		wantSQLFragments:  []string{"UPDATE runtime_resource_bindings", "state = $2", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeResourceBindingMapperSoftDeleteStatement,
		bound:             buildRuntimeResourceBindingMapperSoftDelete(yourbatis.DialectPostgres, orgUUID, wsUUID, "agent", "agent_qdr_1"),
		wantID:            "RuntimeResourceBindingMapper.SoftDelete",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "resourceType", "omaExternalID"},
		wantSQLFragments:  []string{"SET state = 'deleted'"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         runtimeResourceBindingMapperLockAgentInTxStatement,
		bound:             buildRuntimeResourceBindingMapperLockAgentInTx(yourbatis.DialectPostgres, orgUUID, wsUUID, "agent_qdr_1"),
		wantID:            "RuntimeResourceBindingMapper.LockAgentInTx",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "omaExternalID"},
		wantSQLFragments:  []string{"resource_type = 'agent'", "state = 'active'", "FOR UPDATE"},
	})

	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: runtimeResourceBindingMapperAggregateExternalOverviewStatement,
		bound: buildRuntimeResourceBindingMapperAggregateExternalOverview(yourbatis.DialectPostgres, WorkspaceOverviewQuery{
			OrganizationUUID: orgUUID,
			WorkspaceUUID:    wsUUID,
			Start:            start,
			End:              end,
			Timezone:         "UTC",
		}),
		wantID:            "RuntimeResourceBindingMapper.AggregateExternalOverview",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"query.Timezone", "query.OrganizationUUID", "query.WorkspaceUUID", "query.Start", "query.End"},
		wantSQLFragments:  []string{"FROM runtime_resource_bindings s", "s.resource_type = 'session'", "GROUP BY date, agent_id, agent_name"},
	})
}

func TestLockAgentRuntimeResourceBindingInTxAndHelpers(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{
			"id", "organization_uuid", "workspace_uuid", "runtime_profile_uuid", "provider_kind",
			"resource_type", "oma_external_id", "remote_id", "parent_oma_external_id",
			"resource_role", "state", "summary", "created_at", "updated_at",
		},
		rows: [][]driver.Value{{
			int64(7), "org-1", "ws-1", "prof-1", "qoder_cloud",
			"agent", "agent_qdr_1", "agt_1", "", "", "active", []byte(`{"name":"Alpha"}`), now, now,
		}},
	})
	var database DB
	locked, err := database.LockAgentRuntimeResourceBindingInTx(context.Background(), executor, "org-1", "ws-1", "agent_qdr_1")
	if err != nil || locked.RemoteID != "agt_1" {
		t.Fatalf("LockAgentRuntimeResourceBindingInTx() = (%+v, %v)", locked, err)
	}

	emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{
			"id", "organization_uuid", "workspace_uuid", "runtime_profile_uuid", "provider_kind",
			"resource_type", "oma_external_id", "remote_id", "parent_oma_external_id",
			"resource_role", "state", "summary", "created_at", "updated_at",
		},
		queryErr: sql.ErrNoRows,
	})
	if _, err := database.LockAgentRuntimeResourceBindingInTx(context.Background(), emptyExecutor, "org-1", "ws-1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LockAgentRuntimeResourceBindingInTx(missing) err = %v, want ErrNotFound", err)
	}

	if got := escapeILIKEPattern(`a%b_c\d`); got != `a\%b\_c\\d` {
		t.Fatalf("escapeILIKEPattern() = %q", got)
	}

	var _ func(*DB, context.Context, WorkspaceOverviewQuery) ([]WorkspaceOverviewBucket, error) = (*DB).AggregateExternalWorkspaceOverview
}
