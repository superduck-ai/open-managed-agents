//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
)

func TestEnvOAuthFridayPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	service := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil)
	friday, err := f.database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "friday")
	if err != nil || friday.ClientIDEnvelope != nil || friday.ClientSecretEnvelope != nil || !friday.Protected || friday.Scope != "read" || friday.TokenEndpointAuthMethod != "client_secret_basic" || friday.TokenEndpointEncoding != "form" {
		t.Fatal("Friday seed 不正确", err)
	}
	if _, err := f.server.envOAuthProviders.ResolveEnvOAuthProvider(t.Context(), "builtin:friday"); err == nil {
		t.Fatal("未配置 Friday 可以解析")
	}
	if err := service.Delete(t.Context(), platformadmin.Actor{}, friday.ExternalID, friday.Version); !errors.Is(err, platformadmin.ErrOAuthProviderProtected) {
		t.Fatal("Friday 可以删除")
	}
	view, err := service.Update(t.Context(), platformadmin.Actor{}, friday.ExternalID, platformadmin.OAuthProviderUpdateInput{ExpectedVersion: friday.Version, ClientID: new("synthetic-friday-client"), ClientSecret: new("synthetic-friday-secret")})
	if err != nil || !view.Configured {
		t.Fatal("Friday 配置失败", err)
	}
	var calls int
	var expectedChallenge string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		id, secret, ok := r.BasicAuth()
		if !ok || id != "synthetic-friday-client" || secret != "synthetic-friday-secret" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("Friday 未使用独立 Basic/form 客户端")
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") == "" || r.Form.Get("client_secret") != "" {
			t.Error("Friday 换票参数错误")
		}
		challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(challenge[:]) != expectedChallenge {
			t.Error("Friday PKCE verifier 与 challenge 不匹配")
		}
		_, _ = w.Write([]byte(`{"access_token":"synthetic-friday-access","refresh_token":"synthetic-friday-refresh","expires_in":300,"scope":"read"}`))
	}))
	defer upstream.Close()
	friday, err = f.database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "friday")
	if err != nil {
		t.Fatal(err)
	}
	friday.TokenEndpoint = upstream.URL
	if _, err := f.database.UpdatePlatformOAuthProvider(t.Context(), friday); err != nil {
		t.Fatal(err)
	}
	body := `{"provider_id":"builtin:friday","vault_id":"` + f.vault.ExternalID + `","workspace_id":"default","secret_name":"FRIDAY_TOKEN","networking":{"type":"limited","allowed_hosts":["friday-tx.seewo.com"]},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/"+f.organization+"/env-oauth/vault-auth/start", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserExternalID: f.userID, OrganizationUUID: f.organization, WorkspaceExternalID: "wrkspc_default", APIKeyUUID: f.apiKey}))
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", f.organization)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	recorder := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatal("Friday 启动授权失败", recorder.Code, recorder.Body.String())
	}
	var response envOAuthVaultAuthStartResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(response.AuthorizeURL)
	if err != nil || target.Scheme+"://"+target.Host+target.Path != friday.AuthorizeEndpoint || target.Query().Get("scope") != "read" || target.Query().Get("client_id") != "synthetic-friday-client" || target.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("Friday 授权未使用独立端点/PKCE")
	}
	expectedChallenge = target.Query().Get("code_challenge")
	flow, err := f.database.GetEnvOAuthFlow(t.Context(), response.OAuthFlowID)
	if err != nil || flow.ProviderKey != "builtin:friday" || flow.ClientIDEnvelope != nil || flow.CodeVerifierEnvelope == nil || flow.TokenEndpoint != upstream.URL {
		t.Fatal("Friday flow 快照错误", err)
	}
	recorder = f.callback(t, flow.ExternalID, "synthetic-code")
	if recorder.Code != http.StatusOK || calls != 1 {
		t.Fatal("Friday callback 失败", recorder.Code)
	}
	completed, err := f.database.GetEnvOAuthFlow(t.Context(), flow.ExternalID)
	if err != nil || completed.Status != "completed" || completed.CredentialExternalID == "" {
		t.Fatal("Friday 凭证未落库", err)
	}
	providers, err := service.List(t.Context())
	if err != nil || len(providers) != 2 {
		t.Fatal("内置服务方列表错误", err)
	}
	for _, provider := range providers {
		encoded, err := json.Marshal(provider)
		if err != nil || strings.Contains(string(encoded), "synthetic-friday-secret") || strings.Contains(string(encoded), "envelope") {
			t.Fatal("Friday 平台响应泄漏秘密")
		}
	}
	recorder = httptest.NewRecorder()
	f.server.handleEnvOAuthProviders(recorder, httptest.NewRequest(http.MethodGet, "/providers", nil).WithContext(req.Context()))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"builtin:friday"`) || strings.Contains(recorder.Body.String(), "synthetic-friday-client") || strings.Contains(recorder.Body.String(), "synthetic-friday-secret") {
		t.Fatal("组织 providers API 缺少 Friday 或泄漏客户端")
	}
	feishu, err := platformoauth.NewResolver(f.database, f.secretSvc, nil).ResolveEnvOAuthProvider(t.Context(), "builtin:feishu")
	if err != nil || feishu.ClientID != "feishu-client" || feishu.Scope != "offline_access" {
		t.Fatal("Friday 改变飞书行为", err)
	}
	if err := f.database.DeletePlatformOAuthProvider(t.Context(), friday.UUID, friday.Version+1); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("Mapper 允许删除 Friday")
	}
}
