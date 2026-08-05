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

func TestSessionResourceMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	pathParams := sessionResourcePathParams{WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", EntryPath: "/outputs/report.csv"}
	bindingParams := sessionFileResourceBindingParams{
		EntryPath:     "/outputs/report.csv",
		ParentPath:    "/outputs",
		FileUUID:      "file-uuid",
		UpdatedAt:     now,
		ResourceUUID:  "resource-uuid",
		WorkspaceUUID: "workspace-uuid",
		SessionUUID:   "session-uuid",
	}
	directoryParams := sessionResourceDirectoryInsertParams{
		ResourceUUID:       "resource-uuid",
		ResourceExternalID: "resource-external-id",
		OrganizationUUID:   "organization-uuid",
		WorkspaceUUID:      "workspace-uuid",
		SessionUUID:        "session-uuid",
		EntryPath:          "/outputs/reports",
		ParentPath:         "/outputs",
		Now:                now,
	}
	writeParams := sessionResourceFileWriteParams{
		FileUUID:              "file-uuid",
		FileExternalID:        "file-external-id",
		ResourceUUID:          "resource-uuid",
		ResourceExternalID:    "resource-external-id",
		OrganizationUUID:      "organization-uuid",
		WorkspaceUUID:         "workspace-uuid",
		SessionUUID:           "session-uuid",
		EntryPath:             "/outputs/report.csv",
		ParentPath:            "/outputs",
		Filename:              "report.csv",
		SizeBytes:             42,
		MediaType:             "text/csv",
		DetectedMimeType:      "text/csv",
		Metadata:              "{}",
		AuthorizationMetadata: "{}",
		Tags:                  []string{"report"},
		Downloadable:          true,
		MD5:                   "md5",
		SHA256:                "sha256",
		S3Bucket:              "files",
		S3Key:                 "report.csv",
		S3ETag:                "etag",
		S3VersionID:           "version",
		Now:                   now,
	}
	retireParams := sessionResourceRetireParams{ResourceUUID: "resource-uuid", WorkspaceUUID: "workspace-uuid", RetiredAt: now}
	moveParams := sessionResourceMoveParams{
		WorkspaceUUID:         "workspace-uuid",
		SessionUUID:           "session-uuid",
		ResourceUUID:          "resource-uuid",
		SourcePath:            "/outputs/source",
		DestinationPath:       "/outputs/destination",
		DestinationParentPath: "/outputs",
		Now:                   now,
	}
	subtreeParams := sessionResourceSubtreeParams{WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", EntryPath: "/outputs/root", Now: now}
	skillRetireParams := sessionSkillArchiveRetireParams{WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", Now: now}
	skillInsertParams := sessionSkillArchiveInsertParams{
		FileUUID:           "file-uuid",
		ResourceUUID:       "resource-uuid",
		ResourceExternalID: "resource-external-id",
		WorkspaceUUID:      "workspace-uuid",
		SessionUUID:        "session-uuid",
		EntryPath:          "/skills/example",
		Now:                now,
	}

	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "count session file resources",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperCountSessionFileResourcesStatement,
				bound:             buildSessionResourceMapperCountSessionFileResources(yourbatis.DialectPostgres, "workspace-uuid", "session-external-id", "file"),
				wantID:            "SessionResourceMapper.CountSessionFileResources",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "sessionExternalID", "resourceType"},
				wantSQLFragments:  []string{"SELECT count(*) AS resource_count", "payload IS NOT NULL", "deleted_at IS NULL"},
			},
		},
		{
			name: "find mount conflict",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperFindMountConflictStatement,
				bound:             buildSessionResourceMapperFindMountConflict(yourbatis.DialectPostgres, pathParams),
				wantID:            "SessionResourceMapper.FindMountConflict",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.EntryPath", "params.WorkspaceUUID", "params.SessionUUID"},
				wantSQLFragments:  []string{"CROSS JOIN (VALUES (CAST($1 AS text)))", "payload IS NOT NULL", "LIMIT 1"},
			},
		},
		{
			name: "bind session file resource",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperBindSessionFileResourceStatement,
				bound:             buildSessionResourceMapperBindSessionFileResource(yourbatis.DialectPostgres, bindingParams),
				wantID:            "SessionResourceMapper.BindSessionFileResource",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.EntryPath", "params.ParentPath", "params.FileUUID", "params.UpdatedAt", "params.ResourceUUID", "params.WorkspaceUUID", "params.SessionUUID"},
				wantSQLFragments:  []string{"UPDATE session_resources", "file_uuid = $3", "RETURNING", "uuid, external_id"},
			},
		},
		{
			name: "get resource for mutation",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperGetSessionResourceForMutationStatement,
				bound:             buildSessionResourceMapperGetSessionResourceForMutation(yourbatis.DialectPostgres, "workspace-uuid", "session-external-id", "resource-external-id"),
				wantID:            "SessionResourceMapper.GetSessionResourceForMutation",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "sessionExternalID", "resourceExternalID"},
				wantSQLFragments:  []string{"payload IS NOT NULL", "deleted_at IS NULL", "FOR UPDATE"},
			},
		},
		{
			name: "soft delete session resource",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperSoftDeleteSessionResourceStatement,
				bound:             buildSessionResourceMapperSoftDeleteSessionResource(yourbatis.DialectPostgres, "workspace-uuid", "session-external-id", "resource-external-id"),
				wantID:            "SessionResourceMapper.SoftDeleteSessionResource",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"workspaceUUID", "sessionExternalID", "resourceExternalID"},
				wantSQLFragments:  []string{"SET deleted_at = now(), updated_at = now()", "deleted_at IS NULL"},
			},
		},
		{
			name: "insert directory",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperInsertDirectoryStatement,
				bound:             buildSessionResourceMapperInsertDirectory(yourbatis.DialectPostgres, directoryParams),
				wantID:            "SessionResourceMapper.InsertDirectory",
				wantKind:          yourbatis.StatementInsert,
				wantArgumentNames: []string{"params.ResourceUUID", "params.ResourceExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.EntryPath", "params.ParentPath", "params.Now", "params.Now", "params.SessionUUID", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"INSERT INTO session_resources", "FROM sessions session", "RETURNING id"},
			},
		},
		{
			name: "update resource file",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperUpdateResourceFileStatement,
				bound:             buildSessionResourceMapperUpdateResourceFile(yourbatis.DialectPostgres, writeParams),
				wantID:            "SessionResourceMapper.UpdateResourceFile",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.EntryPath", "params.ParentPath", "params.ExpiresAt", "params.Now", "params.ResourceUUID", "params.WorkspaceUUID", "params.SessionUUID"},
				wantSQLFragments:  []string{"resource_type = 'file'", "payload = NULL", "uuid = $5"},
			},
		},
		{
			name: "insert owned file and resource",
			contract: mapperBuilderContract{
				statement: sessionResourceMapperInsertOwnedFileAndResourceStatement,
				bound:     buildSessionResourceMapperInsertOwnedFileAndResource(yourbatis.DialectPostgres, writeParams),
				wantID:    "SessionResourceMapper.InsertOwnedFileAndResource",
				wantKind:  yourbatis.StatementInsert,
				wantArgumentNames: []string{
					"params.FileUUID", "params.FileExternalID", "params.WorkspaceUUID", "params.Filename", "params.MediaType",
					"params.DetectedMimeType", "params.SizeBytes", "params.Metadata", "params.AuthorizationMetadata", "params.Tags",
					"params.Downloadable", "params.MD5", "params.SHA256", "params.S3Bucket", "params.S3Key", "params.S3ETag",
					"params.S3VersionID", "params.Now", "params.CreatedByAPIKeyUUID", "params.SessionUUID", "params.WorkspaceUUID",
					"params.ResourceUUID", "params.ResourceExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.EntryPath",
					"params.ParentPath", "params.ExpiresAt", "params.Now", "params.Now", "params.SessionUUID",
				},
				wantSQLFragments: []string{"WITH inserted_file AS", "INSERT INTO files", "INSERT INTO session_resources", "RETURNING id"},
			},
		},
		{
			name: "retire resource",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperRetireResourceStatement,
				bound:             buildSessionResourceMapperRetireResource(yourbatis.DialectPostgres, retireParams),
				wantID:            "SessionResourceMapper.RetireResource",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.RetiredAt", "params.RetiredAt", "params.ResourceUUID", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"deleted_at = COALESCE(deleted_at, $1)", "updated_at = $2", "uuid = $3"},
			},
		},
		{
			name: "move resource file",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperMoveResourceFileStatement,
				bound:             buildSessionResourceMapperMoveResourceFile(yourbatis.DialectPostgres, moveParams),
				wantID:            "SessionResourceMapper.MoveResourceFile",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.DestinationPath", "params.DestinationParentPath", "params.Now", "params.WorkspaceUUID", "params.SessionUUID", "params.ResourceUUID"},
				wantSQLFragments:  []string{"SET path = $1, parent_path = $2", "session_uuid = $5", "uuid = $6"},
			},
		},
		{
			name: "max moved path bytes",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperMaxMovedPathBytesStatement,
				bound:             buildSessionResourceMapperMaxMovedPathBytes(yourbatis.DialectPostgres, moveParams),
				wantID:            "SessionResourceMapper.MaxMovedPathBytes",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.DestinationPath", "params.SourcePath", "params.WorkspaceUUID", "params.SessionUUID", "params.SourcePath", "params.SourcePath", "params.SourcePath"},
				wantSQLFragments:  []string{"AS max_path_bytes", "workspace_uuid = $3", "path = $5"},
			},
		},
		{
			name: "subtree contains input",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperSubtreeContainsInputStatement,
				bound:             buildSessionResourceMapperSubtreeContainsInput(yourbatis.DialectPostgres, moveParams),
				wantID:            "SessionResourceMapper.SubtreeContainsInput",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.SourcePath", "params.SourcePath", "params.SourcePath"},
				wantSQLFragments:  []string{"SELECT EXISTS", "payload IS NOT NULL", "AS contains_input"},
			},
		},
		{
			name: "find move conflict",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperFindMoveConflictStatement,
				bound:             buildSessionResourceMapperFindMoveConflict(yourbatis.DialectPostgres, moveParams),
				wantID:            "SessionResourceMapper.FindMoveConflict",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.DestinationPath", "params.DestinationPath", "params.DestinationPath"},
				wantSQLFragments:  []string{"expires_at IS NULL OR expires_at > now()", "path = $3", "LIMIT 1"},
			},
		},
		{
			name: "move resource subtree",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperMoveResourceSubtreeStatement,
				bound:             buildSessionResourceMapperMoveResourceSubtree(yourbatis.DialectPostgres, moveParams),
				wantID:            "SessionResourceMapper.MoveResourceSubtree",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.DestinationPath", "params.SourcePath", "params.SourcePath", "params.DestinationParentPath", "params.DestinationPath", "params.SourcePath", "params.Now", "params.WorkspaceUUID", "params.SessionUUID", "params.SourcePath", "params.SourcePath", "params.SourcePath"},
				wantSQLFragments:  []string{"SET path = $1 || substring", "parent_path = CASE", "workspace_uuid = $8", "session_uuid = $9"},
			},
		},
		{
			name: "count directory children",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperCountDirectoryChildrenStatement,
				bound:             buildSessionResourceMapperCountDirectoryChildren(yourbatis.DialectPostgres, subtreeParams),
				wantID:            "SessionResourceMapper.CountDirectoryChildren",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.EntryPath"},
				wantSQLFragments:  []string{"SELECT count(*) AS child_count", "parent_path = $3", "deleted_at IS NULL"},
			},
		},
		{
			name: "subtree contains mounted input",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperSubtreeContainsMountedInputStatement,
				bound:             buildSessionResourceMapperSubtreeContainsMountedInput(yourbatis.DialectPostgres, subtreeParams),
				wantID:            "SessionResourceMapper.SubtreeContainsMountedInput",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionUUID", "params.EntryPath", "params.EntryPath", "params.EntryPath"},
				wantSQLFragments:  []string{"SELECT EXISTS", "payload IS NOT NULL", "AS contains_input"},
			},
		},
		{
			name: "retire resource subtree",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperRetireResourceSubtreeStatement,
				bound:             buildSessionResourceMapperRetireResourceSubtree(yourbatis.DialectPostgres, subtreeParams),
				wantID:            "SessionResourceMapper.RetireResourceSubtree",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.Now", "params.Now", "params.WorkspaceUUID", "params.SessionUUID", "params.EntryPath", "params.EntryPath", "params.EntryPath"},
				wantSQLFragments:  []string{"SET deleted_at = $1, updated_at = $2", "workspace_uuid = $3", "path = $5"},
			},
		},
		{
			name: "retire skill archive resources",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperRetireSkillArchiveResourcesStatement,
				bound:             buildSessionResourceMapperRetireSkillArchiveResources(yourbatis.DialectPostgres, skillRetireParams),
				wantID:            "SessionResourceMapper.RetireSkillArchiveResources",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.Now", "params.Now", "params.WorkspaceUUID", "params.SessionUUID"},
				wantSQLFragments:  []string{"resource_type = 'skill_archive'", "deleted_at IS NULL"},
			},
		},
		{
			name: "insert skill archive resource",
			contract: mapperBuilderContract{
				statement:         sessionResourceMapperInsertSkillArchiveResourceStatement,
				bound:             buildSessionResourceMapperInsertSkillArchiveResource(yourbatis.DialectPostgres, skillInsertParams),
				wantID:            "SessionResourceMapper.InsertSkillArchiveResource",
				wantKind:          yourbatis.StatementInsert,
				wantArgumentNames: []string{"params.ResourceUUID", "params.ResourceExternalID", "params.EntryPath", "params.FileUUID", "params.Now", "params.Now", "params.SessionUUID", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"INSERT INTO session_resources", "'skill_archive'", "FROM sessions session"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestSessionResourceMapperPropagatesExecutionErrors(t *testing.T) {
	ctx := context.Background()
	pathParams := sessionResourcePathParams{}
	bindingParams := sessionFileResourceBindingParams{}
	directoryParams := sessionResourceDirectoryInsertParams{}
	writeParams := sessionResourceFileWriteParams{}
	retireParams := sessionResourceRetireParams{}
	moveParams := sessionResourceMoveParams{}
	subtreeParams := sessionResourceSubtreeParams{}
	skillRetireParams := sessionSkillArchiveRetireParams{}
	skillInsertParams := sessionSkillArchiveInsertParams{}
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{name: "count resources", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.CountSessionFileResources", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).CountSessionFileResources(ctx, "", "", "")
			return err
		}}},
		{name: "find mount conflict", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.FindMountConflict", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, _, err := NewSessionResourceMapper(executor).FindMountConflict(ctx, pathParams)
			return err
		}}},
		{name: "bind resource", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.BindSessionFileResource", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).BindSessionFileResource(ctx, bindingParams)
			return err
		}}},
		{name: "get resource", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.GetSessionResourceForMutation", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).GetSessionResourceForMutation(ctx, "", "", "")
			return err
		}}},
		{name: "soft delete resource", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.SoftDeleteSessionResource", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).SoftDeleteSessionResource(ctx, "", "", "")
			return err
		}}},
		{name: "insert directory", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.InsertDirectory", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).InsertDirectory(ctx, directoryParams)
			return err
		}}},
		{name: "update resource file", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.UpdateResourceFile", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSessionResourceMapper(executor).UpdateResourceFile(ctx, writeParams)
		}}},
		{name: "insert owned file", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.InsertOwnedFileAndResource", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).InsertOwnedFileAndResource(ctx, writeParams)
			return err
		}}},
		{name: "retire resource", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.RetireResource", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSessionResourceMapper(executor).RetireResource(ctx, retireParams)
		}}},
		{name: "move resource file", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.MoveResourceFile", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSessionResourceMapper(executor).MoveResourceFile(ctx, moveParams)
		}}},
		{name: "max moved path", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.MaxMovedPathBytes", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).MaxMovedPathBytes(ctx, moveParams)
			return err
		}}},
		{name: "subtree contains input", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.SubtreeContainsInput", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).SubtreeContainsInput(ctx, moveParams)
			return err
		}}},
		{name: "find move conflict", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.FindMoveConflict", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, _, err := NewSessionResourceMapper(executor).FindMoveConflict(ctx, moveParams)
			return err
		}}},
		{name: "move subtree", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.MoveResourceSubtree", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSessionResourceMapper(executor).MoveResourceSubtree(ctx, moveParams)
		}}},
		{name: "count children", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.CountDirectoryChildren", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).CountDirectoryChildren(ctx, subtreeParams)
			return err
		}}},
		{name: "subtree contains mounted input", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.SubtreeContainsMountedInput", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSessionResourceMapper(executor).SubtreeContainsMountedInput(ctx, subtreeParams)
			return err
		}}},
		{name: "retire subtree", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.RetireResourceSubtree", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSessionResourceMapper(executor).RetireResourceSubtree(ctx, subtreeParams)
		}}},
		{name: "retire skill resources", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.RetireSkillArchiveResources", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSessionResourceMapper(executor).RetireSkillArchiveResources(ctx, skillRetireParams)
		}}},
		{name: "insert skill resource", contract: mapperExecutionErrorContract{statementID: "SessionResourceMapper.InsertSkillArchiveResource", kind: yourbatis.StatementInsert, call: func(executor yourbatis.Executor) error {
			return NewSessionResourceMapper(executor).InsertSkillArchiveResource(ctx, skillInsertParams)
		}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

func TestSessionResourceMapperResultSemantics(t *testing.T) {
	t.Run("returning row scan error", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"created_at"},
			rows:    [][]driver.Value{{"invalid-time"}},
		})
		_, err := NewSessionResourceMapper(executor).BindSessionFileResource(context.Background(), sessionFileResourceBindingParams{})
		if err == nil {
			t.Fatal("BindSessionFileResource error = nil, want scan error")
		}
	})

	t.Run("required row missing", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
		_, err := NewSessionResourceMapper(executor).GetSessionResourceForMutation(context.Background(), "", "", "")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetSessionResourceForMutation error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("optional conflict missing", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"id"}})
		_, found, err := NewSessionResourceMapper(executor).FindMoveConflict(context.Background(), sessionResourceMoveParams{})
		if err != nil || found {
			t.Fatalf("FindMoveConflict = (found %t, error %v), want (false, nil)", found, err)
		}
	})

	t.Run("scalar success", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"max_path_bytes"},
			rows:    [][]driver.Value{{int64(128)}},
		})
		got, err := NewSessionResourceMapper(executor).MaxMovedPathBytes(context.Background(), sessionResourceMoveParams{})
		if err != nil || got != 128 {
			t.Fatalf("MaxMovedPathBytes = (%d, %v), want (128, nil)", got, err)
		}
	})

	t.Run("returning scalar success", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"id"},
			rows:    [][]driver.Value{{int64(41)}},
		})
		got, err := NewSessionResourceMapper(executor).InsertDirectory(context.Background(), sessionResourceDirectoryInsertParams{})
		if err != nil || got != 41 {
			t.Fatalf("InsertDirectory = (%d, %v), want (41, nil)", got, err)
		}
	})

	t.Run("rows affected", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 2})
		got, err := NewSessionResourceMapper(executor).SoftDeleteSessionResource(context.Background(), "", "", "")
		if err != nil || got != 2 {
			t.Fatalf("SoftDeleteSessionResource = (%d, %v), want (2, nil)", got, err)
		}
	})

	t.Run("exec success", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 3})
		err := NewSessionResourceMapper(executor).MoveResourceSubtree(context.Background(), sessionResourceMoveParams{})
		if err != nil || executor.execCallCount != 1 {
			t.Fatalf("MoveResourceSubtree error = %v, exec calls = %d", err, executor.execCallCount)
		}
	})
}
