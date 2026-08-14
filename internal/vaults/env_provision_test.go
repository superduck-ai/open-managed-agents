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
