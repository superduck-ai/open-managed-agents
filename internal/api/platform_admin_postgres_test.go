//go:build integration

package api

import (
	"context"
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

func TestPlatformAdminDoesNotBypassTenantPermissionsPostgres(t *testing.T) {
	database := platformAdminAPIDatabase(t)
	operator, ordinary := uuid.NewV4().String(), uuid.NewV4().String()
	var home string
	if err := database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(t.Context(), db.PlatformAuthOrganizationInput{Name: "权限隔离来源组织"})
		if err != nil {
			return err
		}
		home = org.UUID
		for _, account := range []struct{ id, role string }{{operator, "user"}, {ordinary, "admin"}} {
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
	for _, account := range []string{operator, ordinary} {
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
	server := NewServer(ServerDeps{DB: database, PlatformStore: store, PlatformAuth: provider, CodeSessionCredentials: credentials, FilestoreCredentials: fileCredentials})
	request := func(t *testing.T, method, path, account, org, workspace string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
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
	t.Run("超级管理员仍受业务角色与租户范围限制", func(t *testing.T) {
		request(t, "GET", "/v1/agents?beta=true", operator, home, "default", 200)
		request(t, "GET", "/v1/agents?beta=true", operator, foreign.UUID, "default", 403)
		request(t, "GET", "/v1/agents?beta=true", operator, home, workspace.ExternalID, 403)
		request(t, "GET", "/api/console/organizations/"+home+"/members", operator, home, "default", 403)
		request(t, "GET", "/api/platform-admin/organizations/"+foreign.UUID, ordinary, foreign.UUID, "default", 403)
		request(t, "GET", "/api/platform-admin/organizations/"+foreign.UUID, operator, foreign.UUID, workspace.ExternalID, 200)
		request(t, "GET", "/v1/agents?beta=true", ordinary, home, "default", 200)
	})
	t.Run("移除组织成员不撤销独立后台授权", func(t *testing.T) {
		if _, err := database.DeleteAdminUser(t.Context(), home, "user_"+operator); err != nil {
			t.Fatal(err)
		}
		request(t, "GET", "/v1/agents?beta=true", operator, home, "default", 403)
		request(t, "GET", "/api/platform-admin/organizations/"+foreign.UUID, operator, "失效组织", "失效工作区", 200)
	})
}
