package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestSessionToolsOverrideReferenceAcceptsPublicField(t *testing.T) {
	var ref sessionAgentReference
	raw := `{"type":"agent_with_overrides","id":"agent_fixture","version":3,"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]}`
	if err := json.Unmarshal([]byte(raw), &ref); err != nil {
		t.Fatal(err)
	}
	if err := ref.validateType(); err != nil {
		t.Fatalf("public tools override rejected: %v", err)
	}
}

func TestSessionToolsOverrideReplacesCopyAndPreservesSource(t *testing.T) {
	original := json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"write","enabled":true}]}]`)
	base := db.Agent{ExternalID: "agent_fixture", CurrentVersion: 3, Tools: original, Skills: json.RawMessage(`[]`), MCPServers: json.RawMessage(`[]`)}
	inherited, err := applySessionToolsOverride(base, nil)
	if err != nil || string(inherited.Tools) != string(original) {
		t.Fatal("omitted tools did not inherit", err)
	}
	raw := json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]`)
	effective, err := applySessionToolsOverride(base, raw)
	if err != nil {
		t.Fatal(err)
	}
	if effective.ExternalID != base.ExternalID || effective.CurrentVersion != base.CurrentVersion || string(base.Tools) != string(original) {
		t.Fatal("source identity/config mutated")
	}
	var sets []struct {
		Default map[string]any `json:"default_config"`
		Configs []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"configs"`
	}
	if err = json.Unmarshal(effective.Tools, &sets); err != nil {
		t.Fatal(err)
	}
	read := false
	for _, config := range sets[0].Configs {
		if config.Enabled && config.Name == "write" {
			t.Fatal("replacement merged old Write")
		}
		read = read || (config.Name == "read" && config.Enabled)
	}
	if !read || sets[0].Default["enabled"] != false {
		t.Fatal("Read-only override not applied")
	}
	for _, clear := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[]`)} {
		got, err := applySessionToolsOverride(base, clear)
		if err != nil || string(got.Tools) != "[]" {
			t.Fatal("tools not cleared", string(got.Tools), err)
		}
	}
	base.Skills = json.RawMessage(`[{"id":"skill_fixture","version":1}]`)
	if _, err := applySessionToolsOverride(base, json.RawMessage(`[]`)); err == nil {
		t.Fatal("Read removed while effective skills remain")
	}
}
