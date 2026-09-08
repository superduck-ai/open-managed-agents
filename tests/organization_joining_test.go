package tests

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type joiningMembership struct {
	Organization struct {
		UUID string `json:"uuid"`
	} `json:"organization"`
	Role     string `json:"role"`
	UserUUID string `json:"user_uuid"`
	UserID   string `json:"user_id"`
}

type joiningBootstrap struct {
	CSRFToken string `json:"csrf_token"`
	Account   struct {
		UUID                    string              `json:"uuid"`
		Email                   string              `json:"email_address"`
		DefaultOrganizationUUID string              `json:"default_organization_uuid"`
		Memberships             []joiningMembership `json:"memberships"`
	} `json:"account"`
}

type joiningFixture struct {
	app          *testApp
	cookies      []*http.Cookie
	ownerCookies []*http.Cookie
	email        string
	organization string
	bootstrap    joiningBootstrap
	accessCounts map[string]int
}

func newJoiningFixture(t *testing.T) joiningFixture {
	t.Helper()
	// 此验收会迁移和 seed，只允许主代理提供的临时配置。
	if os.Getenv("CONFIG_FILE") != "/tmp/oma338-test-config.yaml" {
		t.Skip("组织加入验收需要 CONFIG_FILE=/tmp/oma338-test-config.yaml 隔离数据库")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("organization-joining"))
	t.Cleanup(app.close)
	owner := joiningLogin(t, app, "joining-owner-"+uuid.NewV4().String()+"@example.com")
	f := joiningFixture{app: app, ownerCookies: owner, email: "joining-recipient-" + uuid.NewV4().String() + "@example.com", organization: responseCookie(owner, "lastActiveOrg").Value}
	f.accessCounts = f.implicitAccessCounts(t)
	if f.accessCounts["workspace_members"] != 0 {
		t.Fatal("新注册组织不应有 Default 成员记录")
	}
	return f
}

func joiningLogin(t *testing.T, app *testApp, email string) []*http.Cookie {
	t.Helper()
	// 不调用 platformLoginCookies：它会预先把邮箱写成 seed 组织成员。
	response := app.platformRequest(t, http.MethodPost, "/api/auth/verify_magic_link", strings.NewReader(`{"credentials":{"method":"code","code":"123456","email_address":`+quoteJSON(email)+`}}`), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("邮箱登录失败：%d %s", response.StatusCode, readAll(t, response.Body))
	}
	for _, name := range []string{"sessionKey", "lastActiveOrg"} {
		if responseCookie(response.Cookies(), name) == nil {
			t.Fatalf("登录缺少 %s", name)
		}
	}
	return response.Cookies()
}

func (f joiningFixture) request(t *testing.T, method, path string, body io.Reader, headers map[string]string, want int) *http.Response {
	t.Helper()
	response := f.app.platformRequestWithHeaders(t, method, path, body, f.cookies, headers)
	t.Cleanup(func() { response.Body.Close() })
	if response.StatusCode != want {
		t.Fatalf("%s %s：%d，预期 %d：%s", method, path, response.StatusCode, want, readAll(t, response.Body))
	}
	if want == http.StatusForbidden {
		for _, cookie := range response.Cookies() {
			if (cookie.Name == "sessionKey" || cookie.Name == "lastActiveOrg") && cookie.MaxAge < 0 {
				t.Fatalf("权限拒绝清除了 %s", cookie.Name)
			}
		}
	}
	return response
}

func (f joiningFixture) loadBootstrap(t *testing.T, headers map[string]string) joiningBootstrap {
	t.Helper()
	response := f.request(t, http.MethodGet, "/api/bootstrap", nil, headers, http.StatusOK)
	var result joiningBootstrap
	decodeJSON(t, response.Body, &result)
	if result.Account.UUID == "" || result.CSRFToken == "" {
		t.Fatal("bootstrap 缺少账号或 CSRF token")
	}
	return result
}

func (f joiningFixture) invite(t *testing.T, email, status string, expires time.Time) db.AdminInvite {
	t.Helper()
	invite, err := f.app.db.CreateAdminInvite(t.Context(), db.AdminInvite{ExternalID: "invite_joining_" + uuid.NewV4().String(), OrganizationUUID: f.organization, Email: email, Role: "developer", Status: status, InvitedAt: expires.Add(-21 * 24 * time.Hour), ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	return invite
}

func (f joiningFixture) respond(t *testing.T, invite db.AdminInvite, action string, want int) {
	t.Helper()
	response := f.request(t, http.MethodPost, "/api/invitations/"+invite.ExternalID+"/"+action, nil, map[string]string{"X-CSRF-Token": f.bootstrap.CSRFToken}, want)
	if want != http.StatusOK {
		return
	}
	var result struct {
		ID               string `json:"id"`
		Status           string `json:"status"`
		OrganizationUUID string `json:"organization_uuid"`
	}
	decodeJSON(t, response.Body, &result)
	status := "accepted"
	if action == "decline" {
		status = "declined"
	}
	if result.ID != invite.ExternalID || result.OrganizationUUID != invite.OrganizationUUID || result.Status != status {
		t.Fatalf("邀请响应不符合合同：%+v", result)
	}
}

func TestOrganizationJoiningFailures(t *testing.T) {
	f := newJoiningFixture(t)
	valid := f.invite(t, f.email, "pending", time.Now().Add(24*time.Hour))
	t.Run("未登录", func(t *testing.T) {
		f.request(t, http.MethodGet, "/api/invitations", nil, nil, http.StatusUnauthorized)
		for _, action := range []string{"accept", "decline"} {
			f.request(t, http.MethodPost, "/api/invitations/"+valid.ExternalID+"/"+action, nil, nil, http.StatusUnauthorized)
		}
	})
	f.cookies = joiningLogin(t, f.app, f.email)
	f.bootstrap = f.loadBootstrap(t, nil)
	t.Run("缺失或错误CSRF", func(t *testing.T) {
		otherSession := f
		otherSession.cookies = joiningLogin(t, f.app, f.email)
		otherToken := otherSession.loadBootstrap(t, nil).CSRFToken
		for _, action := range []string{"accept", "decline"} {
			for _, token := range []string{"", "invalid-token", otherToken} {
				f.request(t, http.MethodPost, "/api/invitations/"+valid.ExternalID+"/"+action, nil, map[string]string{"X-CSRF-Token": token}, http.StatusForbidden)
			}
		}
	})
	t.Run("跨邮箱与伪造请求邮箱", func(t *testing.T) {
		other := f.invite(t, "other-"+f.email, "pending", time.Now().Add(time.Hour))
		for _, action := range []string{"accept", "decline"} {
			f.request(t, http.MethodPost, "/api/invitations/"+other.ExternalID+"/"+action, strings.NewReader(`{"email":`+quoteJSON(other.Email)+`}`), map[string]string{"X-CSRF-Token": f.bootstrap.CSRFToken}, http.StatusNotFound)
		}
	})
	t.Run("过期与撤销", func(t *testing.T) {
		for _, status := range []string{"pending", "deleted"} {
			expires := time.Now().Add(time.Hour)
			if status == "pending" {
				expires = time.Now().Add(-time.Hour)
			}
			invite := f.invite(t, f.email, status, expires)
			for _, action := range []string{"accept", "decline"} {
				f.respond(t, invite, action, http.StatusConflict)
			}
		}
	})
	stored, err := f.app.db.GetAdminInvite(t.Context(), f.organization, valid.ExternalID)
	if err != nil || stored.Status != "pending" {
		t.Fatalf("失败请求改变了邀请：%+v %v", stored, err)
	}
	if got := f.loadBootstrap(t, nil); len(got.Account.Memberships) != 1 {
		t.Fatalf("失败请求增加了成员关系：%+v", got.Account.Memberships)
	}
}

func TestOrganizationJoiningLifecycle(t *testing.T) {
	f := newJoiningFixture(t)
	invite := f.invite(t, f.email, "pending", time.Now().Add(24*time.Hour))
	f.cookies = joiningLogin(t, f.app, strings.ToUpper(f.email))
	f.bootstrap = f.loadBootstrap(t, nil)
	ownOrg := responseCookie(f.cookies, "lastActiveOrg").Value
	if ownOrg == f.organization || f.bootstrap.Account.DefaultOrganizationUUID != ownOrg || f.bootstrap.Account.Email != f.email || len(f.bootstrap.Account.Memberships) != 1 {
		t.Fatalf("首次登录必须创建自己的组织：%+v", f.bootstrap)
	}
	var pending struct {
		Data []struct {
			ID               string    `json:"id"`
			OrganizationUUID string    `json:"organization_uuid"`
			OrganizationName string    `json:"organization_name"`
			Role             string    `json:"role"`
			InvitedAt        time.Time `json:"invited_at"`
			ExpiresAt        time.Time `json:"expires_at"`
		} `json:"data"`
	}
	decodeJSON(t, f.request(t, http.MethodGet, "/api/invitations", nil, nil, http.StatusOK).Body, &pending)
	if len(pending.Data) != 1 {
		t.Fatalf("首次登录消费了邀请或泄漏其他邮箱：%+v", pending)
	}
	row := pending.Data[0]
	if row.ID != invite.ExternalID || row.OrganizationUUID != f.organization || row.OrganizationName == "" || row.Role != "developer" || row.InvitedAt.IsZero() || !row.ExpiresAt.After(row.InvitedAt) {
		t.Fatalf("邀请列表合同错误：%+v", row)
	}
	f.assertNoImplicitAccess(t)
	for range 2 {
		f.respond(t, invite, "accept", http.StatusOK)
	}
	joined := f.loadBootstrap(t, map[string]string{"X-Organization-UUID": f.organization})
	if joined.Account.UUID != f.bootstrap.Account.UUID || joined.Account.DefaultOrganizationUUID != ownOrg || len(joined.Account.Memberships) != 2 {
		t.Fatalf("接受邀请改变账号身份或默认组织：%+v", joined)
	}
	member := joiningMember(t, joined, f.organization)
	if member.UserUUID == f.bootstrap.Account.UUID || member.UserID == "" || member.Role != "developer" {
		t.Fatalf("缺少独立的组织成员身份：%+v", member)
	}
	f.assertNoImplicitAccess(t)
	loginAgain := f
	loginAgain.cookies = joiningLogin(t, f.app, f.email)
	if got := loginAgain.loadBootstrap(t, nil); got.Account.UUID != f.bootstrap.Account.UUID || got.Account.DefaultOrganizationUUID != ownOrg {
		t.Fatal("加入组织后重新登录丢失注册身份")
	}
	for _, org := range []string{f.organization, ownOrg, f.organization} {
		f.request(t, http.MethodGet, "/v1/environments?beta=true", nil, map[string]string{"X-Organization-UUID": org, "X-Workspace-ID": "default"}, http.StatusOK)
		if got := f.loadBootstrap(t, map[string]string{"X-Organization-UUID": org}); got.Account.UUID != f.bootstrap.Account.UUID {
			t.Fatal("切换组织改变账号 UUID")
		}
	}
	staleHeaders := map[string]string{"X-Organization-UUID": uuid.NewV4().String(), "X-Workspace-ID": "workspace_missing"}
	f.request(t, http.MethodGet, "/v1/environments?beta=true", nil, staleHeaders, http.StatusForbidden)
	if got := f.loadBootstrap(t, staleHeaders); got.Account.UUID != f.bootstrap.Account.UUID {
		t.Fatal("过期组织上下文影响 bootstrap")
	}
	staleCookie := f
	staleCookie.cookies = []*http.Cookie{responseCookie(f.cookies, "sessionKey"), {Name: "lastActiveOrg", Value: uuid.NewV4().String()}}
	if got := staleCookie.loadBootstrap(t, map[string]string{"X-Workspace-ID": "workspace_missing"}); got.Account.UUID != f.bootstrap.Account.UUID {
		t.Fatal("过期组织 cookie 影响 bootstrap")
	}
	if removed, err := f.app.db.RemoveOrgUser(t.Context(), f.organization, member.UserUUID); err != nil || !removed {
		t.Fatalf("移除成员：%t %v", removed, err)
	}
	f.respond(t, invite, "accept", http.StatusConflict)
	f.request(t, http.MethodGet, "/v1/environments?beta=true", nil, map[string]string{"X-Organization-UUID": f.organization}, http.StatusForbidden)
	f.request(t, http.MethodGet, "/v1/environments?beta=true", nil, map[string]string{"X-Organization-UUID": ownOrg}, http.StatusOK)
	after := f.loadBootstrap(t, map[string]string{"X-Organization-UUID": f.organization, "X-Workspace-ID": "workspace_missing"})
	if after.Account.UUID != f.bootstrap.Account.UUID || len(after.Account.Memberships) != 1 || after.Account.DefaultOrganizationUUID != ownOrg {
		t.Fatalf("移除后没有恢复注册组织：%+v", after)
	}
	declined := f.invite(t, f.email, "pending", time.Now().Add(time.Hour))
	for range 2 {
		f.respond(t, declined, "decline", http.StatusOK)
	}
	f.respond(t, declined, "accept", http.StatusConflict)
	owner := f
	owner.cookies = f.ownerCookies
	var admin struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decodeJSON(t, owner.request(t, http.MethodGet, "/v1/organizations/invites/"+declined.ExternalID, nil, map[string]string{"X-Organization-UUID": f.organization}, http.StatusOK).Body, &admin)
	if admin.ID != declined.ExternalID || admin.Status != "deleted" {
		t.Fatalf("Admin declined 兼容映射：%+v", admin)
	}
	var console []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decodeJSON(t, owner.request(t, http.MethodGet, "/api/console/organizations/"+f.organization+"/invites?status=deleted", nil, nil, http.StatusOK).Body, &console)
	if len(console) != 1 || console[0].ID != declined.ExternalID || console[0].Status != "deleted" {
		t.Fatalf("Console declined 兼容映射：%+v", console)
	}
}

func joiningMember(t *testing.T, bootstrap joiningBootstrap, org string) joiningMembership {
	t.Helper()
	for _, member := range bootstrap.Account.Memberships {
		if member.Organization.UUID == org {
			return member
		}
	}
	t.Fatalf("缺少组织 %s 的 membership", org)
	return joiningMembership{}
}

func (f joiningFixture) assertNoImplicitAccess(t *testing.T) {
	t.Helper()
	for table, count := range f.implicitAccessCounts(t) {
		if count != f.accessCounts[table] {
			t.Fatalf("加入组织改变了 %s 数量：%d，原为 %d", table, count, f.accessCounts[table])
		}
	}
}

func (f joiningFixture) implicitAccessCounts(t *testing.T) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	// 仅检查当前测试创建的目标组织；API key 的组织归属通过工作区确定。
	for table, query := range map[string]string{
		"workspace_members": "SELECT count(*) FROM workspace_members WHERE organization_uuid=$1",
		"api_keys":          "SELECT count(*) FROM api_keys k JOIN workspaces w ON w.uuid=k.workspace_uuid WHERE w.organization_uuid=$1",
		"console_api_keys":  "SELECT count(*) FROM console_api_keys WHERE organization_uuid=$1",
	} {
		var count int
		if err := f.app.pool.QueryRow(t.Context(), query, f.organization).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func TestOrganizationJoiningResourceIdentity(t *testing.T) {
	f := newJoiningFixture(t)
	f.cookies = joiningLogin(t, f.app, f.email)
	f.bootstrap = f.loadBootstrap(t, nil)
	f.respond(t, f.invite(t, f.email, "pending", time.Now().Add(time.Hour)), "accept", http.StatusOK)
	member := joiningMember(t, f.loadBootstrap(t, nil), f.organization)
	workspaces, err := f.app.db.ListConsoleWorkspaces(t.Context(), f.organization, false)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := platform.ResolveWorkspaceScope("default", workspaces)
	if err != nil {
		t.Fatal(err)
	}
	seedTestLLMProviderForWorkspace(t, f.app, f.organization, workspace.UUID, "验收占位模型", "https://llm.example.invalid", "test-only-key", "claude-opus-4-6")
	headers := map[string]string{"X-Organization-UUID": f.organization, "X-Workspace-ID": "default", "X-CSRF-Token": f.bootstrap.CSRFToken, "anthropic-beta": "managed-agents-2026-04-01,skills-2025-10-02,files-api-2025-04-14", "anthropic-version": "2023-06-01"}
	create := func(path, body string) string {
		var result struct {
			ID string `json:"id"`
		}
		decodeJSON(t, f.request(t, http.MethodPost, path+"?beta=true", strings.NewReader(body), headers, http.StatusOK).Body, &result)
		if result.ID == "" {
			t.Fatalf("%s 未返回资源 ID", path)
		}
		return result.ID
	}
	agent := create("/v1/agents", `{"name":"加入组织验收","model":"claude-opus-4-6"}`)
	environment := create("/v1/environments", `{"name":"加入组织验收"}`)
	create("/v1/vaults", `{"display_name":"加入组织验收"}`)
	create("/v1/memory_stores", `{"name":"加入组织验收"}`)
	fileBody, contentType := multipartBody(t, "joining.txt", "text/plain", []byte("组织隔离验收"), false)
	headers["Content-Type"] = contentType
	f.request(t, http.MethodPost, "/v1/files?beta=true", fileBody, headers, http.StatusOK)
	skillBody, contentType := skillMultipartBody(t, "Joining Skill", []skillUploadFile{{Filename: "joining/SKILL.md", Content: "---\nname: Joining Skill\ndescription: 组织加入验收\n---\n# 验收\n"}})
	headers["Content-Type"] = contentType
	f.request(t, http.MethodPost, "/v1/skills?beta=true", skillBody, headers, http.StatusOK)
	headers["Content-Type"] = "application/json"
	sessionID := create("/v1/sessions", `{"agent":`+quoteJSON(agent)+`,"environment_id":`+quoteJSON(environment)+`}`)
	session, found, err := f.app.db.GetSession(t.Context(), workspace.UUID, sessionID)
	if err != nil || !found || session.RuntimeUserUUID != member.UserUUID || session.CreatedByAPIKeyUUID != "" {
		t.Fatalf("会话必须绑定目标组织用户：%+v %t %v", session, found, err)
	}
	for _, table := range []string{"files", "skills", "vaults", "memory_stores", "agents", "environments", "sessions"} {
		var count, creators int
		if err := f.app.pool.QueryRow(t.Context(), "SELECT count(*),count(created_by_api_key_uuid) FROM "+table+" WHERE workspace_uuid=$1", workspace.UUID).Scan(&count, &creators); err != nil {
			t.Fatal(err)
		}
		if count == 0 || creators != 0 {
			t.Fatalf("%s 未创建或冒用 API key：%d/%d", table, count, creators)
		}
	}
	f.assertNoImplicitAccess(t)
	if got := f.loadBootstrap(t, nil); got.Account.UUID != f.bootstrap.Account.UUID {
		t.Fatal("资源操作改变登录账号")
	}
}
