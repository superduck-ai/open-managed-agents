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

func TestEvaluationEvaluatorMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	creator := "00000000-0000-4000-8000-000000000004"
	params := evaluationEvaluatorWriteParams{
		UUID: "00000000-0000-4000-8000-000000000010", ExternalID: "evalev_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "score", Description: "nightly", CreatedByUserUUID: &creator, CreatedAt: now,
	}
	page := evaluationEvaluatorPageParams{
		OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		Cursor: &EvaluationEvaluatorPageCursor{CreatedAt: now, UUID: "00000000-0000-4000-8000-000000000019"},
	}
	version := evaluationEvaluatorVersionWriteParams{
		UUID: "00000000-0000-4000-8000-000000000011", ExternalID: "eev_test",
		OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID,
		EvaluatorUUID: params.UUID, Version: 1, EvaluatorType: "llm", Model: "claude",
		PromptTemplate: "Score {{input}}", VariablesJSON: `["input"]`, CreatedAt: now,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: evaluationEvaluatorMapperInsertStatement, bound: buildEvaluationEvaluatorMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "EvaluationEvaluatorMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.Name", "params.Description", "params.CreatedByUserUUID", "params.CreatedAt", "params.CreatedAt",
			},
			wantSQLFragments: []string{"INSERT INTO evaluation_evaluators", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement: evaluationEvaluatorMapperFindByExternalIDStatement,
			bound:     buildEvaluationEvaluatorMapperFindByExternalID(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID),
			wantID:    "EvaluationEvaluatorMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"evaluation_evaluator_versions", "variables_json", "archived_at IS NULL"},
		}},
		{"list page", mapperBuilderContract{
			statement: evaluationEvaluatorMapperListPageStatement, bound: buildEvaluationEvaluatorMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "EvaluationEvaluatorMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.OrganizationUUID", "params.WorkspaceUUID", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.FetchLimit",
			},
			wantSQLFragments: []string{"(d.created_at, d.uuid) < ($3, $4)", "ORDER BY d.created_at DESC, d.uuid DESC", "LIMIT $5"},
		}},
		{"insert version", mapperBuilderContract{
			statement: evaluationEvaluatorVersionMapperInsertStatement, bound: buildEvaluationEvaluatorVersionMapperInsert(yourbatis.DialectPostgres, version),
			wantID: "EvaluationEvaluatorVersionMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.EvaluatorUUID", "params.Version", "params.EvaluatorType", "params.Model", "params.AgentID",
				"params.PromptTemplate", "params.VariablesJSON", "params.CreatedAt",
			},
			wantSQLFragments: []string{"INSERT INTO evaluation_evaluator_versions", "CAST(", "AS jsonb)", "RETURNING"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("list page omits cursor", func(t *testing.T) {
		bound := buildEvaluationEvaluatorMapperListPage(yourbatis.DialectPostgres, evaluationEvaluatorPageParams{
			OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		})
		assertMapperBuilderContract(t, mapperBuilderContract{
			statement: evaluationEvaluatorMapperListPageStatement, bound: bound,
			wantID: "EvaluationEvaluatorMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.OrganizationUUID", "params.WorkspaceUUID", "params.FetchLimit"},
			wantSQLFragments:  []string{"ORDER BY d.created_at DESC, d.uuid DESC", "LIMIT $3"},
		})
		if containsMapperSQL(bound.SQL, "(d.created_at, d.uuid) <") {
			t.Fatalf("ListPage SQL = %q, does not want a cursor predicate", bound.SQL)
		}
	})
}

func TestEvaluationEvaluatorMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	params := evaluationEvaluatorWriteParams{
		UUID: "00000000-0000-4000-8000-000000000010", ExternalID: "evalev_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "score", CreatedAt: now,
	}
	versionParams := evaluationEvaluatorVersionWriteParams{
		UUID: "00000000-0000-4000-8000-000000000011", ExternalID: "eev_test",
		OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID,
		EvaluatorUUID: params.UUID, Version: 1, EvaluatorType: "agent", AgentID: "agent_score",
		VariablesJSON: `["invocation_result"]`, CreatedAt: now,
	}

	t.Run("find decodes variables", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: evaluationEvaluatorPageColumns(), rows: [][]driver.Value{evaluationEvaluatorPageRowValues()},
		})
		row, err := NewEvaluationEvaluatorMapper(executor).FindByExternalID(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID)
		if err != nil {
			t.Fatal(err)
		}
		evaluator, err := row.evaluationEvaluator()
		if err != nil || evaluator.Version.ExternalID != "eev_test" || evaluator.Version.EvaluatorType != "llm" || evaluator.Version.Variables[0] != "input" {
			t.Fatalf("decoded = (%+v, %v)", evaluator, err)
		}
	})

	t.Run("insert version scans variables", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: evaluationEvaluatorVersionColumns(), rows: [][]driver.Value{evaluationEvaluatorVersionRowValues(`[]`)},
		})
		row, err := NewEvaluationEvaluatorVersionMapper(executor).Insert(ctx, versionParams)
		if err != nil {
			t.Fatal(err)
		}
		version, err := row.evaluationEvaluatorVersion()
		if err != nil || version.AgentID != "agent_score" || len(version.Variables) != 0 {
			t.Fatalf("version = (%+v, %v)", version, err)
		}
	})

	t.Run("find zero rows preserves sql ErrNoRows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: evaluationEvaluatorPageColumns()})
		_, err := NewEvaluationEvaluatorMapper(executor).FindByExternalID(ctx, params.OrganizationUUID, params.WorkspaceUUID, "evalev_missing")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("mapper error = %v, want sql.ErrNoRows", err)
		}
	})

	assertMapperExecutionError(t, mapperExecutionErrorContract{
		statementID: "EvaluationEvaluatorMapper.ListPage", kind: yourbatis.StatementSelect, query: true,
		call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationEvaluatorMapper(executor).ListPage(ctx, evaluationEvaluatorPageParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 20,
			})
			return err
		},
	})
}

func evaluationEvaluatorInsertColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "name", "description",
		"created_by_user_uuid", "created_at", "updated_at", "archived_at",
	}
}

func evaluationEvaluatorPageColumns() []string {
	return append(evaluationEvaluatorInsertColumns(),
		"version_uuid", "version_external_id", "version_number", "evaluator_type", "model", "agent_id",
		"prompt_template", "variables_json", "version_created_at",
	)
}

func evaluationEvaluatorPageRowValues() []driver.Value {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000010", "evalev_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"score", "nightly", "00000000-0000-4000-8000-000000000004", now, now, nil,
		"00000000-0000-4000-8000-000000000011", "eev_test", int64(1), "llm", "claude", "",
		"Score {{input}}", `["input"]`, now,
	}
}

func evaluationEvaluatorVersionColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "evaluator_uuid", "version",
		"evaluator_type", "model", "agent_id", "prompt_template", "variables_json", "created_at",
	}
}

func evaluationEvaluatorVersionRowValues(variables string) []driver.Value {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000011", "eev_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000010", int64(1), "agent", "", "agent_score", "", variables, now,
	}
}
