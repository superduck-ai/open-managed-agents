package skills

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScopedSkillAllocationRejectsAmbiguousInstallations(t *testing.T) {
	first := RuntimeSkill{Source: "custom", SkillID: "skill_a", Directory: "demo", Name: "demo", VersionUUID: "00000000-0000-4000-8000-000000000001", Bindings: []RuntimeSkillBinding{{Scope: ""}}}
	for _, mode := range []string{"same-scope", "same-scope-renamed", "unrelated-skill", "mount-collision"} {
		t.Run(mode, func(t *testing.T) {
			second := first
			second.VersionUUID = "00000000-0000-4000-8000-000000000002"
			second.Bindings = []RuntimeSkillBinding{{Scope: "worker"}}
			switch mode {
			case "same-scope":
				second.Bindings[0].Scope = ""
			case "same-scope-renamed":
				second.Bindings[0].Scope = ""
				second.Directory = "renamed"
				second.Name = "renamed"
			case "unrelated-skill":
				second.SkillID = "skill_b"
			case "mount-collision":
				second.Directory = "other"
				second.Name = "other"
				second.InstallDirectory = first.Directory
			}
			if err := allocateSkillDirectories([]RuntimeSkill{first, second}); err == nil {
				t.Fatal("ambiguous installation accepted")
			}
		})
	}
	for _, order := range []bool{false, true} {
		second := first
		second.VersionUUID = "00000000-0000-4000-8000-000000000002"
		second.Bindings = []RuntimeSkillBinding{{Scope: "worker"}}
		values := []RuntimeSkill{first, second}
		if order {
			values[0], values[1] = values[1], values[0]
		}
		if err := allocateSkillDirectories(values); err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if value.MountDirectory() != "version-"+strings.ReplaceAll(value.VersionUUID, "-", "") || value.Directory != "demo" {
				t.Fatal("order-dependent layout or package directory mutated")
			}
		}
	}
}

func TestRuntimeCatalogsKeepEachBindingAndEmptyChild(t *testing.T) {
	snapshot := json.RawMessage(`{"skills":[],"multiagent":{"agents":[{"id":"agent_worker","version":4,"name":"worker","skills":[]},{"id":"agent_empty","version":2,"name":"empty","skills":[]}]}}`)
	installations := []RuntimeSkill{{Source: "custom", SkillID: "skill_a", Name: "demo", Version: "2", VersionUUID: "root-version", Directory: "demo", InstallDirectory: "version-root", Bindings: []RuntimeSkillBinding{{RequestedVersion: "latest"}, {RequestedVersion: "2"}}}, {Source: "custom", SkillID: "skill_a", Name: "demo", Version: "1", VersionUUID: "child-version", Directory: "demo", InstallDirectory: "version-child", Bindings: []RuntimeSkillBinding{{Scope: "worker", AgentID: "agent_worker", AgentVersion: 4, RequestedVersion: "1"}}}}
	for _, mutate := range []string{"foreign-agent", "wrong-version", "unknown-scope"} {
		broken := append([]RuntimeSkill(nil), installations...)
		broken[1].Bindings = append([]RuntimeSkillBinding(nil), installations[1].Bindings...)
		switch mutate {
		case "foreign-agent":
			broken[1].Bindings[0].AgentID = "other"
		case "wrong-version":
			broken[1].Bindings[0].AgentVersion = 5
		case "unknown-scope":
			broken[1].Bindings[0].Scope = "missing"
		}
		if _, err := RuntimeCatalogs(snapshot, broken); err == nil {
			t.Fatal("unproven scope accepted", mutate)
		}
	}
	catalogs, err := RuntimeCatalogs(snapshot, installations)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalogs[""].Skills) != 1 || catalogs[""].Skills[0].VersionUUID != "root-version" || len(catalogs[""].Skills[0].RequestedVersions) != 2 || len(catalogs["worker"].Skills) != 1 || catalogs["worker"].Skills[0].VersionUUID != "child-version" || catalogs["empty"].Skills == nil || len(catalogs["empty"].Skills) != 0 {
		t.Fatalf("scope bleed or alias loss: %+v", catalogs)
	}
}
