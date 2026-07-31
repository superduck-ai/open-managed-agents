package db

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFilesQueriesUseSQLXNamedParameters(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	file := FileRecord{
		UUID:                "00000000-0000-0000-0000-000000000444",
		ExternalID:          "file_test",
		WorkspaceUUID:       "00000000-0000-0000-0000-000000000042",
		Filename:            "data.csv",
		MimeType:            "text/csv",
		SizeBytes:           12,
		SHA256:              strings.Repeat("a", 64),
		S3Bucket:            "files",
		S3Key:               "file_test/data.csv",
		Downloadable:        true,
		CreatedByAPIKeyUUID: "00000000-0000-0000-0000-000000000009",
		CreatedAt:           createdAt,
	}
	cursor := filePageCursorRow{UUID: uuid.MustParse("00000000-0000-0000-0000-000000000010"), CreatedAt: createdAt}
	afterParams := ListFilesPageParams{
		WorkspaceUUID: "00000000-0000-0000-0000-000000000042",
		ScopeID:       "scope_test",
		AfterID:       "file_after",
		Limit:         20,
	}
	beforeParams := ListFilesPageParams{
		WorkspaceUUID: "00000000-0000-0000-0000-000000000042",
		BeforeID:      "file_before",
		Limit:         20,
	}
	listQuery, listArguments := listFilesSQLXQuery(file.WorkspaceUUID, "scope_test")
	cursorQuery, cursorArguments := filePageCursorSQLXQuery(afterParams, afterParams.AfterID)
	afterQuery, afterArguments := listFilesPageSQLXQuery(afterParams, cursor)
	beforeQuery, beforeArguments := listFilesPageSQLXQuery(beforeParams, cursor)
	if !strings.Contains(beforeQuery, "order by created_at asc, uuid asc") {
		t.Fatalf("before page query does not fetch the nearest records first: %q", beforeQuery)
	}
	if !strings.Contains(afterQuery, "order by created_at desc, uuid desc") {
		t.Fatalf("after page query does not retain descending API order: %q", afterQuery)
	}

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{"workspace lock", fileWorkspaceLockQuery, map[string]any{"workspace_uuid": dbUUID(file.WorkspaceUUID)}, 1},
		{"insert file", insertFileQuery, fileRecordArguments(file), 14},
		{"get file", getFileQuery, getFileArguments(file.WorkspaceUUID, file.ExternalID), 2},
		{"get file by uuid", getFileByUUIDQuery, fileUUIDArguments(file.WorkspaceUUID, file.UUID), 2},
		{
			"get file by uuid in organization",
			getFileByUUIDInOrganizationQuery,
			map[string]any{
				"organization_uuid": dbUUID("00000000-0000-0000-0000-000000000007"),
				"file_uuid":         dbUUID(file.UUID),
			},
			2,
		},
		{"list files", listQuery, listArguments, 2},
		{"page cursor", cursorQuery, cursorArguments, 3},
		{"list after page", afterQuery, afterArguments, 6},
		{"list before page", beforeQuery, beforeArguments, 5},
		{"soft delete record", softDeleteFileRecordQuery, getFileArguments(file.WorkspaceUUID, file.ExternalID), 2},
		{
			"active file reference",
			activeFileReferenceQuery,
			map[string]any{
				"workspace_uuid": dbUUID(file.WorkspaceUUID),
				"file_uuid":      dbUUID(file.UUID),
			},
			4,
		},
		{"soft delete", softDeleteFileQuery, getFileArguments(file.WorkspaceUUID, file.ExternalID), 2},
		{
			"enqueue cleanup",
			enqueueObjectCleanupResourceJobQuery,
			map[string]any{
				"workspace_uuid": dbUUID(file.WorkspaceUUID),
				"payload":        []byte(`{"bucket":"files"}`),
			},
			2,
		},
		{
			"lease cleanup",
			leaseObjectCleanupJobsQuery,
			map[string]any{"limit": 10, "worker_id": "worker_test"},
			2,
		},
		{
			"complete cleanup",
			completeObjectCleanupJobQuery,
			map[string]any{"job_uuid": dbUUID("00000000-0000-0000-0000-000000000001")},
			1,
		},
		{
			"fail cleanup",
			failObjectCleanupJobQuery,
			map[string]any{
				"job_uuid":  dbUUID("00000000-0000-0000-0000-000000000001"),
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
