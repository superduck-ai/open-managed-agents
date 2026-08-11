package sessionresource

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeFileSpecRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		fileID    string
		source    json.RawMessage
		mountPath json.RawMessage
	}{
		{name: "empty file ID", mountPath: json.RawMessage(`"/workspace/data.csv"`)},
		{name: "null source", fileID: "file_test", source: json.RawMessage(`null`)},
		{name: "other source", fileID: "file_test", source: json.RawMessage(`"/outputs"`)},
		{name: "relative mount", fileID: "file_test", mountPath: json.RawMessage(`"workspace/data.csv"`)},
		{name: "root mount", fileID: "file_test", mountPath: json.RawMessage(`"/"`)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeFileSpec(test.fileID, "data.csv", test.source, test.mountPath); err == nil {
				t.Fatal("NormalizeFileSpec() succeeded")
			}
		})
	}
}

func TestNewStoredFileSpecRejectsNonCanonicalFields(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		fileID    string
		source    string
		mountPath string
	}{
		{
			name:      "missing source",
			fileID:    "file_test",
			mountPath: "/data.csv",
		},
		{
			name:      "missing file ID",
			source:    "/uploads",
			mountPath: "/data.csv",
		},
		{
			name:      "relative mount",
			fileID:    "file_test",
			source:    "/uploads",
			mountPath: "data.csv",
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewStoredFileSpec(test.fileID, test.source, test.mountPath); err == nil {
				t.Fatal("NewStoredFileSpec() succeeded")
			}
		})
	}
}

func TestParseFilePayloadRejectsMismatchedResourceID(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"id":"sesrsc_other",
		"type":"file",
		"file_id":"file_test",
		"source":"/uploads",
		"mount_path":"/workspace/data.csv"
	}`)
	if _, err := ParseFilePayload(raw, "sesrsc_expected"); err == nil {
		t.Fatal("ParseFilePayload() succeeded")
	}
}

func TestFileSpecBuildsCanonicalPayloadAndMount(t *testing.T) {
	t.Parallel()

	spec, err := NormalizeFileSpec("file_test", "report.csv", nil, nil)
	if err != nil {
		t.Fatalf("NormalizeFileSpec(): %v", err)
	}
	if spec.fileID != "file_test" || spec.mountPath != "/uploads/report.csv" {
		t.Fatalf("spec = %#v", spec)
	}

	fields := spec.PayloadFields("sesrsc_test")
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal fields: %v", err)
	}
	parsed, err := ParseFilePayload(raw, "sesrsc_test")
	if err != nil {
		t.Fatalf("ParseFilePayload(): %v", err)
	}
	if parsed != spec {
		t.Fatalf("parsed = %#v, want %#v", parsed, spec)
	}
	stored, err := NewStoredFileSpec("file_test", "/uploads", "/uploads/report.csv")
	if err != nil {
		t.Fatalf("NewStoredFileSpec(): %v", err)
	}
	if stored != spec {
		t.Fatalf("stored = %#v, want %#v", stored, spec)
	}

	mount, err := spec.SessionFileBinding("sesrsc_test")
	if err != nil {
		t.Fatalf("SessionFileBinding(): %v", err)
	}
	if mount.ResourceID != "sesrsc_test" ||
		mount.FileID != "file_test" ||
		mount.Path != "/uploads/report.csv" {
		t.Fatalf("mount = %#v", mount)
	}
}

func TestDefaultGitHubRepositoryMountPath(t *testing.T) {
	t.Parallel()

	if got := DefaultGitHubRepositoryMountPath("://invalid"); got != "/workspace/repository" {
		t.Fatalf("invalid repository URL mount path = %q", got)
	}
	if got := DefaultGitHubRepositoryMountPath("https://github.com/example/widgets.git"); got != "/workspace/widgets" {
		t.Fatalf("default repository mount path = %q", got)
	}
}

func TestValidateFileSpecsRejectsAggregateConflicts(t *testing.T) {
	t.Parallel()

	if err := ValidateFileSpecs([]FileSpec{
		{fileID: "file_first", mountPath: "/workspace/data"},
		{fileID: "file_second", mountPath: "/workspace/data/child"},
	}); err == nil {
		t.Fatal("ValidateFileSpecs() accepted ancestor conflict")
	}

	specs := make([]FileSpec, 0, MaxFileResources+1)
	for index := 0; index <= MaxFileResources; index++ {
		specs = append(specs, FileSpec{
			fileID:    "file_" + strings.Repeat("x", index+1),
			mountPath: "/workspace/" + strings.Repeat("x", index+1),
		})
	}
	if err := ValidateFileSpecs(specs); err == nil {
		t.Fatal("ValidateFileSpecs() accepted too many files")
	}
}
