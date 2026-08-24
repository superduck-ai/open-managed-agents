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

func TestParseStoredFileSpecRejectsNonCanonicalPayload(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		raw  string
	}{
		{
			name: "missing source",
			raw:  `{"type":"file","file_id":"file_test","mount_path":"/data.csv"}`,
		},
		{
			name: "missing file ID",
			raw:  `{"type":"file","file_id":"","source":"/uploads","mount_path":"/data.csv"}`,
		},
		{
			name: "wrong type",
			raw:  `{"type":"memory_store","file_id":"file_test","source":"/uploads","mount_path":"/data.csv"}`,
		},
		{
			name: "relative mount",
			raw:  `{"type":"file","file_id":"file_test","source":"/uploads","mount_path":"data.csv"}`,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseStoredFileSpec(json.RawMessage(test.raw)); err == nil {
				t.Fatal("ParseStoredFileSpec() succeeded")
			}
		})
	}
}

func TestFileSpecBuildsCanonicalMount(t *testing.T) {
	t.Parallel()

	spec, err := NormalizeFileSpec("file_test", "report.csv", nil, nil)
	if err != nil {
		t.Fatalf("NormalizeFileSpec(): %v", err)
	}
	if spec.fileID != "file_test" || spec.mountPath != "/uploads/report.csv" {
		t.Fatalf("spec = %#v", spec)
	}

	raw := json.RawMessage(`{"type":"file","file_id":"file_test","source":"/uploads","mount_path":"/uploads/report.csv"}`)
	stored, err := ParseStoredFileSpec(raw)
	if err != nil {
		t.Fatalf("ParseStoredFileSpec(): %v", err)
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
		mount.MountPath != "/uploads/report.csv" ||
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
