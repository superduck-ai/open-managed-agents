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

func TestAgentMapperStatements(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	description := "description"
	system := "system"
	config := agentConfigParams{
		Name:        "Agent",
		Description: &description,
		System:      &system,
		Model:       []byte(`{"id":"claude-opus-4-6"}`),
		MCPServers:  []byte(`[]`),
		Metadata:    []byte(`{}`),
		Multiagent:  []byte(`{}`),
		Skills:      []byte(`[]`),
		Tools:       []byte(`[]`),
	}
	insertParams := insertAgentParams{
		UUID:                "00000000-0000-4000-8000-000000000001",
		ExternalID:          "agent_mapper",
		WorkspaceUUID:       "00000000-0000-4000-8000-000000000002",
		CreatedByAPIKeyUUID: nullableString("00000000-0000-4000-8000-000000000003"),
		Config:              config,
		CreatedAt:           now,
	}
	versionParams := insertAgentVersionParams{
		ExternalID:      "agentver_mapper",
		WorkspaceUUID:   insertParams.WorkspaceUUID,
		AgentUUID:       insertParams.UUID,
		AgentExternalID: insertParams.ExternalID,
		Version:         2,
		Config:          config,
		AgentCreatedAt:  now,
		AgentUpdatedAt:  now.Add(time.Minute),
	}
	updateParams := updateAgentParams{
		WorkspaceUUID:  insertParams.WorkspaceUUID,
		ExternalID:     insertParams.ExternalID,
		CurrentVersion: 2,
		Config:         config,
		UpdatedAt:      now.Add(time.Minute),
	}

	tests := []struct {
		name      string
		statement yourbatis.Statement
		bound     yourbatis.BoundSQL
		id        string
		kind      yourbatis.StatementKind
		values    []any
		fragments []string
	}{
		{
			name:      "insert",
			statement: agentMapperInsertStatement,
			bound:     buildAgentMapperInsert(yourbatis.DialectPostgres, insertParams),
			id:        "AgentMapper.Insert",
			kind:      yourbatis.StatementInsert,
			values: []any{
				insertParams.UUID, insertParams.ExternalID, insertParams.WorkspaceUUID,
				insertParams.CreatedByAPIKeyUUID, config.Name, config.Description, config.System,
				config.Model, config.MCPServers, config.Metadata, config.Multiagent, config.Skills,
				config.Tools, now, now,
			},
			fragments: []string{"INSERT INTO agents", "CAST($8 AS jsonb)", "RETURNING"},
		},
		{
			name:      "insert version",
			statement: agentMapperInsertVersionStatement,
			bound:     buildAgentMapperInsertVersion(yourbatis.DialectPostgres, versionParams),
			id:        "AgentMapper.InsertVersion",
			kind:      yourbatis.StatementInsert,
			values: []any{
				versionParams.ExternalID, versionParams.WorkspaceUUID, versionParams.AgentUUID,
				versionParams.AgentExternalID, versionParams.Version, config.Name, config.Description,
				config.System, config.Model, config.MCPServers, config.Metadata, config.Multiagent,
				config.Skills, config.Tools, versionParams.AgentCreatedAt,
				versionParams.AgentUpdatedAt, versionParams.ArchivedAt,
			},
			fragments: []string{"INSERT INTO agent_versions", "CAST($9 AS jsonb)"},
		},
		{
			name:      "find",
			statement: agentMapperFindByExternalIDStatement,
			bound: buildAgentMapperFindByExternalID(
				yourbatis.DialectPostgres,
				insertParams.WorkspaceUUID,
				insertParams.ExternalID,
			),
			id:        "AgentMapper.FindByExternalID",
			kind:      yourbatis.StatementSelect,
			values:    []any{insertParams.WorkspaceUUID, insertParams.ExternalID},
			fragments: []string{"workspace_uuid = $1", "external_id = $2", "deleted_at IS NULL"},
		},
		{
			name:      "find version",
			statement: agentMapperFindVersionStatement,
			bound: buildAgentMapperFindVersion(
				yourbatis.DialectPostgres,
				insertParams.WorkspaceUUID,
				insertParams.ExternalID,
				2,
			),
			id:        "AgentMapper.FindVersion",
			kind:      yourbatis.StatementSelect,
			values:    []any{insertParams.WorkspaceUUID, insertParams.ExternalID, 2},
			fragments: []string{"FROM agent_versions", "workspace_uuid = $1", "version = $3"},
		},
		{
			name:      "lock",
			statement: agentMapperLockByExternalIDStatement,
			bound: buildAgentMapperLockByExternalID(
				yourbatis.DialectPostgres,
				insertParams.WorkspaceUUID,
				insertParams.ExternalID,
			),
			id:        "AgentMapper.LockByExternalID",
			kind:      yourbatis.StatementSelect,
			values:    []any{insertParams.WorkspaceUUID, insertParams.ExternalID},
			fragments: []string{"workspace_uuid = $1", "FOR UPDATE"},
		},
		{
			name:      "update",
			statement: agentMapperUpdateByExternalIDStatement,
			bound:     buildAgentMapperUpdateByExternalID(yourbatis.DialectPostgres, updateParams),
			id:        "AgentMapper.UpdateByExternalID",
			kind:      yourbatis.StatementUpdate,
			values: []any{
				updateParams.CurrentVersion, config.Name, config.Description, config.System,
				config.Model, config.MCPServers, config.Metadata, config.Multiagent, config.Skills,
				config.Tools, updateParams.UpdatedAt, updateParams.WorkspaceUUID, updateParams.ExternalID,
			},
			fragments: []string{"UPDATE agents", "model = CAST($5 AS jsonb)", "workspace_uuid = $12", "RETURNING"},
		},
		{
			name:      "archive",
			statement: agentMapperArchiveByExternalIDStatement,
			bound: buildAgentMapperArchiveByExternalID(
				yourbatis.DialectPostgres,
				insertParams.WorkspaceUUID,
				insertParams.ExternalID,
			),
			id:        "AgentMapper.ArchiveByExternalID",
			kind:      yourbatis.StatementUpdate,
			values:    []any{insertParams.WorkspaceUUID, insertParams.ExternalID},
			fragments: []string{"UPDATE agents", "archived_at = COALESCE", "workspace_uuid = $1", "RETURNING"},
		},
		{
			name:      "list page",
			statement: agentMapperListPageStatement,
			bound: buildAgentMapperListPage(yourbatis.DialectPostgres, agentPageFilter{
				WorkspaceUUID: insertParams.WorkspaceUUID,
				Limit:         11,
			}),
			id:        "AgentMapper.ListPage",
			kind:      yourbatis.StatementSelect,
			values:    []any{insertParams.WorkspaceUUID, 11},
			fragments: []string{"workspace_uuid = $1", "archived_at IS NULL", "ORDER BY created_at DESC, uuid DESC", "LIMIT $2"},
		},
		{
			name:      "find UUID",
			statement: agentMapperFindUUIDByExternalIDStatement,
			bound: buildAgentMapperFindUUIDByExternalID(
				yourbatis.DialectPostgres,
				insertParams.WorkspaceUUID,
				insertParams.ExternalID,
			),
			id:        "AgentMapper.FindUUIDByExternalID",
			kind:      yourbatis.StatementSelect,
			values:    []any{insertParams.WorkspaceUUID, insertParams.ExternalID},
			fragments: []string{"SELECT uuid", "workspace_uuid = $1", "external_id = $2"},
		},
		{
			name:      "list versions",
			statement: agentMapperListVersionsPageStatement,
			bound: buildAgentMapperListVersionsPage(
				yourbatis.DialectPostgres,
				insertParams.UUID,
				nil,
				11,
			),
			id:        "AgentMapper.ListVersionsPage",
			kind:      yourbatis.StatementSelect,
			values:    []any{insertParams.UUID, 11},
			fragments: []string{"agent_uuid = $1", "ORDER BY version DESC, uuid DESC", "LIMIT $2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.statement.ID != test.id || test.statement.Kind != test.kind || test.statement.Source == "" {
				t.Fatalf("statement = %+v, want ID %q, kind %q, and source", test.statement, test.id, test.kind)
			}
			if values := test.bound.Values(); !reflect.DeepEqual(values, test.values) {
				t.Fatalf("values = %#v, want %#v", values, test.values)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(test.bound.SQL, fragment) {
					t.Fatalf("SQL = %q, want fragment %q", test.bound.SQL, fragment)
				}
			}
			for _, argument := range test.bound.Args {
				wantSensitive := strings.Contains(argument.Name, ".Config.") && !strings.HasSuffix(argument.Name, ".Name")
				if argument.Sensitive != wantSensitive {
					t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, wantSensitive)
				}
			}
			if strings.Contains(test.bound.SQL, "CAST($1 AS uuid)") {
				t.Fatalf("SQL contains UUID parameter cast ceremony: %q", test.bound.SQL)
			}
		})
	}
}

func TestAgentMapperResultSemantics(t *testing.T) {
	workspaceUUID := "00000000-0000-4000-8000-000000000002"
	externalID := "agent_mapper"

	t.Run("single row zero result", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: agentMapperTestColumns()})
		_, err := NewAgentMapper(executor).FindByExternalID(context.Background(), workspaceUUID, externalID)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("single row scan", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: agentMapperTestColumns(),
			rows:    [][]driver.Value{agentMapperTestRow(externalID)},
		})
		row, err := NewAgentMapper(executor).Insert(context.Background(), insertAgentParams{})
		if err != nil || row.ExternalID != externalID || row.WorkspaceUUID != workspaceUUID {
			t.Fatalf("Insert() = (%+v, %v)", row, err)
		}
	})

	t.Run("single row scan error", func(t *testing.T) {
		values := agentMapperTestRow(externalID)
		values[14] = "not-a-timestamp"
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: agentMapperTestColumns(),
			rows:    [][]driver.Value{values},
		})
		_, err := NewAgentMapper(executor).FindByExternalID(context.Background(), workspaceUUID, externalID)
		if err == nil {
			t.Fatal("FindByExternalID() error = nil, want scan error")
		}
	})

	t.Run("many rows empty and populated", func(t *testing.T) {
		emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: agentMapperTestColumns()})
		rows, err := NewAgentMapper(emptyExecutor).ListPage(context.Background(), agentPageFilter{})
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListPage() = (%+v, %v), want empty result", rows, err)
		}

		populatedExecutor := newMapperTestExecutor(t, mapperTestResponse{
			columns: agentMapperTestColumns(),
			rows:    [][]driver.Value{agentMapperTestRow(externalID), agentMapperTestRow("agent_second")},
		})
		rows, err = NewAgentMapper(populatedExecutor).ListVersionsPage(context.Background(), workspaceUUID, nil, 2)
		if err != nil || len(rows) != 2 || rows[1].ExternalID != "agent_second" {
			t.Fatalf("ListVersionsPage() = (%+v, %v)", rows, err)
		}
	})

	t.Run("scalar zero and populated", func(t *testing.T) {
		emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
		_, err := NewAgentMapper(emptyExecutor).FindUUIDByExternalID(context.Background(), workspaceUUID, externalID)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindUUIDByExternalID() error = %v, want sql.ErrNoRows", err)
		}

		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"uuid"},
			rows:    [][]driver.Value{{workspaceUUID}},
		})
		value, err := NewAgentMapper(executor).FindUUIDByExternalID(context.Background(), workspaceUUID, externalID)
		if err != nil || value != workspaceUUID {
			t.Fatalf("FindUUIDByExternalID() = (%q, %v)", value, err)
		}
	})

	t.Run("execution error", func(t *testing.T) {
		wantErr := errors.New("insert version failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{execErr: wantErr})
		err := NewAgentMapper(executor).InsertVersion(context.Background(), insertAgentVersionParams{})
		if !errors.Is(err, wantErr) {
			t.Fatalf("InsertVersion() error = %v, want %v", err, wantErr)
		}
	})
}

func TestAgentMapperListVersionsCursor(t *testing.T) {
	cursor := &AgentVersionPageCursor{
		Version: 3,
		UUID:    "00000000-0000-4000-8000-000000000003",
	}
	bound := buildAgentMapperListVersionsPage(
		yourbatis.DialectPostgres,
		"00000000-0000-4000-8000-000000000001",
		cursor,
		6,
	)
	wantValues := []any{
		"00000000-0000-4000-8000-000000000001",
		cursor.Version,
		cursor.UUID,
		6,
	}
	if values := bound.Values(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("values = %#v, want %#v", values, wantValues)
	}
	if !strings.Contains(bound.SQL, "(version, uuid) < ($2, $3)") {
		t.Fatalf("SQL = %q, want cursor predicate", bound.SQL)
	}
}

func agentMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "workspace_uuid", "created_by_api_key_uuid", "current_version",
		"name", "description", "system", "model", "mcp_servers", "metadata", "multiagent",
		"skills", "tools", "created_at", "updated_at", "archived_at", "deleted_at",
	}
}

func agentMapperTestRow(externalID string) []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000001",
		externalID,
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
		int64(1),
		"Agent",
		nil,
		nil,
		[]byte(`{"id":"claude-opus-4-6"}`),
		[]byte(`[]`),
		[]byte(`{}`),
		[]byte(`{}`),
		[]byte(`[]`),
		[]byte(`[]`),
		now,
		now,
		nil,
		nil,
	}
}
