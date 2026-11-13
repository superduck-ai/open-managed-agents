package skills

import (
	"encoding/json/v2"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/superduck-ai/open-managed-agents/skills"
)

func TestBundledPackageRejectsNameMismatch(t *testing.T) {
	files := fstest.MapFS{"demo/SKILL.md": {Data: []byte("---\nname: another\n---\n# Another")}}
	if _, err := readBundledPackage(files, bundledSkill{ID: "demo", Directory: "demo"}); err == nil {
		t.Fatal("accepted mismatched bundled name")
	}
}

func TestBundledCatalogPackages(t *testing.T) {
	data, err := fs.ReadFile(catalog.Files, "catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Skills []bundledSkill `json:"skills"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Skills) != 4 {
		t.Fatalf("bundled count=%d", len(manifest.Skills))
	}
	for _, entry := range manifest.Skills {
		t.Run(entry.ID, func(t *testing.T) {
			pkg, err := readBundledPackage(catalog.Files, entry)
			if err != nil {
				t.Fatal(err)
			}
			if pkg.Name != entry.ID || pkg.Description == "" || pkg.Size == 0 {
				t.Fatalf("incomplete bundled package: %+v", pkg)
			}
			again, err := readBundledPackage(catalog.Files, entry)
			if err != nil || pkg.SHA256 != again.SHA256 {
				t.Fatalf("package bytes changed between loads: %v", err)
			}
			if _, err := skillIconKey(entry.IconKey); err != nil {
				t.Fatal(err)
			}
		})
	}
}
