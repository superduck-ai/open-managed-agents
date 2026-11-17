//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
)

func TestEnvOAuthUnavailableProviderOnlyBlocksNewCredentialsPostgres(t *testing.T) {
	for _, state := range []struct {
		name              string
		enabled, archived bool
	}{{"停用", false, false}, {"归档", true, true}} {
		t.Run(state.name, func(t *testing.T) {
			f := newEnvOAuthFixture(t)
			f.server.cfg.Auth.ConsoleURL = "https://app.example.test"
			principal := auth.Principal{CredentialType: auth.CredentialTypeAPIKey, UserExternalID: f.userID, OrganizationUUID: f.organization, WorkspaceUUID: f.workspace.UUID, WorkspaceExternalID: f.workspace.ExternalID, APIKeyUUID: f.apiKey}
			create := func(name string) *httptest.ResponseRecorder {
				body := `{"display_name":"` + name + `","auth":{"type":"env_oauth","provider":"builtin:feishu","secret_name":"` + name + `","access_token":"synthetic-access","networking":{"type":"limited","allowed_hosts":["open.feishu.cn"]}}}`
				req := httptest.NewRequest(http.MethodPost, "/"+f.vault.ExternalID+"/credentials?beta=true", strings.NewReader(body))
				req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
				response := httptest.NewRecorder()
				f.server.vaults.ServeHTTP(response, req)
				return response
			}
			provider, err := f.database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
			if err != nil {
				t.Fatal(err)
			}
			service := platformadmin.NewOAuthProviderService(f.database, f.secretSvc, nil)
			disabled, err := service.Update(t.Context(), platformadmin.Actor{}, provider.ExternalID, platformadmin.OAuthProviderUpdateInput{ExpectedVersion: provider.Version, Enabled: new(state.enabled), Archived: new(state.archived), DefaultSecretName: new("TEAM_FEISHU_TOKEN")})
			if err != nil {
				t.Fatal(err)
			}
			if response := create("BLOCKED_TOKEN"); response.Code != http.StatusBadRequest {
				t.Fatalf("停用客户端接受新凭证：%d %s", response.Code, response.Body.String())
			}
			enabled, err := service.Update(t.Context(), platformadmin.Actor{}, provider.ExternalID, platformadmin.OAuthProviderUpdateInput{ExpectedVersion: disabled.Version, Enabled: new(true), Archived: new(false)})
			if err != nil {
				t.Fatal(err)
			}
			response := create("EXISTING_TOKEN")
			if response.Code != http.StatusOK {
				t.Fatalf("启用客户端创建失败：%d %s", response.Code, response.Body.String())
			}
			var existing struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &existing); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Update(t.Context(), platformadmin.Actor{}, provider.ExternalID, platformadmin.OAuthProviderUpdateInput{ExpectedVersion: enabled.Version, Enabled: new(state.enabled), Archived: new(state.archived)}); err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct {
				name   string
				target string
				status int
			}{{"NEW_TOKEN", "", http.StatusConflict}, {"EXISTING_TOKEN", "", http.StatusConflict}, {"EXISTING_TOKEN", existing.ID, http.StatusOK}} {
				body := `{"credential_id":"` + test.target + `","provider_id":"builtin:feishu","vault_id":"` + f.vault.ExternalID + `","workspace_id":"default","secret_name":"` + test.name + `","networking":{"type":"limited","allowed_hosts":["open.feishu.cn"]},"redirect_url":"https://app.example.test/oauth/vault/env-success"}`
				req := httptest.NewRequest(http.MethodPost, "/env-oauth/vault-auth/start", strings.NewReader(body))
				req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
				route := chi.NewRouteContext()
				route.URLParams.Add("orgUuid", f.organization)
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
				response := httptest.NewRecorder()
				f.server.handleEnvOAuthVaultAuthStart(response, req)
				if response.Code != test.status {
					t.Fatalf("%s：状态=%d，预期=%d，响应=%s", test.name, response.Code, test.status, response.Body.String())
				}
			}
			metadata, err := f.server.envOAuthProviders.ListProviders(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range metadata {
				if row.ProviderKey == "builtin:feishu" && (row.Enabled || row.DefaultSecretName != "TEAM_FEISHU_TOKEN") {
					t.Fatal("组织客户端元数据错误")
				}
			}
		})
	}
}
