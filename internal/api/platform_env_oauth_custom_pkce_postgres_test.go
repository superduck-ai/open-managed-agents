//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
)

func TestEnvOAuthCustomPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	service := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil)
	view, err := service.Create(t.Context(), platformadmin.Actor{AccountUUID: uuid.NewV4().String()}, platformadmin.OAuthProviderCreateInput{
		Name: "Example OAuth", AuthorizeEndpoint: "https://oauth.example.test/authorize", TokenEndpoint: "https://oauth.example.test/token", Scope: "read", ClientID: "synthetic-custom-client", ClientSecret: "synthetic-custom-secret", TokenEndpointAuthMethod: new("client_secret_basic"), TokenEndpointEncoding: new("form"),
	})
	if err != nil || !view.Configured {
		t.Fatal("自定义客户端注册失败", err)
	}
	var calls int
	var expectedChallenge string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		id, secret, ok := r.BasicAuth()
		if !ok || id != "synthetic-custom-client" || secret != "synthetic-custom-secret" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("Custom 未使用独立 Basic/form 客户端")
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") == "" || r.Form.Get("client_secret") != "" {
			t.Error("Custom 换票参数错误")
		}
		challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(challenge[:]) != expectedChallenge {
			t.Error("Custom PKCE verifier 与 challenge 不匹配")
		}
		_, _ = w.Write([]byte(`{"access_token":"synthetic-custom-access","refresh_token":"synthetic-custom-refresh","expires_in":300,"scope":"read"}`))
	}))
	defer upstream.Close()
	custom, err := f.database.GetPlatformOAuthProviderByExternalID(t.Context(), view.ID)
	if err != nil {
		t.Fatal(err)
	}
	custom.TokenEndpoint = upstream.URL
	if _, err := f.database.UpdatePlatformOAuthProvider(t.Context(), custom); err != nil {
		t.Fatal(err)
	}
	body := `{"provider_id":"custom:` + view.ID + `","vault_id":"` + f.vault.ExternalID + `","workspace_id":"default","secret_name":"CUSTOM_TOKEN","networking":{"type":"limited","allowed_hosts":["oauth.example.test"]},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/"+f.organization+"/env-oauth/vault-auth/start", strings.NewReader(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserExternalID: f.userID, OrganizationUUID: f.organization, WorkspaceExternalID: "wrkspc_default", APIKeyUUID: f.apiKey}))
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", f.organization)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	recorder := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatal("Custom 启动授权失败", recorder.Code, recorder.Body.String())
	}
	var response envOAuthVaultAuthStartResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(response.AuthorizeURL)
	if err != nil || target.Scheme+"://"+target.Host+target.Path != custom.AuthorizeEndpoint || target.Query().Get("scope") != "read" || target.Query().Get("client_id") != "synthetic-custom-client" || target.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("Custom 授权未使用独立端点/PKCE")
	}
	expectedChallenge = target.Query().Get("code_challenge")
	flow, err := f.database.GetEnvOAuthFlow(t.Context(), response.OAuthFlowID)
	if err != nil || flow.ProviderKey != "custom:"+view.ID || flow.ClientIDEnvelope == nil || flow.CodeVerifierEnvelope == nil || flow.TokenEndpoint != upstream.URL {
		t.Fatal("Custom flow 快照错误", err)
	}
	recorder = f.callback(t, flow.ExternalID, "synthetic-code")
	if recorder.Code != http.StatusOK || calls != 1 {
		t.Fatal("Custom callback 失败", recorder.Code)
	}
	completed, err := f.database.GetEnvOAuthFlow(t.Context(), flow.ExternalID)
	if err != nil || completed.Status != "completed" || completed.CredentialExternalID == "" {
		t.Fatal("Custom 凭证未落库", err)
	}
	providers, err := service.List(t.Context())
	if err != nil || len(providers) != 2 {
		t.Fatal("内置服务方列表错误", err)
	}
	for _, provider := range providers {
		encoded, err := json.Marshal(provider)
		if err != nil || strings.Contains(string(encoded), "synthetic-custom-secret") || strings.Contains(string(encoded), "envelope") {
			t.Fatal("Custom 平台响应泄漏秘密")
		}
	}
	recorder = httptest.NewRecorder()
	f.server.handleEnvOAuthProviders(recorder, httptest.NewRequest(http.MethodGet, "/providers", nil).WithContext(req.Context()))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"custom:`+view.ID+`"`) || strings.Contains(recorder.Body.String(), "synthetic-custom-client") || strings.Contains(recorder.Body.String(), "synthetic-custom-secret") {
		t.Fatal("组织 providers API 缺少 Custom 或泄漏客户端")
	}
	feishu, err := platformoauth.NewResolver(f.database, f.secretSvc, nil).ResolveEnvOAuthProvider(t.Context(), "builtin:feishu")
	if err != nil || feishu.ClientID != "feishu-client" || feishu.Scope != "offline_access" {
		t.Fatal("Custom 改变飞书行为", err)
	}
	if err := f.database.DeletePlatformOAuthProvider(t.Context(), custom.UUID, custom.Version+1); err != nil {
		t.Fatal("Custom 删除失败")
	}
}
