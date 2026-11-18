package vaults

import (
	"errors"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var ErrCredentialAuthorizationManaged = errors.New("当前挂载凭据需要由其 Vault 管理者重新授权")

func EnvironmentCredentialPlaceholder(credential db.VaultCredential) (string, error) {
	if credential.AuthType == "env_oauth" {
		placeholder, ok := EnvOAuthCredentialPlaceholder(credential.Auth)
		if !ok {
			return "", errors.New("ENV OAuth 占位符不可用")
		}
		return placeholder, nil
	}
	value, err := decodeEnvironmentCredentialAuth(credential.Auth)
	if err != nil {
		return "", err
	}
	return value.Placeholder, nil
}

func SelectCredentialBindings(scope db.CredentialScope, credentials []db.VaultCredential) []db.VaultCredential {
	if !scope.PinBindings {
		return credentials
	}
	selected := make([]db.VaultCredential, 0, len(credentials))
	seen := make(map[string]bool)
	for _, credential := range credentials {
		if !slices.Contains(scope.VaultIDs, credential.VaultExternalID) {
			continue
		}
		kind := "mcp:"
		if credential.AuthType == "env_oauth" || credential.AuthType == "environment_variable" {
			kind = "env:"
		}
		key := kind + credential.CredentialKey
		if seen[key] {
			continue
		}
		seen[key] = true
		selected = append(selected, credential)
	}
	return selected
}
