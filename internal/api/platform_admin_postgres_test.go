//go:build integration

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

func platformAdminAPIDatabase(t *testing.T) *db.DB {
	t.Helper()
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL；跳过不能视为验收通过")
	}
	connection, err := pgx.Connect(t.Context(), address)
	if err != nil {
		t.Fatal(err)
	}
	schema := "platform_admin_api_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := connection.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		_ = connection.Close(t.Context())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = connection.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = connection.Close(context.Background())
	})
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := db.Open(t.Context(), config.Config{Database: config.DatabaseConfig{URL: parsed.String()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestPlatformAdminOrganizationAccessPostgres(t *testing.T) {
	database := platformAdminAPIDatabase(t)
	operator, ordinary, outsider := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	var home string
	if err := database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(t.Context(), db.PlatformAuthOrganizationInput{Name: "权限隔离来源组织"})
		if err != nil {
			return err
		}
		home = org.UUID
		for _, account := range []struct{ id, role string }{{operator, "user"}, {ordinary, "admin"}, {outsider, "user"}} {
			if _, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: account.id, ExternalID: "user_" + account.id, OrganizationUUID: home, Email: account.id + "@example.com", Role: account.role}); err != nil {
				return err
			}
		}
		_, err = tx.InsertWorkspace(t.Context(), db.PlatformAuthWorkspaceInput{UUID: uuid.NewV4().String(), ExternalID: "wrkspc_home", OrganizationUUID: home, Name: "Default"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	store := platformsession.NewMemoryStore()
	for _, account := range []string{operator, ordinary, outsider} {
		if err := store.Save(t.Context(), account, platformsession.Session{UserUUID: account, UserExternalID: "user_" + account, VerifiedEmail: account + "@example.com", OrganizationUUID: home, AuthenticationMethod: "email_code_v1", ExpiresAt: new(time.Now().Add(time.Hour))}); err != nil {
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
	provider := platformauth.New(config.AuthConfig{SMTP: config.EmailSMTPConfig{Addr: "localhost:1587", Password: "隔离测试"}}, database, nil, nil)
	keyProvider, err := localkeys.New(localkeys.KeyMaterial{KEK: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerDeps{VaultSecrets: secrets.NewService(keyProvider), DB: database, PlatformStore: store, PlatformAuth: provider, CodeSessionCredentials: credentials, FilestoreCredentials: fileCredentials})
	request := func(t *testing.T, method, path, account, org, workspace string, want int, bodies ...string) {
		t.Helper()
		body := `{}`
		if len(bodies) > 0 {
			body = bodies[0]
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "sessionKey", Value: account})
		r.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken(account))
		r.Header.Set("X-Organization-UUID", org)
		r.Header.Set("X-Workspace-ID", workspace)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s：状态 %d，预期 %d：%s", method, path, w.Code, want, w.Body.String())
		}
	}
	request(t, "GET", "/api/platform-admin/organizations", operator, home, "default", 403)
	request(t, "GET", "/v1/agents?beta=true", operator, home, "default", 200)
	service := platformadmin.New(database, nil)
	if err := service.SetAdministrator(t.Context(), operator, "测试运维", "权限隔离验收", true); err != nil {
		t.Fatal(err)
	}
	foreign, _, err := service.CreateOrganization(t.Context(), platformadmin.Actor{AccountUUID: operator}, platformadmin.CreateOrganization{Name: "未加入的组织", Reason: "权限隔离验收", AdminAccountUUIDs: []string{ordinary}}, "isolation")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := database.CreateAdminWorkspace(t.Context(), db.AdminWorkspace{UUID: uuid.NewV4().String(), ExternalID: "wrkspc_private", OrganizationUUID: home, Name: "未加入的工作区", CreatedAt: time.Now(), DataResidency: []byte(`{}`), Tags: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("普通账号拒绝且超管实时授权目标组织", func(t *testing.T) {
		request(t, "GET", "/v1/agents?beta=true", outsider, foreign.UUID, "default", 403)
		request(t, "GET", "/v1/agents?beta=true&organization_uuid="+foreign.UUID, operator, "", "default", 403)
		request(t, "GET", "/v1/agents?beta=true", operator, foreign.UUID, workspace.ExternalID, 403)
		request(t, "GET", "/api/console/organizations/"+home+"/members", operator, foreign.UUID, "default", 404)
		request(t, "GET", "/api/bootstrap", operator, foreign.UUID, "default", 200)
		request(t, "GET", "/v1/agents?beta=true", operator, home, "default", 200)
		request(t, "GET", "/v1/agents?beta=true", operator, foreign.UUID, "default", 200)
		request(t, "GET", "/v1/agents?beta=true", operator, home, workspace.ExternalID, 200)
		request(t, "GET", "/api/console/organizations/"+home+"/members", operator, home, "default", 200)
		request(t, "GET", "/api/platform-admin/organizations/"+foreign.UUID, ordinary, foreign.UUID, "default", 403)
		request(t, "GET", "/api/platform-admin/organizations/"+foreign.UUID, operator, foreign.UUID, workspace.ExternalID, 200)
		request(t, "GET", "/v1/agents?beta=true", ordinary, home, "default", 200)
	})
	t.Run("旧会话和开发认证配置不能激活平台访问", func(t *testing.T) {
		if err := store.Save(t.Context(), "legacy", platformsession.Session{UserUUID: operator, UserExternalID: "user_" + operator, VerifiedEmail: operator + "@example.com", OrganizationUUID: home}); err != nil {
			t.Fatal(err)
		}
		request(t, "GET", "/v1/agents?beta=true", "legacy", foreign.UUID, "default", 403)
		trustedProvider := server.platformAuth
		server.platformAuth = platformauth.New(config.AuthConfig{}, database, nil, nil)
		defer func() { server.platformAuth = trustedProvider }()
		request(t, "GET", "/v1/agents?beta=true", operator, foreign.UUID, "default", 403)
	})

	t.Run("目标不存在与平台会话写入必须校验跨站请求令牌", func(t *testing.T) {
		request(t, "GET", "/api/bootstrap", operator, uuid.NewV4().String(), "default", 404)
		r := httptest.NewRequest(http.MethodPost, "/v1/agents?beta=true", strings.NewReader(`{"name":"denied","model":"claude-sonnet"}`))
		r.AddCookie(&http.Cookie{Name: "sessionKey", Value: operator})
		r.Header.Set("X-Organization-UUID", foreign.UUID)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "Invalid CSRF token") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("非成员平台来源可创建资源且不创建成员", func(t *testing.T) {
		request(t, "POST", "/api/console/organizations/"+foreign.UUID+"/llm_providers", operator, foreign.UUID, "default", 201, `{"name":"Platform","slug":"platform","base_url":"https://models.example.com","api_key":"test-provider-key","model_ids":["claude-sonnet"]}`)
		request(t, "POST", "/v1/agents?beta=true", outsider, foreign.UUID, "default", 403, `{"name":"refused","model":"claude-sonnet"}`)
		request(t, "POST", "/v1/agents?beta=true", operator, foreign.UUID, "default", 200, `{"name":"platform-agent","model":"claude-sonnet"}`)
		request(t, "GET", "/api/console/organizations/"+foreign.UUID+"/workspaces", operator, foreign.UUID, "default", 200)
		request(t, "GET", "/api/console/organizations/"+foreign.UUID+"/llm_providers", operator, foreign.UUID, "default", 200)
		request(t, "POST", "/api/console/organizations/"+foreign.UUID+"/workspaces/default/api_keys", operator, foreign.UUID, "default", 200, `{"name":"platform key"}`)
		if _, err := database.GetActivePlatformUserByEmail(t.Context(), foreign.UUID, operator+"@example.com"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("不应创建成员: %v", err)
		}
		keys, err := database.ListConsoleAPIKeys(t.Context(), foreign.UUID, nil)
		if err != nil || len(keys) != 1 || keys[0].CreatedByUserUUID != nil {
			t.Fatalf("keys=%+v err=%v", keys, err)
		}
	})

	t.Run("平台成员变更复用事务并保留最后管理员和默认工作区保护", func(t *testing.T) {
		existing, err := database.GetActivePlatformUserByEmail(t.Context(), foreign.UUID, ordinary+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		memberPath := "/api/console/organizations/" + foreign.UUID + "/members/" + existing.UUID
		request(t, "POST", memberPath, operator, foreign.UUID, "default", 409, `{"role":"user"}`)
		current, err := database.GetPlatformAdminOrganization(t.Context(), foreign.UUID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.GrantAdministrator(t.Context(), platformadmin.Actor{AccountUUID: operator}, foreign.UUID, platformadmin.AdministratorChange{Change: platformadmin.Change{ExpectedVersion: current.Version}, AccountUUID: outsider}); err != nil {
			t.Fatal(err)
		}
		target, err := database.GetActivePlatformUserByEmail(t.Context(), foreign.UUID, outsider+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		targetPath := "/api/console/organizations/" + foreign.UUID + "/members/" + target.UUID
		request(t, "POST", targetPath, operator, foreign.UUID, "default", 200, `{"role":"user"}`)
		workspace, err := database.CreateAdminWorkspace(t.Context(), db.AdminWorkspace{UUID: uuid.NewV4().String(), ExternalID: "wrkspc_target", OrganizationUUID: foreign.UUID, Name: "目标工作区", CreatedAt: time.Now(), DataResidency: []byte(`{}`), Tags: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		body := `{"user_id":"` + target.UUID + `","workspace_role":"workspace_user"}`
		base := "/api/console/organizations/" + foreign.UUID + "/workspaces/"
		request(t, "POST", base+"default/members", operator, foreign.UUID, "default", 409, body)
		request(t, "POST", base+workspace.ExternalID+"/members", operator, foreign.UUID, "default", 200, body)
		request(t, "GET", base+workspace.ExternalID+"/members", operator, foreign.UUID, workspace.ExternalID, 200)
		request(t, "DELETE", base+workspace.ExternalID+"/members/"+target.UUID, operator, foreign.UUID, "default", 200)
		request(t, "DELETE", targetPath, operator, foreign.UUID, "default", 200)
	})

	t.Run("撤权新请求只保留真实普通成员权限", func(t *testing.T) {
		if err := service.SetAdministrator(t.Context(), ordinary, "测试运维", "保留管理员", true); err != nil {
			t.Fatal(err)
		}
		if err := service.SetAdministrator(t.Context(), operator, "测试运维", "逐请求撤权", false); err != nil {
			t.Fatal(err)
		}
		request(t, "GET", "/v1/agents?beta=true", operator, foreign.UUID, "default", 403)
		request(t, "GET", "/v1/agents?beta=true", operator, home, "default", 200)
		request(t, "GET", "/v1/agents?beta=true", operator, home, workspace.ExternalID, 403)
		if err := service.SetAdministrator(t.Context(), operator, "测试运维", "恢复测试授权", true); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("移除组织成员不撤销独立后台授权", func(t *testing.T) {
		if _, err := database.DeleteAdminUser(t.Context(), home, "user_"+operator); err != nil {
			t.Fatal(err)
		}
		request(t, "GET", "/v1/agents?beta=true", operator, home, "default", 200)
		request(t, "GET", "/api/platform-admin/organizations/"+foreign.UUID, operator, "失效组织", "失效工作区", 200)
	})
	t.Run("撤权后新请求恢复真实成员权限", func(t *testing.T) {
		if err := service.SetAdministrator(t.Context(), ordinary, "测试运维", "保留平台管理员", true); err != nil {
			t.Fatal(err)
		}
		if err := service.SetAdministrator(t.Context(), operator, "测试运维", "撤权验收", false); err != nil {
			t.Fatal(err)
		}
		request(t, "GET", "/v1/agents?beta=true", operator, foreign.UUID, "default", 403)
	})

	t.Run("授权依赖故障明确拒绝", func(t *testing.T) {
		database.Close()
		request(t, "GET", "/api/bootstrap", operator, foreign.UUID, "default", 503)
	})

}
