package vaults

import "github.com/superduck-ai/open-managed-agents/internal/db"

// PrepareEnvCredentialMount is the Session-mount seam for Environment Variable
// Credentials: reject when MITM is disabled and any env credential is attached,
// reject legacy credentials missing Opaque Placeholder / Injection Location, and
// return secret_name → placeholder for sandbox startup (Vault Attachment Order,
// first secret_name wins; Platform Reserved Environment Names are never set).
func PrepareEnvCredentialMount(mitmEnabled bool, credentials []db.VaultCredential) (map[string]string, error) {
	placeholders := make(map[string]string)
	hasEnv := false
	for i := range credentials {
		cred := credentials[i]
		if credentialAuthType(cred.AuthType) != credentialAuthTypeEnvironmentVariable {
			continue
		}
		hasEnv = true
		value, err := decodeEnvironmentCredentialAuth(cred.Auth)
		if err != nil {
			return nil, err
		}
		if PlatformReservedSecretName(value.SecretName) {
			continue
		}
		if _, exists := placeholders[value.SecretName]; exists {
			continue
		}
		placeholders[value.SecretName] = value.Placeholder
	}
	if hasEnv && !mitmEnabled {
		return nil, ErrMITMRequiredForEnvCredentials
	}
	return placeholders, nil
}
