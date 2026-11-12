package agentsnapshot

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestToolResponseDoesNotInventOrOverrideExplicitSchema(t *testing.T) {
	for _, raw := range []string{
		`{`, `null`, `{}`, `[{"type":"custom","name":"read","input_schema":{"type":"object"}}]`,
		`[{"type":"mcp_toolset","configs":[{"name":"read"}]}]`,
		`[{"type":"agent_toolset_20260401","configs":[{"enabled":true}]}]`,
		`[{"type":"agent_toolset_20260401","configs":[{"name":"read","type":"write"}]}]`,
		`[{"type":"agent_toolset_20260401","configs":[{"name":"read","type":null}]}]`,
	} {
		if got := ToolsForResponse(json.RawMessage(raw)); string(got) != raw {
			t.Fatal("changed explicit/nonbuiltin/invalid data", string(got))
		}
	}
}

func TestToolResponseProjectsEightActualColdPoliciesWithoutChangingPolicy(t *testing.T) {
	data, err := os.ReadFile("testdata/907e-cold-tool-configs.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		CaseID string          `json:"case_id"`
		Tools  json.RawMessage `json:"tools"`
	}
	if err = json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 8 {
		t.Fatal("missing real baselines")
	}
	for _, record := range records {
		t.Run(record.CaseID, func(t *testing.T) {
			original := string(record.Tools)
			projected := ToolsForResponse(record.Tools)
			if string(record.Tools) != original {
				t.Fatal("mutated persisted input bytes")
			}
			var before, after []map[string]any
			json.Unmarshal(record.Tools, &before)
			json.Unmarshal(projected, &after)
			count := 0
			for _, tool := range after {
				for _, entry := range tool["configs"].([]any) {
					c := entry.(map[string]any)
					if c["type"] != c["name"] {
						t.Fatal("missing/wrong tool discriminator", c)
					}
					delete(c, "type")
					count++
				}
			}
			if count != 2 || !reflect.DeepEqual(before, after) {
				t.Fatal("changed enabled/default/policy or another field", before, after)
			}
			if string(ToolsForResponse(projected)) != string(projected) {
				t.Fatal("not idempotent")
			}
		})
	}
}

func TestAgentResponseToolsPreservesSnapshotIdentityUnknownFieldsAndStoredBytes(t *testing.T) {
	raw := json.RawMessage(`{"id":"agent-owned","version":7,"type":"agent","skills":[{"type":"custom","skill_id":"skill-owned","version":"version-owned"}],"metadata":{"extension":true},"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true,"permission_policy":{"type":"always_ask"},"extension":{"x":1}}]}]}`)
	original := string(raw)
	projected := ForResponse(raw)
	if string(raw) != original {
		t.Fatal("changed persisted snapshot bytes")
	}
	var before, after map[string]any
	json.Unmarshal(raw, &before)
	json.Unmarshal(projected, &after)
	tool := after["tools"].([]any)[0].(map[string]any)
	c := tool["configs"].([]any)[0].(map[string]any)
	if c["type"] != "read" || c["permission_policy"].(map[string]any)["type"] != "always_ask" {
		t.Fatal("lost discriminator/policy", c)
	}
	delete(c, "type")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("snapshot or unknown fields changed")
	}
	if string(ForResponse(projected)) != string(projected) {
		t.Fatal("snapshot projection not idempotent")
	}
	for _, plain := range []string{`null`, `{}`, `{"id":"agent-only"}`, `{"tools":null}`} {
		if got := ForResponse(json.RawMessage(plain)); string(got) != plain {
			t.Fatal("invented missing snapshot config", string(got))
		}
	}
}
