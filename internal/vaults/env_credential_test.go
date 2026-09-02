package vaults

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestParseSecretName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "empty", input: "", wantErr: "must be non-empty"},
		{name: "whitespace only", input: "   ", wantErr: "must be non-empty"},
		{name: "leading digit", input: "1TOKEN", wantErr: "POSIX environment variable name"},
		{name: "equals", input: "FOO=BAR", wantErr: "POSIX environment variable name"},
		{name: "hyphen", input: "FOO-BAR", wantErr: "POSIX environment variable name"},
		{name: "too long", input: strings.Repeat("A", maxSecretNameLength+1), wantErr: "at most 255 characters"},
		{name: "reserved", input: "CLAUDE_CODE_REMOTE", wantErr: "is reserved"},
		{name: "reserved lowercase", input: "claude_code_remote", wantErr: "is reserved"},
		{name: "reserved with surrounding space", input: " CLAUDE_CODE_REMOTE ", wantErr: "is reserved"},
		{name: "trim surrounding space", input: " TOKEN ", want: "TOKEN"},
		{name: "lowercase user name", input: "my_api_key", want: "my_api_key"},
		{name: "leading underscore", input: "_PRIVATE", want: "_PRIVATE"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseSecretName(test.input)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseSecretName(%q) error = %v, want %q", test.input, err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSecretName(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("parseSecretName(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

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

	trimmed, err := normalizeCredentialAuthForCreate(json.RawMessage(`{
		"type":"environment_variable",
		"secret_name":" NOTION_API_KEY ",
		"secret_value":"ntn_secret",
		"networking":{"type":"unrestricted"}
	}`))
	if err != nil {
		t.Fatalf("normalize trimmed create: %v", err)
	}
	trimmedAuth, err := decodeCredentialAuth(trimmed.PublicAuth)
	if err != nil {
		t.Fatalf("decode trimmed: %v", err)
	}
	if name := trimmedAuth.value.(*environmentVariableCredentialAuth).SecretName; name != "NOTION_API_KEY" {
		t.Fatalf("secret_name = %q, want trimmed", name)
	}
}

func TestApplyInjectionLocation(t *testing.T) {
	t.Parallel()
	headerOnly := credentialInjectionLocation{Header: true, Body: false}

	_, err := applyInjectionLocation(headerOnly, json.RawMessage(`null`))
	if err == nil || !strings.Contains(err.Error(), "omitted instead of null") {
		t.Fatalf("null error = %v", err)
	}
	_, err = applyInjectionLocation(headerOnly, json.RawMessage(`{"header":false,"body":false}`))
	if err == nil || !strings.Contains(err.Error(), "must enable header or body") {
		t.Fatalf("disabled error = %v", err)
	}

	got, err := applyInjectionLocation(headerOnly, nil)
	if err != nil {
		t.Fatalf("omit: %v", err)
	}
	if !got.Header || got.Body {
		t.Fatalf("omit = %+v, want header-only", got)
	}
	got, err = applyInjectionLocation(headerOnly, json.RawMessage(`{"body":true}`))
	if err != nil {
		t.Fatalf("patch body: %v", err)
	}
	if !got.Header || !got.Body {
		t.Fatalf("patch body = %+v", got)
	}
}

func TestNormalizeEnvironmentVariableForUpdateRejectsLegacy(t *testing.T) {
	t.Parallel()
	_, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "environment_variable",
		CredentialKey: "TOKEN",
		Auth:          json.RawMessage(`{"type":"environment_variable","secret_name":"TOKEN","networking":{"type":"unrestricted"}}`),
	}, []byte(`{"type":"environment_variable","secret_value":"secret"}`), json.RawMessage(`{"type":"environment_variable"}`))
	if err == nil || !strings.Contains(err.Error(), "missing placeholder") {
		t.Fatalf("error = %v, want missing placeholder", err)
	}
}
