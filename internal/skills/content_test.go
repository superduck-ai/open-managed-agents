package skills

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type previewStore struct {
	storage.ObjectStore
	data []byte
}

func (s previewStore) Name() string { return "skills" }
func (s previewStore) Open(context.Context, string, *storage.ByteRange) (storage.Object, error) {
	return storage.Object{Body: io.NopCloser(bytes.NewReader(s.data))}, nil
}

func TestPackagePreviewIntegrityAndContent(t *testing.T) {
	pkg, err := skillPackageFromFiles([]normalizedSkillFile{
		{Name: "demo/SKILL.md", Data: []byte("---\nname: demo\ndescription: Test\n---\n# Instructions\n\nRead references/guide.md.")},
		{Name: "demo/references/guide.md", Data: []byte("# Guide")},
	}, MaxSkillPackageBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nil, previewStore{data: pkg.Zip}, nil)
	for _, tc := range []struct {
		name, bucket, hash string
		limit              int64
		wantError          bool
	}{
		{"wrong bucket", "other", pkg.SHA256, MaxSkillPackageBytes, true},
		{"wrong checksum", "skills", strings.Repeat("0", 64), MaxSkillPackageBytes, true},
		{"oversize archive", "skills", pkg.SHA256, 1, true},
		{"valid content", "skills", pkg.SHA256, MaxSkillPackageBytes, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := pkg
			input.SHA256 = tc.hash
			loaded, err := handler.loadPackage(t.Context(), tc.bucket, "demo.zip", input, tc.limit)
			var result systemPackageResponse
			if err == nil {
				result, err = systemPackageDTO(loaded)
			}
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if err == nil && (len(result.Files) != 2 || !strings.Contains(result.SkillMD, "# Instructions")) {
				t.Fatalf("preview = %+v", result)
			}
		})
	}
}

func TestPackageFilePreview(t *testing.T) {
	files := []normalizedSkillFile{
		{Name: "demo/SKILL.md", Data: []byte("---\nname: demo\ndescription: Test\n---\n# 原文\n")},
		{Name: "demo/scripts/main.py", Data: []byte("print('<script>原文</script>')\r\n")},
		{Name: "demo/Main.java", Data: []byte("class Main {}\n")},
		{Name: "demo/tool.bin", Data: []byte{0x7f, 'E', 'L', 'F', 0, 1}},
		{Name: "demo/invalid.txt", Data: []byte{0xff, 0xfe, 0x01}},
		{Name: "demo/large.txt", Data: bytes.Repeat([]byte("a"), maxSkillFilePreviewBytes+1)},
		{Name: "demo/exact.txt", Data: bytes.Repeat([]byte("a"), maxSkillFilePreviewBytes)},
		{Name: "demo/empty.txt", Data: nil},
	}
	pkg, err := skillPackageFromFiles(files, MaxSkillPackageBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"other/SKILL.md", "demo/../SKILL.md", "demo/./SKILL.md", "demo//SKILL.md", "demo/scripts", "demo/missing.py", "demo\\SKILL.md"} {
		t.Run(path, func(t *testing.T) {
			if _, err := previewPackageFile(pkg, path); err == nil {
				t.Fatal("expected missing or invalid path to be rejected")
			}
		})
	}
	for _, tc := range []struct{ path, reason string }{
		{"demo/tool.bin", "binary"}, {"demo/invalid.txt", "binary"}, {"demo/large.txt", "too_large"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			result, err := previewPackageFile(pkg, tc.path)
			if err != nil || result.Previewable || result.Content != "" || result.Reason != tc.reason {
				t.Fatalf("preview = %+v, error = %v", result, err)
			}
		})
	}
	for _, file := range files {
		switch file.Name {
		case "demo/tool.bin", "demo/invalid.txt", "demo/large.txt":
			continue
		}
		t.Run(file.Name, func(t *testing.T) {
			result, err := previewPackageFile(pkg, file.Name)
			if err != nil || !result.Previewable || result.Content != string(file.Data) || result.Path != file.Name {
				t.Fatalf("unexpected preview of %s, error = %v", file.Name, err)
			}
		})
	}
}

func TestServePackagePreviewAndDownload(t *testing.T) {
	pkg, err := skillPackageFromFiles([]normalizedSkillFile{
		{Name: "demo/SKILL.md", Data: []byte("---\nname: demo\ndescription: Test\n---\n# Instructions")},
		{Name: "demo/tool.bin", Data: []byte{0, 1, 2, 3}},
	}, MaxSkillPackageBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nil, previewStore{data: pkg.Zip}, nil)
	t.Run("corrupt archive cannot be downloaded", func(t *testing.T) {
		input := pkg
		input.SHA256 = strings.Repeat("0", 64)
		w := httptest.NewRecorder()
		err := handler.servePackage(w, httptest.NewRequest(http.MethodGet, "/?download=1", nil), "skills", "key", input, MaxSkillPackageBytes)
		if err == nil || w.Body.Len() != 0 {
			t.Fatal("corrupt archive was served")
		}
	})
	for _, query := range []string{"?download=1", "?path=demo%2Ftool.bin", ""} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := handler.servePackage(w, httptest.NewRequest(http.MethodGet, "/"+query, nil), "skills", "key", pkg, MaxSkillPackageBytes)
			if err != nil {
				t.Fatal(err)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store header")
			}
			if query == "?download=1" {
				if !bytes.Equal(w.Body.Bytes(), pkg.Zip) || w.Header().Get("Content-Disposition") != "attachment; filename=demo.zip" || w.Header().Get("Content-Type") != "application/zip" {
					t.Fatal("download must preserve full archive and use .zip filename")
				}
			} else if query != "" {
				var result skillFilePreview
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Reason != "binary" {
					t.Fatalf("file preview = %+v, error = %v", result, err)
				}
			} else {
				var result systemPackageResponse
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Files) != 2 || !strings.Contains(result.SkillMD, "# Instructions") {
					t.Fatalf("default preview = %+v, error = %v", result, err)
				}
			}
		})
	}
}
