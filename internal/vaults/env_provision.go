package vaults

import "github.com/superduck-ai/open-managed-agents/internal/db"

// PrepareEnvCredentialMount is the Session-mount seam for Environment Variable
// Credentials: reject when MITM is disabled and any env credential is attached,
// reject legacy credentials missing Opaque Placeholder / Injection Location, and
// return secret_name → placeholder for sandbox startup (Vault Attachment Order,
// first secret_name wins; Platform Reserved Environment Names are never set).
func PrepareEnvCredentialMount(mitmEnabled bool, credentials []db.VaultCredential) (map[string]string, error) {
	hasEnv, bound, err := uniqueEnvironmentCredentials(credentials)
	if err != nil {
		return nil, err
	}
	placeholders := make(map[string]string, len(bound))
	for _, item := range bound {
		placeholders[item.value.SecretName] = item.value.Placeholder
	}
	if hasEnv && !mitmEnabled {
		return nil, ErrMITMRequiredForEnvCredentials
	}
	return placeholders, nil
}
