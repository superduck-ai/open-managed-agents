package webhooks

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"testing"
)

func TestConsoleEventCatalogMatchesSubscriptionAllowlist(t *testing.T) {
	source, err := os.ReadFile("../../web/src/features/settings/webhooks/events.ts")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`type:\s*['"]([a-z_]+\.[a-z_]+)['"]`).FindAllSubmatch(source, -1)
	seen := make(map[string]bool)
	for _, match := range matches {
		eventType := string(match[1])
		if seen[eventType] {
			t.Errorf("duplicate Console event: %s", eventType)
		}
		seen[eventType] = true
		if _, ok := supportedEndpointEventTypes[eventType]; !ok {
			t.Errorf("Console event rejected by API: %s", eventType)
		}
	}
	for eventType := range supportedEndpointEventTypes {
		if !seen[eventType] {
			t.Errorf("API event missing from Console: %s", eventType)
		}
	}
	if len(seen) != 35 {
		t.Errorf("Console event count=%d, want 35", len(seen))
	}
}

func TestOpenAPIEventCatalogMatchesSubscriptionAllowlist(t *testing.T) {
	for _, language := range []string{"en", "zh"} {
		t.Run(language, func(t *testing.T) {
			source, err := os.ReadFile("../../openapi/oma." + language + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Components struct {
					Schemas map[string]struct {
						Required   []string `json:"required"`
						Properties map[string]struct {
							Items struct {
								Enum []string `json:"enum"`
							} `json:"items"`
						} `json:"properties"`
					} `json:"schemas"`
				} `json:"components"`
			}
			if err := json.Unmarshal(source, &document); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Webhook", "WebhookCreate", "WebhookUpdate"} {
				events := document.Components.Schemas[name].Properties["enabled_events"].Items.Enum
				seen := make(map[string]bool)
				for _, event := range events {
					if seen[event] {
						t.Errorf("%s duplicates %s", name, event)
					}
					seen[event] = true
					if _, ok := supportedEndpointEventTypes[event]; !ok {
						t.Errorf("%s documents unsupported event %s", name, event)
					}
				}
				for event := range supportedEndpointEventTypes {
					if !seen[event] {
						t.Errorf("%s missing %s", name, event)
					}
				}
			}
			required := document.Components.Schemas["WebhookCreate"].Required
			if slices.Contains(required, "name") || !slices.Contains(required, "url") || !slices.Contains(required, "enabled_events") {
				t.Fatalf("incorrect required create fields: %v", required)
			}
		})
	}
}
