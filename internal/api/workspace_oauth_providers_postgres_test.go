//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
)

func TestWorkspaceOAuthProviderIsolationPostgres(t *testing.T) {
	f := newEnvOAuthFixture(t)
	userUUID := uuid.NewV4().String()
	userID := "user_workspace_oauth"
	if err := f.database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		_, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: userUUID, ExternalID: userID, OrganizationUUID: f.organization, Email: "workspace-oauth@example.test", Role: "user"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	other, err := f.database.CreateAdminWorkspace(t.Context(), db.AdminWorkspace{UUID: uuid.NewV4().String(), ExternalID: "wrkspc_other", OrganizationUUID: f.organization, Name: "Other", CreatedAt: time.Now(), UpdatedAt: time.Now(), Tags: json.RawMessage(`{}`), DataResidency: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.CreateAdminWorkspaceMember(t.Context(), db.AdminWorkspaceMember{ExternalID: "wmbr_oauth", OrganizationUUID: f.organization, WorkspaceUUID: other.UUID, WorkspaceExternalID: other.ExternalID, UserUUID: userUUID, UserExternalID: userID, WorkspaceRole: "workspace_user", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{CredentialType: auth.CredentialTypePlatformSession, UserUUID: userUUID, UserExternalID: userID, OrganizationUUID: f.organization, WorkspaceUUID: f.workspace.UUID, WorkspaceExternalID: f.workspace.ExternalID, APIKeyUUID: f.apiKey}
	providerID := ""
	request := func(method, workspaceID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/env-oauth/providers?workspace_id="+workspaceID, strings.NewReader(body))
		route := chi.NewRouteContext()
		route.URLParams.Add("orgUuid", f.organization)
		route.URLParams.Add("providerID", providerID)
		req = req.WithContext(context.WithValue(auth.WithPrincipal(req.Context(), principal), chi.RouteCtxKey, route))
		response := httptest.NewRecorder()
		if method == http.MethodPatch || (method == http.MethodGet && providerID != "") {
			f.server.handleWorkspaceOAuthProvider(response, req)
		} else if method == http.MethodGet {
			f.server.handleEnvOAuthProviders(response, req)
		} else {
			f.server.handleCreateWorkspaceOAuthProvider(response, req)
		}
		return response
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "workspace-client" || r.Form.Get("client_secret") != "workspace-secret" || r.Form.Get("code_verifier") == "" {
			t.Error("工作区客户端换票参数错误")
		}
		_, _ = w.Write([]byte(`{"access_token":"workspace-access","refresh_token":"workspace-refresh","expires_in":3600}`))
	}))
	defer upstream.Close()
	body := `{"name":"Workspace provider","authorize_endpoint":"https://auth.example.test/authorize","token_endpoint":"` + upstream.URL + `","default_allowed_hosts":[" api.example.test "],"default_secret_name":"TEAM_TOKEN"}`
	response := request(http.MethodPost, f.workspace.ExternalID, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("普通用户创建失败：%d %s", response.Code, response.Body.String())
	}
	var created platformadmin.OAuthProviderView
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Body.String(), "workspace-secret") {
		t.Fatal("客户端密钥被回显")
	}
	if created.Configured {
		t.Fatal("空客户端服务方被标记为已配置")
	}
	providerID = created.ID
	detail := request(http.MethodGet, f.workspace.ExternalID, "")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), `"client_secret":`) {
		t.Fatal("详情不可读或回显了密钥", detail.Code)
	}
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		cross := request(method, other.ExternalID, `{"expected_version":1,"client_id":"bad","client_secret":"bad"}`)
		if cross.Code != http.StatusNotFound {
			t.Fatal("跨工作区可读写客户端", method, cross.Code)
		}
	}
	globalProvider, err := f.database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
	if err != nil {
		t.Fatal(err)
	}
	providerID = globalProvider.ExternalID
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		denied := request(method, f.workspace.ExternalID, `{"expected_version":1,"client_id":"bad","client_secret":"bad"}`)
		if denied.Code != http.StatusNotFound {
			t.Fatal("工作区入口可读写平台客户端", method, denied.Code)
		}
	}
	providerID = created.ID
	configured := request(http.MethodPatch, f.workspace.ExternalID, `{"expected_version":1,"client_id":"workspace-client","client_secret":"workspace-secret"}`)
	if configured.Code != http.StatusOK {
		t.Fatal("普通成员无法补填客户端", configured.Code, configured.Body.String())
	}
	if err := json.Unmarshal(configured.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.Configured || created.ClientID != "workspace-client" || strings.Contains(configured.Body.String(), "workspace-secret") {
		t.Fatal("补填结果错误或密钥被回显")
	}
	stale := request(http.MethodPatch, f.workspace.ExternalID, `{"expected_version":1,"scope":"changed"}`)
	if stale.Code != http.StatusConflict {
		t.Fatal("过期版本可覆盖客户端", stale.Code)
	}
	preserved := request(http.MethodPatch, f.workspace.ExternalID, `{"expected_version":2,"client_id":"workspace-client","scope":"read"}`)
	if preserved.Code != http.StatusOK {
		t.Fatal("未填写密钥时不能保存", preserved.Code)
	}
	providerID = ""
	stored, err := f.database.GetPlatformOAuthProviderByExternalID(t.Context(), created.ID)
	if err != nil || stored.WorkspaceUUID != f.workspace.UUID || !slices.Equal(stored.DefaultAllowedHosts, []string{"api.example.test"}) {
		t.Fatal("工作区或默认主机未持久化", err)
	}
	for _, scope := range []struct {
		workspace string
		visible   bool
	}{{f.workspace.ExternalID, true}, {other.ExternalID, false}} {
		listed := request(http.MethodGet, scope.workspace, "")
		if listed.Code != http.StatusOK {
			t.Fatalf("目录查询失败：%d", listed.Code)
		}
		if scope.visible && !strings.Contains(listed.Body.String(), `"workspace_scoped":true`) {
			t.Fatal("工作区服务方缺少作用域标识")
		}
		if strings.Contains(listed.Body.String(), created.ProviderKey) != scope.visible {
			t.Fatal("工作区目录隔离失败")
		}
	}
	global, err := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil).List(t.Context())
	if err != nil || slices.ContainsFunc(global, func(row platformadmin.OAuthProviderView) bool { return row.ID == created.ID }) {
		t.Fatal("工作区客户端进入平台目录", err)
	}
	if duplicate := request(http.MethodPost, f.workspace.ExternalID, body); duplicate.Code != http.StatusConflict {
		t.Fatal("同工作区同名未拒绝", duplicate.Code)
	}
	if independent := request(http.MethodPost, other.ExternalID, body); independent.Code != http.StatusCreated {
		t.Fatal("不同工作区不能使用同名服务方", independent.Code, independent.Body.String())
	}
	otherVault := f.vault
	otherVault.UUID = uuid.NewV4().String()
	otherVault.ExternalID = "vlt_other"
	otherVault.WorkspaceUUID = other.UUID
	otherVault, err = f.database.CreateVault(t.Context(), otherVault)
	if err != nil {
		t.Fatal(err)
	}
	start := `{"provider_id":"` + created.ProviderKey + `","vault_id":"` + otherVault.ExternalID + `","workspace_id":"` + other.ExternalID + `","secret_name":"TOKEN","networking":{"type":"limited","allowed_hosts":["api.example.test"]},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
	f.server.cfg.Auth.ConsoleURL = "https://app.example.test"
	req := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(start))
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", f.organization)
	req = req.WithContext(context.WithValue(auth.WithPrincipal(req.Context(), principal), chi.RouteCtxKey, route))
	result := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(result, req)
	if result.Code != http.StatusServiceUnavailable {
		t.Fatal("跨工作区 ID 可启动授权", result.Code)
	}
	ownBody := strings.NewReplacer(otherVault.ExternalID, f.vault.ExternalID, other.ExternalID, f.workspace.ExternalID).Replace(start)
	ownRequest := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(ownBody)).WithContext(req.Context())
	ownResponse := httptest.NewRecorder()
	f.server.handleEnvOAuthVaultAuthStart(ownResponse, ownRequest)
	if ownResponse.Code != http.StatusOK {
		t.Fatal("本工作区授权启动失败", ownResponse.Code, ownResponse.Body.String())
	}
	var started envOAuthVaultAuthStartResponse
	if err := json.Unmarshal(ownResponse.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	callback := f.callback(t, started.OAuthFlowID, "workspace-code")
	if callback.Code != http.StatusOK {
		t.Fatal("工作区回调失败", callback.Code)
	}
	flow, err := f.database.GetEnvOAuthFlow(t.Context(), started.OAuthFlowID)
	if err != nil || flow.Status != "completed" || flow.CredentialExternalID == "" {
		t.Fatal("工作区凭据未完成落库", err)
	}
	principal.WorkspaceUUID = other.UUID
	manual := `{"display_name":"Cross-workspace","auth":{"type":"env_oauth","provider":"` + created.ProviderKey + `","secret_name":"TOKEN","access_token":"manual","networking":{"type":"limited","allowed_hosts":["api.example.test"]}}}`
	req = httptest.NewRequest(http.MethodPost, "/"+otherVault.ExternalID+"/credentials?beta=true", strings.NewReader(manual))
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	result = httptest.NewRecorder()
	f.server.vaults.ServeHTTP(result, req)
	if result.Code != http.StatusBadRequest {
		t.Fatal("跨工作区 ID 可手工创建凭据", result.Code, result.Body.String())
	}
}
