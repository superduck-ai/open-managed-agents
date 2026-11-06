package vaults

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPrepareEnvCredentialMount(t *testing.T) {
	t.Parallel()

	_, err := PrepareEnvCredentialMount(false, []db.VaultCredential{readyEnvCredential("TOKEN", "oma_ph_abc")})
	if !errors.Is(err, ErrMITMRequiredForEnvCredentials) {
		t.Fatalf("error = %v, want ErrMITMRequiredForEnvCredentials", err)
	}

	got, err := PrepareEnvCredentialMount(true, []db.VaultCredential{
		readyEnvCredential("SHARED", "oma_ph_first"),
		readyEnvCredential("SHARED", "oma_ph_second"),
		readyEnvCredential("ANTHROPIC_API_KEY", "oma_ph_reserved"),
		readyEnvCredential("anthropic_api_key", "oma_ph_reserved_lower"),
	})
	if err != nil {
		t.Fatalf("PrepareEnvCredentialMount: %v", err)
	}
	if got["SHARED"] != "oma_ph_first" {
		t.Fatalf("SHARED = %q, want first wins", got["SHARED"])
	}
	if _, ok := got["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("reserved name must not be provisioned: %#v", got)
	}
	if _, ok := got["anthropic_api_key"]; ok {
		t.Fatalf("reserved name case variant must not be provisioned: %#v", got)
	}
}

func readyEnvCredential(secretName, placeholder string) db.VaultCredential {
	auth, _ := json.Marshal(map[string]any{
		"type":               "environment_variable",
		"secret_name":        secretName,
		"placeholder":        placeholder,
		"networking":         map[string]any{"type": "unrestricted"},
		"injection_location": map[string]any{"header": true, "body": false},
	})
	return db.VaultCredential{AuthType: "environment_variable", Auth: auth}
}

func TestPrepareEnvCredentialMountMergesEnvOAuth(t *testing.T) {
	t.Parallel()

	got, err := PrepareEnvCredentialMount(true, []db.VaultCredential{
		readyEnvCredential("SHARED", "oma_ph_env"),
		envOAuthCredentialRow("SHARED", "oma_ph_oauth"),
		envOAuthCredentialRow("LARKSUITE_CLI_USER_ACCESS_TOKEN", "oma_ph_feishu"),
	})
	if err != nil {
		t.Fatalf("PrepareEnvCredentialMount: %v", err)
	}
	if got["SHARED"] != "oma_ph_env" {
		t.Fatalf("SHARED = %q, want environment_variable priority", got["SHARED"])
	}
	if got["LARKSUITE_CLI_USER_ACCESS_TOKEN"] != "oma_ph_feishu" {
		t.Fatalf("LARKSUITE_CLI_USER_ACCESS_TOKEN = %q", got["LARKSUITE_CLI_USER_ACCESS_TOKEN"])
	}

	_, err = PrepareEnvCredentialMount(false, []db.VaultCredential{envOAuthCredentialRow("TOKEN", "oma_ph_oauth")})
	if !errors.Is(err, ErrMITMRequiredForEnvCredentials) {
		t.Fatalf("error = %v, want ErrMITMRequiredForEnvCredentials", err)
	}
}
