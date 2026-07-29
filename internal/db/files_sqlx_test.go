package db

import (
	"io/fs"
	"strings"
	"testing"
	"time"
)

func TestVisibleFileHistoryPredicateHasMatchingIndex(t *testing.T) {
	migration, err := fs.ReadFile(embeddedMigrations, "migrations/00036_unify_session_resources_and_files.sql")
	if err != nil {
		t.Fatalf("read session resource migration: %v", err)
	}
	want := `create index session_resources_owned_file_uuid_v1_idx
	on session_resources (workspace_id, file_uuid)
	where file_uuid is not null and payload is null;`
	if !strings.Contains(strings.ReplaceAll(string(migration), "\r\n", "\n"), want) {
		t.Fatalf("migration lacks an index for historical Owned File lookups")
	}
}

func TestFilesQueriesUseSQLXNamedParameters(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	file := FileRecord{
		UUID:              "00000000-0000-0000-0000-000000000444",
		ExternalID:        "file_test",
		WorkspaceID:       42,
		Filename:          "data.csv",
		MimeType:          "text/csv",
		SizeBytes:         12,
		SHA256:            strings.Repeat("a", 64),
		S3Bucket:          "files",
		S3Key:             "file_test/data.csv",
		Downloadable:      true,
		CreatedByAPIKeyID: 9,
		CreatedAt:         createdAt,
	}
	cursor := filePageCursorRow{ID: 10, CreatedAt: createdAt}
	afterParams := ListFilesPageParams{
		WorkspaceID: 42,
		ScopeID:     "scope_test",
		AfterID:     "file_after",
		Limit:       20,
	}
	beforeParams := ListFilesPageParams{
		WorkspaceID: 42,
		BeforeID:    "file_before",
		Limit:       20,
	}
	listQuery, listArguments := listFilesSQLXQuery(42, "scope_test")
	cursorQuery, cursorArguments := filePageCursorSQLXQuery(afterParams, afterParams.AfterID)
	afterQuery, afterArguments := listFilesPageSQLXQuery(afterParams, cursor)
	beforeQuery, beforeArguments := listFilesPageSQLXQuery(beforeParams, cursor)
	if !strings.Contains(sessionCatalogFileSQLXColumns, "file.created_at as created_at") {
		t.Fatalf("Session Catalog must return the real File created_at: %q", sessionCatalogFileSQLXColumns)
	}
	for name, query := range map[string]string{
		"list":        listQuery,
		"page cursor": cursorQuery,
		"list after":  afterQuery,
		"list before": beforeQuery,
	} {
		if !strings.Contains(query, "owner.payload is null") ||
			!strings.Contains(query, "'/outputs/'") {
			t.Fatalf("%s query does not enforce File Catalog visibility: %q", name, query)
		}
	}
	if !strings.Contains(beforeQuery, "order by created_at asc, id asc") {
		t.Fatalf("before page query does not fetch the nearest records first: %q", beforeQuery)
	}
	if !strings.Contains(afterQuery, "order by created_at desc, id desc") {
		t.Fatalf("after page query does not retain descending API order: %q", afterQuery)
	}

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{"workspace lock", fileWorkspaceLockQuery, map[string]any{"workspace_id": int64(42)}, 1},
		{"insert file", insertFileQuery, fileRecordArguments(file), 21},
		{"get file", getFileQuery, getFileArguments(42, file.ExternalID), 2},
		{"get file by uuid", getFileByUUIDQuery, fileUUIDArguments(42, file.UUID), 2},
		{
			"get file by uuid in organization",
			getFileByUUIDInOrganizationQuery,
			map[string]any{"organization_id": int64(7), "file_uuid": file.UUID},
			2,
		},
		{"list files", listQuery, listArguments, 2},
		{"page cursor", cursorQuery, cursorArguments, 3},
		{"list after page", afterQuery, afterArguments, 6},
		{"list before page", beforeQuery, beforeArguments, 5},
		{"soft delete record", softDeleteFileRecordQuery, fileUUIDArguments(42, file.UUID), 2},
		{
			"active file reference",
			activeFileReferenceQuery,
			map[string]any{"workspace_id": int64(42), "file_uuid": file.UUID},
			2,
		},
		{"soft delete", softDeleteFileQuery, fileUUIDArguments(42, file.UUID), 2},
		{
			"enqueue cleanup",
			enqueueObjectCleanupResourceJobQuery,
			map[string]any{
				"workspace_id":  int64(42),
				"bucket":        "files",
				"object_key":    "file_test/data.csv",
				"resource_type": "file",
				"resource_id":   file.ExternalID,
			},
			7,
		},
		{
			"lease cleanup",
			leaseObjectCleanupJobsQuery,
			map[string]any{"limit": 10, "worker_id": "worker_test"},
			2,
		},
		{"complete cleanup", completeObjectCleanupJobQuery, map[string]any{"job_id": int64(1)}, 1},
		{
			"fail cleanup",
			failObjectCleanupJobQuery,
			map[string]any{
				"job_id":    int64(1),
				"status":    "retry",
				"run_after": createdAt,
				"attempts":  2,
				"reason":    "delete failed",
			},
			5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains colon syntax after binding: %q", query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}
