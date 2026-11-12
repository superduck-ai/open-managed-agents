package filestore

import (
	"encoding/json"
	"io"
	"testing"
)

func TestSkillArchiveVersionPathPreservesOriginalBytes(t *testing.T) {
	raw := buildSkillArchiveTestZip(t, map[string]string{"demo/SKILL.md": "# original immutable A", "demo/references/proof.txt": "A exact proof"})
	base := skillArchiveTestEntry(raw)
	base.Path = "/skills/version-fixed"
	for _, metadata := range []string{`{"skill_package_directory":"wrong"}`, `{"skill_package_directory":null}`, `{"skill_package_directory":"../demo"}`, `{"skill_package_directory":5}`} {
		broken := base
		broken.Metadata = json.RawMessage(metadata)
		if _, err := indexSkillArchive(broken, raw); err == nil {
			t.Fatal("bad package metadata accepted", metadata)
		}
	}
	base.Metadata = json.RawMessage(`{"skill_package_directory":"demo"}`)
	archive, err := indexSkillArchive(base, raw)
	if err != nil {
		t.Fatal(err)
	}
	node, ok := archive.nodes["/skills/version-fixed/references/proof.txt"]
	if !ok {
		t.Fatal("prefix remapping lost member")
	}
	reader, err := node.file.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "A exact proof" {
		t.Fatal("archive member changed", err)
	}
	if _, ok := archive.nodes["/skills/demo/SKILL.md"]; ok {
		t.Fatal("old package prefix leaked")
	}
	if string(archive.data) != string(raw) {
		t.Fatal("archive bytes rewritten")
	}
}
