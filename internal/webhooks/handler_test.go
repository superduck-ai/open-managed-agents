package webhooks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWebhookOptionalFields(t *testing.T) {
	for _, field := range []string{"name", "description"} {
		for _, raw := range []string{"null", "123", "true", "[]", "{}"} {
			t.Run(field+" rejects "+raw, func(t *testing.T) {
				if _, err := parseWebhookOptionalString(map[string]json.RawMessage{field: json.RawMessage(raw)}, field); err == nil {
					t.Fatal("expected a string validation error")
				}
			})
		}
		for _, raw := range []string{`""`, `"   "`} {
			t.Run(field+" accepts empty "+raw, func(t *testing.T) {
				value, err := parseWebhookOptionalString(map[string]json.RawMessage{field: json.RawMessage(raw)}, field)
				if err != nil || value != "" {
					t.Fatalf("value=%q error=%v", value, err)
				}
			})
		}
		value, err := parseWebhookOptionalString(nil, field)
		if err != nil || value != "" {
			t.Fatalf("omitted %s: value=%q error=%v", field, value, err)
		}
	}
	if _, err := parseWebhookRawString(json.RawMessage(`""`), "url"); err == nil {
		t.Fatal("empty URL must still be rejected")
	}
	name, _ := json.Marshal(strings.Repeat("a", 256))
	if _, err := parseWebhookRawString(name, "name"); err == nil {
		t.Fatal("name limit must still apply")
	}
}

func TestWebhookURLValidation(t *testing.T) {
	for _, target := range []string{
		"not-a-url", "http://example.com", "https://example.com:8443",
		"https://user:password@example.com", "https://example.com/path#fragment",
		"https://127.0.0.1", "https://localhost", "https://[::1]",
	} {
		t.Run("reject "+target, func(t *testing.T) {
			if err := validateWebhookURL(target, false); err == nil {
				t.Fatal("expected invalid webhook URL")
			}
		})
	}
	if err := validateWebhookURL("ftp://example.com", true); err == nil {
		t.Fatal("local override must not allow unsupported protocols")
	}
	for _, target := range []string{"https://example.com/hooks", "https://example.com:443/hooks?version=1"} {
		if err := validateWebhookURL(target, false); err != nil {
			t.Fatalf("valid URL rejected: %v", err)
		}
	}
	if err := validateWebhookURL("http://127.0.0.1:18081/hook", true); err != nil {
		t.Fatalf("local override rejected: %v", err)
	}
}
