package db

import (
	"strings"
	"testing"
	"time"
)

func TestBuiltinSkillQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "upsert skill",
			query: upsertBuiltinSkillQuery,
			arguments: map[string]any{
				"external_id":    "builtin_test",
				"display_title":  "Test",
				"latest_version": "20260727",
				"created_at":     now,
			},
			wantArgCount: 5,
		},
		{
			name:  "upsert version",
			query: upsertBuiltinSkillVersionQuery,
			arguments: map[string]any{
				"external_id":       "bsv_test",
				"skill_id":          int64(1),
				"skill_external_id": "builtin_test",
				"version":           "20260727",
				"name":              "test",
				"description":       "Test",
				"directory":         "test",
				"s3_bucket":         "bucket",
				"s3_key":            "key",
				"size_bytes":        int64(10),
				"sha256":            "sha",
				"created_at":        now,
			},
			wantArgCount: 12,
		},
		{
			name:  "list skills",
			query: listBuiltinSkillsPageQuery,
			arguments: map[string]any{
				"limit":  21,
				"offset": 0,
			},
			wantArgCount: 2,
		},
		{
			name:         "count skills",
			query:        countBuiltinSkillsQuery,
			arguments:    map[string]any{},
			wantArgCount: 0,
		},
		{
			name:  "get skill",
			query: getBuiltinSkillQuery,
			arguments: map[string]any{
				"external_id": "builtin_test",
			},
			wantArgCount: 1,
		},
		{
			name:  "list versions",
			query: listBuiltinSkillVersionsPageQuery,
			arguments: map[string]any{
				"skill_id": int64(1),
				"limit":    21,
				"offset":   0,
			},
			wantArgCount: 3,
		},
		{
			name:  "get version",
			query: getBuiltinSkillVersionQuery,
			arguments: map[string]any{
				"skill_external_id": "builtin_test",
				"version":           "20260727",
			},
			wantArgCount: 2,
		},
		{
			name:  "list missing versions",
			query: listMissingBuiltinSkillVersionsQuery,
			arguments: map[string]any{
				"keep_external_ids": []byte(`["builtin_test"]`),
			},
			wantArgCount: 1,
		},
		{
			name:  "soft delete missing versions",
			query: softDeleteMissingBuiltinSkillVersionsQuery,
			arguments: map[string]any{
				"keep_external_ids": []byte(`["builtin_test"]`),
				"deleted_at":        now,
			},
			wantArgCount: 2,
		},
		{
			name:  "soft delete missing skills",
			query: softDeleteMissingBuiltinSkillsQuery,
			arguments: map[string]any{
				"keep_external_ids": []byte(`["builtin_test"]`),
				"deleted_at":        now,
			},
			wantArgCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, tt.query, tt.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if len(arguments) != tt.wantArgCount {
				t.Fatalf("bindNamed() arguments = %#v, want %d arguments", arguments, tt.wantArgCount)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("bound query still contains a named parameter: %s", query)
			}
		})
	}
}
