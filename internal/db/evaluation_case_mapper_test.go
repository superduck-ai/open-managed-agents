package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestEvaluationCaseMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	checkpoints := `["done"]`
	files := `[]`
	params := evaluationCaseWriteParams{
		UUID: "00000000-0000-4000-8000-000000000005", ExternalID: "evalcase_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		DatasetUUID: "00000000-0000-4000-8000-000000000003", VersionUUID: "00000000-0000-4000-8000-000000000008",
		Position: 1, Name: "greeting", Description: "note", TurnsJSON: `["hello"]`,
		CheckpointsJSON: &checkpoints, InputFilesJSON: &files, CreatedAt: now,
	}
	page := evaluationCasePageParams{
		OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, VersionUUID: params.VersionUUID,
		FetchLimit: 21, Cursor: &EvaluationCasePageCursor{Position: 1, UUID: params.UUID},
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"max position", mapperBuilderContract{
			statement: evaluationCaseMapperMaxPositionStatement,
			bound:     buildEvaluationCaseMapperMaxPosition(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, params.VersionUUID),
			wantID:    "EvaluationCaseMapper.MaxPosition", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "versionUUID"},
			wantSQLFragments:  []string{"COALESCE(MAX(position), 0)", "version_uuid = $3"},
		}},
		{"insert", mapperBuilderContract{
			statement: evaluationCaseMapperInsertStatement, bound: buildEvaluationCaseMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "EvaluationCaseMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.DatasetUUID", "params.VersionUUID", "params.Position", "params.Name", "params.Description",
				"params.TurnsJSON", "params.CheckpointsJSON", "params.InputFilesJSON", "params.ReferenceFilesJSON",
				"params.CreatedAt", "params.CreatedAt",
			},
			wantSQLFragments: []string{"INSERT INTO evaluation_cases", "CAST(", "AS jsonb)", "RETURNING"},
		}},
		{"update", mapperBuilderContract{
			statement: evaluationCaseMapperUpdateStatement, bound: buildEvaluationCaseMapperUpdate(yourbatis.DialectPostgres, evaluationCaseUpdateParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, VersionUUID: params.VersionUUID,
				ExternalID: params.ExternalID, Name: params.Name, Description: params.Description, TurnsJSON: params.TurnsJSON,
				CheckpointsJSON: params.CheckpointsJSON, InputFilesJSON: params.InputFilesJSON, UpdatedAt: now,
			}),
			wantID: "EvaluationCaseMapper.Update", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.Name", "params.Description", "params.TurnsJSON", "params.CheckpointsJSON",
				"params.InputFilesJSON", "params.ReferenceFilesJSON", "params.UpdatedAt",
				"params.OrganizationUUID", "params.WorkspaceUUID", "params.VersionUUID", "params.ExternalID",
			},
			wantSQLFragments: []string{"UPDATE evaluation_cases", "CAST(", "AS jsonb)", "external_id = $11", "RETURNING"},
		}},
		{"delete", mapperBuilderContract{
			statement: evaluationCaseMapperDeleteStatement,
			bound:     buildEvaluationCaseMapperDelete(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, params.VersionUUID, params.ExternalID),
			wantID:    "EvaluationCaseMapper.Delete", wantKind: yourbatis.StatementDelete,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "versionUUID", "externalID"},
			wantSQLFragments:  []string{"DELETE FROM evaluation_cases", "external_id = $4", "RETURNING"},
		}},
		{"list page", mapperBuilderContract{
			statement: evaluationCaseMapperListPageStatement, bound: buildEvaluationCaseMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "EvaluationCaseMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.OrganizationUUID", "params.WorkspaceUUID", "params.VersionUUID",
				"params.Cursor.Position", "params.Cursor.UUID", "params.FetchLimit",
			},
			wantSQLFragments: []string{"version_uuid = $3", "(position, uuid) > ($4, $5)", "ORDER BY position ASC, uuid ASC", "LIMIT $6"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestEvaluationCaseMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	params := evaluationCaseWriteParams{
		UUID: "00000000-0000-4000-8000-000000000005", ExternalID: "evalcase_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		DatasetUUID: "00000000-0000-4000-8000-000000000003", VersionUUID: "00000000-0000-4000-8000-000000000008",
		Position: 1, Name: "greeting", TurnsJSON: `["hello"]`, CreatedAt: now,
	}
	rowResponse := mapperTestResponse{columns: evaluationCaseMapperTestColumns(), rows: [][]driver.Value{evaluationCaseMapperTestRow(`["done"]`, `[]`)}}

	t.Run("insert scans turns and attachments", func(t *testing.T) {
		executor := newMapperTestExecutor(t, rowResponse)
		row, err := NewEvaluationCaseMapper(executor).Insert(ctx, params)
		if err != nil || row.Position != 1 || row.Name != "greeting" {
			t.Fatalf("insert = (%+v, %v)", row, err)
		}
		item, decodeErr := row.evaluationCase("evalds_test", "edv_test")
		if decodeErr != nil || len(item.Turns) != 1 || item.Turns[0] != "hello" || string(item.Checkpoints) != `["done"]` || item.VersionExternalID != "edv_test" {
			t.Fatalf("decoded case = (%+v, %v)", item, decodeErr)
		}
	})

	t.Run("null attachments stay empty", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: evaluationCaseMapperTestColumns(), rows: [][]driver.Value{evaluationCaseMapperTestRow("", "")},
		})
		row, err := NewEvaluationCaseMapper(executor).Insert(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		item, decodeErr := row.evaluationCase("evalds_test", "edv_test")
		if decodeErr != nil || item.Checkpoints != nil || item.InputFiles != nil || item.ReferenceFiles != nil {
			t.Fatalf("decoded case = (%+v, %v)", item, decodeErr)
		}
	})

	t.Run("max position scans scalar", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"coalesce"}, rows: [][]driver.Value{{int64(4)}}})
		position, err := NewEvaluationCaseMapper(executor).MaxPosition(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.VersionUUID)
		if err != nil || position != 4 {
			t.Fatalf("MaxPosition() = (%d, %v)", position, err)
		}
	})

	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"coalesce"}})
	if _, err := NewEvaluationCaseMapper(executor).MaxPosition(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.VersionUUID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("zero rows = %v", err)
	}
	assertMapperExecutionError(t, mapperExecutionErrorContract{
		statementID: "EvaluationCaseMapper.ListPage", kind: yourbatis.StatementSelect, query: true,
		call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationCaseMapper(executor).ListPage(ctx, evaluationCasePageParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, VersionUUID: params.VersionUUID, FetchLimit: 20,
			})
			return err
		},
	})
}

func TestCreateEvaluationCasesRejectsEmptyWriteBeforeStore(t *testing.T) {
	_, err := (&DB{}).CreateEvaluationCases(context.Background(), "org", "workspace", "evalds_test", nil, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty create = %v, want ErrNotFound", err)
	}
}

func TestEvaluationCaseJSONRoundTrip(t *testing.T) {
	encoded, err := encodeEvaluationTurns([]string{"hello"})
	if err != nil || encoded != `["hello"]` {
		t.Fatalf("encode turns = (%q, %v)", encoded, err)
	}
	if _, err := decodeEvaluationTurns("{"); err == nil {
		t.Fatal("invalid turns JSON succeeded")
	}
	checkpoints := encodeOptionalJSON(json.RawMessage(`{"keep":true}`))
	if checkpoints == nil || decodeOptionalJSON(checkpoints) == nil {
		t.Fatal("optional JSON dropped")
	}
	if encodeOptionalJSON(nil) != nil || decodeOptionalJSON(nil) != nil {
		t.Fatal("empty optional JSON stored")
	}
}

func evaluationCaseMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "dataset_uuid", "version_uuid",
		"position", "name", "description", "turns_json", "checkpoints_json", "input_files_json",
		"reference_files_json", "created_at", "updated_at",
	}
}

func evaluationCaseMapperTestRow(checkpoints, files string) []driver.Value {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000005", "evalcase_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000008",
		int64(1), "greeting", "note", `["hello"]`, nullableDriver(checkpoints), nullableDriver(files), nil, now, now,
	}
}

func nullableDriver(value string) any {
	if value == "" {
		return nil
	}
	return value
}
