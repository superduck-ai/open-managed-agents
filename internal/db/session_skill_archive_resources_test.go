package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionSkillArchiveResourceMapperBuildsPostgresArguments(t *testing.T) {
	workspaceUUID := "00000000-0000-4000-8000-000000000042"
	sessionUUID := "00000000-0000-4000-8000-000000000044"
	filesystemUUID := "00000000-0000-4000-8000-000000000043"
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	retireParams := sessionSkillArchiveRetireParams{
		WorkspaceUUID: workspaceUUID,
		SessionUUID:   sessionUUID,
		Now:           now,
	}
	insertParams := sessionSkillArchiveInsertParams{
		ResourceUUID:       "00000000-0000-4000-8000-000000000047",
		ResourceExternalID: "sesrsc_011CZkZBJq5dWxk9fVLNcPht",
		FileUUID:           "00000000-0000-4000-8000-000000000048",
		FileExternalID:     "file_011CZkZBJq5dWxk9fVLNcPht",
		WorkspaceUUID:      workspaceUUID,
		SessionUUID:        sessionUUID,
		EntryPath:          "/skills/demo",
		Filename:           "demo.zip",
		Source:             "custom",
		SizeBytes:          1024,
		SHA256:             strings.Repeat("a", 64),
		S3Bucket:           "skills",
		S3Key:              "skills/demo.zip",
		Now:                now,
	}
	tests := []struct {
		name         string
		bound        yourbatis.BoundSQL
		wantArgCount int
	}{
		{
			"filesystem",
			buildFilestoreFilesystemMapperFindSessionFilesystemByExternalID(yourbatis.DialectPostgres, workspaceUUID, "session_41"),
			3,
		},
		{"retire", buildSessionResourceMapperRetireSkillArchiveResources(yourbatis.DialectPostgres, retireParams), 4},
		{"retire files", buildFileMapperRetireSkillArchiveFiles(yourbatis.DialectPostgres, retireParams), 4},
		{"insert file", buildFileMapperInsertSkillArchiveFile(yourbatis.DialectPostgres, insertParams), 11},
		{"insert", buildSessionResourceMapperInsertSkillArchiveResource(yourbatis.DialectPostgres, insertParams), 8},
		{"list", buildSessionResourceFileMapperListSkillArchiveResources(yourbatis.DialectPostgres, workspaceUUID, filesystemUUID), 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.bound.Args) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(test.bound.Args), test.wantArgCount)
			}
		})
	}
}

func TestNormalizeSessionSkillArchiveResources(t *testing.T) {
	valid := SessionSkillArchiveResourceInput{
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
		mutate func(*SessionSkillArchiveResourceInput)
	}{
		{"unsupported source", func(input *SessionSkillArchiveResourceInput) { input.Source = "other" }},
		{"nested directory", func(input *SessionSkillArchiveResourceInput) { input.Directory = "demo/nested" }},
		{"missing version", func(input *SessionSkillArchiveResourceInput) { input.SkillVersionUUID = "" }},
		{"invalid version", func(input *SessionSkillArchiveResourceInput) { input.SkillVersionUUID = "not-a-uuid" }},
		{"missing object key", func(input *SessionSkillArchiveResourceInput) { input.S3Key = "" }},
		{"zero size", func(input *SessionSkillArchiveResourceInput) { input.SizeBytes = 0 }},
		{"short checksum", func(input *SessionSkillArchiveResourceInput) { input.SHA256 = "abc" }},
		{"non-hex checksum", func(input *SessionSkillArchiveResourceInput) { input.SHA256 = strings.Repeat("z", 64) }},
	} {
		t.Run("rejects "+test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := normalizeSessionSkillArchiveResources([]SessionSkillArchiveResourceInput{input}); err == nil {
				t.Fatal("validation error = nil")
			}
		})
	}
	t.Run("rejects duplicate path", func(t *testing.T) {
		second := valid
		second.SkillVersionUUID = "00000000-0000-4000-8000-000000000045"
		if _, err := normalizeSessionSkillArchiveResources([]SessionSkillArchiveResourceInput{
			valid,
			second,
		}); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("error = %v, want ErrDuplicate", err)
		}
	})
	t.Run("rejects duplicate version", func(t *testing.T) {
		second := valid
		second.Directory = "other"
		if _, err := normalizeSessionSkillArchiveResources([]SessionSkillArchiveResourceInput{
			valid,
			second,
		}); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("error = %v, want ErrDuplicate", err)
		}
	})
	t.Run("normalizes valid Resource", func(t *testing.T) {
		entries, err := normalizeSessionSkillArchiveResources([]SessionSkillArchiveResourceInput{valid})
		if err != nil {
			t.Fatalf("validation error = %v", err)
		}
		if len(entries) != 1 ||
			entries[0].Path != "/skills/demo" ||
			entries[0].Filename != "demo.zip" ||
			entries[0].SHA256 != strings.Repeat("a", 64) {
			t.Fatalf("normalized entries = %#v", entries)
		}
	})
}

func TestFilestoreArchiveResourceDoesNotOwnCatalogBytes(t *testing.T) {
	sizeBytes := int64(1024)
	resource := SessionResourceFile{
		Kind:      SessionResourceFileKindArchive,
		SizeBytes: &sizeBytes,
	}
	if got := resource.OwnedBytes(); got != 0 {
		t.Fatalf("OwnedBytes() = %d, want 0", got)
	}
}
