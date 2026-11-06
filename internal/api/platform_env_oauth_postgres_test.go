//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type envOAuthFixture struct {
	server       *Server
	database     *db.DB
	secretSvc    *secrets.Service
	organization string
	workspace    db.AdminWorkspace
	vault        db.Vault
	apiKey       string
}

func newEnvOAuthFixture(t *testing.T) envOAuthFixture {
	t.Helper()
	return newEnvOAuthFixtureWithFeishu(t, "feishu-client", "feishu-secret")
}

func newEnvOAuthFixtureWithFeishu(t *testing.T, clientID, clientSecret string) envOAuthFixture {
	t.Helper()
	database := platformAdminAPIDatabase(t)
	user := uuid.NewV4().String()
	apiKey := uuid.NewV4().String()
	var organization string
	if err := database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(t.Context(), db.PlatformAuthOrganizationInput{Name: "ENV OAuth"})
		if err != nil {
			return err
		}
		organization = org.UUID
		if _, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: user, ExternalID: "user_" + user, OrganizationUUID: organization, Email: user + "@example.com", Role: "admin"}); err != nil {
			return err
		}
		_, err = tx.InsertWorkspace(t.Context(), db.PlatformAuthWorkspaceInput{UUID: uuid.NewV4().String(), ExternalID: "wrkspc_default", OrganizationUUID: organization, Name: "Default"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	workspace, err := database.GetAdminWorkspace(t.Context(), organization, "wrkspc_default")
	if err != nil {
		t.Fatal(err)
	}
	vault, err := database.CreateVault(t.Context(), db.Vault{
		UUID: uuid.NewV4().String(), ExternalID: "vlt_env_oauth_test", OrganizationUUID: organization,
		WorkspaceUUID: workspace.UUID, CreatedByAPIKeyUUID: apiKey, DisplayName: "ENV OAuth vault",
		Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := localkeys.New(localkeys.KeyMaterial{KEK: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	secretSvc := secrets.NewService(provider)
	server := NewServer(ServerDeps{
		DB:           database,
		VaultSecrets: secretSvc,
		Config: config.Config{Vault: config.VaultConfig{FeishuOAuthClient: config.FeishuOAuthClientConfig{
			ClientID: clientID, ClientSecret: clientSecret,
		}}},
	})
	return envOAuthFixture{
		server: server, database: database, secretSvc: secretSvc,
		organization: organization, workspace: workspace, vault: vault, apiKey: apiKey,
	}
}

func (f envOAuthFixture) createFlow(t *testing.T, tokenEndpoint, secretName string, expiresAt time.Time) db.EnvOAuthFlow {
	t.Helper()
	now := time.Now().UTC()
	flow, err := f.database.CreateEnvOAuthFlow(t.Context(), db.EnvOAuthFlow{
		UUID: uuid.NewV4().String(), ExternalID: uuid.NewV4().String(),
		OrganizationUUID: f.organization, WorkspaceUUID: f.workspace.UUID,
		VaultUUID: f.vault.UUID, VaultExternalID: f.vault.ExternalID,
		ProviderKey: vaultsapi.EnvOAuthProviderBuiltinFeishu, SecretName: secretName,
		DisplayName: "Feishu CLI", RedirectURL: "https://app.example.test/oauth/vault/env-success",
		AuthorizeEndpoint: "https://accounts.feishu.cn/open-apis/authen/v1/authorize",
		TokenEndpoint:     tokenEndpoint, ClientID: "feishu-client", Scope: "offline_access",
		TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "json",
		NetworkingType: "limited", AllowedHosts: []string{"open.feishu.cn"},
		Status: "pending", CreatedByAPIKeyUUID: f.apiKey,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func (f envOAuthFixture) callback(t *testing.T, state, code string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	target := "/oauth/vault/env-success?state=" + url.QueryEscape(state)
	if code != "" {
		target += "&code=" + url.QueryEscape(code)
	}
	f.server.handleEnvOAuthVaultAuthCallback(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestEnvOAuthVaultAuthStartPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	body := `{"provider_id":"builtin:feishu","vault_id":"` + f.vault.ExternalID + `","workspace_id":"default","secret_name":"` + vaultsapi.EnvOAuthFeishuDefaultSecretName + `","networking":{"type":"limited","allowed_hosts":["open.feishu.cn","accounts.feishu.cn"]},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/"+f.organization+"/env-oauth/vault-auth/start", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
		OrganizationUUID: f.organization, WorkspaceExternalID: "wrkspc_default", APIKeyUUID: f.apiKey,
	}))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("orgUuid", f.organization)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	recorder := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response envOAuthVaultAuthStartResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(response.OAuthFlowID, "eof_") {
		t.Fatalf("flow id = %q", response.OAuthFlowID)
	}
	authorizeURL, err := url.Parse(response.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authorizeURL.Query()
	if query.Get("client_id") != "feishu-client" || query.Get("state") != response.OAuthFlowID || query.Get("scope") != "offline_access" || query.Get("redirect_uri") != "https://app.example.test/oauth/vault/env-success" {
		t.Fatalf("authorize query = %v", query)
	}
	flow, err := f.database.GetEnvOAuthFlow(t.Context(), response.OAuthFlowID)
	if err != nil {
		t.Fatal(err)
	}
	if flow.SecretName != vaultsapi.EnvOAuthFeishuDefaultSecretName || flow.NetworkingType != "limited" || len(flow.AllowedHosts) != 2 {
		t.Fatalf("flow = %+v", flow)
	}
}

func TestEnvOAuthVaultAuthStartRejectsDisabledBuiltin(t *testing.T) {
	f := newEnvOAuthFixtureWithFeishu(t, "", "")
	body := `{"provider_id":"builtin:feishu","vault_id":"` + f.vault.ExternalID + `","workspace_id":"default","secret_name":"TOKEN","networking":{"type":"unrestricted"},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/"+f.organization+"/env-oauth/vault-auth/start", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{OrganizationUUID: f.organization, WorkspaceExternalID: "wrkspc_default"}))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("orgUuid", f.organization)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	recorder := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestEnvOAuthVaultAuthCallbackPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	var tokenRequests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "u-access", "refresh_token": "ur-refresh", "expires_in": 3600, "scope": "offline_access",
		})
	}))
	defer upstream.Close()

	flow := f.createFlow(t, upstream.URL, vaultsapi.EnvOAuthFeishuDefaultSecretName, time.Now().UTC().Add(time.Minute))
	recorder := f.callback(t, flow.ExternalID, "auth-code")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("callback = %d: %s", recorder.Code, recorder.Body.String())
	}
	completed, err := f.database.GetEnvOAuthFlow(t.Context(), flow.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.CredentialExternalID == "" {
		t.Fatalf("flow = %+v", completed)
	}
	credentials, _, err := f.database.ListVaultCredentialsPage(t.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceUUID: f.workspace.UUID, VaultExternalID: f.vault.ExternalID, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 {
		t.Fatalf("credentials = %d, want 1", len(credentials))
	}
	credential := credentials[0]
	if credential.AuthType != "env_oauth" || credential.CredentialKey != vaultsapi.EnvOAuthFeishuDefaultSecretName || credential.SecretEnvelope == nil {
		t.Fatalf("credential = %+v", credential)
	}
	plaintext, err := f.secretSvc.Open(t.Context(), secrets.Binding{
		OrganizationUUID: f.organization, WorkspaceUUID: f.workspace.UUID,
		VaultExternalID: f.vault.ExternalID, CredentialExternalID: credential.ExternalID,
	}, *credential.SecretEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plaintext)
	if !strings.Contains(string(plaintext), "u-access") || !strings.Contains(string(plaintext), "ur-refresh") {
		t.Fatalf("sealed secret = %s", plaintext)
	}
	placeholder, ok := vaultsapi.EnvOAuthCredentialPlaceholder(credential.Auth)
	if !ok || !strings.HasPrefix(placeholder, "oma_ph_") {
		t.Fatalf("placeholder = %q", placeholder)
	}

	second := f.createFlow(t, upstream.URL, vaultsapi.EnvOAuthFeishuDefaultSecretName, time.Now().UTC().Add(time.Minute))
	if recorder := f.callback(t, second.ExternalID, "auth-code-2"); !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("reauthorize callback = %s", recorder.Body.String())
	}
	credentials, _, err = f.database.ListVaultCredentialsPage(t.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceUUID: f.workspace.UUID, VaultExternalID: f.vault.ExternalID, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].ExternalID != credential.ExternalID || credentials[0].SecretVersion <= credential.SecretVersion {
		t.Fatalf("reauthorization did not update in place: %+v", credentials)
	}
	if tokenRequests != 2 {
		t.Fatalf("token requests = %d", tokenRequests)
	}
}

func TestEnvOAuthVaultAuthCallbackFailuresPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer upstream.Close()

	recorder := f.callback(t, "eof_unknown", "code")
	if !strings.Contains(recorder.Body.String(), `"status":"error"`) {
		t.Fatalf("unknown state callback = %s", recorder.Body.String())
	}

	expired := f.createFlow(t, upstream.URL, "EXPIRED_TOKEN", time.Now().UTC().Add(-time.Minute))
	if recorder := f.callback(t, expired.ExternalID, "code"); !strings.Contains(recorder.Body.String(), `"status":"error"`) {
		t.Fatalf("expired callback = %s", recorder.Body.String())
	}
	failed, err := f.database.GetEnvOAuthFlow(t.Context(), expired.ExternalID)
	if err != nil || failed.Status != "failed" {
		t.Fatalf("expired flow = %+v: %v", failed, err)
	}

	failing := f.createFlow(t, upstream.URL, "FAILING_TOKEN", time.Now().UTC().Add(time.Minute))
	if recorder := f.callback(t, failing.ExternalID, "code"); !strings.Contains(recorder.Body.String(), envOAuthErrorTokenExchangeFailed) {
		t.Fatalf("exchange failure callback = %s", recorder.Body.String())
	}
}

func TestPlatformOAuthProviderServicePostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	service := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil)
	actor := platformadmin.Actor{AccountUUID: uuid.NewV4().String()}
	created, err := service.Create(t.Context(), actor, platformadmin.OAuthProviderCreateInput{
		Name: "Example OAuth", AuthorizeEndpoint: "https://auth.example.test/authorize",
		TokenEndpoint: "https://auth.example.test/token", Scope: "read", ClientID: "client", ClientSecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.HasClientSecret || created.ID == "" {
		t.Fatalf("created = %+v", created)
	}
	if _, err := service.Create(t.Context(), actor, platformadmin.OAuthProviderCreateInput{
		Name: "Example OAuth", AuthorizeEndpoint: "https://auth.example.test/authorize",
		TokenEndpoint: "https://auth.example.test/token", ClientID: "client", ClientSecret: "secret",
	}); err != platformadmin.ErrOAuthProviderNameConflict {
		t.Fatalf("duplicate error = %v", err)
	}
	providers, err := service.List(t.Context())
	if err != nil || len(providers) != 1 {
		t.Fatalf("list = %+v: %v", providers, err)
	}
	scope := "read write"
	updated, err := service.Update(t.Context(), actor, created.ID, platformadmin.OAuthProviderUpdateInput{Scope: &scope})
	if err != nil || updated.Scope != "read write" {
		t.Fatalf("update = %+v: %v", updated, err)
	}
	if err := service.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(t.Context(), created.ID); err != platformadmin.ErrNotFound {
		t.Fatalf("second delete error = %v", err)
	}
}
