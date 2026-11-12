//go:build integration

package environments

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositorySkillScanUsesActualCheckoutAndOneLevelFrontmatter(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		".claude/skills/a/SKILL.md":           "---\nname: same\ndescription: |\n  Actual checkout description\n  continued\n---\nPRIVATE_BODY_NOT_CATALOGUED",
		".claude/skills/b/SKILL.md":           "---\r\nname: same\r\ndescription: second path\r\n---\r\nbody",
		".claude/skills/missing/SKILL.md":     "not frontmatter",
		".claude/skills/invalid/SKILL.md":     "---\nname: [wrong]\ndescription: nope\n---\nbody",
		".claude/skills/SKILL.md":             "---\nname: root\ndescription: nope\n---\nbody",
		".claude/skills/nested/deep/SKILL.md": "---\nname: nested\ndescription: nope\n---\nbody",
		"skills/foreign/SKILL.md":             "---\nname: foreign\ndescription: nope\n---\nbody",
	} {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	roots, _ := json.Marshal([]string{root})
	cmd := exec.CommandContext(context.Background(), "python3", "-c", repositorySkillScanScript)
	cmd.Stdin = bytes.NewReader(roots)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	skills, err := parseRepositorySkillScan(output, []string{root})
	if err != nil || len(skills) != 2 {
		t.Fatal("actual repository discovery failed", err, skills)
	}
	if skills[0].Name != "same" || skills[1].Name != "same" || skills[0].Path == skills[1].Path || skills[0].Description != "Actual checkout description\ncontinued" {
		t.Fatal(skills)
	}
	if string(output) == "" || strings.Contains(string(output), "PRIVATE_BODY_NOT_CATALOGUED") {
		t.Fatal("missing actual scan")
	}
}
