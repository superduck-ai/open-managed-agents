package vaults

import (
	"encoding/json"
	"time"
)

type EnvOAuthCredentialBuildInput struct {
	Provider         string
	SecretName       string
	Placeholder      string
	NetworkingType   string
	AllowedHosts     []string
	AccessToken      string
	RefreshToken     string
	Scope            string
	ExpiresInSeconds int64
	Now              time.Time
}

func BuildEnvOAuthCredentialPayloads(input EnvOAuthCredentialBuildInput) (publicAuth, secretPayload json.RawMessage, err error) {
	secretName, err := parseSecretName(input.SecretName)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := ParseEnvOAuthProviderKey(input.Provider); err != nil {
		return nil, nil, err
	}
	networking, err := normalizeCredentialNetworkingRequired(&credentialNetworkingInput{
		Type:         input.NetworkingType,
		AllowedHosts: input.AllowedHosts,
	})
	if err != nil {
		return nil, nil, err
	}
	placeholder := input.Placeholder
	if placeholder == "" {
		placeholder, err = generateOpaquePlaceholder()
		if err != nil {
			return nil, nil, err
		}
	}
	var expiresAt *string
	if input.ExpiresInSeconds > 0 {
		value := input.Now.UTC().Add(time.Duration(input.ExpiresInSeconds) * time.Second).Format(time.RFC3339)
		expiresAt = &value
	}
	var scope *string
	if input.Scope != "" {
		value := input.Scope
		scope = &value
	}
	public := &envOAuthCredentialAuth{
		Type:              credentialAuthTypeEnvOAuth,
		Provider:          input.Provider,
		SecretName:        secretName,
		Placeholder:       placeholder,
		Networking:        networking,
		InjectionLocation: credentialInjectionLocation{Header: true},
		ExpiresAt:         expiresAt,
		Scope:             scope,
		HasRefreshToken:   input.RefreshToken != "",
	}
	secret := envOAuthCredentialSecret{
		Type:         credentialAuthTypeEnvOAuth,
		AccessToken:  input.AccessToken,
		RefreshToken: input.RefreshToken,
	}
	state, err := credentialAuthStateFromValues(credentialAuthTypeEnvOAuth, secretName, public, secret)
	if err != nil {
		return nil, nil, err
	}
	return state.PublicAuth, state.SecretPayload, nil
}
