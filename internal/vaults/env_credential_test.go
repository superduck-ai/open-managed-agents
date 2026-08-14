package vaults

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeEnvironmentVariableForCreate(t *testing.T) {
	t.Parallel()

	_, err := normalizeCredentialAuthForCreate(json.RawMessage(`{
		"type":"environment_variable",
		"secret_name":"NOTION_API_KEY",
		"secret_value":"ntn_secret"
	}`))
	if err == nil || !strings.Contains(err.Error(), "auth.networking") {
		t.Fatalf("error = %v, want auth.networking required", err)
	}

	state, err := normalizeCredentialAuthForCreate(json.RawMessage(`{
		"type":"environment_variable",
		"secret_name":"NOTION_API_KEY",
		"secret_value":"ntn_secret",
		"networking":{"type":"unrestricted"}
	}`))
	if err != nil {
		t.Fatalf("normalize create: %v", err)
	}
	auth, err := decodeCredentialAuth(state.PublicAuth)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	value := auth.value.(*environmentVariableCredentialAuth)
	if !value.InjectionLocation.Header || value.InjectionLocation.Body {
		t.Fatalf("injection_location = %+v", value.InjectionLocation)
	}
	if !strings.HasPrefix(value.Placeholder, opaquePlaceholderPrefix) {
		t.Fatalf("placeholder = %q", value.Placeholder)
	}
}
