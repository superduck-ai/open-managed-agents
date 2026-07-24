package sandboxmount

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeFileSource(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "another source", raw: json.RawMessage(`"/outputs"`)},
		{name: "non string", raw: json.RawMessage(`42`)},
		{name: "null", raw: json.RawMessage(`null`)},
	} {
		t.Run("rejects "+test.name, func(t *testing.T) {
			if _, err := NormalizeFileSource(test.raw); err == nil {
				t.Fatalf("NormalizeFileSource(%s) succeeded", test.raw)
			}
		})
	}

	t.Run("defaults omitted source", func(t *testing.T) {
		source, err := NormalizeFileSource(nil)
		if err != nil {
			t.Fatalf("NormalizeFileSource(): %v", err)
		}
		if source != FileSource {
			t.Fatalf("source = %q, want %q", source, FileSource)
		}
	})
	t.Run("accepts uploads", func(t *testing.T) {
		source, err := NormalizeFileSource(json.RawMessage(`"/uploads"`))
		if err != nil {
			t.Fatalf("NormalizeFileSource(): %v", err)
		}
		if source != FileSource {
			t.Fatalf("source = %q, want %q", source, FileSource)
		}
	})
}

func TestFileBackingPath(t *testing.T) {
	for _, test := range []struct {
		name      string
		mountPath string
	}{
		{name: "relative", mountPath: "workspace/data.csv"},
		{name: "root", mountPath: "/"},
		{name: "traversal", mountPath: "/workspace/../etc/passwd"},
		{name: "control character", mountPath: "/workspace/data\n.csv"},
	} {
		t.Run("rejects "+test.name, func(t *testing.T) {
			if _, err := FileBackingPath(test.mountPath); err == nil {
				t.Fatalf("FileBackingPath(%q) succeeded", test.mountPath)
			}
		})
	}

	t.Run("maps mount path into uploads", func(t *testing.T) {
		backingPath, err := FileBackingPath("/workspace/data.csv")
		if err != nil {
			t.Fatalf("FileBackingPath(): %v", err)
		}
		if backingPath != "/uploads/workspace/data.csv" {
			t.Fatalf("backing path = %q", backingPath)
		}
	})
	t.Run("keeps runtime-looking names isolated beneath uploads", func(t *testing.T) {
		backingPath, err := FileBackingPath("/tmp/rclone-mount-config.json")
		if err != nil {
			t.Fatalf("FileBackingPath(): %v", err)
		}
		if backingPath != "/uploads/tmp/rclone-mount-config.json" {
			t.Fatalf("backing path = %q", backingPath)
		}
	})
}

func TestValidateFileMountPaths(t *testing.T) {
	t.Run("rejects too many files", func(t *testing.T) {
		mountPaths := make([]string, 0, MaxFileResources+1)
		for index := 0; index <= MaxFileResources; index++ {
			mountPaths = append(mountPaths, "/workspace/"+strings.Repeat("x", index+1))
		}
		if err := ValidateFileMountPaths(mountPaths); err == nil {
			t.Fatal("ValidateFileMountPaths() accepted too many files")
		}
	})
	t.Run("rejects duplicate paths", func(t *testing.T) {
		if err := ValidateFileMountPaths([]string{"/workspace/data.csv", "/workspace/data.csv"}); err == nil {
			t.Fatal("ValidateFileMountPaths() accepted duplicate paths")
		}
	})
	t.Run("rejects ancestry conflicts", func(t *testing.T) {
		if err := ValidateFileMountPaths([]string{"/workspace/data", "/workspace/data/file.csv"}); err == nil {
			t.Fatal("ValidateFileMountPaths() accepted ancestry conflict")
		}
	})
	t.Run("accepts distinct paths", func(t *testing.T) {
		if err := ValidateFileMountPaths([]string{"/workspace/data.csv", "/workspace/config.json"}); err != nil {
			t.Fatalf("ValidateFileMountPaths(): %v", err)
		}
	})
}

func TestDefaultFileMountPath(t *testing.T) {
	if mountPath := DefaultFileMountPath("file_abc123"); mountPath != "/file_abc123" {
		t.Fatalf("default mount path = %q", mountPath)
	}
}
