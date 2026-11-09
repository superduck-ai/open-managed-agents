package platformoauth

import (
	"bytes"
	"context"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"log/slog"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type ProviderStore interface {
	GetPlatformOAuthProviderByBuiltinKey(context.Context, string) (db.PlatformOAuthProvider, error)
	GetPlatformOAuthProviderByExternalID(context.Context, string) (db.PlatformOAuthProvider, error)
	ListPlatformOAuthProviders(context.Context) ([]db.PlatformOAuthProvider, error)
}

type Resolver struct {
	db        ProviderStore
	secretSvc *secrets.Service
	logger    *slog.Logger
}

func NewResolver(database ProviderStore, secretSvc *secrets.Service, logger *slog.Logger) *Resolver {
	return &Resolver{db: database, secretSvc: secretSvc, logger: logging.LoggerOrDefault(logger)}
}

func ProviderSecretBinding(providerExternalID string) secrets.Binding {
	return secrets.Binding{OrganizationUUID: "platform", WorkspaceUUID: "platform", VaultExternalID: "platform_oauth_providers", CredentialExternalID: providerExternalID}
}

func ProviderClientIDBinding(providerExternalID string) secrets.Binding {
	binding := ProviderSecretBinding(providerExternalID)
	binding.CredentialExternalID += ":client_id"
	return binding
}

func FlowClientIDBinding(flow db.EnvOAuthFlow) secrets.Binding {
	return secrets.Binding{OrganizationUUID: flow.OrganizationUUID, WorkspaceUUID: flow.WorkspaceUUID, VaultExternalID: "env_oauth_flows", CredentialExternalID: flow.ExternalID}
}

func ProviderKey(provider db.PlatformOAuthProvider) string {
	if provider.BuiltinKey != "" {
		return "builtin:" + provider.BuiltinKey
	}
	return vaults.EnvOAuthCustomProviderKey(provider.ExternalID)
}

func (r *Resolver) ResolveEnvOAuthProvider(ctx context.Context, providerKey string) (vaults.EnvOAuthProviderSnapshot, error) {
	custom, externalID, err := vaults.ParseEnvOAuthProviderKey(providerKey)
	if err != nil || r.db == nil {
		return vaults.EnvOAuthProviderSnapshot{}, vaults.ErrEnvOAuthProviderUnavailable
	}
	var provider db.PlatformOAuthProvider
	if custom {
		provider, err = r.db.GetPlatformOAuthProviderByExternalID(ctx, externalID)
	} else {
		provider, err = r.db.GetPlatformOAuthProviderByBuiltinKey(ctx, strings.TrimPrefix(externalID, "builtin:"))
	}
	if !custom && errors.Is(err, db.ErrNotFound) {
		r.auditMissingBuiltin(ctx, providerKey)
	}
	if err != nil || (custom && provider.BuiltinKey != "") || provider.ClientIDEnvelope == nil || provider.ClientSecretEnvelope == nil {
		return vaults.EnvOAuthProviderSnapshot{}, vaults.ErrEnvOAuthProviderUnavailable
	}
	if err := ValidateProviderDefaults(provider); err != nil {
		return vaults.EnvOAuthProviderSnapshot{}, err
	}
	clientID, err := OpenClientField(ctx, r.secretSvc, ProviderClientIDBinding(provider.ExternalID), provider.ClientIDEnvelope)
	if err != nil {
		return vaults.EnvOAuthProviderSnapshot{}, err
	}
	clientSecret, err := OpenClientField(ctx, r.secretSvc, ProviderSecretBinding(provider.ExternalID), provider.ClientSecretEnvelope)
	if err != nil {
		return vaults.EnvOAuthProviderSnapshot{}, err
	}
	snapshot := ProviderSnapshot(provider)
	snapshot.ClientID, snapshot.ClientSecret = clientID, clientSecret
	return snapshot, nil
}

func ProviderSnapshot(provider db.PlatformOAuthProvider) vaults.EnvOAuthProviderSnapshot {
	return vaults.EnvOAuthProviderSnapshot{
		ProviderKey: ProviderKey(provider), Name: provider.Name, Builtin: provider.BuiltinKey != "",
		Configured:        provider.ClientIDEnvelope != nil && provider.ClientSecretEnvelope != nil,
		AuthorizeEndpoint: provider.AuthorizeEndpoint, TokenEndpoint: provider.TokenEndpoint, Scope: provider.Scope,
		TokenEndpointAuthMethod: provider.TokenEndpointAuthMethod, TokenEndpointEncoding: provider.TokenEndpointEncoding,
		DefaultSecretName: provider.DefaultSecretName, DefaultAllowedHosts: slices.Clone(provider.DefaultAllowedHosts),
	}
}

func (r *Resolver) ListProviders(ctx context.Context) ([]vaults.EnvOAuthProviderSnapshot, error) {
	if r.db == nil {
		return nil, vaults.ErrEnvOAuthProviderUnavailable
	}
	rows, err := r.db.ListPlatformOAuthProviders(ctx)
	if err != nil {
		return nil, vaults.ErrEnvOAuthProviderUnavailable
	}
	providers := make([]vaults.EnvOAuthProviderSnapshot, 0, len(rows))
	for _, provider := range rows {
		if err := ValidateProviderDefaults(provider); err != nil {
			return nil, err
		}
		providers = append(providers, ProviderSnapshot(provider))
	}
	if missing := MissingBuiltinProviderKey(rows); missing != "" {
		r.auditMissingBuiltin(ctx, missing)
		return nil, vaults.ErrEnvOAuthProviderUnavailable
	}
	return providers, nil
}

func OpenClientField(ctx context.Context, service *secrets.Service, binding secrets.Binding, envelope *secrets.Envelope) (string, error) {
	if service == nil || envelope == nil {
		return "", vaults.ErrEnvOAuthProviderUnavailable
	}
	plaintext, err := service.Open(ctx, binding, *envelope)
	if err != nil {
		return "", vaults.ErrEnvOAuthProviderUnavailable
	}
	defer clear(plaintext)
	if len(plaintext) == 0 {
		return "", vaults.ErrEnvOAuthProviderUnavailable
	}
	return string(plaintext), nil
}

func SealClientField(ctx context.Context, service *secrets.Service, binding secrets.Binding, value string) (*secrets.Envelope, error) {
	if service == nil || value == "" {
		return nil, vaults.ErrEnvOAuthProviderUnavailable
	}
	plaintext := []byte(value)
	defer clear(plaintext)
	envelope, err := service.Seal(ctx, binding, plaintext)
	if err != nil {
		return nil, vaults.ErrEnvOAuthProviderUnavailable
	}
	opened, err := service.Open(ctx, binding, envelope)
	defer clear(opened)
	if err != nil || !bytes.Equal(opened, plaintext) {
		return nil, vaults.ErrEnvOAuthProviderUnavailable
	}
	return &envelope, nil
}

func (r *Resolver) auditMissingBuiltin(ctx context.Context, providerKey string) {
	r.logger.ErrorContext(ctx, "platform oauth provider audit", "actor_type", "system", "provider_key", providerKey,
		"action", "integrity", "result", "failure", "error_code", "builtin_provider_missing", "request_id", httpapi.RequestID(ctx))
}

func ValidateProviderDefaults(provider db.PlatformOAuthProvider) error {
	if provider.DefaultSecretName != "" {
		if _, err := vaults.ParseEnvOAuthSecretName(provider.DefaultSecretName); err != nil {
			return vaults.ErrEnvOAuthProviderUnavailable
		}
	}
	if len(provider.DefaultAllowedHosts) > 0 {
		if err := vaults.ValidateEnvOAuthNetworking("limited", provider.DefaultAllowedHosts); err != nil {
			return vaults.ErrEnvOAuthProviderUnavailable
		}
	}
	return nil
}

func MissingBuiltinProviderKey(providers []db.PlatformOAuthProvider) string {
	for _, key := range []string{"feishu", "friday"} {
		if !slices.ContainsFunc(providers, func(provider db.PlatformOAuthProvider) bool { return provider.BuiltinKey == key }) {
			return "builtin:" + key
		}
	}
	return ""
}

func FlowCodeVerifierBinding(flow db.EnvOAuthFlow) secrets.Binding {
	binding := FlowClientIDBinding(flow)
	binding.CredentialExternalID += ":code_verifier"
	return binding
}
