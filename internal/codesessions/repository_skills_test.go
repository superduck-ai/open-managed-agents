package codesessions

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRepositorySkillRootsRejectInvalidAndUnrelatedConfiguration(t *testing.T) {
	for _, raw := range []string{
		`{"worker_protocol":"dsr1","runtime_tools":["Read"],"sources":[{"type":"git_repository","mount_path":"relative"}]}`,
		`{"worker_protocol":"dsr1","runtime_tools":["Read"],"sources":[{"type":"git_repository","mount_path":"/repo/../other"}]}`,
	} {
		if _, err := repositorySkillRoots(json.RawMessage(raw)); err == nil {
			t.Fatal("invalid root accepted")
		}
	}
	for _, raw := range []string{
		`{"worker_protocol":"ccrv2","runtime_tools":["Read"],"sources":[{"type":"git_repository","mount_path":"/repo"}]}`,
		`{"worker_protocol":"dsr1","runtime_tools":["Bash"],"sources":[{"type":"git_repository","mount_path":"/repo"}]}`,
		`{"worker_protocol":"dsr1","runtime_tools":["Read"],"sources":[{"type":"file","mount_path":"/repo"}]}`,
	} {
		roots, err := repositorySkillRoots(json.RawMessage(raw))
		if err != nil || len(roots) != 0 {
			t.Fatal("unrelated configuration triggered scanning", err, roots)
		}
	}
}

func TestRepositoryCatalogRetainsAttachedSkillsAndUnknownConfiguration(t *testing.T) {
	raw := json.RawMessage(`{"worker_protocol":"dsr1","runtime_tools":["Read"],"sources":[{"type":"git_repository","mount_path":"/repo"},{"type":"git_repository","mount_path":"/other"}],"runtime_skills":[{"name":"same","path":"/attached/SKILL.md","version":"7","extension":9007199254740993}],"extension":9007199254740993}`)
	before := string(raw)
	roots, err := repositorySkillRoots(raw)
	if err != nil || !reflect.DeepEqual(roots, []string{"/repo", "/other"}) {
		t.Fatal(roots, err)
	}
	merged, err := mergeRepositorySkills(raw, []RepositorySkill{{Name: "same", Description: "repository", Path: "/repo/.claude/skills/same/SKILL.md"}, {Name: "same", Description: "second", Path: "/other/.claude/skills/same/SKILL.md"}})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(merged, &fields); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]json.RawMessage
	if err = json.Unmarshal(fields["runtime_skills"], &entries); err != nil || len(entries) != 3 {
		t.Fatal(err, entries)
	}
	if string(entries[0]["extension"]) != "9007199254740993" || string(fields["extension"]) != "9007199254740993" || string(raw) != before {
		t.Fatal("attached fields or exact input changed")
	}
}
