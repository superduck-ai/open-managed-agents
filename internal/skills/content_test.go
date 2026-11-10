package skills

import (
	"bytes"
	"context"
	"io"
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
			result, err := handler.packagePreview(t.Context(), tc.bucket, "demo.zip", input, tc.limit)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if err == nil && (len(result.Files) != 2 || !strings.Contains(result.SkillMD, "# Instructions")) {
				t.Fatalf("preview = %+v", result)
			}
		})
	}
}
