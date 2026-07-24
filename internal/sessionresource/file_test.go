package sessionresource

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
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
			if _, err := NormalizeFileSpec(test.fileID, test.source, test.mountPath); err == nil {
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

	spec, err := NormalizeFileSpec("file_test", nil, nil)
	if err != nil {
		t.Fatalf("NormalizeFileSpec(): %v", err)
	}
	if spec.fileID != "file_test" || spec.mountPath != "/file_test" {
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
	stored, err := ParseStoredFileSpec(raw)
	if err != nil {
		t.Fatalf("ParseStoredFileSpec(): %v", err)
	}
	if stored != spec {
		t.Fatalf("stored = %#v, want %#v", stored, spec)
	}

	mount, err := spec.SessionFileMount("sesrsc_test")
	if err != nil {
		t.Fatalf("SessionFileMount(): %v", err)
	}
	if mount.ResourceExternalID != "sesrsc_test" ||
		mount.FileExternalID != "file_test" ||
		mount.Path != "/uploads/file_test" {
		t.Fatalf("mount = %#v", mount)
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

	specs := make([]FileSpec, 0, sandboxmount.MaxFileResources+1)
	for index := 0; index <= sandboxmount.MaxFileResources; index++ {
		specs = append(specs, FileSpec{
			fileID:    "file_" + strings.Repeat("x", index+1),
			mountPath: "/workspace/" + strings.Repeat("x", index+1),
		})
	}
	if err := ValidateFileSpecs(specs); err == nil {
		t.Fatal("ValidateFileSpecs() accepted too many files")
	}
}
