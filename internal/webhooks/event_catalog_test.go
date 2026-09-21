package webhooks

import (
	"os"
	"regexp"
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
	if len(seen) != 21 {
		t.Errorf("Console event count=%d, want 21", len(seen))
	}
}
