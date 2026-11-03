package agents

import "testing"

func TestAgentToolConfigResponseDiscriminators(t *testing.T) {
	for _, name := range []string{"read", "bash", "write", "edit", "glob", "grep", "web_fetch", "web_search"} {
		configs, err := normalizeAgentToolConfigs([]map[string]any{{"name": name, "enabled": false}}, "always_ask")
		if err != nil {
			t.Fatal(err)
		}
		for _, config := range configs {
			if config["type"] != config["name"] {
				t.Fatalf("missing response discriminator: %v", config)
			}
		}
		if configs[0]["enabled"] != false || permissionPolicyType(configs[0], "") != "always_ask" {
			t.Fatal(configs)
		}
	}
}
