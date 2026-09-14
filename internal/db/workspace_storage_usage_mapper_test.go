package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestWorkspaceStorageUsageMapperBuilderContracts(t *testing.T) {
	workspaceUUID := "workspace-uuid"
	params := workspaceStorageUsageParams{
		WorkspaceUUID:  workspaceUUID,
		FilesBytes:     11,
		FilestoreBytes: 13,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "lock workspace",
			contract: mapperBuilderContract{
				statement:         workspaceStorageUsageMapperLockWorkspaceStatement,
				bound:             buildWorkspaceStorageUsageMapperLockWorkspace(yourbatis.DialectPostgres, workspaceUUID),
				wantID:            "WorkspaceStorageUsageMapper.LockWorkspace",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"workspaceUUID"},
				wantSQLFragments:  []string{"pg_advisory_xact_lock", "CAST($1 AS text)"},
			},
		},
		{
			name: "get storage bytes",
			contract: mapperBuilderContract{
				statement:         workspaceStorageUsageMapperGetWorkspaceStorageBytesStatement,
				bound:             buildWorkspaceStorageUsageMapperGetWorkspaceStorageBytes(yourbatis.DialectPostgres, workspaceUUID),
				wantID:            "WorkspaceStorageUsageMapper.GetWorkspaceStorageBytes",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID"},
				wantSQLFragments:  []string{"files_bytes + filestore_bytes", "workspace_uuid = $1"},
			},
		},
		{
			name: "reconcile storage usage",
			contract: mapperBuilderContract{
				statement:         workspaceStorageUsageMapperReconcileWorkspaceStorageUsageStatement,
				bound:             buildWorkspaceStorageUsageMapperReconcileWorkspaceStorageUsage(yourbatis.DialectPostgres, workspaceUUID),
				wantID:            "WorkspaceStorageUsageMapper.ReconcileWorkspaceStorageUsage",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "workspaceUUID", "workspaceUUID", "workspaceUUID"},
				wantSQLFragments:  []string{"AS files_bytes", "AS filestore_bytes", "resource.file_ownership = 'owned'", "resource.resource_type = 'skill_archive'"},
			},
		},
		{
			name: "upsert storage usage",
			contract: mapperBuilderContract{
				statement:         workspaceStorageUsageMapperUpsertWorkspaceStorageUsageStatement,
				bound:             buildWorkspaceStorageUsageMapperUpsertWorkspaceStorageUsage(yourbatis.DialectPostgres, params),
				wantID:            "WorkspaceStorageUsageMapper.UpsertWorkspaceStorageUsage",
				wantKind:          yourbatis.StatementInsert,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.FilesBytes", "params.FilestoreBytes"},
				wantSQLFragments:  []string{"INSERT INTO workspace_storage_usage", "ON CONFLICT (workspace_uuid) DO UPDATE"},
			},
		},
		{
			name: "ensure storage usage",
			contract: mapperBuilderContract{
				statement:         workspaceStorageUsageMapperEnsureWorkspaceStorageUsageStatement,
				bound:             buildWorkspaceStorageUsageMapperEnsureWorkspaceStorageUsage(yourbatis.DialectPostgres, workspaceUUID),
				wantID:            "WorkspaceStorageUsageMapper.EnsureWorkspaceStorageUsage",
				wantKind:          yourbatis.StatementInsert,
				wantArgumentNames: []string{"workspaceUUID"},
				wantSQLFragments:  []string{"INSERT INTO workspace_storage_usage", "DO NOTHING"},
			},
		},
		{
			name: "get storage usage for update",
			contract: mapperBuilderContract{
				statement:         workspaceStorageUsageMapperGetWorkspaceStorageUsageForUpdateStatement,
				bound:             buildWorkspaceStorageUsageMapperGetWorkspaceStorageUsageForUpdate(yourbatis.DialectPostgres, workspaceUUID),
				wantID:            "WorkspaceStorageUsageMapper.GetWorkspaceStorageUsageForUpdate",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID"},
				wantSQLFragments:  []string{"files_bytes", "filestore_bytes", "FOR UPDATE"},
			},
		},
		{
			name: "update storage usage",
			contract: mapperBuilderContract{
				statement:         workspaceStorageUsageMapperUpdateWorkspaceStorageUsageStatement,
				bound:             buildWorkspaceStorageUsageMapperUpdateWorkspaceStorageUsage(yourbatis.DialectPostgres, params),
				wantID:            "WorkspaceStorageUsageMapper.UpdateWorkspaceStorageUsage",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.FilesBytes", "params.FilestoreBytes", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"UPDATE workspace_storage_usage", "files_bytes = $1", "workspace_uuid = $3"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestWorkspaceStorageUsageMapperPropagatesExecutionErrors(t *testing.T) {
	executionErr := errors.New("database unavailable")
	workspaceUUID := "workspace-uuid"
	params := workspaceStorageUsageParams{WorkspaceUUID: workspaceUUID}
	tests := []struct {
		name        string
		statementID string
		kind        yourbatis.StatementKind
		query       bool
		call        func(WorkspaceStorageUsageMapper) error
	}{
		{name: "lock workspace", statementID: "WorkspaceStorageUsageMapper.LockWorkspace", kind: yourbatis.StatementUpdate, call: func(mapper WorkspaceStorageUsageMapper) error {
			return mapper.LockWorkspace(context.Background(), workspaceUUID)
		}},
		{name: "get storage bytes", statementID: "WorkspaceStorageUsageMapper.GetWorkspaceStorageBytes", kind: yourbatis.StatementSelect, query: true, call: func(mapper WorkspaceStorageUsageMapper) error {
			_, err := mapper.GetWorkspaceStorageBytes(context.Background(), workspaceUUID)
			return err
		}},
		{name: "reconcile storage usage", statementID: "WorkspaceStorageUsageMapper.ReconcileWorkspaceStorageUsage", kind: yourbatis.StatementSelect, query: true, call: func(mapper WorkspaceStorageUsageMapper) error {
			_, err := mapper.ReconcileWorkspaceStorageUsage(context.Background(), workspaceUUID)
			return err
		}},
		{name: "upsert storage usage", statementID: "WorkspaceStorageUsageMapper.UpsertWorkspaceStorageUsage", kind: yourbatis.StatementInsert, call: func(mapper WorkspaceStorageUsageMapper) error {
			return mapper.UpsertWorkspaceStorageUsage(context.Background(), params)
		}},
		{name: "ensure storage usage", statementID: "WorkspaceStorageUsageMapper.EnsureWorkspaceStorageUsage", kind: yourbatis.StatementInsert, call: func(mapper WorkspaceStorageUsageMapper) error {
			return mapper.EnsureWorkspaceStorageUsage(context.Background(), workspaceUUID)
		}},
		{name: "get storage usage for update", statementID: "WorkspaceStorageUsageMapper.GetWorkspaceStorageUsageForUpdate", kind: yourbatis.StatementSelect, query: true, call: func(mapper WorkspaceStorageUsageMapper) error {
			_, err := mapper.GetWorkspaceStorageUsageForUpdate(context.Background(), workspaceUUID)
			return err
		}},
		{name: "update storage usage", statementID: "WorkspaceStorageUsageMapper.UpdateWorkspaceStorageUsage", kind: yourbatis.StatementUpdate, call: func(mapper WorkspaceStorageUsageMapper) error {
			return mapper.UpdateWorkspaceStorageUsage(context.Background(), params)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := mapperTestResponse{execErr: executionErr}
			if test.query {
				response = mapperTestResponse{queryErr: executionErr}
			}
			executor := newMapperTestExecutor(t, response)
			err := test.call(NewWorkspaceStorageUsageMapper(executor))
			if !errors.Is(err, executionErr) {
				t.Fatalf("mapper error = %v, want %v", err, executionErr)
			}
			assertMapperTestExecution(t, executor, test.statementID, test.kind, executor.bound.Values())
		})
	}
}

func TestWorkspaceStorageUsageMapperScanSemantics(t *testing.T) {
	t.Run("scan error", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"total_bytes"},
			rows:    [][]driver.Value{{"invalid"}},
		})
		_, err := NewWorkspaceStorageUsageMapper(executor).GetWorkspaceStorageBytes(context.Background(), "workspace-uuid")
		if err == nil {
			t.Fatal("GetWorkspaceStorageBytes error = nil, want scan error")
		}
	})

	t.Run("zero rows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"total_bytes"}})
		_, err := NewWorkspaceStorageUsageMapper(executor).GetWorkspaceStorageBytes(context.Background(), "workspace-uuid")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetWorkspaceStorageBytes error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("scalar success", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"total_bytes"},
			rows:    [][]driver.Value{{int64(24)}},
		})
		got, err := NewWorkspaceStorageUsageMapper(executor).GetWorkspaceStorageBytes(context.Background(), "workspace-uuid")
		if err != nil || got != 24 {
			t.Fatalf("GetWorkspaceStorageBytes = (%d, %v), want (24, nil)", got, err)
		}
	})

	t.Run("row success", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"files_bytes", "filestore_bytes"},
			rows:    [][]driver.Value{{int64(11), int64(13)}},
		})
		got, err := NewWorkspaceStorageUsageMapper(executor).GetWorkspaceStorageUsageForUpdate(context.Background(), "workspace-uuid")
		if err != nil || got.FilesBytes != 11 || got.FilestoreBytes != 13 {
			t.Fatalf("GetWorkspaceStorageUsageForUpdate = (%+v, %v), want files=11 filestore=13", got, err)
		}
	})
}
