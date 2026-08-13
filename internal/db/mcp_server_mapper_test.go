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

func TestMCPServerMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC)
	params := mcpServerWriteParams{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "mcpsrv_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "internal-docs", TransportType: "url", EndpointURL: "https://example.test/mcp",
		CreatedAt: now, UpdatedAt: now,
	}
	page := mcpServerPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, Search: "docs", FetchLimit: 21,
		Cursor: &WorkspaceMCPServerPageCursor{CreatedAt: now, UUID: "00000000-0000-4000-8000-000000000009"},
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: mCPServerMapperInsertStatement, bound: buildMCPServerMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "MCPServerMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.Name", "params.TransportType", "params.EndpointURL", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.EndpointURL"},
			wantSQLFragments:           []string{"INSERT INTO mcp_servers", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement: mCPServerMapperFindByExternalIDStatement,
			bound:     buildMCPServerMapperFindByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "MCPServerMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"FROM mcp_servers", "external_id = $2"},
		}},
		{"update", mapperBuilderContract{
			statement: mCPServerMapperUpdateByExternalIDStatement,
			bound:     buildMCPServerMapperUpdateByExternalID(yourbatis.DialectPostgres, params),
			wantID:    "MCPServerMapper.UpdateByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.Name", "params.TransportType", "params.EndpointURL", "params.UpdatedAt", "params.WorkspaceUUID", "params.ExternalID"},
			wantSensitiveArgumentNames: []string{"params.EndpointURL"}, wantSQLFragments: []string{"UPDATE mcp_servers", "RETURNING"},
		}},
		{"archive", mapperBuilderContract{
			statement: mCPServerMapperArchiveByExternalIDStatement,
			bound:     buildMCPServerMapperArchiveByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "MCPServerMapper.ArchiveByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"archived_at = COALESCE", "RETURNING"},
		}},
		{"soft delete", mapperBuilderContract{
			statement: mCPServerMapperSoftDeleteByExternalIDStatement,
			bound:     buildMCPServerMapperSoftDeleteByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "MCPServerMapper.SoftDeleteByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"deleted_at = COALESCE", "RETURNING"},
		}},
		{"list page", mapperBuilderContract{
			statement: mCPServerMapperListPageStatement, bound: buildMCPServerMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "MCPServerMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.WorkspaceUUID", "params.Search", "params.Search", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.FetchLimit",
			},
			wantSensitiveArgumentNames: []string{"params.Search", "params.Search"},
			wantSQLFragments:           []string{"archived_at IS NULL", "POSITION", "(created_at, uuid) < ($4, $5)", "LIMIT $6"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("list page omits optional filters", func(t *testing.T) {
		bound := buildMCPServerMapperListPage(yourbatis.DialectPostgres, mcpServerPageMapperParams{
			WorkspaceUUID:   params.WorkspaceUUID,
			FetchLimit:      21,
			IncludeArchived: true,
		})
		assertMapperBuilderContract(t, mapperBuilderContract{
			statement: mCPServerMapperListPageStatement,
			bound:     bound,
			wantID:    "MCPServerMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.FetchLimit"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "ORDER BY created_at DESC, uuid DESC", "LIMIT $2"},
		})
		for _, unexpected := range []string{"archived_at IS NULL", "POSITION", "(created_at, uuid) <"} {
			if containsMapperSQL(bound.SQL, unexpected) {
				t.Fatalf("ListPage SQL = %q, does not want optional fragment %q", bound.SQL, unexpected)
			}
		}
	})
}

func TestMCPServerMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	rowValues := mcpServerMapperTestRow(nil, nil)
	rowResponse := func() mapperTestResponse {
		return mapperTestResponse{columns: mcpServerMapperTestColumns(), rows: [][]driver.Value{rowValues}}
	}

	params := mcpServerWriteParams{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "mcpsrv_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001", WorkspaceUUID: "00000000-0000-4000-8000-000000000002",
		Name: "internal-docs", TransportType: "url", EndpointURL: "https://example.test/mcp",
		CreatedAt: time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC),
	}
	tests := []struct {
		name string
		call func(MCPServerMapper) (mcpServerMapperRow, error)
	}{
		{name: "insert returns row", call: func(mapper MCPServerMapper) (mcpServerMapperRow, error) {
			return mapper.Insert(ctx, params)
		}},
		{name: "find returns scoped row", call: func(mapper MCPServerMapper) (mcpServerMapperRow, error) {
			return mapper.FindByExternalID(ctx, params.WorkspaceUUID, params.ExternalID)
		}},
		{name: "update returns row", call: func(mapper MCPServerMapper) (mcpServerMapperRow, error) {
			return mapper.UpdateByExternalID(ctx, params)
		}},
		{name: "archive returns nullable row", call: func(mapper MCPServerMapper) (mcpServerMapperRow, error) {
			return mapper.ArchiveByExternalID(ctx, params.WorkspaceUUID, params.ExternalID)
		}},
		{name: "soft delete returns nullable row", call: func(mapper MCPServerMapper) (mcpServerMapperRow, error) {
			return mapper.SoftDeleteByExternalID(ctx, params.WorkspaceUUID, params.ExternalID)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := newMapperTestExecutor(t, rowResponse())
			row, err := test.call(NewMCPServerMapper(executor))
			if err != nil || row.Name != "internal-docs" || row.EndpointURL != "https://example.test/mcp" {
				t.Fatalf("mapper call = (%+v, %v)", row, err)
			}
		})
	}

	t.Run("list returns multiple rows and nullable timestamps", func(t *testing.T) {
		archivedAt := params.CreatedAt.Add(time.Minute)
		deletedAt := archivedAt.Add(time.Minute)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: mcpServerMapperTestColumns(),
			rows: [][]driver.Value{
				mcpServerMapperTestRow(nil, nil),
				mcpServerMapperTestRow(archivedAt, deletedAt),
			},
		})
		rows, err := NewMCPServerMapper(executor).ListPage(ctx, mcpServerPageMapperParams{
			WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 20, IncludeArchived: true,
		})
		if err != nil || len(rows) != 2 || rows[0].ArchivedAt != nil || rows[1].ArchivedAt == nil || rows[1].DeletedAt == nil {
			t.Fatalf("ListPage() = (%+v, %v)", rows, err)
		}
	})

	zeroRowCalls := []struct {
		name string
		call func(MCPServerMapper) error
	}{
		{name: "find", call: func(mapper MCPServerMapper) error {
			_, err := mapper.FindByExternalID(ctx, params.WorkspaceUUID, "mcpsrv_missing")
			return err
		}},
		{name: "update", call: func(mapper MCPServerMapper) error {
			_, err := mapper.UpdateByExternalID(ctx, params)
			return err
		}},
		{name: "archive", call: func(mapper MCPServerMapper) error {
			_, err := mapper.ArchiveByExternalID(ctx, params.WorkspaceUUID, "mcpsrv_missing")
			return err
		}},
		{name: "soft delete", call: func(mapper MCPServerMapper) error {
			_, err := mapper.SoftDeleteByExternalID(ctx, params.WorkspaceUUID, "mcpsrv_missing")
			return err
		}},
	}
	for _, test := range zeroRowCalls {
		t.Run(test.name+" zero rows preserves sql ErrNoRows", func(t *testing.T) {
			executor := newMapperTestExecutor(t, mapperTestResponse{columns: mcpServerMapperTestColumns()})
			if err := test.call(NewMCPServerMapper(executor)); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("mapper error = %v, want sql.ErrNoRows", err)
			}
		})
	}

	executionCalls := []mapperExecutionErrorContract{
		{statementID: "MCPServerMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMCPServerMapper(executor).Insert(ctx, params)
			return err
		}},
		{statementID: "MCPServerMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMCPServerMapper(executor).FindByExternalID(ctx, params.WorkspaceUUID, params.ExternalID)
			return err
		}},
		{statementID: "MCPServerMapper.UpdateByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMCPServerMapper(executor).UpdateByExternalID(ctx, params)
			return err
		}},
		{statementID: "MCPServerMapper.ArchiveByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMCPServerMapper(executor).ArchiveByExternalID(ctx, params.WorkspaceUUID, params.ExternalID)
			return err
		}},
		{statementID: "MCPServerMapper.SoftDeleteByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMCPServerMapper(executor).SoftDeleteByExternalID(ctx, params.WorkspaceUUID, params.ExternalID)
			return err
		}},
		{statementID: "MCPServerMapper.ListPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMCPServerMapper(executor).ListPage(ctx, mcpServerPageMapperParams{
				WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 20,
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

func mcpServerMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "name", "transport_type",
		"endpoint_url", "created_at", "updated_at", "archived_at", "deleted_at",
	}
}

func mcpServerMapperTestRow(archivedAt, deletedAt any) []driver.Value {
	now := time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000003", "mcpsrv_test",
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"internal-docs", "url", "https://example.test/mcp", now, now, archivedAt, deletedAt,
	}
}
