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

func TestEvaluationExperimentMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	creator := "00000000-0000-4000-8000-000000000004"
	env := `{"TRACE":"1"}`
	resources := `[{"type":"file"}]`
	params := evaluationExperimentWriteParams{
		UUID: "00000000-0000-4000-8000-000000000050", ExternalID: "evalexp_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "nightly", Description: "note", DatasetUUID: "00000000-0000-4000-8000-000000000003", DatasetExternalID: "evalds_test",
		VersionUUID: "00000000-0000-4000-8000-000000000008", VersionExternalID: "edv_test",
		AgentExternalID: "agent_test", AgentVersion: 4, EnvironmentExternalID: "env_test", TimeoutSeconds: 60,
		EnvJSON: &env, ResourcesJSON: &resources, EvaluatorSetKey: "eev_a|eev_b", Status: "pending",
		CreatedByUserUUID: &creator, CreatedAt: now,
	}
	page := evaluationExperimentPageParams{
		OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		Cursor: &EvaluationExperimentPageCursor{CreatedAt: now, UUID: params.UUID},
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: evaluationExperimentMapperInsertStatement, bound: buildEvaluationExperimentMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "EvaluationExperimentMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.Name", "params.Description", "params.DatasetUUID", "params.DatasetExternalID",
				"params.VersionUUID", "params.VersionExternalID", "params.AgentExternalID", "params.AgentVersion",
				"params.EnvironmentExternalID", "params.TimeoutSeconds", "params.EnvJSON", "params.ResourcesJSON",
				"params.EvaluatorSetKey", "params.Status", "params.CreatedByUserUUID", "params.CreatedAt", "params.CreatedAt",
			},
			wantSQLFragments: []string{"INSERT INTO evaluation_experiments", "CAST(", "AS jsonb)", "0 AS item_count", "RETURNING"},
		}},
		{"list page", mapperBuilderContract{
			statement: evaluationExperimentMapperListPageStatement, bound: buildEvaluationExperimentMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "EvaluationExperimentMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.OrganizationUUID", "params.WorkspaceUUID", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.FetchLimit",
			},
			wantSQLFragments: []string{"(e.created_at, e.uuid) < ($3, $4)", "ORDER BY e.created_at DESC, e.uuid DESC", "LIMIT $5"},
		}},
		{"compare", mapperBuilderContract{
			statement: evaluationExperimentMapperListMatchingStatement,
			bound: buildEvaluationExperimentMapperListMatching(yourbatis.DialectPostgres, evaluationExperimentCompareParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID,
				VersionExternalID: "edv_test", EvaluatorSetKey: "eev_a|eev_b", Status: "completed",
			}),
			wantID: "EvaluationExperimentMapper.ListMatching", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.OrganizationUUID", "params.WorkspaceUUID", "params.VersionExternalID", "params.EvaluatorSetKey", "params.Status",
			},
			wantSQLFragments: []string{"e.version_external_id = $3", "e.evaluator_set_key = $4", "e.status = $5"},
		}},
		{"update status", mapperBuilderContract{
			statement: evaluationExperimentMapperUpdateStatusStatement,
			bound: buildEvaluationExperimentMapperUpdateStatus(yourbatis.DialectPostgres, evaluationExperimentStatusParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID,
				Status: "running", UpdatedAt: now,
			}),
			wantID: "EvaluationExperimentMapper.UpdateStatus", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.Status", "params.UpdatedAt", "params.OrganizationUUID", "params.WorkspaceUUID", "params.ExternalID",
			},
			wantSQLFragments: []string{"SET status = $1", "external_id = $5"},
		}},
		{"set session", mapperBuilderContract{
			statement: evaluationExperimentItemMapperSetSessionStatement,
			bound: buildEvaluationExperimentItemMapperSetSession(yourbatis.DialectPostgres, evaluationExperimentItemSessionParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, ExternalID: "evalitem_test",
				SessionExternalID: &creator, Status: "running", UpdatedAt: now,
			}),
			wantID: "EvaluationExperimentItemMapper.SetSession", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.SessionExternalID", "params.Status", "params.ErrorMessage", "params.UpdatedAt",
				"params.OrganizationUUID", "params.WorkspaceUUID", "params.ExternalID",
			},
			wantSQLFragments: []string{"session_external_id = $1", "external_id = $7"},
		}},
		{"cases by version", mapperBuilderContract{
			statement: evaluationCaseMapperListByVersionStatement,
			bound:     buildEvaluationCaseMapperListByVersion(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, params.VersionUUID),
			wantID:    "EvaluationCaseMapper.ListByVersion", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "versionUUID"},
			wantSQLFragments:  []string{"version_uuid = $3", "ORDER BY position ASC, uuid ASC"},
		}},
		{"scoped dataset version", mapperBuilderContract{
			statement: evaluationDatasetVersionMapperFindScopedByExternalIDStatement,
			bound:     buildEvaluationDatasetVersionMapperFindScopedByExternalID(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, "edv_test"),
			wantID:    "EvaluationDatasetVersionMapper.FindScopedByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"dataset_external_id", "archived_at IS NULL"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestEvaluationExperimentMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	params := evaluationExperimentWriteParams{
		UUID: "00000000-0000-4000-8000-000000000050", ExternalID: "evalexp_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "nightly", CreatedAt: now,
	}
	t.Run("insert scans zero counts", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: evaluationExperimentColumns(), rows: [][]driver.Value{evaluationExperimentRowValues(now)},
		})
		row, err := NewEvaluationExperimentMapper(executor).Insert(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		experiment := row.evaluationExperiment()
		if experiment.ExternalID != "evalexp_test" || experiment.ItemCount != 0 || string(experiment.Env) != `{"TRACE":"1"}` {
			t.Fatalf("experiment = %#v", experiment)
		}
	})
	t.Run("find zero rows preserves sql ErrNoRows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: evaluationExperimentColumns()})
		_, err := NewEvaluationExperimentMapper(executor).FindByExternalID(ctx, params.OrganizationUUID, params.WorkspaceUUID, "evalexp_missing")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("mapper error = %v, want sql.ErrNoRows", err)
		}
	})
	assertMapperExecutionError(t, mapperExecutionErrorContract{
		statementID: "EvaluationExperimentMapper.ListPage", kind: yourbatis.StatementSelect, query: true,
		call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationExperimentMapper(executor).ListPage(ctx, evaluationExperimentPageParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 20,
			})
			return err
		},
	})
}

func evaluationExperimentColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "name", "description",
		"dataset_uuid", "dataset_external_id", "version_uuid", "version_external_id",
		"agent_external_id", "agent_version", "environment_external_id", "timeout_seconds",
		"env_json", "resources_json", "evaluator_set_key", "status", "created_by_user_uuid",
		"created_at", "updated_at", "item_count", "session_count", "evaluator_count",
	}
}

func evaluationExperimentRowValues(now time.Time) []driver.Value {
	return []driver.Value{
		"00000000-0000-4000-8000-000000000050", "evalexp_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"nightly", "note", "00000000-0000-4000-8000-000000000003", "evalds_test",
		"00000000-0000-4000-8000-000000000008", "edv_test", "agent_test", int64(4), "env_test", int64(60),
		`{"TRACE":"1"}`, nil, "eev_a|eev_b", "pending", nil, now, now, int64(0), int64(0), int64(0),
	}
}
