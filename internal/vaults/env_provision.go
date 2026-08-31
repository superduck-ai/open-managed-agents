package vaults

import "github.com/superduck-ai/open-managed-agents/internal/db"

// PrepareEnvCredentialMount is the Session-mount seam for Vault credentials on
// Managed Agent startup: reject when MITM is disabled and any environment_variable
// credential is attached (egress substitution needs MITM); reject legacy env
// credentials missing Opaque Placeholder / Injection Location; and return
// secret_name → placeholder for sandbox startup (Vault Attachment Order, first
// secret_name wins; Platform Reserved Environment Names are never set).
// static_bearer / mcp_oauth do not require MITM at mount: without MITM, Session
// still starts and MCP injection stays on the explicit Session MCP HTTP proxy.
func PrepareEnvCredentialMount(mitmEnabled bool, credentials []db.VaultCredential) (map[string]string, error) {
	hasEnv, bound, err := uniqueEnvironmentCredentials(credentials)
	if err != nil {
		return nil, err
	}
	placeholders := make(map[string]string, len(bound))
	for _, item := range bound {
		placeholders[item.value.SecretName] = item.value.Placeholder
	}
	if !mitmEnabled && hasEnv {
		return nil, ErrMITMRequiredForEnvCredentials
	}
	return placeholders, nil
}
