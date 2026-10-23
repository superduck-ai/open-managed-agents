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

func TestEvaluationDatasetMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	creator := "00000000-0000-4000-8000-000000000004"
	params := evaluationDatasetWriteParams{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "evalds_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "billing", Description: "nightly", CreatedByUserUUID: &creator, CreatedAt: now,
	}
	page := evaluationDatasetPageParams{
		OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		Cursor: &EvaluationDatasetPageCursor{CreatedAt: now, UUID: "00000000-0000-4000-8000-000000000009"},
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: evaluationDatasetMapperInsertStatement, bound: buildEvaluationDatasetMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "EvaluationDatasetMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.Name", "params.Description", "params.CreatedByUserUUID", "params.CreatedAt", "params.CreatedAt",
			},
			wantSQLFragments: []string{"INSERT INTO evaluation_datasets", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement: evaluationDatasetMapperFindByExternalIDStatement,
			bound:     buildEvaluationDatasetMapperFindByExternalID(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID),
			wantID:    "EvaluationDatasetMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"evaluation_dataset_versions", "case_count", "archived_at IS NULL"},
		}},
		{"find for update", mapperBuilderContract{
			statement: evaluationDatasetMapperFindForUpdateStatement,
			bound:     buildEvaluationDatasetMapperFindForUpdate(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID),
			wantID:    "EvaluationDatasetMapper.FindForUpdate", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"FROM evaluation_datasets", "archived_at IS NULL", "FOR UPDATE"},
		}},
		{"list page", mapperBuilderContract{
			statement: evaluationDatasetMapperListPageStatement, bound: buildEvaluationDatasetMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "EvaluationDatasetMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.OrganizationUUID", "params.WorkspaceUUID", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.FetchLimit",
			},
			wantSQLFragments: []string{"(d.created_at, d.uuid) < ($3, $4)", "ORDER BY d.created_at DESC, d.uuid DESC", "LIMIT $5"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("list page omits cursor", func(t *testing.T) {
		bound := buildEvaluationDatasetMapperListPage(yourbatis.DialectPostgres, evaluationDatasetPageParams{
			OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		})
		assertMapperBuilderContract(t, mapperBuilderContract{
			statement: evaluationDatasetMapperListPageStatement, bound: bound,
			wantID: "EvaluationDatasetMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.OrganizationUUID", "params.WorkspaceUUID", "params.FetchLimit"},
			wantSQLFragments:  []string{"ORDER BY d.created_at DESC, d.uuid DESC", "LIMIT $3"},
		})
		if containsMapperSQL(bound.SQL, "(d.created_at, d.uuid) <") {
			t.Fatalf("ListPage SQL = %q, does not want a cursor predicate", bound.SQL)
		}
	})
}

func TestEvaluationDatasetVersionMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	params := evaluationDatasetVersionWriteParams{
		UUID: "00000000-0000-4000-8000-000000000008", ExternalID: "edv_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		DatasetUUID: "00000000-0000-4000-8000-000000000003", Version: 1, CreatedAt: now,
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: evaluationDatasetVersionMapperInsertStatement, bound: buildEvaluationDatasetVersionMapperInsert(yourbatis.DialectPostgres, params),
		wantID: "EvaluationDatasetVersionMapper.Insert", wantKind: yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
			"params.DatasetUUID", "params.Version", "params.CreatedAt",
		},
		wantSQLFragments: []string{"INSERT INTO evaluation_dataset_versions", "RETURNING"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: evaluationDatasetVersionMapperFindLatestStatement,
		bound:     buildEvaluationDatasetVersionMapperFindLatest(yourbatis.DialectPostgres, params.OrganizationUUID, params.WorkspaceUUID, params.DatasetUUID),
		wantID:    "EvaluationDatasetVersionMapper.FindLatest", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "datasetUUID"},
		wantSQLFragments:  []string{"ORDER BY version DESC, uuid DESC", "LIMIT 1"},
	})
}

func TestEvaluationDatasetMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	params := evaluationDatasetWriteParams{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "evalds_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "billing", CreatedAt: now,
	}

	t.Run("insert returns dataset row", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: evaluationDatasetInsertColumns(), rows: [][]driver.Value{evaluationDatasetInsertRow(nil)},
		})
		row, err := NewEvaluationDatasetMapper(executor).Insert(ctx, params)
		if err != nil || row.Name != "billing" || row.Description != "nightly" {
			t.Fatalf("insert = (%+v, %v)", row, err)
		}
	})

	t.Run("find returns latest version and case count", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: evaluationDatasetPageColumns(), rows: [][]driver.Value{evaluationDatasetPageRowValues(nil)},
		})
		row, err := NewEvaluationDatasetMapper(executor).FindByExternalID(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID)
		if err != nil || row.VersionExternalID != "edv_test" || row.VersionNumber != 1 || row.CaseCount != 2 {
			t.Fatalf("find = (%+v, %v)", row, err)
		}
		dataset := row.evaluationDataset()
		if dataset.Version.ExternalID != "edv_test" || dataset.Version.CaseCount != 2 || dataset.CreatedByUserUUID == "" {
			t.Fatalf("decoded dataset = %+v", dataset)
		}
	})

	t.Run("null creator stays empty", func(t *testing.T) {
		values := evaluationDatasetPageRowValues(nil)
		values[6] = nil
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: evaluationDatasetPageColumns(), rows: [][]driver.Value{values}})
		row, err := NewEvaluationDatasetMapper(executor).FindByExternalID(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID)
		if err != nil {
			t.Fatal(err)
		}
		if dataset := row.evaluationDataset(); dataset.CreatedByUserUUID != "" {
			t.Fatalf("creator = %q", dataset.CreatedByUserUUID)
		}
	})

	zeroRowCalls := []struct {
		name    string
		columns []string
		call    func(EvaluationDatasetMapper) error
	}{
		{name: "find", columns: evaluationDatasetPageColumns(), call: func(mapper EvaluationDatasetMapper) error {
			_, err := mapper.FindByExternalID(ctx, params.OrganizationUUID, params.WorkspaceUUID, "evalds_missing")
			return err
		}},
		{name: "find for update", columns: evaluationDatasetInsertColumns(), call: func(mapper EvaluationDatasetMapper) error {
			_, err := mapper.FindForUpdate(ctx, params.OrganizationUUID, params.WorkspaceUUID, "evalds_missing")
			return err
		}},
		{name: "insert", columns: evaluationDatasetInsertColumns(), call: func(mapper EvaluationDatasetMapper) error {
			_, err := mapper.Insert(ctx, params)
			return err
		}},
	}
	for _, test := range zeroRowCalls {
		t.Run(test.name+" zero rows preserves sql ErrNoRows", func(t *testing.T) {
			executor := newMapperTestExecutor(t, mapperTestResponse{columns: test.columns})
			if err := test.call(NewEvaluationDatasetMapper(executor)); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("mapper error = %v, want sql.ErrNoRows", err)
			}
		})
	}

	executionCalls := []mapperExecutionErrorContract{
		{statementID: "EvaluationDatasetMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationDatasetMapper(executor).Insert(ctx, params)
			return err
		}},
		{statementID: "EvaluationDatasetMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationDatasetMapper(executor).FindByExternalID(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID)
			return err
		}},
		{statementID: "EvaluationDatasetMapper.FindForUpdate", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationDatasetMapper(executor).FindForUpdate(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.ExternalID)
			return err
		}},
		{statementID: "EvaluationDatasetMapper.ListPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationDatasetMapper(executor).ListPage(ctx, evaluationDatasetPageParams{
				OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 20,
			})
			return err
		}},
	}
	for _, contract := range executionCalls {
		t.Run(contract.statementID+" returns execution error", func(t *testing.T) {
			assertMapperExecutionError(t, contract)
		})
	}
}

func TestEvaluationDatasetVersionMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	params := evaluationDatasetVersionWriteParams{
		UUID: "00000000-0000-4000-8000-000000000008", ExternalID: "edv_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		DatasetUUID: "00000000-0000-4000-8000-000000000003", Version: 1, CreatedAt: now,
	}
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: evaluationDatasetVersionColumns(), rows: [][]driver.Value{evaluationDatasetVersionRowValues()}})
	row, err := NewEvaluationDatasetVersionMapper(executor).FindLatest(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.DatasetUUID)
	if err != nil || row.evaluationDatasetVersion().ExternalID != "edv_test" || row.Version != 1 {
		t.Fatalf("find latest = (%+v, %v)", row, err)
	}
	executor = newMapperTestExecutor(t, mapperTestResponse{columns: evaluationDatasetVersionColumns()})
	if _, err := NewEvaluationDatasetVersionMapper(executor).FindLatest(ctx, params.OrganizationUUID, params.WorkspaceUUID, params.DatasetUUID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("zero rows = %v", err)
	}
	assertMapperExecutionError(t, mapperExecutionErrorContract{
		statementID: "EvaluationDatasetVersionMapper.Insert", kind: yourbatis.StatementInsert, query: true,
		call: func(executor yourbatis.Executor) error {
			_, err := NewEvaluationDatasetVersionMapper(executor).Insert(ctx, params)
			return err
		},
	})
}

func evaluationDatasetInsertColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "name", "description",
		"created_by_user_uuid", "created_at", "updated_at", "archived_at",
	}
}

func evaluationDatasetPageColumns() []string {
	return append(evaluationDatasetInsertColumns(), "version_uuid", "version_external_id", "version_number", "version_created_at", "case_count")
}

func evaluationDatasetInsertRow(archivedAt any) []driver.Value {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000003", "evalds_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"billing", "nightly", "00000000-0000-4000-8000-000000000004", now, now, archivedAt,
	}
}

func evaluationDatasetPageRowValues(archivedAt any) []driver.Value {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	return append(evaluationDatasetInsertRow(archivedAt),
		"00000000-0000-4000-8000-000000000008", "edv_test", int64(1), now, int64(2),
	)
}

func evaluationDatasetVersionColumns() []string {
	return []string{"uuid", "external_id", "organization_uuid", "workspace_uuid", "dataset_uuid", "version", "created_at"}
}

func evaluationDatasetVersionRowValues() []driver.Value {
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000008", "edv_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", int64(1), now,
	}
}
