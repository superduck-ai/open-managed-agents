package db

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFilestoreSkillArchiveEntryQueriesUseSQLXNamedParameters(t *testing.T) {
	arguments := map[string]any{
		"session_external_id":          "session_41",
		"organization_uuid":            "00000000-0000-4000-8000-000000000041",
		"workspace_uuid":               "00000000-0000-4000-8000-000000000042",
		"filesystem_uuid":              "00000000-0000-4000-8000-000000000043",
		"source":                       "custom",
		"skill_version_uuid":           "00000000-0000-4000-8000-000000000044",
		"entry_path":                   "/skills/demo",
		"s3_bucket":                    "skills",
		"s3_key":                       "skills/demo.zip",
		"size_bytes":                   int64(1024),
		"sha256":                       strings.Repeat("a", 64),
		"created_by_api_key_uuid":      "00000000-0000-4000-8000-000000000045",
		"created_by_session_uuid":      "00000000-0000-4000-8000-000000000046",
		"created_by_code_session_uuid": nil,
		"now":                          time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name         string
		query        string
		wantArgCount int
	}{
		{"filesystem", filestoreSkillArchiveEntryFilesystemQuery, 3},
		{"retire", filestoreSkillArchiveEntryRetireQuery, 4},
		{"insert", filestoreSkillArchiveEntryInsertQuery, 15},
		{"list", filestoreSkillArchiveEntryListQuery, 2},
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

func TestNormalizeFilestoreSkillArchiveEntries(t *testing.T) {
	valid := FilestoreSkillArchiveEntryInput{
		Source:           "custom",
		SkillVersionUUID: "00000000-0000-4000-8000-000000000044",
		Directory:        "demo",
		S3Bucket:         "skills",
		S3Key:            "skills/demo.zip",
		SizeBytes:        1024,
		SHA256:           strings.Repeat("A", 64),
	}
	for _, test := range []struct {
		name   string
		mutate func(*FilestoreSkillArchiveEntryInput)
	}{
		{"unsupported source", func(input *FilestoreSkillArchiveEntryInput) { input.Source = "other" }},
		{"nested directory", func(input *FilestoreSkillArchiveEntryInput) { input.Directory = "demo/nested" }},
		{"missing version", func(input *FilestoreSkillArchiveEntryInput) { input.SkillVersionUUID = "" }},
		{"invalid version", func(input *FilestoreSkillArchiveEntryInput) { input.SkillVersionUUID = "not-a-uuid" }},
		{"missing object key", func(input *FilestoreSkillArchiveEntryInput) { input.S3Key = "" }},
		{"zero size", func(input *FilestoreSkillArchiveEntryInput) { input.SizeBytes = 0 }},
		{"short checksum", func(input *FilestoreSkillArchiveEntryInput) { input.SHA256 = "abc" }},
		{"non-hex checksum", func(input *FilestoreSkillArchiveEntryInput) { input.SHA256 = strings.Repeat("z", 64) }},
	} {
		t.Run("rejects "+test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := normalizeFilestoreSkillArchiveEntries([]FilestoreSkillArchiveEntryInput{input}); err == nil {
				t.Fatal("validation error = nil")
			}
		})
	}
	t.Run("rejects duplicate path", func(t *testing.T) {
		second := valid
		second.SkillVersionUUID = "00000000-0000-4000-8000-000000000045"
		if _, err := normalizeFilestoreSkillArchiveEntries([]FilestoreSkillArchiveEntryInput{
			valid,
			second,
		}); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("error = %v, want ErrDuplicate", err)
		}
	})
	t.Run("rejects duplicate version", func(t *testing.T) {
		second := valid
		second.Directory = "other"
		if _, err := normalizeFilestoreSkillArchiveEntries([]FilestoreSkillArchiveEntryInput{
			valid,
			second,
		}); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("error = %v, want ErrDuplicate", err)
		}
	})
	t.Run("normalizes valid entry", func(t *testing.T) {
		entries, err := normalizeFilestoreSkillArchiveEntries([]FilestoreSkillArchiveEntryInput{valid})
		if err != nil {
			t.Fatalf("validation error = %v", err)
		}
		if len(entries) != 1 ||
			entries[0].Path != "/skills/demo" ||
			entries[0].SHA256 != strings.Repeat("a", 64) {
			t.Fatalf("normalized entries = %#v", entries)
		}
	})
}

func TestFilestoreArchiveEntryDoesNotOwnCatalogBytes(t *testing.T) {
	sizeBytes := int64(1024)
	entry := FilestoreEntry{
		Kind:      FilestoreEntryKindArchive,
		SizeBytes: &sizeBytes,
	}
	if got := entry.OwnedBytes(); got != 0 {
		t.Fatalf("OwnedBytes() = %d, want 0", got)
	}
}
