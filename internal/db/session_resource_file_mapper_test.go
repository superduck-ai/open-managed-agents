package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionResourceFileMapperBuilderContracts(t *testing.T) {
	pathParams := sessionResourcePathParams{
		WorkspaceUUID: "workspace-uuid",
		SessionUUID:   "session-uuid",
		EntryPath:     "/outputs/report.csv",
	}
	identityParams := sessionResourceIdentityParams{
		WorkspaceUUID: "workspace-uuid",
		SessionUUID:   "session-uuid",
		ResourceUUID:  "resource-uuid",
	}
	moveParams := sessionResourceMoveParams{
		WorkspaceUUID:   "workspace-uuid",
		SessionUUID:     "session-uuid",
		ResourceUUID:    "resource-uuid",
		DestinationPath: "/outputs/moved",
	}
	pageParams := sessionResourceFilePageMapperParams{
		WorkspaceUUID:   "workspace-uuid",
		SessionUUID:     "session-uuid",
		DirectoryPath:   "/outputs",
		DirectoryPrefix: "/outputs/",
		CursorUUID:      "cursor-uuid",
		CursorPath:      "/outputs/after.csv",
		FetchLimit:      21,
		Recursive:       true,
		HasCursor:       true,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "find resource file",
			contract: mapperBuilderContract{
				statement:         sessionResourceFileMapperFindResourceFileStatement,
				bound:             buildSessionResourceFileMapperFindResourceFile(yourbatis.DialectPostgres, pathParams),
				wantID:            "SessionResourceFileMapper.FindResourceFile",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.EntryPath"},
				wantSQLFragments:  []string{"FROM session_resources resource", "workspace_uuid = $1", "path = $3"},
			},
		},
		{
			name: "find active resource file",
			contract: mapperBuilderContract{
				statement:         sessionResourceFileMapperFindActiveResourceFileStatement,
				bound:             buildSessionResourceFileMapperFindActiveResourceFile(yourbatis.DialectPostgres, pathParams),
				wantID:            "SessionResourceFileMapper.FindActiveResourceFile",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.EntryPath"},
				wantSQLFragments:  []string{"path = $3", "expires_at IS NULL OR expires_at > now()"},
			},
		},
		{
			name: "get resource file by uuid",
			contract: mapperBuilderContract{
				statement:         sessionResourceFileMapperGetResourceFileByUUIDStatement,
				bound:             buildSessionResourceFileMapperGetResourceFileByUUID(yourbatis.DialectPostgres, identityParams),
				wantID:            "SessionResourceFileMapper.GetResourceFileByUUID",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.ResourceUUID", "params.WorkspaceUUID", "params.SessionUUID"},
				wantSQLFragments:  []string{"uuid = $1", "workspace_uuid = $2", "session_uuid = $3"},
			},
		},
		{
			name: "get move result",
			contract: mapperBuilderContract{
				statement:         sessionResourceFileMapperGetResourceFileForMoveResultStatement,
				bound:             buildSessionResourceFileMapperGetResourceFileForMoveResult(yourbatis.DialectPostgres, identityParams),
				wantID:            "SessionResourceFileMapper.GetResourceFileForMoveResult",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.ResourceUUID"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "session_uuid = $2", "uuid = $3"},
			},
		},
		{
			name: "get moved directory",
			contract: mapperBuilderContract{
				statement:         sessionResourceFileMapperGetMovedDirectoryStatement,
				bound:             buildSessionResourceFileMapperGetMovedDirectory(yourbatis.DialectPostgres, moveParams),
				wantID:            "SessionResourceFileMapper.GetMovedDirectory",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.DestinationPath"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "session_uuid = $2", "path = $3"},
			},
		},
		{
			name: "list resource files page",
			contract: mapperBuilderContract{
				statement: sessionResourceFileMapperListResourceFilesPageStatement,
				bound:     buildSessionResourceFileMapperListResourceFilesPage(yourbatis.DialectPostgres, pageParams),
				wantID:    "SessionResourceFileMapper.ListResourceFilesPage",
				wantKind:  yourbatis.StatementSelect,
				wantArgumentNames: []string{
					"params.WorkspaceUUID",
					"params.SessionUUID",
					"params.DirectoryPrefix",
					"params.DirectoryPrefix",
					"params.CursorPath",
					"params.CursorUUID",
					"params.FetchLimit",
				},
				wantSQLFragments: []string{"left(path, char_length($3)) = $4", "(path, uuid) > ($5, $6)", "ORDER BY path ASC, uuid ASC", "LIMIT $7"},
			},
		},
		{
			name: "list skill archive resources",
			contract: mapperBuilderContract{
				statement:         sessionResourceFileMapperListSkillArchiveResourcesStatement,
				bound:             buildSessionResourceFileMapperListSkillArchiveResources(yourbatis.DialectPostgres, "workspace-uuid", "filesystem-uuid"),
				wantID:            "SessionResourceFileMapper.ListSkillArchiveResources",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "filesystemUUID", "workspaceUUID"},
				wantSQLFragments:  []string{"FROM filestore_filesystems", "kind = 'archive'", "ORDER BY path, id"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestSessionResourceFileMapperPropagatesQueryErrors(t *testing.T) {
	pathParams := sessionResourcePathParams{}
	identityParams := sessionResourceIdentityParams{}
	moveParams := sessionResourceMoveParams{}
	pageParams := sessionResourceFilePageMapperParams{}
	tests := []struct {
		name        string
		statementID string
		call        func(SessionResourceFileMapper) error
	}{
		{name: "find resource file", statementID: "SessionResourceFileMapper.FindResourceFile", call: func(mapper SessionResourceFileMapper) error {
			_, _, err := mapper.FindResourceFile(context.Background(), pathParams)
			return err
		}},
		{name: "find active resource file", statementID: "SessionResourceFileMapper.FindActiveResourceFile", call: func(mapper SessionResourceFileMapper) error {
			_, _, err := mapper.FindActiveResourceFile(context.Background(), pathParams)
			return err
		}},
		{name: "get resource file by uuid", statementID: "SessionResourceFileMapper.GetResourceFileByUUID", call: func(mapper SessionResourceFileMapper) error {
			_, err := mapper.GetResourceFileByUUID(context.Background(), identityParams)
			return err
		}},
		{name: "get move result", statementID: "SessionResourceFileMapper.GetResourceFileForMoveResult", call: func(mapper SessionResourceFileMapper) error {
			_, err := mapper.GetResourceFileForMoveResult(context.Background(), identityParams)
			return err
		}},
		{name: "get moved directory", statementID: "SessionResourceFileMapper.GetMovedDirectory", call: func(mapper SessionResourceFileMapper) error {
			_, err := mapper.GetMovedDirectory(context.Background(), moveParams)
			return err
		}},
		{name: "list resource files", statementID: "SessionResourceFileMapper.ListResourceFilesPage", call: func(mapper SessionResourceFileMapper) error {
			_, err := mapper.ListResourceFilesPage(context.Background(), pageParams)
			return err
		}},
		{name: "list skill archives", statementID: "SessionResourceFileMapper.ListSkillArchiveResources", call: func(mapper SessionResourceFileMapper) error {
			_, err := mapper.ListSkillArchiveResources(context.Background(), "", "")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, mapperExecutionErrorContract{
				statementID: test.statementID,
				kind:        yourbatis.StatementSelect,
				query:       true,
				call: func(executor yourbatis.Executor) error {
					return test.call(NewSessionResourceFileMapper(executor))
				},
			})
		})
	}
}

func TestSessionResourceFileMapperResultSemantics(t *testing.T) {
	t.Run("required row scan error", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"id"},
			rows:    [][]driver.Value{{"invalid"}},
		})
		_, err := NewSessionResourceFileMapper(executor).GetResourceFileByUUID(context.Background(), sessionResourceIdentityParams{})
		if err == nil {
			t.Fatal("GetResourceFileByUUID error = nil, want scan error")
		}
	})

	t.Run("required row missing", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"id"}})
		_, err := NewSessionResourceFileMapper(executor).GetResourceFileByUUID(context.Background(), sessionResourceIdentityParams{})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetResourceFileByUUID error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("optional row missing", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"id"}})
		_, found, err := NewSessionResourceFileMapper(executor).FindResourceFile(context.Background(), sessionResourcePathParams{})
		if err != nil || found {
			t.Fatalf("FindResourceFile = (found %t, error %v), want (false, nil)", found, err)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"id"}})
		rows, err := NewSessionResourceFileMapper(executor).ListResourceFilesPage(context.Background(), sessionResourceFilePageMapperParams{})
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListResourceFilesPage = (%#v, %v), want empty list and nil error", rows, err)
		}
	})
}
