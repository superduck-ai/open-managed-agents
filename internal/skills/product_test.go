package skills

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

func TestProductSkillVersionRejectsMissingVersion(t *testing.T) {
	_, err := productSkillVersion("dream", []byte("---\nname: dream\n---\n\n# Dream\n"))
	if err == nil || !errors.Is(err, errProductSkillVersionMissing) {
		t.Fatalf("productSkillVersion err = %v, want missing version", err)
	}
}

func TestProductSkillVersionReadsFrontmatter(t *testing.T) {
	version, err := productSkillVersion("dream", []byte("---\nname: dream\nversion: 1.0.0\n---\n\n# Dream\n"))
	if err != nil {
		t.Fatalf("productSkillVersion: %v", err)
	}
	if version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", version)
	}
}

func TestPackProductSkillContainsOnlySkillMD(t *testing.T) {
	pkg, err := packProductSkill("dream", []byte("---\nname: dream\nversion: 1.0.0\n---\n\n# Dream\n"))
	if err != nil {
		t.Fatalf("packProductSkill: %v", err)
	}
	if pkg.Directory != "dream" {
		t.Fatalf("directory = %q, want dream", pkg.Directory)
	}
	names := zipEntryNames(t, pkg.Zip)
	if len(names) != 1 || names[0] != "dream/SKILL.md" {
		t.Fatalf("zip entries = %v, want [dream/SKILL.md]", names)
	}
}

func TestLoadProductSkillSourcesReadsOnlySkillMD(t *testing.T) {
	fsys := fstest.MapFS{
		"product/dream/SKILL.md":     {Data: []byte("---\nname: dream\nversion: 1.0.0\n---\n")},
		"product/dream/SKILL.md_bak": {Data: []byte("stale")},
		"product/dream/notes.md":     {Data: []byte("notes")},
	}
	sources, err := loadProductSkillSources(fsys)
	if err != nil {
		t.Fatalf("loadProductSkillSources: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != "dream" {
		t.Fatalf("sources = %+v, want dream only", sources)
	}
	if strings.Contains(string(sources[0].Markdown), "stale") || strings.Contains(string(sources[0].Markdown), "notes") {
		t.Fatalf("loaded markdown included extra files: %q", sources[0].Markdown)
	}
}

func TestLoadProductSkillSourcesRequiresEmbeddedSkills(t *testing.T) {
	_, err := loadProductSkillSources(fstest.MapFS{})
	if err == nil || !strings.Contains(err.Error(), "no product skills") {
		t.Fatalf("loadProductSkillSources err = %v, want no product skills", err)
	}
}

func TestEmbeddedProductSkillsIncludeVersionedDream(t *testing.T) {
	sources, err := loadProductSkillSources(productSkillFS)
	if err != nil {
		t.Fatalf("loadProductSkillSources: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != "dream" {
		t.Fatalf("embedded sources = %+v, want dream", sources)
	}
	version, err := productSkillVersion(sources[0].ID, sources[0].Markdown)
	if err != nil {
		t.Fatalf("productSkillVersion: %v", err)
	}
	if version != "1.0.0" {
		t.Fatalf("embedded dream version = %q, want 1.0.0", version)
	}
	markdown := string(sources[0].Markdown)
	if !strings.Contains(markdown, "/mnt/memory") || !strings.Contains(markdown, "/mnt/transcripts/dream") {
		t.Fatal("embedded dream skill missing OMA mount paths")
	}
	if strings.Contains(markdown, "team/") || !strings.Contains(markdown, "absence of evidence here is not staleness") {
		t.Fatal("embedded dream skill must apply conservative pruning to the whole Store instead of a CLI team/ subdirectory")
	}
	if strings.Contains(markdown, "Use that value for `originSessionId`") || !strings.Contains(markdown, "not memory provenance") {
		t.Fatal("embedded dream skill must forbid deriving originSessionId from JSONL identifiers")
	}
	if !strings.Contains(markdown, `grep -h '"type":"user.message"'`) || !strings.Contains(markdown, "Never `cat` a whole `.jsonl` file") {
		t.Fatal("embedded dream skill missing bounded transcript overview step")
	}
	if !strings.Contains(markdown, "### Surface patterns and playbooks") || !strings.Contains(markdown, "later transcript `created_at` wins") {
		t.Fatal("embedded dream skill missing pattern synthesis or latest-wins rule")
	}
	if strings.Contains(markdown, "will be denied") || strings.Contains(markdown, "Project AGENTS.md instructions are loaded in your system prompt") {
		t.Fatal("embedded dream skill still states CLI-only enforcement or AGENTS.md assumptions")
	}
	if strings.Contains(markdown, "context: fork") || strings.Contains(markdown, "/remember") {
		t.Fatal("embedded dream skill still uses fork or /remember CLI conventions")
	}
}

func TestKeepBuiltinSkillsForPruneIncludesDream(t *testing.T) {
	keep, err := keepBuiltinSkillsForPrune([]string{"xlsx"})
	if err != nil {
		t.Fatalf("keepBuiltinSkillsForPrune: %v", err)
	}
	if len(keep) != 2 || keep[0] != "xlsx" || keep[1] != "dream" {
		t.Fatalf("keep = %v, want [xlsx dream]", keep)
	}
}

func zipEntryNames(t *testing.T, data []byte) []string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry: %v", err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			t.Fatalf("read zip entry: %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("close zip entry: %v", err)
		}
		names = append(names, file.Name)
	}
	return names
}
