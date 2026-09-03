package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestDeploymentMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := deploymentMapperTestWriteParams(now)
	params.ScheduleChanged = true
	unchangedSchedule := params
	unchangedSchedule.ScheduleChanged = false
	page := deploymentPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		Cursor:          &DeploymentPageCursor{CreatedAt: now, UUID: "00000000-0000-4000-8000-000000000009"},
		AgentExternalID: params.AgentExternalID, Status: params.Status,
		CreatedAtGTE: &now, CreatedAtLTE: &now,
	}
	pausedReason := []byte(`{"reason":"test"}`)
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: deploymentMapperInsertStatement, bound: buildDeploymentMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "DeploymentMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.CreatedByAPIKeyUUID", "params.EnvironmentUUID", "params.EnvironmentExternalID",
				"params.AgentUUID", "params.AgentExternalID", "params.AgentVersion", "params.AgentSnapshot",
				"params.Name", "params.Description", "params.Metadata", "params.RuntimeUserUUID", "params.InitialEvents", "params.Resources",
				"params.ResourceSecrets", "params.VaultIDs", "params.Schedule", "params.LastRunAt", "params.Status",
				"params.PausedReason", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: deploymentSensitiveArgumentNames(true),
			wantSQLFragments:           []string{"INSERT INTO deployments", "CAST($11 AS jsonb)", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement: deploymentMapperFindByExternalIDStatement,
			bound:     buildDeploymentMapperFindByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "DeploymentMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"FROM deployments", "workspace_uuid = $1", "external_id = $2"},
		}},
		{"count scheduled organization", mapperBuilderContract{
			statement: deploymentMapperCountScheduledByOrganizationStatement,
			bound:     buildDeploymentMapperCountScheduledByOrganization(yourbatis.DialectPostgres, params.OrganizationUUID),
			wantID:    "DeploymentMapper.CountScheduledByOrganization", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID"}, wantSQLFragments: []string{"COUNT(*)", "schedule IS NOT NULL"},
		}},
		{"lock", mapperBuilderContract{
			statement: deploymentMapperLockByExternalIDStatement,
			bound:     buildDeploymentMapperLockByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "DeploymentMapper.LockByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"FOR UPDATE"},
		}},
		{"update", mapperBuilderContract{
			statement: deploymentMapperUpdateByExternalIDStatement,
			bound:     buildDeploymentMapperUpdateByExternalID(yourbatis.DialectPostgres, params),
			wantID:    "DeploymentMapper.UpdateByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.EnvironmentUUID", "params.EnvironmentExternalID", "params.AgentUUID", "params.AgentExternalID",
				"params.AgentVersion", "params.AgentSnapshot", "params.Name", "params.Description", "params.Metadata",
				"params.InitialEvents", "params.Resources", "params.ResourceSecrets", "params.VaultIDs", "params.Schedule",
				"params.UpdatedAt", "params.WorkspaceUUID", "params.ExternalID",
			},
			wantSensitiveArgumentNames: deploymentSensitiveArgumentNames(false),
			wantSQLFragments:           []string{"UPDATE deployments", "schedule = CAST($14 AS jsonb)", "workspace_uuid = $16", "RETURNING"},
		}},
		{"update without schedule change", mapperBuilderContract{
			statement: deploymentMapperUpdateByExternalIDStatement,
			bound:     buildDeploymentMapperUpdateByExternalID(yourbatis.DialectPostgres, unchangedSchedule),
			wantID:    "DeploymentMapper.UpdateByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.EnvironmentUUID", "params.EnvironmentExternalID", "params.AgentUUID", "params.AgentExternalID",
				"params.AgentVersion", "params.AgentSnapshot", "params.Name", "params.Description", "params.Metadata",
				"params.InitialEvents", "params.Resources", "params.ResourceSecrets", "params.VaultIDs", "params.UpdatedAt",
				"params.WorkspaceUUID", "params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{
				"params.AgentSnapshot", "params.Metadata", "params.InitialEvents", "params.Resources",
				"params.ResourceSecrets", "params.VaultIDs",
			},
			wantSQLFragments: []string{"UPDATE deployments", "workspace_uuid = $15", "RETURNING"},
		}},
		{"archive", mapperBuilderContract{
			statement: deploymentMapperArchiveByExternalIDStatement,
			bound:     buildDeploymentMapperArchiveByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "DeploymentMapper.ArchiveByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"archived_at = COALESCE", "RETURNING"},
		}},
		{"archive root agent deployments", mapperBuilderContract{
			statement: deploymentMapperArchiveByRootAgentStatement,
			bound:     buildDeploymentMapperArchiveByRootAgent(yourbatis.DialectPostgres, params.WorkspaceUUID, params.AgentExternalID),
			wantID:    "DeploymentMapper.ArchiveByRootAgent", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "agentExternalID"},
			wantSQLFragments:  []string{"agent_external_id = $2", "archived_at = COALESCE"},
		}},
		{"pause", mapperBuilderContract{
			statement: deploymentMapperPauseByExternalIDStatement,
			bound:     buildDeploymentMapperPauseByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID, pausedReason),
			wantID:    "DeploymentMapper.PauseByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"pausedReason", "workspaceUUID", "externalID"},
			wantSensitiveArgumentNames: []string{"pausedReason"}, wantSQLFragments: []string{"status = 'paused'", "CAST($1 AS jsonb)"},
		}},
		{"unpause", mapperBuilderContract{
			statement: deploymentMapperUnpauseByExternalIDStatement,
			bound:     buildDeploymentMapperUnpauseByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "DeploymentMapper.UnpauseByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"status = 'active'", "RETURNING"},
		}},
		{"list page", mapperBuilderContract{
			statement: deploymentMapperListPageStatement, bound: buildDeploymentMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "DeploymentMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.WorkspaceUUID", "params.AgentExternalID", "params.Status", "params.CreatedAtGTE",
				"params.CreatedAtLTE", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.FetchLimit",
			},
			wantSQLFragments: []string{"archived_at IS NULL", "agent_external_id = $2", "ORDER BY created_at DESC", "LIMIT $8"},
		}},
		{"list active schedules", mapperBuilderContract{
			statement: deploymentMapperListActiveSchedulesStatement,
			bound:     buildDeploymentMapperListActiveSchedules(yourbatis.DialectPostgres),
			wantID:    "DeploymentMapper.ListActiveSchedules", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{},
			wantSQLFragments:  []string{"SELECT workspace_uuid, external_id, schedule", "schedule IS NOT NULL"},
		}},
		{"pause after scheduled run", mapperBuilderContract{
			statement: deploymentMapperPauseAfterScheduledRunStatement,
			bound: buildDeploymentMapperPauseAfterScheduledRun(yourbatis.DialectPostgres, pauseScheduledDeploymentParams{
				WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID,
				PausedReason: pausedReason, LastRunAt: now,
			}),
			wantID: "DeploymentMapper.PauseAfterScheduledRun", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.LastRunAt", "params.PausedReason", "params.LastRunAt", "params.WorkspaceUUID",
				"params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.PausedReason"},
			wantSQLFragments:           []string{"status = 'paused'", "workspace_uuid = $4"},
		}},
		{"update last run", mapperBuilderContract{
			statement: deploymentMapperUpdateLastRunStatement,
			bound:     buildDeploymentMapperUpdateLastRun(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID, now),
			wantID:    "DeploymentMapper.UpdateLastRun", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"lastRunAt", "lastRunAt", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"last_run_at = $1", "workspace_uuid = $3"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("update without schedule change preserves schedule", func(t *testing.T) {
		bound := buildDeploymentMapperUpdateByExternalID(yourbatis.DialectPostgres, unchangedSchedule)
		if containsSQL(bound.SQL, "schedule =") {
			t.Fatalf("SQL unexpectedly changes schedule state: %q", bound.SQL)
		}
	})

	t.Run("include archived omits archived filter", func(t *testing.T) {
		page.IncludeArchived = true
		bound := buildDeploymentMapperListPage(yourbatis.DialectPostgres, page)
		if containsSQL(bound.SQL, "archived_at IS NULL") {
			t.Fatalf("SQL unexpectedly filters archived deployments: %q", bound.SQL)
		}
	})
}

func TestDeploymentRunMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := deploymentRunMapperTestWriteParams(now)
	page := deploymentRunPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		Cursor:               &DeploymentRunPageCursor{CreatedAt: now, UUID: "00000000-0000-4000-8000-000000000010"},
		DeploymentExternalID: params.DeploymentExternalID, TriggerType: params.TriggerType,
		HasErrorFilter: true, HasError: true, CreatedAtGT: &now, CreatedAtGTE: &now, CreatedAtLT: &now, CreatedAtLTE: &now,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: deploymentRunMapperInsertStatement, bound: buildDeploymentRunMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "DeploymentRunMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.CreatedByAPIKeyUUID", "params.DeploymentUUID", "params.DeploymentExternalID",
				"params.AgentUUID", "params.AgentExternalID", "params.AgentVersion", "params.AgentSnapshot",
				"params.SessionExternalID", "params.Error", "params.TriggerType", "params.ScheduledAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.AgentSnapshot", "params.Error"},
			wantSQLFragments:           []string{"INSERT INTO deployment_runs", "CAST($11 AS jsonb)", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement: deploymentRunMapperFindByExternalIDStatement,
			bound:     buildDeploymentRunMapperFindByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "DeploymentRunMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"FROM deployment_runs", "external_id = $2"},
		}},
		{"list page", mapperBuilderContract{
			statement: deploymentRunMapperListPageStatement, bound: buildDeploymentRunMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "DeploymentRunMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.WorkspaceUUID", "params.DeploymentExternalID", "params.TriggerType", "params.CreatedAtGT",
				"params.CreatedAtGTE", "params.CreatedAtLT", "params.CreatedAtLTE", "params.Cursor.CreatedAt",
				"params.Cursor.UUID", "params.FetchLimit",
			},
			wantSQLFragments: []string{"error IS NOT NULL", "created_at > $4", "ORDER BY created_at DESC", "LIMIT $10"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("has error false", func(t *testing.T) {
		page.HasError = false
		bound := buildDeploymentRunMapperListPage(yourbatis.DialectPostgres, page)
		if !containsSQL(bound.SQL, "error IS NULL") || containsSQL(bound.SQL, "error IS NOT NULL") {
			t.Fatalf("SQL has wrong error filter: %q", bound.SQL)
		}
	})
}

func TestDeploymentMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	t.Run("row scans string UUIDs", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: deploymentMapperTestColumns(), rows: [][]driver.Value{deploymentMapperTestRow()},
		})
		row, err := NewDeploymentMapper(executor).FindByExternalID(ctx, "workspace", "deployment")
		if err != nil || row.deployment().UUID != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("FindByExternalID() = (%+v, %v)", row, err)
		}
	})

	t.Run("zero rows preserves sql ErrNoRows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: deploymentMapperTestColumns()})
		_, err := NewDeploymentMapper(executor).FindByExternalID(ctx, "workspace", "missing")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("rows affected", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rows, err := NewDeploymentMapper(executor).UpdateLastRun(ctx, "workspace", "deployment", time.Now())
		if err != nil || rows != 1 {
			t.Fatalf("UpdateLastRun() = (%d, %v)", rows, err)
		}
	})
}

func TestDeploymentMapperPropagatesExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{"select", mapperExecutionErrorContract{
			statementID: "DeploymentMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewDeploymentMapper(executor).FindByExternalID(ctx, "workspace", "deployment")
				return err
			},
		}},
		{"returning", mapperExecutionErrorContract{
			statementID: "DeploymentRunMapper.Insert", kind: yourbatis.StatementInsert, query: true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewDeploymentRunMapper(executor).Insert(ctx, deploymentRunWriteParams{})
				return err
			},
		}},
		{"rows", mapperExecutionErrorContract{
			statementID: "DeploymentMapper.UpdateLastRun", kind: yourbatis.StatementUpdate,
			call: func(executor yourbatis.Executor) error {
				_, err := NewDeploymentMapper(executor).UpdateLastRun(ctx, "workspace", "deployment", time.Now())
				return err
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperExecutionError(t, test.contract) })
	}
}

func deploymentMapperTestWriteParams(now time.Time) deploymentWriteParams {
	return deploymentWriteParams{
		UUID: "00000000-0000-4000-8000-000000000001", ExternalID: "dep_test",
		OrganizationUUID:    "00000000-0000-4000-8000-000000000002",
		WorkspaceUUID:       "00000000-0000-4000-8000-000000000003",
		CreatedByAPIKeyUUID: nullableString("00000000-0000-4000-8000-000000000004"),
		EnvironmentUUID:     "00000000-0000-4000-8000-000000000005", EnvironmentExternalID: "env_test",
		AgentUUID: "00000000-0000-4000-8000-000000000006", AgentExternalID: "agent_test", AgentVersion: 1,
		AgentSnapshot: []byte(`{}`), Name: "test", Metadata: []byte(`{}`), InitialEvents: []byte(`[]`),
		Resources: []byte(`[]`), ResourceSecrets: []byte(`[]`), VaultIDs: []byte(`[]`), Schedule: []byte(`{}`),
		Status: "active", PausedReason: []byte(`null`), CreatedAt: now, UpdatedAt: now,
	}
}

func deploymentRunMapperTestWriteParams(now time.Time) deploymentRunWriteParams {
	return deploymentRunWriteParams{
		UUID: "00000000-0000-4000-8000-000000000011", ExternalID: "drun_test",
		OrganizationUUID:    "00000000-0000-4000-8000-000000000002",
		WorkspaceUUID:       "00000000-0000-4000-8000-000000000003",
		CreatedByAPIKeyUUID: nullableString("00000000-0000-4000-8000-000000000004"),
		DeploymentUUID:      "00000000-0000-4000-8000-000000000001", DeploymentExternalID: "dep_test",
		AgentUUID: "00000000-0000-4000-8000-000000000006", AgentExternalID: "agent_test", AgentVersion: 1,
		AgentSnapshot: []byte(`{}`), Error: []byte(`null`), TriggerType: "schedule", ScheduledAt: &now, CreatedAt: now,
	}
}

func deploymentSensitiveArgumentNames(includePausedReason bool) []string {
	names := []string{
		"params.AgentSnapshot", "params.Metadata", "params.InitialEvents", "params.Resources",
		"params.ResourceSecrets", "params.VaultIDs", "params.Schedule",
	}
	if includePausedReason {
		names = append(names, "params.PausedReason")
	}
	return names
}

func deploymentMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "created_by_api_key_uuid",
		"environment_uuid", "environment_external_id", "agent_uuid", "agent_external_id", "agent_version",
		"agent_snapshot", "name", "description", "metadata", "runtime_user_uuid", "initial_events", "resources", "resource_secrets",
		"vault_ids", "schedule", "last_run_at", "status", "paused_reason", "created_at", "updated_at", "archived_at", "deleted_at",
	}
}

func deploymentMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000001", "dep_test", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000004",
		"00000000-0000-4000-8000-000000000005", "env_test", "00000000-0000-4000-8000-000000000006",
		"agent_test", int64(1), []byte(`{}`), "test", nil, []byte(`{}`), nil, []byte(`[]`), []byte(`[]`), []byte(`[]`),
		[]byte(`[]`), []byte(`{}`), nil, "active", []byte(`null`), now, now, nil, nil,
	}
}

func containsSQL(sql, fragment string) bool {
	return strings.Contains(sql, fragment)
}
