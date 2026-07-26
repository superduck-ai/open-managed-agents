package db

import (
	"strings"
	"testing"
	"time"
)

func TestFilestoreSkillArchiveQueriesUseSQLXNamedParameters(t *testing.T) {
	arguments := map[string]any{
		"workspace_id":        int64(41),
		"session_external_id": "session_41",
		"organization_uuid":   "00000000-0000-4000-8000-000000000041",
		"workspace_uuid":      "00000000-0000-4000-8000-000000000042",
		"filesystem_id":       int64(43),
		"filesystem_uuid":     "00000000-0000-4000-8000-000000000043",
		"source":              "custom",
		"skill_version_uuid":  "00000000-0000-4000-8000-000000000044",
		"virtual_path":        "/skills/demo",
		"s3_bucket":           "skills",
		"s3_key":              "skills/demo.zip",
		"size_bytes":          int64(1024),
		"sha256":              strings.Repeat("a", 64),
		"now":                 time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name         string
		query        string
		wantArgCount int
	}{
		{"filesystem", filestoreSkillArchiveFilesystemQuery, 3},
		{"delete", filestoreSkillArchiveDeleteQuery, 2},
		{"insert", filestoreSkillArchiveInsertQuery, 12},
		{"list", filestoreSkillArchiveListQuery, 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, boundArguments, err := bindNamed(postgresRebinder{}, test.query, arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains colon syntax after binding: %q", query)
			}
			if len(boundArguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(boundArguments), test.wantArgCount)
			}
		})
	}
}

func TestValidateFilestoreSkillArchiveInput(t *testing.T) {
	valid := FilestoreSkillArchiveInput{
		Source:           "custom",
		SkillVersionUUID: "00000000-0000-4000-8000-000000000044",
		Directory:        "demo",
		S3Bucket:         "skills",
		S3Key:            "skills/demo.zip",
		SizeBytes:        1024,
		SHA256:           strings.Repeat("a", 64),
	}
	for _, test := range []struct {
		name   string
		mutate func(*FilestoreSkillArchiveInput)
	}{
		{"unsupported source", func(input *FilestoreSkillArchiveInput) { input.Source = "other" }},
		{"nested directory", func(input *FilestoreSkillArchiveInput) { input.Directory = "demo/nested" }},
		{"missing version", func(input *FilestoreSkillArchiveInput) { input.SkillVersionUUID = "" }},
		{"invalid version", func(input *FilestoreSkillArchiveInput) { input.SkillVersionUUID = "not-a-uuid" }},
		{"missing object key", func(input *FilestoreSkillArchiveInput) { input.S3Key = "" }},
		{"zero size", func(input *FilestoreSkillArchiveInput) { input.SizeBytes = 0 }},
		{"short checksum", func(input *FilestoreSkillArchiveInput) { input.SHA256 = "abc" }},
		{"non-hex checksum", func(input *FilestoreSkillArchiveInput) { input.SHA256 = strings.Repeat("z", 64) }},
	} {
		t.Run("rejects "+test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if err := validateFilestoreSkillArchiveInput(input); err == nil {
				t.Fatal("validation error = nil")
			}
		})
	}
	t.Run("accepts valid", func(t *testing.T) {
		if err := validateFilestoreSkillArchiveInput(valid); err != nil {
			t.Fatalf("validation error = %v", err)
		}
	})
}
