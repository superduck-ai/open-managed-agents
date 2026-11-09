package platformoauth

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type providerStoreStub struct {
	provider      db.PlatformOAuthProvider
	err           error
	builtinLookup *string
}

func (s providerStoreStub) GetPlatformOAuthProviderByBuiltinKey(_ context.Context, key string) (db.PlatformOAuthProvider, error) {
	if s.builtinLookup != nil {
		*s.builtinLookup = key
	}
	return s.provider, s.err
}
func (s providerStoreStub) GetPlatformOAuthProviderByExternalID(context.Context, string) (db.PlatformOAuthProvider, error) {
	return s.provider, s.err
}
func (s providerStoreStub) ListPlatformOAuthProviders(context.Context) ([]db.PlatformOAuthProvider, error) {
	return []db.PlatformOAuthProvider{s.provider, {BuiltinKey: "friday"}}, s.err
}

func providerFixture(t *testing.T) (*secrets.Service, db.PlatformOAuthProvider) {
	t.Helper()
	key, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{7}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := secrets.NewService(key)
	provider := db.PlatformOAuthProvider{UUID: "provider-uuid", ExternalID: "poap_FeishuBuiltinOAuthClient", BuiltinKey: "feishu", Protected: true,
		Name: "Feishu", Scope: "offline_access custom", TokenEndpointEncoding: "json", DefaultSecretName: "DB_VARIABLE", DefaultAllowedHosts: []string{"example.test"}}
	provider.ClientIDEnvelope, err = SealClientField(t.Context(), service, ProviderClientIDBinding(provider.ExternalID), "client")
	if err != nil {
		t.Fatal(err)
	}
	provider.ClientSecretEnvelope, err = SealClientField(t.Context(), service, ProviderSecretBinding(provider.ExternalID), "secret")
	if err != nil {
		t.Fatal(err)
	}
	return service, provider
}

func TestResolverRejectsUnavailableAndAlias(t *testing.T) {
	service, provider := providerFixture(t)
	for _, key := range []string{"", "unknown", "builtin:other", "custom:", "custom:" + provider.ExternalID} {
		if _, err := NewResolver(providerStoreStub{provider: provider}, service, nil).ResolveEnvOAuthProvider(t.Context(), key); !errors.Is(err, vaults.ErrEnvOAuthProviderUnavailable) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	for _, store := range []providerStoreStub{{err: db.ErrNotFound}, {provider: db.PlatformOAuthProvider{BuiltinKey: "feishu"}}} {
		if _, err := NewResolver(store, service, nil).ResolveEnvOAuthProvider(t.Context(), "builtin:feishu"); !errors.Is(err, vaults.ErrEnvOAuthProviderUnavailable) {
			t.Fatal("unavailable provider accepted")
		}
	}
	provider.ClientIDEnvelope, provider.ClientSecretEnvelope = provider.ClientSecretEnvelope, provider.ClientIDEnvelope
	if _, err := NewResolver(providerStoreStub{provider: provider}, service, nil).ResolveEnvOAuthProvider(t.Context(), "builtin:feishu"); err == nil {
		t.Fatal("swapped fields accepted")
	}
}

func TestResolverUsesDatabaseMetadata(t *testing.T) {
	service, provider := providerFixture(t)
	resolver := NewResolver(providerStoreStub{provider: provider}, service, nil)
	snapshot, err := resolver.ResolveEnvOAuthProvider(t.Context(), "builtin:feishu")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ClientID != "client" || snapshot.ClientSecret != "secret" || snapshot.DefaultSecretName != "DB_VARIABLE" || snapshot.Scope != provider.Scope {
		t.Fatal("database snapshot mismatch")
	}
	rows, err := NewResolver(providerStoreStub{provider: provider}, nil, nil).ListProviders(t.Context())
	if err != nil || len(rows) != 2 || !rows[0].Configured || rows[0].ClientID != "" || rows[0].ClientSecret != "" {
		t.Fatal("metadata listing requires keys or exposes client")
	}
}

func TestClientBindingsRejectCopies(t *testing.T) {
	service, provider := providerFixture(t)
	for _, binding := range []secrets.Binding{ProviderClientIDBinding("another-provider"), ProviderSecretBinding(provider.ExternalID), FlowClientIDBinding(db.EnvOAuthFlow{OrganizationUUID: "org", WorkspaceUUID: "workspace", ExternalID: "flow"})} {
		if _, err := service.Open(t.Context(), binding, *provider.ClientIDEnvelope); err == nil {
			t.Fatal("copied ciphertext accepted")
		}
	}
}

func TestFlowClientIDRejectsOtherTenantAndFlow(t *testing.T) {
	service, _ := providerFixture(t)
	flow := db.EnvOAuthFlow{OrganizationUUID: "org", WorkspaceUUID: "workspace", ExternalID: "eof_original"}
	envelope, err := SealClientField(t.Context(), service, FlowClientIDBinding(flow), "client")
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []db.EnvOAuthFlow{
		{OrganizationUUID: "other", WorkspaceUUID: flow.WorkspaceUUID, ExternalID: flow.ExternalID},
		{OrganizationUUID: flow.OrganizationUUID, WorkspaceUUID: "other", ExternalID: flow.ExternalID},
		{OrganizationUUID: flow.OrganizationUUID, WorkspaceUUID: flow.WorkspaceUUID, ExternalID: "other"},
	} {
		if _, err := OpenClientField(t.Context(), service, FlowClientIDBinding(changed), envelope); err == nil {
			t.Fatal("flow ID 跨租户或记录复制被接受")
		}
	}
}

type corruptWrappingProvider struct{ secrets.KeyProvider }

func (p corruptWrappingProvider) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	wrapped, err := p.KeyProvider.WrapDEK(ctx, dek)
	if err == nil {
		wrapped.Ciphertext[0] ^= 1
	}
	return wrapped, err
}

func TestClientSealRejectsUnreadableWrappedKey(t *testing.T) {
	key, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{5}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := secrets.NewService(corruptWrappingProvider{KeyProvider: key})
	if envelope, err := SealClientField(t.Context(), service, ProviderClientIDBinding("poap_test"), "client"); err == nil || envelope != nil {
		t.Fatal("无法解密的新信封被接受")
	}
}

func TestProviderDefaultsRejectInvalidDomains(t *testing.T) {
	service, provider := providerFixture(t)
	for _, changed := range []db.PlatformOAuthProvider{
		{BuiltinKey: "feishu", DefaultAllowedHosts: []string{"not a valid host"}},
		{BuiltinKey: "feishu", DefaultSecretName: "not a variable"},
	} {
		if _, err := NewResolver(providerStoreStub{provider: changed}, service, nil).ListProviders(t.Context()); err == nil {
			t.Fatal("无效默认配置未失败关闭")
		}
	}
	provider.DefaultAllowedHosts = []string{"not a valid host"}
	if _, err := NewResolver(providerStoreStub{provider: provider}, service, nil).ResolveEnvOAuthProvider(t.Context(), "builtin:feishu"); err == nil {
		t.Fatal("无效域名用于授权")
	}
}
