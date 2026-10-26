package sessions

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionSkillsReferenceAcceptsPublicField(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `[{"type":"custom","skill_id":"skill_new","version":"v2"}]`} {
		ref := sessionAgentReference{Type: "agent_with_overrides", Skills: json.RawMessage(raw)}
		if err := ref.validateType(); err != nil {
			t.Fatalf("valid public skills override rejected: %v", err)
		}
	}
}

func TestSessionSkillsFinalConfigurationAndSourceIsolation(t *testing.T) {
	source := db.Agent{ExternalID: "agent_source", CurrentVersion: 4, Skills: json.RawMessage(`[{"type":"custom","skill_id":"old","version":"v1"}]`), MCPServers: json.RawMessage(`[]`), Tools: json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]`)}
	original := source
	original.Skills = bytes.Clone(source.Skills)
	original.Tools = bytes.Clone(source.Tools)
	original.MCPServers = bytes.Clone(source.MCPServers)
	for _, tc := range []struct {
		name, tools, skills string
		fail                bool
	}{
		{"inherit", "", "", false},
		{"replace", "", `[{"type":"custom","skill_id":"new","version":"v2"}]`, false},
		{"clear-null", "", "null", false},
		{"clear-array", "", "[]", false},
		{"combined-clear", "[]", "null", false},
		{"no-read", "[]", `[{"type":"custom","skill_id":"new"}]`, true},
		{"bad-type", "", `{}`, true},
		{"bad-skill-type", "", `[{"type":"future","skill_id":"new"}]`, true},
		{"bad-id", "", `[{"type":"custom","skill_id":""}]`, true},
		{"bad-version", "", `[{"type":"custom","skill_id":"new","version":3}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			effective, err := applySessionConfigurationOverrides(source, json.RawMessage(tc.tools), nil, json.RawMessage(tc.skills))
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected outcome: %v", err)
			}
			if err == nil {
				if effective.ExternalID != source.ExternalID || effective.CurrentVersion != source.CurrentVersion {
					t.Fatal("identity changed")
				}
				if tc.name == "replace" && string(effective.Skills) == string(source.Skills) {
					t.Fatal("skills not replaced")
				}
				if tc.name == "inherit" && string(effective.Skills) != string(source.Skills) {
					t.Fatal("omitted skills changed")
				}
				if (tc.name == "clear-null" || tc.name == "clear-array" || tc.name == "combined-clear") && string(effective.Skills) != "[]" {
					t.Fatal("clear failed")
				}
			}
			if !reflect.DeepEqual(source, original) {
				t.Fatal("source Agent mutated")
			}
		})
	}
	noRead := source
	noRead.Tools = json.RawMessage(`[]`)
	noRead.Skills = json.RawMessage(`[]`)
	if _, err := applySessionConfigurationOverrides(noRead, nil, nil, json.RawMessage(`[{"type":"custom","skill_id":"new"}]`)); err == nil {
		t.Fatal("adding skills without inherited Read accepted")
	}
}
