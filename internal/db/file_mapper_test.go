package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestFileMapperBuildsPostgresArguments(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	workspaceUUID := "00000000-0000-4000-8000-000000000042"
	fileUUID := "00000000-0000-4000-8000-000000000444"
	file := FileRecord{
		UUID:                "00000000-0000-4000-8000-000000000444",
		ExternalID:          "file_test",
		WorkspaceUUID:       "00000000-0000-4000-8000-000000000042",
		Filename:            "data.csv",
		MimeType:            "text/csv",
		SizeBytes:           12,
		SHA256:              strings.Repeat("a", 64),
		S3Bucket:            "files",
		S3Key:               "file_test/data.csv",
		Downloadable:        true,
		CreatedByAPIKeyUUID: "00000000-0000-4000-8000-000000000009",
		CreatedAt:           createdAt,
	}

	pageParams := newFileMapperListParams(file.WorkspaceUUID, "scope_test")
	pageParams.CursorExternalID = "file_after"
	pageParams.CursorUUID = "00000000-0000-4000-8000-000000000010"
	pageParams.CursorCreatedAt = createdAt
	pageParams.HasCursor = true
	pageParams.Limit = 21
	sessionPageParams := pageParams
	sessionPageParams.ScopeID = "sesn_test"
	sessionPageParams.SessionScope = true

	tests := []struct {
		name         string
		bound        yourbatis.BoundSQL
		wantArgCount int
		wantClauses  []string
	}{
		{
			name:         "insert file",
			bound:        buildFileMapperInsertFile(yourbatis.DialectPostgres, fileMapperRecordParameters(file)),
			wantArgCount: 14,
			wantClauses:  []string{"INSERT INTO files", "VALUES (", "$14"},
		},
		{
			name:         "get visible file",
			bound:        buildFileMapperGetFile(yourbatis.DialectPostgres, workspaceUUID, file.ExternalID),
			wantArgCount: 2,
			wantClauses:  []string{"owner.payload IS NULL", "'/outputs/'"},
		},
		{
			name: "list files by UUIDs",
			bound: buildFileMapperListFilesByUUIDs(yourbatis.DialectPostgres, fileMapperFileUUIDsParams{
				WorkspaceUUID: workspaceUUID,
				FileUUIDs:     []string{"00000000-0000-4000-8000-000000000444", "00000000-0000-4000-8000-000000000555"},
			}),
			wantArgCount: 3,
			wantClauses:  []string{"workspace_uuid = $1", "uuid IN", "deleted_at IS NULL"},
		},
		{
			name:         "list scoped files",
			bound:        buildFileMapperListFiles(yourbatis.DialectPostgres, pageParams),
			wantArgCount: 2,
			wantClauses:  []string{"scope_id = $2", "ORDER BY created_at DESC, uuid DESC"},
		},
		{
			name:         "after page",
			bound:        buildFileMapperListFilesPage(yourbatis.DialectPostgres, pageParams),
			wantArgCount: 6,
			wantClauses:  []string{"created_at < $3", "uuid < $5", "ORDER BY created_at DESC", "LIMIT $6"},
		},
		{
			name: "before page",
			bound: func() yourbatis.BoundSQL {
				params := pageParams
				params.Before = true
				return buildFileMapperListFilesPage(yourbatis.DialectPostgres, params)
			}(),
			wantArgCount: 6,
			wantClauses:  []string{"created_at > $3", "uuid > $5", "ORDER BY created_at ASC"},
		},
		{
			name:         "session page cursor",
			bound:        buildFileMapperFindSessionPageCursor(yourbatis.DialectPostgres, sessionPageParams),
			wantArgCount: 3,
			wantClauses:  []string{"session_resources", "file.external_id = $3"},
		},
		{
			name:         "session page",
			bound:        buildFileMapperListSessionFilesPage(yourbatis.DialectPostgres, sessionPageParams),
			wantArgCount: 6,
			wantClauses:  []string{"file.created_at AS created_at", "resource.created_at < $3", "LIMIT $6"},
		},
		{
			name:         "soft delete file",
			bound:        buildFileMapperSoftDeleteFile(yourbatis.DialectPostgres, workspaceUUID, fileUUID),
			wantArgCount: 2,
			wantClauses:  []string{"UPDATE files", "deleted_at = now()"},
		},
		{
			name:         "enqueue cleanup",
			bound:        buildFileMapperEnqueueObjectCleanupJob(yourbatis.DialectPostgres, workspaceUUID, []byte(`{"bucket":"files"}`)),
			wantArgCount: 2,
			wantClauses:  []string{"INSERT INTO jobs", "CAST($2 AS jsonb)"},
		},
		{
			name:         "lease cleanup",
			bound:        buildFileMapperLeaseObjectCleanupJobs(yourbatis.DialectPostgres, "worker_test", 10),
			wantArgCount: 2,
			wantClauses:  []string{"FOR UPDATE SKIP LOCKED", "locked_by = $2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.bound.Args) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(test.bound.Args), test.wantArgCount)
			}
			for _, clause := range test.wantClauses {
				if !strings.Contains(test.bound.SQL, clause) {
					t.Fatalf("bound query does not contain %q: %q", clause, test.bound.SQL)
				}
			}
		})
	}
}

func TestFileMapperOwnedAndCleanupBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	listParams := fileMapperListParams{
		WorkspaceUUID:    "workspace-uuid",
		ScopeID:          "session-external-id",
		CursorExternalID: "file-external-id",
		SessionScope:     true,
		HasScope:         true,
	}
	writeParams := sessionResourceFileWriteParams{
		ResourceUUID:          "resource-uuid",
		WorkspaceUUID:         "workspace-uuid",
		SessionUUID:           "session-uuid",
		Filename:              "report.csv",
		MediaType:             "text/csv",
		DetectedMimeType:      "text/csv",
		SizeBytes:             42,
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
	}
	retireParams := sessionResourceRetireParams{ResourceUUID: "resource-uuid", WorkspaceUUID: "workspace-uuid", RetiredAt: now}
	subtreeParams := sessionResourceSubtreeParams{WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", EntryPath: "/outputs/root", Now: now}
	failureParams := objectCleanupJobFailureParams{JobUUID: "job-uuid", Status: "retry", RunAfter: now, Attempts: 2, Reason: "temporary"}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "get file by uuid",
			contract: mapperBuilderContract{
				statement:         fileMapperGetFileByUUIDStatement,
				bound:             buildFileMapperGetFileByUUID(yourbatis.DialectPostgres, "workspace-uuid", "file-uuid"),
				wantID:            "FileMapper.GetFileByUUID",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "fileUUID"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "uuid = $2", "deleted_at IS NULL"},
			},
		},
		{
			name: "get file by uuid in organization",
			contract: mapperBuilderContract{
				statement:         fileMapperGetFileByUUIDInOrganizationStatement,
				bound:             buildFileMapperGetFileByUUIDInOrganization(yourbatis.DialectPostgres, "organization-uuid", "file-uuid"),
				wantID:            "FileMapper.GetFileByUUIDInOrganization",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"organizationUUID", "fileUUID"},
				wantSQLFragments:  []string{"JOIN workspaces w", "w.organization_uuid = $1", "f.uuid = $2"},
			},
		},
		{
			name: "list session files",
			contract: mapperBuilderContract{
				statement:         fileMapperListSessionFilesStatement,
				bound:             buildFileMapperListSessionFiles(yourbatis.DialectPostgres, listParams),
				wantID:            "FileMapper.ListSessionFiles",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.ScopeID"},
				wantSQLFragments:  []string{"FROM session_resources resource", "resource.session_external_id = $2", "ORDER BY resource.created_at DESC"},
			},
		},
		{
			name: "find page cursor",
			contract: mapperBuilderContract{
				statement:         fileMapperFindPageCursorStatement,
				bound:             buildFileMapperFindPageCursor(yourbatis.DialectPostgres, listParams),
				wantID:            "FileMapper.FindPageCursor",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"params.WorkspaceUUID", "params.CursorExternalID", "params.ScopeID"},
				wantSQLFragments:  []string{"external_id = $2", "scope_id = $3", "deleted_at IS NULL"},
			},
		},
		{
			name: "get file for delete",
			contract: mapperBuilderContract{
				statement:         fileMapperGetFileForDeleteStatement,
				bound:             buildFileMapperGetFileForDelete(yourbatis.DialectPostgres, "workspace-uuid", "file-uuid"),
				wantID:            "FileMapper.GetFileForDelete",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "fileUUID"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "uuid = $2", "FOR UPDATE"},
			},
		},
		{
			name: "get file for share",
			contract: mapperBuilderContract{
				statement:         fileMapperGetFileForShareStatement,
				bound:             buildFileMapperGetFileForShare(yourbatis.DialectPostgres, "workspace-uuid", "file-external-id"),
				wantID:            "FileMapper.GetFileForShare",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "fileExternalID"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "external_id = $2", "FOR SHARE"},
			},
		},
		{
			name: "has active reference",
			contract: mapperBuilderContract{
				statement:         fileMapperHasActiveReferenceStatement,
				bound:             buildFileMapperHasActiveReference(yourbatis.DialectPostgres, "workspace-uuid", "file-uuid"),
				wantID:            "FileMapper.HasActiveReference",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "fileUUID"},
				wantSQLFragments:  []string{"SELECT EXISTS", "resource.workspace_uuid = $1", "resource.file_uuid = $2"},
			},
		},
		{
			name: "update owned file",
			contract: mapperBuilderContract{
				statement: fileMapperUpdateOwnedFileStatement,
				bound:     buildFileMapperUpdateOwnedFile(yourbatis.DialectPostgres, writeParams),
				wantID:    "FileMapper.UpdateOwnedFile",
				wantKind:  yourbatis.StatementUpdate,
				wantArgumentNames: []string{
					"params.Filename", "params.MediaType", "params.DetectedMimeType", "params.SizeBytes", "params.Metadata",
					"params.AuthorizationMetadata", "params.Tags", "params.Downloadable", "params.MD5", "params.SHA256",
					"params.S3Bucket", "params.S3Key", "params.S3ETag", "params.S3VersionID", "params.WorkspaceUUID",
					"params.ResourceUUID", "params.WorkspaceUUID", "params.SessionUUID",
				},
				wantSQLFragments: []string{"UPDATE files", "detected_mime_type = $3", "resource.session_uuid = $18"},
			},
		},
		{
			name: "retire owned file",
			contract: mapperBuilderContract{
				statement:         fileMapperRetireOwnedFileStatement,
				bound:             buildFileMapperRetireOwnedFile(yourbatis.DialectPostgres, retireParams),
				wantID:            "FileMapper.RetireOwnedFile",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.RetiredAt", "params.WorkspaceUUID", "params.ResourceUUID", "params.WorkspaceUUID"},
				wantSQLFragments:  []string{"UPDATE files", "deleted_at = COALESCE(file.deleted_at, $1)", "resource.payload IS NULL"},
			},
		},
		{
			name: "retire owned files in subtree",
			contract: mapperBuilderContract{
				statement:         fileMapperRetireOwnedFilesInSubtreeStatement,
				bound:             buildFileMapperRetireOwnedFilesInSubtree(yourbatis.DialectPostgres, subtreeParams),
				wantID:            "FileMapper.RetireOwnedFilesInSubtree",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.Now", "params.WorkspaceUUID", "params.WorkspaceUUID", "params.SessionUUID", "params.EntryPath", "params.EntryPath", "params.EntryPath"},
				wantSQLFragments:  []string{"UPDATE files", "resource.session_uuid = $4", "resource.path = $5"},
			},
		},
		{
			name: "complete object cleanup job",
			contract: mapperBuilderContract{
				statement:         fileMapperCompleteObjectCleanupJobStatement,
				bound:             buildFileMapperCompleteObjectCleanupJob(yourbatis.DialectPostgres, "job-uuid"),
				wantID:            "FileMapper.CompleteObjectCleanupJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"jobUUID"},
				wantSQLFragments:  []string{"UPDATE jobs", "status = 'completed'", "uuid = $1"},
			},
		},
		{
			name: "fail object cleanup job",
			contract: mapperBuilderContract{
				statement:         fileMapperFailObjectCleanupJobStatement,
				bound:             buildFileMapperFailObjectCleanupJob(yourbatis.DialectPostgres, failureParams),
				wantID:            "FileMapper.FailObjectCleanupJob",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"params.Status", "params.RunAfter", "params.Attempts", "params.Reason", "params.JobUUID"},
				wantSQLFragments:  []string{"UPDATE jobs", "status = $1", "run_after = $2", "uuid = $5"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestFileMapperFileAndSkillBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	recordParams := fileMapperRecordParams{CreatedAt: now}
	listParams := fileMapperListParams{
		WorkspaceUUID:    "workspace-uuid",
		ScopeID:          "scope-id",
		CursorExternalID: "cursor-external-id",
		CursorUUID:       "cursor-uuid",
		CursorCreatedAt:  now,
		Limit:            21,
		HasScope:         true,
		HasCursor:        true,
	}
	sessionListParams := listParams
	sessionListParams.SessionScope = true
	skillRetireParams := sessionSkillArchiveRetireParams{WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", Now: now}
	skillInsertParams := sessionSkillArchiveInsertParams{WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", Now: now}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{name: "insert file", contract: mapperBuilderContract{
			statement: fileMapperInsertFileStatement,
			bound:     buildFileMapperInsertFile(yourbatis.DialectPostgres, recordParams),
			wantID:    "FileMapper.InsertFile",
			wantKind:  yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.FileUUID", "params.FileExternalID", "params.WorkspaceUUID", "params.Filename", "params.MimeType",
				"params.SizeBytes", "params.SHA256", "params.S3Bucket", "params.S3Key", "params.Downloadable",
				"params.ScopeType", "params.ScopeID", "params.CreatedByAPIKeyUUID", "params.CreatedAt",
			},
			wantSQLFragments: []string{"INSERT INTO files", "created_by_api_key_uuid", "VALUES ("},
		}},
		{name: "get file", contract: mapperBuilderContract{
			statement:         fileMapperGetFileStatement,
			bound:             buildFileMapperGetFile(yourbatis.DialectPostgres, "workspace-uuid", "file-external-id"),
			wantID:            "FileMapper.GetFile",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "fileExternalID"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "external_id = $2", "owner.payload IS NULL"},
		}},
		{name: "list files", contract: mapperBuilderContract{
			statement:         fileMapperListFilesStatement,
			bound:             buildFileMapperListFiles(yourbatis.DialectPostgres, listParams),
			wantID:            "FileMapper.ListFiles",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.ScopeID"},
			wantSQLFragments:  []string{"scope_id = $2", "ORDER BY created_at DESC, uuid DESC"},
		}},
		{name: "find session page cursor", contract: mapperBuilderContract{
			statement:         fileMapperFindSessionPageCursorStatement,
			bound:             buildFileMapperFindSessionPageCursor(yourbatis.DialectPostgres, sessionListParams),
			wantID:            "FileMapper.FindSessionPageCursor",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.ScopeID", "params.CursorExternalID"},
			wantSQLFragments:  []string{"FROM session_resources resource", "session_external_id = $2", "file.external_id = $3"},
		}},
		{name: "list files page", contract: mapperBuilderContract{
			statement:         fileMapperListFilesPageStatement,
			bound:             buildFileMapperListFilesPage(yourbatis.DialectPostgres, listParams),
			wantID:            "FileMapper.ListFilesPage",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.ScopeID", "params.CursorCreatedAt", "params.CursorCreatedAt", "params.CursorUUID", "params.Limit"},
			wantSQLFragments:  []string{"created_at < $3", "uuid < $5", "LIMIT $6"},
		}},
		{name: "list session files page", contract: mapperBuilderContract{
			statement:         fileMapperListSessionFilesPageStatement,
			bound:             buildFileMapperListSessionFilesPage(yourbatis.DialectPostgres, sessionListParams),
			wantID:            "FileMapper.ListSessionFilesPage",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.ScopeID", "params.CursorCreatedAt", "params.CursorCreatedAt", "params.CursorUUID", "params.Limit"},
			wantSQLFragments:  []string{"resource.created_at < $3", "resource.uuid < $5", "LIMIT $6"},
		}},
		{name: "soft delete", contract: mapperBuilderContract{
			statement:         fileMapperSoftDeleteFileStatement,
			bound:             buildFileMapperSoftDeleteFile(yourbatis.DialectPostgres, "workspace-uuid", "file-uuid"),
			wantID:            "FileMapper.SoftDeleteFile",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "fileUUID"},
			wantSQLFragments:  []string{"UPDATE files", "deleted_at = now()", "uuid = $2"},
		}},
		{name: "retire skill files", contract: mapperBuilderContract{
			statement:         fileMapperRetireSkillArchiveFilesStatement,
			bound:             buildFileMapperRetireSkillArchiveFiles(yourbatis.DialectPostgres, skillRetireParams),
			wantID:            "FileMapper.RetireSkillArchiveFiles",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.Now", "params.WorkspaceUUID", "params.WorkspaceUUID", "params.SessionUUID"},
			wantSQLFragments:  []string{"UPDATE files file", "resource_type = 'skill_archive'", "resource.session_uuid = $4"},
		}},
		{name: "insert skill file", contract: mapperBuilderContract{
			statement:         fileMapperInsertSkillArchiveFileStatement,
			bound:             buildFileMapperInsertSkillArchiveFile(yourbatis.DialectPostgres, skillInsertParams),
			wantID:            "FileMapper.InsertSkillArchiveFile",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"params.FileUUID", "params.FileExternalID", "params.Filename", "params.SizeBytes", "params.Source", "params.SHA256", "params.S3Bucket", "params.S3Key", "params.Now", "params.SessionUUID", "params.WorkspaceUUID"},
			wantSQLFragments:  []string{"INSERT INTO files", "skill_source", "FROM sessions session"},
		}},
		{name: "enqueue cleanup", contract: mapperBuilderContract{
			statement:         fileMapperEnqueueObjectCleanupJobStatement,
			bound:             buildFileMapperEnqueueObjectCleanupJob(yourbatis.DialectPostgres, "workspace-uuid", []byte(`{"bucket":"files"}`)),
			wantID:            "FileMapper.EnqueueObjectCleanupJob",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"workspaceUUID", "payload"},
			wantSQLFragments:  []string{"INSERT INTO jobs", "CAST($2 AS jsonb)"},
		}},
		{name: "lease cleanup", contract: mapperBuilderContract{
			statement:         fileMapperLeaseObjectCleanupJobsStatement,
			bound:             buildFileMapperLeaseObjectCleanupJobs(yourbatis.DialectPostgres, "worker", 10),
			wantID:            "FileMapper.LeaseObjectCleanupJobs",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"limit", "workerID"},
			wantSQLFragments:  []string{"FOR UPDATE SKIP LOCKED", "locked_by = $2", "RETURNING"},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestFileMapperOwnedAndCleanupMethodsPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	listParams := fileMapperListParams{}
	writeParams := sessionResourceFileWriteParams{}
	retireParams := sessionResourceRetireParams{}
	subtreeParams := sessionResourceSubtreeParams{}
	failureParams := objectCleanupJobFailureParams{}
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{name: "get by uuid", contract: mapperExecutionErrorContract{statementID: "FileMapper.GetFileByUUID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).GetFileByUUID(ctx, "", "")
			return err
		}}},
		{name: "get by uuid in organization", contract: mapperExecutionErrorContract{statementID: "FileMapper.GetFileByUUIDInOrganization", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).GetFileByUUIDInOrganization(ctx, "", "")
			return err
		}}},
		{name: "list session files", contract: mapperExecutionErrorContract{statementID: "FileMapper.ListSessionFiles", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).ListSessionFiles(ctx, listParams)
			return err
		}}},
		{name: "find page cursor", contract: mapperExecutionErrorContract{statementID: "FileMapper.FindPageCursor", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, _, err := NewFileMapper(executor).FindPageCursor(ctx, listParams)
			return err
		}}},
		{name: "get for delete", contract: mapperExecutionErrorContract{statementID: "FileMapper.GetFileForDelete", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).GetFileForDelete(ctx, "", "")
			return err
		}}},
		{name: "get for share", contract: mapperExecutionErrorContract{statementID: "FileMapper.GetFileForShare", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).GetFileForShare(ctx, "", "")
			return err
		}}},
		{name: "has active reference", contract: mapperExecutionErrorContract{statementID: "FileMapper.HasActiveReference", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).HasActiveReference(ctx, "", "")
			return err
		}}},
		{name: "update owned file", contract: mapperExecutionErrorContract{statementID: "FileMapper.UpdateOwnedFile", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).UpdateOwnedFile(ctx, writeParams)
		}}},
		{name: "retire owned file", contract: mapperExecutionErrorContract{statementID: "FileMapper.RetireOwnedFile", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).RetireOwnedFile(ctx, retireParams)
		}}},
		{name: "retire owned subtree", contract: mapperExecutionErrorContract{statementID: "FileMapper.RetireOwnedFilesInSubtree", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).RetireOwnedFilesInSubtree(ctx, subtreeParams)
		}}},
		{name: "complete cleanup", contract: mapperExecutionErrorContract{statementID: "FileMapper.CompleteObjectCleanupJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).CompleteObjectCleanupJob(ctx, "")
		}}},
		{name: "fail cleanup", contract: mapperExecutionErrorContract{statementID: "FileMapper.FailObjectCleanupJob", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).FailObjectCleanupJob(ctx, failureParams)
		}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

func TestFileMapperFileAndSkillMethodsPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	listParams := fileMapperListParams{}
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{name: "insert file", contract: mapperExecutionErrorContract{statementID: "FileMapper.InsertFile", kind: yourbatis.StatementInsert, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).InsertFile(ctx, fileMapperRecordParams{})
		}}},
		{name: "get file", contract: mapperExecutionErrorContract{statementID: "FileMapper.GetFile", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).GetFile(ctx, "", "")
			return err
		}}},
		{name: "list files", contract: mapperExecutionErrorContract{statementID: "FileMapper.ListFiles", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).ListFiles(ctx, listParams)
			return err
		}}},
		{name: "find session page cursor", contract: mapperExecutionErrorContract{statementID: "FileMapper.FindSessionPageCursor", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, _, err := NewFileMapper(executor).FindSessionPageCursor(ctx, listParams)
			return err
		}}},
		{name: "list files page", contract: mapperExecutionErrorContract{statementID: "FileMapper.ListFilesPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).ListFilesPage(ctx, listParams)
			return err
		}}},
		{name: "list session files page", contract: mapperExecutionErrorContract{statementID: "FileMapper.ListSessionFilesPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).ListSessionFilesPage(ctx, listParams)
			return err
		}}},
		{name: "soft delete", contract: mapperExecutionErrorContract{statementID: "FileMapper.SoftDeleteFile", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).SoftDeleteFile(ctx, "", "")
		}}},
		{name: "retire skill files", contract: mapperExecutionErrorContract{statementID: "FileMapper.RetireSkillArchiveFiles", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).RetireSkillArchiveFiles(ctx, sessionSkillArchiveRetireParams{})
		}}},
		{name: "insert skill file", contract: mapperExecutionErrorContract{statementID: "FileMapper.InsertSkillArchiveFile", kind: yourbatis.StatementInsert, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).InsertSkillArchiveFile(ctx, sessionSkillArchiveInsertParams{})
		}}},
		{name: "enqueue cleanup", contract: mapperExecutionErrorContract{statementID: "FileMapper.EnqueueObjectCleanupJob", kind: yourbatis.StatementInsert, call: func(executor yourbatis.Executor) error {
			return NewFileMapper(executor).EnqueueObjectCleanupJob(ctx, "", nil)
		}}},
		{name: "lease cleanup", contract: mapperExecutionErrorContract{statementID: "FileMapper.LeaseObjectCleanupJobs", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewFileMapper(executor).LeaseObjectCleanupJobs(ctx, "", 0)
			return err
		}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperExecutionError(t, test.contract)
		})
	}
}

func TestFileMapperResultSemantics(t *testing.T) {
	t.Run("required uuid lookup scan error", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"uuid"},
			rows:    [][]driver.Value{{"invalid-uuid"}},
		})
		_, err := NewFileMapper(executor).GetFileByUUID(context.Background(), "workspace-uuid", "file-uuid")
		if err == nil {
			t.Fatal("GetFileByUUID error = nil, want scan error")
		}
	})

	t.Run("required uuid lookup missing", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
		_, err := NewFileMapper(executor).GetFileByUUID(context.Background(), "workspace-uuid", "file-uuid")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetFileByUUID error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("required uuid lookup success", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{
				"uuid", "external_id", "workspace_uuid", "filename", "mime_type", "size_bytes", "sha256",
				"s3_bucket", "s3_key", "downloadable", "scope_type", "scope_id", "created_by_api_key_uuid", "created_at",
			},
			rows: [][]driver.Value{{
				"00000000-0000-4000-8000-000000000001", "file_test", "00000000-0000-4000-8000-000000000002",
				"report.csv", "text/csv", int64(42), "sha256", "files", "report.csv", true, nil, nil,
				"00000000-0000-4000-8000-000000000003", createdAt,
			}},
		})
		row, err := NewFileMapper(executor).GetFileByUUID(context.Background(), "workspace-uuid", "file-uuid")
		if err != nil || row.ExternalID != "file_test" || row.SizeBytes != 42 {
			t.Fatalf("GetFileByUUID = (%+v, %v), want file_test with 42 bytes", row, err)
		}
	})

	t.Run("optional cursor missing", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "created_at"}})
		_, found, err := NewFileMapper(executor).FindPageCursor(context.Background(), fileMapperListParams{})
		if err != nil || found {
			t.Fatalf("FindPageCursor = (found %t, error %v), want (false, nil)", found, err)
		}
	})

	t.Run("empty session file list", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
		rows, err := NewFileMapper(executor).ListSessionFiles(context.Background(), fileMapperListParams{})
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListSessionFiles = (%#v, %v), want empty list and nil error", rows, err)
		}
	})

	t.Run("active reference scalar", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"active_reference"},
			rows:    [][]driver.Value{{true}},
		})
		active, err := NewFileMapper(executor).HasActiveReference(context.Background(), "workspace-uuid", "file-uuid")
		if err != nil || !active {
			t.Fatalf("HasActiveReference = (%t, %v), want (true, nil)", active, err)
		}
	})

	t.Run("owned file update success", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		err := NewFileMapper(executor).UpdateOwnedFile(context.Background(), sessionResourceFileWriteParams{})
		if err != nil || executor.execCallCount != 1 {
			t.Fatalf("UpdateOwnedFile error = %v, exec calls = %d", err, executor.execCallCount)
		}
	})

	for _, test := range []struct {
		name string
		call func(FileMapper) error
	}{
		{name: "complete cleanup success", call: func(mapper FileMapper) error {
			return mapper.CompleteObjectCleanupJob(context.Background(), "job-uuid")
		}},
		{name: "fail cleanup success", call: func(mapper FileMapper) error {
			return mapper.FailObjectCleanupJob(context.Background(), objectCleanupJobFailureParams{})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
			if err := test.call(NewFileMapper(executor)); err != nil {
				t.Fatalf("cleanup mutation error = %v", err)
			}
		})
	}
}
