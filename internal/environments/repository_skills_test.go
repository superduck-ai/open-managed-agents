package environments

import "testing"

func TestRepositorySkillScanRejectsUnknownPathsAndCommandFailures(t *testing.T) {
	for _, value := range []string{
		`[{"path":"/foreign/.claude/skills/a/SKILL.md","frontmatter":"name: foreign\ndescription: invalid"}]`,
		`[{"path":"/repo/.claude/skills/tools/a/SKILL.md","frontmatter":"name: nested\ndescription: invalid"}]`,
		`[{"path":"/repo/.claude/skills/../a/SKILL.md","frontmatter":"name: dirty\ndescription: invalid"}]`,
	} {
		if _, err := parseRepositorySkillScan([]byte(value), []string{"/repo"}); err == nil {
			t.Fatal("foreign or noncanonical source accepted", value)
		}
	}
	if _, err := parseRepositorySkillScan([]byte("not json"), []string{"/repo"}); err == nil {
		t.Fatal("invalid scan result accepted")
	}
}

func TestRepositorySkillMetadataKeepsDistinctPaths(t *testing.T) {
	rows := []byte(`[{"path":"/repo/.claude/skills/a/SKILL.md","frontmatter":"name: same\ndescription: |\n  first line\n  second line\n"},{"path":"/repo/.claude/skills/b/SKILL.md","frontmatter":"name: same\ndescription: second"},{"path":"/repo/.claude/skills/c/SKILL.md","frontmatter":"name: [invalid]\ndescription: bad"}]`)
	entries, err := parseRepositorySkillScan(rows, []string{"/repo"})
	if err != nil || len(entries) != 2 || entries[0].Name != "same" || entries[1].Name != "same" || entries[0].Path == entries[1].Path || entries[0].Description != "first line\nsecond line" {
		t.Fatal(entries, err)
	}
}
