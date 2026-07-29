package db

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestInsertMemoryVersionQueryUsesSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	arguments := map[string]any{
		"uuid":                           "11111111-1111-4111-8111-111111111111",
		"external_id":                    "memver_test",
		"organization_id":                int64(1),
		"workspace_id":                   int64(2),
		"memory_store_id":                int64(3),
		"memory_store_external_id":       "memstore_test",
		"memory_id":                      int64(4),
		"memory_external_id":             "mem_test",
		"operation":                      "created",
		"path":                           "/test.md",
		"content_size_bytes":             int64(4),
		"content_sha256":                 "sha256",
		"s3_bucket":                      "bucket",
		"s3_key":                         "key",
		"created_by_actor_type":          "api_key",
		"created_by_api_key_id":          int64(5),
		"created_by_api_key_external_id": "sk-ant-test",
		"created_by_session_id":          nil,
		"created_by_user_id":             nil,
		"created_at":                     now,
	}

	query, values, err := bindNamed(postgresRebinder{}, insertMemoryVersionQuery, arguments)
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	if strings.Contains(query, ":") {
		t.Fatalf("bound query retains named parameters: %q", query)
	}
	if strings.Contains(query, "::") {
		t.Fatalf("bound query contains PostgreSQL shorthand cast: %q", query)
	}
	if len(values) != len(arguments) {
		t.Fatalf("bound argument count = %d, want %d", len(values), len(arguments))
	}
}

func TestMemoryColumnListsSupportStructScan(t *testing.T) {
	tests := []struct {
		name    string
		columns string
		want    []string
	}{
		{
			name:    "memory store",
			columns: memoryStoreColumns(),
			want:    []string{"CAST(uuid AS text) as uuid", "metadata", "archived_at"},
		},
		{
			name:    "memory",
			columns: memoryColumns(),
			want: []string{
				"coalesce(current_version_id, 0) as current_version_id",
				"coalesce(current_version_external_id, '') as current_version_external_id",
			},
		},
		{
			name:    "memory version",
			columns: memoryVersionColumns(),
			want:    []string{"CAST(uuid AS text) as uuid", "redacted_by_actor_type", "created_at"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(test.columns, "::") {
				t.Fatalf("columns contain PostgreSQL shorthand cast: %q", test.columns)
			}
			for _, fragment := range test.want {
				if !strings.Contains(test.columns, fragment) {
					t.Fatalf("columns do not contain %q: %q", fragment, test.columns)
				}
			}
		})
	}
}

func TestMemoryVersionRowConvertsNullableFields(t *testing.T) {
	row := memoryVersionRow{
		CreatedByActorType:         "session",
		CreatedBySessionID:         sql.NullString{String: "sesn_test", Valid: true},
		Path:                       sql.NullString{String: "/test.md", Valid: true},
		ContentSizeBytes:           sql.NullInt64{Int64: 4, Valid: true},
		RedactedByActorType:        sql.NullString{String: "api_key", Valid: true},
		RedactedByAPIKeyExternalID: sql.NullString{String: "sk-ant-test", Valid: true},
	}

	version := row.version()
	if version.Path == nil || *version.Path != "/test.md" {
		t.Fatalf("version path = %#v, want /test.md", version.Path)
	}
	if version.ContentSizeBytes == nil || *version.ContentSizeBytes != 4 {
		t.Fatalf("version content size = %#v, want 4", version.ContentSizeBytes)
	}
	if version.CreatedBy.SessionID != "sesn_test" {
		t.Fatalf("created by session = %q, want sesn_test", version.CreatedBy.SessionID)
	}
	if version.RedactedBy == nil || version.RedactedBy.APIKeyExternalID != "sk-ant-test" {
		t.Fatalf("redacted by = %#v, want API key actor", version.RedactedBy)
	}
}
