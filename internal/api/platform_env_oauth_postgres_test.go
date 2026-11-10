//go:build integration

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
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
	userID       string
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
	if clientID != "" {
		builtin, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
		if err != nil {
			t.Fatal(err)
		}
		builtin.ClientIDEnvelope, err = platformoauth.SealClientField(t.Context(), secretSvc, platformoauth.ProviderClientIDBinding(builtin.ExternalID), clientID)
		if err != nil {
			t.Fatal(err)
		}
		builtin.ClientSecretEnvelope, err = platformoauth.SealClientField(t.Context(), secretSvc, platformoauth.ProviderSecretBinding(builtin.ExternalID), clientSecret)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.UpdatePlatformOAuthProvider(t.Context(), builtin); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	fileCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	store := platformsession.NewMemoryStore()
	server := NewServer(ServerDeps{
		DB:                     database,
		VaultSecrets:           secretSvc,
		PlatformStore:          store,
		PlatformAuth:           platformauth.New(config.AuthConfig{}, database, nil, nil),
		CodeSessionCredentials: credentials,
		FilestoreCredentials:   fileCredentials,
	})
	return envOAuthFixture{
		server: server, database: database, secretSvc: secretSvc,
		organization: organization, workspace: workspace, vault: vault, apiKey: apiKey, userID: "user_" + user,
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
		TokenEndpoint:     tokenEndpoint, Scope: "offline_access",
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
		UserExternalID: f.userID, OrganizationUUID: f.organization, WorkspaceExternalID: "wrkspc_default", APIKeyUUID: f.apiKey,
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
	if flow.ClientIDEnvelope != nil {
		t.Fatal("飞书 client_id 以明文写入 flow")
	}
	if flow.SecretName != vaultsapi.EnvOAuthFeishuDefaultSecretName || flow.NetworkingType != "limited" || len(flow.AllowedHosts) != 2 {
		t.Fatalf("flow = %+v", flow)
	}
}

func TestEnvOAuthVaultAuthStartRejectsDisabledBuiltin(t *testing.T) {
	f := newEnvOAuthFixtureWithFeishu(t, "", "")
	body := `{"provider_id":"builtin:feishu","vault_id":"` + f.vault.ExternalID + `","workspace_id":"default","secret_name":"TOKEN","networking":{"type":"unrestricted"},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/"+f.organization+"/env-oauth/vault-auth/start", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserExternalID: f.userID, OrganizationUUID: f.organization, WorkspaceExternalID: "wrkspc_default"}))
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
		var request struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.ClientID != "feishu-client" || request.ClientSecret != "feishu-secret" {
			t.Errorf("换票未使用部署配置: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
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

	beforeReplay, err := f.database.GetVaultCredential(t.Context(), f.workspace.UUID, f.vault.ExternalID, credential.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	replay := f.callback(t, flow.ExternalID, "auth-code")
	if !strings.Contains(replay.Body.String(), `"status":"error"`) || strings.Contains(replay.Body.String(), `"credential_id"`) || tokenRequests != 1 {
		t.Fatalf("重放响应=%s 换票次数=%d", replay.Body.String(), tokenRequests)
	}
	afterReplay, err := f.database.GetVaultCredential(t.Context(), f.workspace.UUID, f.vault.ExternalID, credential.ExternalID)
	if err != nil || !reflect.DeepEqual(beforeReplay, afterReplay) {
		t.Fatal("重放修改凭证", err)
	}
	replayFlow, err := f.database.GetEnvOAuthFlow(t.Context(), flow.ExternalID)
	if err != nil || !reflect.DeepEqual(completed, replayFlow) {
		t.Fatal("重放修改 flow", err)
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

func TestEnvOAuthVaultAuthCallbackClaimsFlowOncePostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	var exchanges int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&exchanges, 1)
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "u-access", "refresh_token": "ur-refresh", "expires_in": 3600, "scope": "offline_access",
		})
	}))
	defer upstream.Close()
	flow := f.createFlow(t, upstream.URL, "CONCURRENT_TOKEN", time.Now().UTC().Add(time.Minute))

	bodies := make([]string, 2)
	var wg sync.WaitGroup
	for index := range bodies {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			bodies[index] = f.callback(t, flow.ExternalID, "auth-code").Body.String()
		}(index)
	}
	wg.Wait()

	succeeded, inProgress := 0, 0
	for _, body := range bodies {
		if strings.Contains(body, `"status":"ok"`) {
			succeeded++
		}
		if strings.Contains(body, envOAuthErrorFlowInProgress) {
			inProgress++
		}
	}
	if succeeded != 1 || inProgress != 1 {
		t.Fatalf("callbacks = %v", bodies)
	}
	if exchanges != 1 {
		t.Fatalf("token exchanges = %d, want 1", exchanges)
	}
	completed, err := f.database.GetEnvOAuthFlow(t.Context(), flow.ExternalID)
	if err != nil || completed.Status != "completed" || completed.CredentialExternalID == "" {
		t.Fatalf("flow = %+v: %v", completed, err)
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
	if err != nil || len(providers) != 3 {
		t.Fatalf("list = %+v: %v", providers, err)
	}
	scope := "read write"
	updated, err := service.Update(t.Context(), actor, created.ID, platformadmin.OAuthProviderUpdateInput{ExpectedVersion: created.Version, Scope: &scope})
	if err != nil || updated.Scope != "read write" {
		t.Fatalf("update = %+v: %v", updated, err)
	}
	if err := service.Delete(t.Context(), actor, created.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(t.Context(), actor, created.ID, updated.Version); err != platformadmin.ErrNotFound {
		t.Fatalf("second delete error = %v", err)
	}
}

func (f envOAuthFixture) start(t *testing.T, principal auth.Principal) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"provider_id":"builtin:feishu","vault_id":"` + f.vault.ExternalID + `","workspace_id":"` + f.workspace.ExternalID + `","secret_name":"TOKEN","networking":{"type":"unrestricted"},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/"+f.organization+"/env-oauth/vault-auth/start", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("orgUuid", f.organization)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	recorder := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(recorder, req)
	return recorder
}

func TestEnvOAuthStartTargetWorkspaceAccessPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	userUUID := uuid.NewV4().String()
	userID := "user_" + userUUID
	if err := f.database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		_, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: userUUID, ExternalID: userID, OrganizationUUID: f.organization, Email: userUUID + "@example.com", Role: "user"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	workspace := f.workspace
	workspace.Name = "Target"
	workspace.UUID, workspace.ExternalID, workspace.IsDefault = uuid.NewV4().String(), "wrkspc_target", false
	var err error
	f.workspace, err = f.database.CreateAdminWorkspace(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	vault := f.vault
	vault.UUID, vault.ExternalID, vault.WorkspaceUUID = uuid.NewV4().String(), "vlt_target", f.workspace.UUID
	f.vault, err = f.database.CreateVault(t.Context(), vault)
	if err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{OrganizationUUID: f.organization, UserExternalID: userID, WorkspaceExternalID: "wrkspc_default", APIKeyUUID: f.apiKey}
	assertDenied := func() {
		t.Helper()
		response := f.start(t, principal)
		if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "eof_") {
			t.Fatalf("denied response=%d %s", response.Code, response.Body.String())
		}
		credentials, _, err := f.database.ListVaultCredentialsPage(t.Context(), db.ListVaultCredentialsPageParams{WorkspaceUUID: f.workspace.UUID, VaultExternalID: f.vault.ExternalID, Limit: 50})
		if err != nil || len(credentials) != 0 {
			t.Fatalf("credentials=%d err=%v", len(credentials), err)
		}
	}
	assertDenied()
	if _, err := f.database.CreateAdminWorkspaceMember(t.Context(), db.AdminWorkspaceMember{ExternalID: "member_target", OrganizationUUID: f.organization, WorkspaceUUID: f.workspace.UUID, WorkspaceExternalID: f.workspace.ExternalID, UserUUID: userUUID, UserExternalID: userID, WorkspaceRole: "workspace_user", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if response := f.start(t, principal); response.Code != http.StatusOK {
		t.Fatalf("member start=%d %s", response.Code, response.Body.String())
	}
	if _, err := f.database.DeleteAdminWorkspaceMember(t.Context(), f.organization, f.workspace.ExternalID, userID); err != nil {
		t.Fatal(err)
	}
	assertDenied()
	principal.UserExternalID = f.userID
	if response := f.start(t, principal); response.Code != http.StatusOK {
		t.Fatalf("admin start=%d %s", response.Code, response.Body.String())
	}
	principal.UserExternalID = ""
	principal.CredentialType = auth.CredentialTypePlatformSession
	principal.AccountUUID = uuid.NewV4().String()
	principal.OrganizationAccessSource = auth.OrganizationAccessPlatformAdmin
	principal.WorkspaceAccess = auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: string(auth.OrganizationAccessPlatformAdmin)}
	if response := f.start(t, principal); response.Code != http.StatusOK {
		t.Fatalf("platform admin start=%d %s", response.Code, response.Body.String())
	}
}

func TestEnvOAuthCallbackRejectsDifferentProviderPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
	}))
	defer upstream.Close()
	provider, err := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil).Create(t.Context(), platformadmin.Actor{AccountUUID: uuid.NewV4().String()}, platformadmin.OAuthProviderCreateInput{Name: "Other", AuthorizeEndpoint: "https://other.example/authorize", TokenEndpoint: "https://other.example/token", ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	first := f.createFlow(t, upstream.URL, "TOKEN", time.Now().Add(time.Minute))
	second := first
	second.UUID, second.ExternalID, second.ProviderKey = uuid.NewV4().String(), uuid.NewV4().String(), "custom:"+provider.ID
	second.ClientIDEnvelope, err = platformoauth.SealClientField(t.Context(), f.secretSvc, platformoauth.FlowClientIDBinding(second), "client")
	if err != nil {
		t.Fatal(err)
	}
	second.NetworkingType, second.AllowedHosts = "unrestricted", nil
	second, err = f.database.CreateEnvOAuthFlow(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	if response := f.callback(t, first.ExternalID, "first"); !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatal(response.Body.String())
	}
	list := func() []db.VaultCredential {
		t.Helper()
		rows, _, err := f.database.ListVaultCredentialsPage(t.Context(), db.ListVaultCredentialsPageParams{WorkspaceUUID: f.workspace.UUID, VaultExternalID: f.vault.ExternalID, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := list()
	if response := f.callback(t, second.ExternalID, "second"); !strings.Contains(response.Body.String(), envOAuthErrorCredentialKeyConflict) {
		t.Fatal(response.Body.String())
	}
	if after := list(); !reflect.DeepEqual(before, after) {
		t.Fatal("跨服务方回调修改了既有凭证")
	}
}

func TestEnvOAuthInjectionLocationHTTPPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	request := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path+"?beta=true", strings.NewReader(body))
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{CredentialType: auth.CredentialTypeAPIKey, APIKeyUUID: f.apiKey, OrganizationUUID: f.organization, WorkspaceUUID: f.workspace.UUID}))
		response := httptest.NewRecorder()
		f.server.vaults.ServeHTTP(response, req)
		return response
	}
	path := "/" + f.vault.ExternalID + "/credentials/"
	create := func(location string) string {
		return `{"display_name":"Token","auth":{"type":"env_oauth","provider":"builtin:feishu","secret_name":"TOKEN","access_token":"access","networking":{"type":"unrestricted"},"injection_location":` + location + `}}`
	}
	for _, location := range []string{`{"header":false,"body":true}`, `{"header":true,"body":true}`} {
		response := request(path, create(location))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("非法位置创建=%d %s", response.Code, response.Body.String())
		}
	}
	rows, _, err := f.database.ListVaultCredentialsPage(t.Context(), db.ListVaultCredentialsPageParams{WorkspaceUUID: f.workspace.UUID, VaultExternalID: f.vault.ExternalID, Limit: 50})
	if err != nil || len(rows) != 0 {
		t.Fatal("非法输入落库", err)
	}
	response := request(path, create(`{"header":true,"body":false}`))
	if response.Code != http.StatusOK {
		t.Fatalf("合法创建=%d %s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatal("创建响应缺 ID", err)
	}
	before, err := f.database.GetVaultCredential(t.Context(), f.workspace.UUID, f.vault.ExternalID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	response = request(path+created.ID, `{"auth":{"type":"env_oauth","injection_location":{"header":false,"body":true},"access_token":"replacement"}}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("非法位置更新=%d %s", response.Code, response.Body.String())
	}
	after, err := f.database.GetVaultCredential(t.Context(), f.workspace.UUID, f.vault.ExternalID, created.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("非法更新修改了凭证", err)
	}
}

func TestEnvOAuthBuiltinConcurrentConfigurationPostgres(t *testing.T) {
	f := newEnvOAuthFixtureWithFeishu(t, "", "")
	builtin, err := f.database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
	if err != nil {
		t.Fatal(err)
	}
	service := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil)
	results := make(chan error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			_, err := service.Update(t.Context(), platformadmin.Actor{AccountUUID: uuid.NewV4().String()}, builtin.ExternalID,
				platformadmin.OAuthProviderUpdateInput{ExpectedVersion: builtin.Version, ClientID: new("configured-client"), ClientSecret: new("configured-secret")})
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, platformadmin.ErrOAuthProviderVersion) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	stored, err := f.database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
	if err != nil || stored.Version != builtin.Version+1 || stored.UUID != builtin.UUID {
		t.Fatal("内置行身份或 CAS 错误")
	}
	if err := f.database.DeletePlatformOAuthProvider(t.Context(), stored.UUID, stored.Version); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("Mapper 允许删除受保护行")
	}
	if err := service.Delete(t.Context(), platformadmin.Actor{}, stored.ExternalID, stored.Version); !errors.Is(err, platformadmin.ErrOAuthProviderProtected) {
		t.Fatal("服务未拒绝删除")
	}
	rows, err := f.server.envOAuthProviders.ListProviders(t.Context())
	if err != nil || len(rows) != 2 || !slices.ContainsFunc(rows, func(row vaultsapi.EnvOAuthProviderSnapshot) bool {
		return row.ProviderKey == "builtin:feishu" && row.Configured
	}) || rows[0].ClientID != "" || rows[0].ClientSecret != "" {
		t.Fatal("组织元数据泄漏客户端或重复 seed")
	}
}

func TestEnvOAuthCustomEncryptedSnapshotPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("client_id") != "old-client" || r.Form.Get("client_secret") != "new-secret" {
			t.Error("回调未使用历史 ID 快照和当前 secret")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "custom-access", "refresh_token": "custom-refresh", "expires_in": 3600})
	}))
	defer upstream.Close()
	service := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil)
	actor := platformadmin.Actor{AccountUUID: uuid.NewV4().String()}
	provider, err := service.Create(t.Context(), actor, platformadmin.OAuthProviderCreateInput{Name: "Snapshot", AuthorizeEndpoint: "https://auth.test/authorize", TokenEndpoint: upstream.URL, ClientID: "old-client", ClientSecret: "old-secret"})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"provider_id":"` + provider.ProviderKey + `","vault_id":"` + f.vault.ExternalID + `","workspace_id":"default","secret_name":"CUSTOM_TOKEN","networking":{"type":"unrestricted"},"redirect_url":"https://app.test/oauth/vault/env-success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/"+f.organization+"/env-oauth/vault-auth/start", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserExternalID: f.userID, OrganizationUUID: f.organization, WorkspaceExternalID: f.workspace.ExternalID}))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("orgUuid", f.organization)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	recorder := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	var started envOAuthVaultAuthStartResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	flow, err := f.database.GetEnvOAuthFlow(t.Context(), started.OAuthFlowID)
	if err != nil || flow.ClientIDEnvelope == nil {
		t.Fatal("start 未密封自定义客户端 ID")
	}
	if value, err := platformoauth.OpenClientField(t.Context(), f.secretSvc, platformoauth.FlowClientIDBinding(flow), flow.ClientIDEnvelope); err != nil || value != "old-client" {
		t.Fatal("客户端快照绑定错误")
	}
	if _, err := service.Update(t.Context(), actor, provider.ID, platformadmin.OAuthProviderUpdateInput{ExpectedVersion: provider.Version, ClientID: new("new-client"), ClientSecret: new("new-secret")}); err != nil {
		t.Fatal(err)
	}
	if result := f.callback(t, flow.ExternalID, "fixture-code"); !strings.Contains(result.Body.String(), `"status":"ok"`) {
		t.Fatal(result.Body.String())
	}
}
