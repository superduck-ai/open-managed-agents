package db

import (
	"strings"
	"testing"
	"time"
)

func TestSkillVersionInsertQueryBindsNamedArguments(t *testing.T) {
	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	query, arguments, err := bindNamed(postgresRebinder{}, insertSkillVersionQuery, map[string]any{
		"uuid":                    "11111111-1111-4111-8111-111111111111",
		"external_id":             "skillv_test",
		"workspace_uuid":          "00000000-0000-0000-0000-000000000001",
		"skill_uuid":              "00000000-0000-0000-0000-000000000002",
		"skill_external_id":       "skill_test",
		"version":                 "1.0.0",
		"name":                    "test",
		"description":             "test skill",
		"directory":               "test",
		"s3_bucket":               "bucket",
		"s3_key":                  "skills/test",
		"size_bytes":              int64(42),
		"sha256":                  strings.Repeat("a", 64),
		"created_by_api_key_uuid": "00000000-0000-0000-0000-000000000003",
		"created_at":              now,
	})
	if err != nil {
		t.Fatalf("bind named query: %v", err)
	}
	if strings.Contains(query, ":") {
		t.Fatalf("query retains named parameter syntax: %q", query)
	}
	if len(arguments) != 15 {
		t.Fatalf("argument count = %d, want 15", len(arguments))
	}
}

func TestSkillSQLXColumnListsAvoidPostgreSQLShorthandCasts(t *testing.T) {
	for _, columns := range []string{skillColumns(), skillVersionColumns()} {
		if strings.Contains(columns, "::") {
			t.Fatalf("column list contains shorthand cast that conflicts with sqlx named parsing: %q", columns)
		}
	}
}
