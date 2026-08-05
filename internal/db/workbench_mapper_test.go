package db

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/yourbatis"
)

type workbenchMapperContract struct {
	name      string
	statement yourbatis.Statement
	bound     yourbatis.BoundSQL
	id        string
	kind      yourbatis.StatementKind
	values    []any
	fragments []string
}

func TestWorkbenchMapperStatements(t *testing.T) {
	const (
		organizationUUID = "00000000-0000-4000-8000-000000000001"
		workspaceUUID    = "00000000-0000-4000-8000-000000000002"
		promptRefUUID    = "00000000-0000-4000-8000-000000000003"
		testCaseUUID     = "00000000-0000-4000-8000-000000000004"
		promptUUID       = "prompt_test"
		revisionUUID     = "revision_test"
		evaluationUUID   = "evaluation_test"
	)
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	latestRevisionUUID := revisionUUID
	deletedAt := now.Add(time.Minute)
	versionJSON := `{"number":1}`
	promptParams := upsertWorkbenchPromptParams{
		OrganizationUUID:      organizationUUID,
		PromptUUID:            promptUUID,
		WorkspaceUUID:         workspaceUUID,
		WorkspaceDisplayID:    "workspace_test",
		Name:                  "Prompt",
		IsSharedWithWorkspace: true,
		LatestRevisionUUID:    &latestRevisionUUID,
		DeletedAt:             &deletedAt,
		CreatedAt:             now,
	}
	resetPromptParams := resetWorkbenchPromptParams{
		OrganizationUUID:   organizationUUID,
		PromptUUID:         promptUUID,
		WorkspaceUUID:      workspaceUUID,
		WorkspaceDisplayID: "workspace_test",
	}
	revisionParams := upsertWorkbenchRevisionParams{
		OrganizationUUID: organizationUUID,
		PromptUUID:       promptUUID,
		RevisionUUID:     revisionUUID,
		PayloadJSON:      `{"model":"test"}`,
	}
	kvParams := upsertWorkbenchKVParams{
		OrganizationUUID: organizationUUID,
		PromptUUID:       promptUUID,
		Key:              "model",
		Value:            "test",
		VersionJSON:      &versionJSON,
	}
	evaluationParams := upsertWorkbenchEvaluationParams{
		OrganizationUUID: organizationUUID,
		RevisionUUID:     revisionUUID,
		EvaluationUUID:   evaluationUUID,
		PayloadJSON:      `{"score":1}`,
	}

	tests := []workbenchMapperContract{
		{
			name: "find prompt", statement: workbenchPromptMapperFindByPromptUUIDStatement,
			bound: buildWorkbenchPromptMapperFindByPromptUUID(yourbatis.DialectPostgres, organizationUUID, promptUUID),
			id:    "WorkbenchPromptMapper.FindByPromptUUID", kind: yourbatis.StatementSelect,
			values: []any{organizationUUID, promptUUID}, fragments: []string{"FROM workbench_prompts", "organization_uuid = $1", "prompt_uuid = $2"},
		},
		{
			name: "list prompts", statement: workbenchPromptMapperListByWorkspaceStatement,
			bound: buildWorkbenchPromptMapperListByWorkspace(yourbatis.DialectPostgres, organizationUUID, workspaceUUID),
			id:    "WorkbenchPromptMapper.ListByWorkspace", kind: yourbatis.StatementSelect,
			values: []any{organizationUUID, workspaceUUID}, fragments: []string{"workspace_uuid = $2", "deleted_at IS NULL", "ORDER BY updated_at DESC"},
		},
		{
			name: "upsert prompt", statement: workbenchPromptMapperUpsertStatement,
			bound: buildWorkbenchPromptMapperUpsert(yourbatis.DialectPostgres, promptParams),
			id:    "WorkbenchPromptMapper.Upsert", kind: yourbatis.StatementInsert,
			values: []any{
				organizationUUID, promptUUID, workspaceUUID, "workspace_test", "Prompt", true,
				&latestRevisionUUID, &deletedAt, now,
			},
			fragments: []string{"INSERT INTO workbench_prompts", "ON CONFLICT", "RETURNING"},
		},
		{
			name: "reset prompt", statement: workbenchPromptMapperResetAndReturnUUIDStatement,
			bound: buildWorkbenchPromptMapperResetAndReturnUUID(yourbatis.DialectPostgres, resetPromptParams),
			id:    "WorkbenchPromptMapper.ResetAndReturnUUID", kind: yourbatis.StatementInsert,
			values:    []any{organizationUUID, promptUUID, workspaceUUID, "workspace_test"},
			fragments: []string{"INSERT INTO workbench_prompts", "deleted_at = CURRENT_TIMESTAMP", "RETURNING uuid"},
		},
		{
			name: "find revision", statement: workbenchPromptRevisionMapperFindStatement,
			bound: buildWorkbenchPromptRevisionMapperFind(yourbatis.DialectPostgres, organizationUUID, promptUUID, revisionUUID),
			id:    "WorkbenchPromptRevisionMapper.Find", kind: yourbatis.StatementSelect,
			values:    []any{organizationUUID, promptUUID, revisionUUID},
			fragments: []string{"FROM workbench_prompt_revisions", "JOIN workbench_prompts", "r.revision_uuid = $3"},
		},
		{
			name: "upsert revision", statement: workbenchPromptRevisionMapperUpsertStatement,
			bound: buildWorkbenchPromptRevisionMapperUpsert(yourbatis.DialectPostgres, revisionParams),
			id:    "WorkbenchPromptRevisionMapper.Upsert", kind: yourbatis.StatementInsert,
			values:    []any{organizationUUID, promptUUID, organizationUUID, promptUUID, revisionUUID, revisionParams.PayloadJSON},
			fragments: []string{"WITH target_prompt", "INSERT INTO workbench_prompt_revisions", "CAST($6 AS jsonb)"},
		},
		{
			name: "delete revisions", statement: workbenchPromptRevisionMapperDeleteByPromptRefUUIDStatement,
			bound: buildWorkbenchPromptRevisionMapperDeleteByPromptRefUUID(yourbatis.DialectPostgres, promptRefUUID),
			id:    "WorkbenchPromptRevisionMapper.DeleteByPromptRefUUID", kind: yourbatis.StatementDelete,
			values: []any{promptRefUUID}, fragments: []string{"DELETE FROM workbench_prompt_revisions", "prompt_ref_uuid = $1"},
		},
		{
			name: "find key value", statement: workbenchPromptKVMapperFindStatement,
			bound: buildWorkbenchPromptKVMapperFind(yourbatis.DialectPostgres, organizationUUID, promptUUID, kvParams.Key),
			id:    "WorkbenchPromptKVMapper.Find", kind: yourbatis.StatementSelect,
			values:    []any{organizationUUID, promptUUID, kvParams.Key},
			fragments: []string{"FROM workbench_prompt_kv", "JOIN workbench_prompts", "k.key = $3"},
		},
		{
			name: "upsert key value", statement: workbenchPromptKVMapperUpsertStatement,
			bound: buildWorkbenchPromptKVMapperUpsert(yourbatis.DialectPostgres, kvParams),
			id:    "WorkbenchPromptKVMapper.Upsert", kind: yourbatis.StatementInsert,
			values:    []any{organizationUUID, promptUUID, organizationUUID, promptUUID, kvParams.Key, kvParams.Value, &versionJSON},
			fragments: []string{"WITH target_prompt", "INSERT INTO workbench_prompt_kv", "CAST($7 AS jsonb)"},
		},
		{
			name: "delete key value", statement: workbenchPromptKVMapperDeleteStatement,
			bound: buildWorkbenchPromptKVMapperDelete(yourbatis.DialectPostgres, organizationUUID, promptUUID, kvParams.Key),
			id:    "WorkbenchPromptKVMapper.Delete", kind: yourbatis.StatementDelete,
			values:    []any{organizationUUID, promptUUID, kvParams.Key},
			fragments: []string{"DELETE FROM workbench_prompt_kv", "USING workbench_prompts", "k.key = $3"},
		},
		{
			name: "delete key values by prompt", statement: workbenchPromptKVMapperDeleteByPromptRefUUIDStatement,
			bound: buildWorkbenchPromptKVMapperDeleteByPromptRefUUID(yourbatis.DialectPostgres, promptRefUUID),
			id:    "WorkbenchPromptKVMapper.DeleteByPromptRefUUID", kind: yourbatis.StatementDelete,
			values: []any{promptRefUUID}, fragments: []string{"DELETE FROM workbench_prompt_kv", "prompt_ref_uuid = $1"},
		},
		{
			name: "list evaluation revisions", statement: workbenchEvaluationMapperListRevisionIDsStatement,
			bound: buildWorkbenchEvaluationMapperListRevisionIDs(yourbatis.DialectPostgres, organizationUUID),
			id:    "WorkbenchEvaluationMapper.ListRevisionIDs", kind: yourbatis.StatementSelect,
			values: []any{organizationUUID}, fragments: []string{"SELECT DISTINCT revision_uuid", "organization_uuid = $1"},
		},
		{
			name: "list evaluations", statement: workbenchEvaluationMapperListByRevisionStatement,
			bound: buildWorkbenchEvaluationMapperListByRevision(yourbatis.DialectPostgres, organizationUUID, revisionUUID),
			id:    "WorkbenchEvaluationMapper.ListByRevision", kind: yourbatis.StatementSelect,
			values:    []any{organizationUUID, revisionUUID},
			fragments: []string{"FROM workbench_evaluations", "JOIN workbench_prompt_revisions", "r.revision_uuid = $2"},
		},
		{
			name: "find evaluation", statement: workbenchEvaluationMapperFindStatement,
			bound: buildWorkbenchEvaluationMapperFind(yourbatis.DialectPostgres, organizationUUID, evaluationUUID),
			id:    "WorkbenchEvaluationMapper.Find", kind: yourbatis.StatementSelect,
			values: []any{organizationUUID, evaluationUUID}, fragments: []string{"FROM workbench_evaluations", "e.evaluation_uuid = $2"},
		},
		{
			name: "upsert evaluation", statement: workbenchEvaluationMapperUpsertStatement,
			bound: buildWorkbenchEvaluationMapperUpsert(yourbatis.DialectPostgres, evaluationParams),
			id:    "WorkbenchEvaluationMapper.Upsert", kind: yourbatis.StatementInsert,
			values:    []any{organizationUUID, revisionUUID, organizationUUID, revisionUUID, evaluationUUID, evaluationParams.PayloadJSON},
			fragments: []string{"WITH target_revision", "INSERT INTO workbench_evaluations", "CAST($6 AS jsonb)"},
		},
		{
			name: "delete evaluation", statement: workbenchEvaluationMapperDeleteStatement,
			bound: buildWorkbenchEvaluationMapperDelete(yourbatis.DialectPostgres, organizationUUID, evaluationUUID),
			id:    "WorkbenchEvaluationMapper.Delete", kind: yourbatis.StatementDelete,
			values: []any{organizationUUID, evaluationUUID}, fragments: []string{"DELETE FROM workbench_evaluations", "evaluation_uuid = $2", "RETURNING"},
		},
		{
			name: "delete evaluations by organization", statement: workbenchEvaluationMapperDeleteByOrganizationStatement,
			bound: buildWorkbenchEvaluationMapperDeleteByOrganization(yourbatis.DialectPostgres, organizationUUID),
			id:    "WorkbenchEvaluationMapper.DeleteByOrganization", kind: yourbatis.StatementDelete,
			values: []any{organizationUUID}, fragments: []string{"DELETE FROM workbench_evaluations", "organization_uuid = $1"},
		},
		{
			name: "insert generated test case", statement: workbenchGeneratedTestCaseMapperInsertStatement,
			bound: buildWorkbenchGeneratedTestCaseMapperInsert(yourbatis.DialectPostgres, organizationUUID, `{"input":"value"}`),
			id:    "WorkbenchGeneratedTestCaseMapper.Insert", kind: yourbatis.StatementInsert,
			values:    []any{organizationUUID, `{"input":"value"}`},
			fragments: []string{"INSERT INTO workbench_generated_test_cases", "CAST($2 AS jsonb)"},
		},
		{
			name: "trim generated test cases", statement: workbenchGeneratedTestCaseMapperDeleteOlderThanLimitStatement,
			bound: buildWorkbenchGeneratedTestCaseMapperDeleteOlderThanLimit(yourbatis.DialectPostgres, organizationUUID, 10),
			id:    "WorkbenchGeneratedTestCaseMapper.DeleteOlderThanLimit", kind: yourbatis.StatementDelete,
			values: []any{organizationUUID, organizationUUID, 10}, fragments: []string{"uuid NOT IN", "ORDER BY created_at DESC", "LIMIT $3"},
		},
		{
			name: "lock generated test cases", statement: workbenchGeneratedTestCaseMapperListForUpdateStatement,
			bound: buildWorkbenchGeneratedTestCaseMapperListForUpdate(yourbatis.DialectPostgres, organizationUUID),
			id:    "WorkbenchGeneratedTestCaseMapper.ListForUpdate", kind: yourbatis.StatementSelect,
			values: []any{organizationUUID}, fragments: []string{"CAST(values AS text)", "ORDER BY created_at ASC", "FOR UPDATE"},
		},
		{
			name: "delete generated test case", statement: workbenchGeneratedTestCaseMapperDeleteByUUIDStatement,
			bound: buildWorkbenchGeneratedTestCaseMapperDeleteByUUID(yourbatis.DialectPostgres, testCaseUUID),
			id:    "WorkbenchGeneratedTestCaseMapper.DeleteByUUID", kind: yourbatis.StatementDelete,
			values: []any{testCaseUUID}, fragments: []string{"DELETE FROM workbench_generated_test_cases", "uuid = $1"},
		},
		{
			name: "delete generated test cases by organization", statement: workbenchGeneratedTestCaseMapperDeleteByOrganizationStatement,
			bound: buildWorkbenchGeneratedTestCaseMapperDeleteByOrganization(yourbatis.DialectPostgres, organizationUUID),
			id:    "WorkbenchGeneratedTestCaseMapper.DeleteByOrganization", kind: yourbatis.StatementDelete,
			values: []any{organizationUUID}, fragments: []string{"DELETE FROM workbench_generated_test_cases", "organization_uuid = $1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertWorkbenchMapperContract(t, test)
		})
	}
}

func assertWorkbenchMapperContract(t *testing.T, contract workbenchMapperContract) {
	t.Helper()
	if contract.statement.ID != contract.id || contract.statement.Kind != contract.kind || contract.statement.Source == "" {
		t.Fatalf("statement = %+v, want ID %q, kind %q, and source", contract.statement, contract.id, contract.kind)
	}
	if values := contract.bound.Values(); !reflect.DeepEqual(values, contract.values) {
		t.Fatalf("values = %#v, want %#v", values, contract.values)
	}
	if strings.Contains(contract.bound.SQL, "::") || strings.Contains(contract.bound.SQL, " AS uuid)") {
		t.Fatalf("SQL contains unsupported cast syntax: %q", contract.bound.SQL)
	}
	for _, fragment := range contract.fragments {
		if !strings.Contains(contract.bound.SQL, fragment) {
			t.Fatalf("SQL = %q, want fragment %q", contract.bound.SQL, fragment)
		}
	}
	for _, argument := range contract.bound.Args {
		wantSensitive := argument.Name == "params.PayloadJSON" ||
			argument.Name == "params.Value" ||
			argument.Name == "params.VersionJSON" ||
			argument.Name == "valuesJSON"
		if argument.Sensitive != wantSensitive {
			t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, wantSensitive)
		}
	}
}

func TestWorkbenchMapperRowsMapDatabaseValues(t *testing.T) {
	prompt := workbenchPromptRow{
		OrgUUID:            "00000000-0000-4000-8000-000000000002",
		PromptUUID:         "prompt_test",
		WorkspaceUUID:      "00000000-0000-4000-8000-000000000001",
		WorkspaceDisplayID: "workspace_test",
		LatestRevisionUUID: sql.NullString{String: "revision_test", Valid: true},
	}.record()
	if prompt.LatestRevisionUUID == nil || *prompt.LatestRevisionUUID != "revision_test" {
		t.Fatalf("latest revision = %#v, want revision_test", prompt.LatestRevisionUUID)
	}

	evaluation := workbenchEvaluationRow{PayloadJSON: `{"score":1}`}.record()
	if evaluation.Payload["score"] != float64(1) {
		t.Fatalf("evaluation payload = %#v, want score 1", evaluation.Payload)
	}
}

func TestWorkbenchNoRowsMapsToPlatformNotFound(t *testing.T) {
	if err := mapNoRows(sql.ErrNoRows); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("mapNoRows(sql.ErrNoRows) = %v, want platform.ErrNotFound", err)
	}
}
