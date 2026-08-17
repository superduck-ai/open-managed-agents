package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type dreamMapperContract struct {
	statement      yourbatis.Statement
	bound          yourbatis.BoundSQL
	id             string
	kind           yourbatis.StatementKind
	argumentNames  []string
	fragments      []string
	sensitiveNames []string
}

func TestDreamMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 16, 1, 2, 3, 0, time.UTC)
	insertParams := insertDreamParams{
		UUID: "dream-uuid", ExternalID: "dream_test", OrganizationUUID: "org-uuid",
		WorkspaceUUID: "workspace-uuid", CreatedByAPIKeyUUID: "key-uuid",
		InputStoreUUID: "store-uuid", SessionIDs: []byte(`["ses_1"]`),
		Instructions: ptrDBString("distill"), Model: "claude-opus-4-8", Status: DreamStatusPending, CreatedAt: now,
	}
	statusParams := updateDreamStatusParams{
		WorkspaceUUID: "workspace-uuid", ExternalID: "dream_test", Status: DreamStatusRunning,
	}
	outputParams := setDreamOutputStoreParams{
		WorkspaceUUID: "workspace-uuid", ExternalID: "dream_test", OutputStoreUUID: "output-store-uuid",
	}
	errorParams := setDreamErrorParams{
		WorkspaceUUID: "workspace-uuid", ExternalID: "dream_test", Error: "distillation failed",
	}
	listParams := listDreamsParams{
		WorkspaceUUID: "workspace-uuid", Limit: 21,
		HasCursor: true, CursorCreatedAt: now, CursorUUID: "cursor-uuid",
	}
	tests := []dreamMapperContract{
		{
			statement: dreamMapperInsertStatement,
			bound:     buildDreamMapperInsert(yourbatis.DialectPostgres, insertParams),
			id:        "DreamMapper.Insert", kind: yourbatis.StatementInsert,
			argumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.CreatedByAPIKeyUUID", "params.InputStoreUUID", "params.SessionIDs",
				"params.Instructions", "params.Model", "params.Status", "params.CreatedAt", "params.CreatedAt",
			},
			fragments:      []string{"INSERT INTO dreams", "CAST($7 AS jsonb)", "RETURNING"},
			sensitiveNames: []string{"params.SessionIDs"},
		},
		{
			statement: dreamMapperFindByExternalIDStatement,
			bound:     buildDreamMapperFindByExternalID(yourbatis.DialectPostgres, "workspace-uuid", "dream_test"),
			id:        "DreamMapper.FindByExternalID", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments:     []string{"FROM dreams", "workspace_uuid = $1", "external_id = $2"},
		},
		{
			statement: dreamMapperFindForUpdateStatement,
			bound:     buildDreamMapperFindForUpdate(yourbatis.DialectPostgres, "workspace-uuid", "dream_test"),
			id:        "DreamMapper.FindForUpdate", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments:     []string{"FROM dreams", "workspace_uuid = $1", "external_id = $2", "archived_at IS NULL", "FOR UPDATE"},
		},
		{
			statement: dreamMapperListPageStatement,
			bound:     buildDreamMapperListPage(yourbatis.DialectPostgres, listParams),
			id:        "DreamMapper.ListPage", kind: yourbatis.StatementSelect,
			argumentNames: []string{
				"params.WorkspaceUUID", "params.CursorCreatedAt", "params.CursorCreatedAt",
				"params.CursorUUID", "params.Limit",
			},
			fragments: []string{
				"FROM dreams", "workspace_uuid = $1", "archived_at IS NULL",
				"created_at < $2", "uuid < $4", "ORDER BY created_at DESC, uuid DESC", "LIMIT $5",
			},
		},
		{
			statement: dreamMapperUpdateStatusStatement,
			bound:     buildDreamMapperUpdateStatus(yourbatis.DialectPostgres, statusParams),
			id:        "DreamMapper.UpdateStatus", kind: yourbatis.StatementUpdate,
			argumentNames: []string{"params.Status", "params.WorkspaceUUID", "params.ExternalID", "params.Status"},
			fragments:     []string{"UPDATE dreams", "SET status = $1", "archived_at IS NULL", "status IN ('pending', 'running')", "status != $4"},
		},
		{
			statement: dreamMapperSetOutputStoreStatement,
			bound:     buildDreamMapperSetOutputStore(yourbatis.DialectPostgres, outputParams),
			id:        "DreamMapper.SetOutputStore", kind: yourbatis.StatementUpdate,
			argumentNames: []string{"params.OutputStoreUUID", "params.WorkspaceUUID", "params.ExternalID"},
			fragments:     []string{"UPDATE dreams", "SET output_store_uuid = $1", "status = 'running'"},
		},
		{
			statement: dreamMapperSetErrorStatement,
			bound:     buildDreamMapperSetError(yourbatis.DialectPostgres, errorParams),
			id:        "DreamMapper.SetError", kind: yourbatis.StatementUpdate,
			argumentNames: []string{"params.Error", "params.WorkspaceUUID", "params.ExternalID"},
			fragments:     []string{"UPDATE dreams", "SET error = $1", "status = 'failed'", "status = 'running'"},
		},
		{
			statement: dreamMapperArchiveByExternalIDStatement,
			bound:     buildDreamMapperArchiveByExternalID(yourbatis.DialectPostgres, "workspace-uuid", "dream_test"),
			id:        "DreamMapper.ArchiveByExternalID", kind: yourbatis.StatementUpdate,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments: []string{
				"UPDATE dreams", "SET archived_at", "status IN ('succeeded', 'failed', 'cancelled')",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			assertDreamMapperContract(t, test)
		})
	}
}

func TestDreamMapperListPageBranches(t *testing.T) {
	now := time.Date(2026, time.August, 16, 1, 2, 3, 0, time.UTC)
	t.Run("without cursor", func(t *testing.T) {
		bound := buildDreamMapperListPage(yourbatis.DialectPostgres, listDreamsParams{
			WorkspaceUUID: "workspace-uuid", Limit: 21,
		})
		compact := compactDreamMapperSQL(bound.SQL)
		if strings.Contains(compact, "created_at <") || strings.Contains(compact, "CursorCreatedAt") {
			t.Fatalf("ListPage without cursor must not include cursor predicate: %q", bound.SQL)
		}
	})
	t.Run("with cursor", func(t *testing.T) {
		bound := buildDreamMapperListPage(yourbatis.DialectPostgres, listDreamsParams{
			WorkspaceUUID: "workspace-uuid", Limit: 21,
			HasCursor: true, CursorCreatedAt: now, CursorUUID: "cursor-uuid",
		})
		if strings.Contains(bound.SQL, "CursorCreatedAt") {
			t.Fatalf("ListPage SQL retains param names: %q", bound.SQL)
		}
		compact := compactDreamMapperSQL(bound.SQL)
		if !strings.Contains(compact, "created_at < $2 OR (created_at = $3 AND uuid < $4)") {
			t.Fatalf("ListPage cursor predicate missing: %q", bound.SQL)
		}
	})
}

func TestDreamMapperExecutionModes(t *testing.T) {
	ctx := context.Background()
	tests := []mapperExecutionErrorContract{
		{statementID: "DreamMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).Insert(ctx, insertDreamParams{})
			return err
		}},
		{statementID: "DreamMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).FindByExternalID(ctx, "workspace", "dream")
			return err
		}},
		{statementID: "DreamMapper.FindForUpdate", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).FindForUpdate(ctx, "workspace", "dream")
			return err
		}},
		{statementID: "DreamMapper.ListPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).ListPage(ctx, listDreamsParams{})
			return err
		}},
		{statementID: "DreamMapper.UpdateStatus", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).UpdateStatus(ctx, updateDreamStatusParams{})
			return err
		}},
		{statementID: "DreamMapper.SetOutputStore", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).SetOutputStore(ctx, setDreamOutputStoreParams{})
			return err
		}},
		{statementID: "DreamMapper.SetError", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).SetError(ctx, setDreamErrorParams{})
			return err
		}},
		{statementID: "DreamMapper.ArchiveByExternalID", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewDreamMapper(executor).ArchiveByExternalID(ctx, "workspace", "dream")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.statementID, func(t *testing.T) {
			assertMapperExecutionError(t, test)
		})
	}
}

func TestDreamMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	t.Run("single row not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: dreamMapperTestColumns()})
		_, err := NewDreamMapper(executor).FindByExternalID(ctx, "workspace", "dream")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("full row round trip", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: dreamMapperTestColumns(),
			rows:    [][]driver.Value{dreamMapperTestRow()},
		})
		row, err := NewDreamMapper(executor).Insert(ctx, insertDreamParams{})
		dream, mapErr := dreamFromMapperRow(row, err)
		if mapErr != nil || dream.UUID != "00000000-0000-4000-8000-000000000001" ||
			dream.ExternalID != "dream_test" || len(dream.SessionIDs) != 2 ||
			dream.SessionIDs[0] != "ses_1" || dream.Status != "pending" ||
			dream.Instructions == nil || *dream.Instructions != "distill carefully" ||
			dream.OutputStoreUUID != nil || dream.ArchivedAt != nil {
			t.Fatalf("Insert() = (%+v, %v)", dream, mapErr)
		}
	})

	t.Run("nullable columns row", func(t *testing.T) {
		row := dreamMapperTestRow()
		row[7] = nil  // instructions
		row[10] = nil // output_store_uuid
		row[11] = nil // error
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: dreamMapperTestColumns(),
			rows:    [][]driver.Value{row},
		})
		got, err := NewDreamMapper(executor).FindByExternalID(ctx, "workspace", "dream")
		dream, mapErr := dreamFromMapperRow(got, err)
		if mapErr != nil || dream.Instructions != nil || dream.OutputStoreUUID != nil || dream.Error != nil {
			t.Fatalf("FindByExternalID() = (%+v, %v)", dream, mapErr)
		}
	})

	t.Run("many rows and rows affected", func(t *testing.T) {
		listExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: dreamMapperTestColumns()})
		rows, err := NewDreamMapper(listExecutor).ListPage(ctx, listDreamsParams{})
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListPage() = (%+v, %v)", rows, err)
		}
		rowsExecutor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		affected, err := NewDreamMapper(rowsExecutor).UpdateStatus(ctx, updateDreamStatusParams{})
		if err != nil || affected != 1 {
			t.Fatalf("UpdateStatus() = (%d, %v)", affected, err)
		}
	})
}

func assertDreamMapperContract(t *testing.T, contract dreamMapperContract) {
	t.Helper()
	if contract.statement.ID != contract.id || contract.statement.Kind != contract.kind || contract.statement.Source == "" {
		t.Fatalf("statement = %+v, want ID %q, kind %q, and source", contract.statement, contract.id, contract.kind)
	}
	argumentNames := make([]string, len(contract.bound.Args))
	for index := range contract.bound.Args {
		argumentNames[index] = contract.bound.Args[index].Name
	}
	if !reflect.DeepEqual(argumentNames, contract.argumentNames) {
		t.Fatalf("argument names = %#v, want %#v", argumentNames, contract.argumentNames)
	}
	if strings.Contains(contract.bound.SQL, "#{") || strings.Contains(contract.bound.SQL, "::") {
		t.Fatalf("SQL retains unsupported placeholder or cast syntax: %q", contract.bound.SQL)
	}
	compact := compactDreamMapperSQL(contract.bound.SQL)
	for _, fragment := range contract.fragments {
		if !strings.Contains(compact, compactDreamMapperSQL(fragment)) {
			t.Fatalf("SQL = %q, want fragment %q", contract.bound.SQL, fragment)
		}
	}
	sensitive := make(map[string]bool)
	for _, name := range contract.sensitiveNames {
		sensitive[name] = true
	}
	for _, argument := range contract.bound.Args {
		if argument.Sensitive != sensitive[argument.Name] {
			t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, sensitive[argument.Name])
		}
	}
}

func compactDreamMapperSQL(query string) string {
	return strings.Join(strings.Fields(query), " ")
}

func dreamMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "created_by_api_key_uuid",
		"input_store_uuid", "session_ids", "instructions", "model", "status", "output_store_uuid",
		"error", "created_at", "updated_at", "archived_at",
	}
}

func dreamMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 16, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000001", "dream_test",
		"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004", "00000000-0000-4000-8000-000000000005",
		[]byte(`["ses_1","ses_2"]`), "distill carefully", "claude-opus-4-8", "pending",
		nil, nil, now, now, nil,
	}
}

func ptrDBString(value string) *string {
	return &value
}
