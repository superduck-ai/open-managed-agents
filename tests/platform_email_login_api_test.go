package tests

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestPlatformEmailLoginRoutes(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-email-login-bucket"))
	defer app.close()

	methodsResp := app.platformRequest(t, http.MethodGet, "/api/auth/login_methods", nil, nil)
	defer methodsResp.Body.Close()
	if methodsResp.StatusCode != http.StatusOK {
		t.Fatalf("login methods status = %d, want 200: %s", methodsResp.StatusCode, readAll(t, methodsResp.Body))
	}
	var methods map[string]any
	decodeJSON(t, methodsResp.Body, &methods)
	if got, ok := methods["methods"].([]any); !ok || len(got) != 2 || got[0] != "google" || got[1] != "magic_link" {
		t.Fatalf("login methods = %#v, want google and magic_link", methods)
	}

	sendResp := app.platformRequest(t, http.MethodPost, "/api/auth/send_magic_link", strings.NewReader(`{"email_address":"ada@example.com"}`), nil)
	defer sendResp.Body.Close()
	if sendResp.StatusCode != http.StatusOK {
		t.Fatalf("send magic link status = %d, want 200: %s", sendResp.StatusCode, readAll(t, sendResp.Body))
	}
	var send map[string]any
	decodeJSON(t, sendResp.Body, &send)
	if send["sent"] != true || send["fallback_code_configuration"] != nil || send["sso_url"] != nil || send["magic_link_intent_available"] != nil {
		t.Fatalf("send magic link = %#v, want source-compatible sent response", send)
	}

	verifyResp := app.platformRequest(t, http.MethodPost, "/api/auth/verify_magic_link", strings.NewReader(`{"credentials":{"method":"code","code":"123456","email_address":"Ada.Login@Example.com"}}`), nil)
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("verify magic link status = %d, want 200: %s", verifyResp.StatusCode, readAll(t, verifyResp.Body))
	}
	var verify map[string]any
	decodeJSON(t, verifyResp.Body, &verify)
	account, ok := verify["account"].(map[string]any)
	if verify["success"] != true || verify["created"] != true || !ok || account["email_address"] != "ada.login@example.com" {
		t.Fatalf("verify magic link = %#v, want verified lower-case account", verify)
	}
	if account["full_name"] == "" || account["display_name"] == "" {
		t.Fatalf("verify magic link account names = %#v/%#v, want populated names", account["full_name"], account["display_name"])
	}
	if verify["secret"] != nil || verify["state"] != nil {
		t.Fatalf("web verify should not include android fields: %#v", verify)
	}
	sessionCookie := responseCookie(verifyResp.Cookies(), "sessionKey")
	orgCookie := responseCookie(verifyResp.Cookies(), "lastActiveOrg")
	if sessionCookie == nil || !strings.HasPrefix(sessionCookie.Value, "sk-ant-sid-session-key-") || orgCookie == nil || orgCookie.Value == "" {
		t.Fatalf("verify cookies = %#v", verifyResp.Cookies())
	}
	if defaultOrgUUID := loadDefaultOrganizationUUID(t, app); orgCookie.Value == defaultOrgUUID {
		t.Fatalf("magic-link signup org = %s, want a user-specific organization, not org_default", orgCookie.Value)
	}

	bootstrapResp := app.platformRequest(t, http.MethodGet, "/api/bootstrap", nil, []*http.Cookie{sessionCookie, orgCookie})
	defer bootstrapResp.Body.Close()
	if bootstrapResp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status = %d, want 200: %s", bootstrapResp.StatusCode, readAll(t, bootstrapResp.Body))
	}
	var bootstrap map[string]any
	decodeJSON(t, bootstrapResp.Body, &bootstrap)
	bootstrapAccount, ok := bootstrap["account"].(map[string]any)
	if !ok || bootstrapAccount["email_address"] != "ada.login@example.com" {
		t.Fatalf("bootstrap account = %#v, want magic-link account", bootstrap["account"])
	}
	if bootstrapAccount["full_name"] == "" || bootstrapAccount["display_name"] == "" {
		t.Fatalf("bootstrap account names = %#v/%#v, want populated names", bootstrapAccount["full_name"], bootstrapAccount["display_name"])
	}
	memberships, ok := bootstrapAccount["memberships"].([]any)
	if !ok || len(memberships) != 1 {
		t.Fatalf("bootstrap memberships = %#v, want one signup organization", bootstrapAccount["memberships"])
	}
	membership, ok := memberships[0].(map[string]any)
	if !ok {
		t.Fatalf("bootstrap membership = %#v, want object", memberships[0])
	}
	organization, ok := membership["organization"].(map[string]any)
	if !ok || organization["uuid"] != orgCookie.Value {
		t.Fatalf("bootstrap organization = %#v, want selected org %s", membership["organization"], orgCookie.Value)
	}
	workspacesResp := app.platformRequest(t, http.MethodGet, "/api/console/organizations/"+orgCookie.Value+"/workspaces", nil, []*http.Cookie{sessionCookie, orgCookie})
	defer workspacesResp.Body.Close()
	if workspacesResp.StatusCode != http.StatusOK {
		t.Fatalf("signup workspaces status = %d, want 200: %s", workspacesResp.StatusCode, readAll(t, workspacesResp.Body))
	}
	var workspaces []map[string]any
	decodeJSON(t, workspacesResp.Body, &workspaces)
	if len(workspaces) != 0 {
		t.Fatalf("signup workspaces = %#v, want no custom workspaces in console list", workspaces)
	}
	var defaultWorkspaceCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from organizations o
		join workspaces w on w.organization_id = o.id
		where o.uuid::text = $1
		  and lower(w.name) = 'default'
		  and w.archived_at is null
	`, orgCookie.Value).Scan(&defaultWorkspaceCount); err != nil {
		t.Fatalf("count default workspace: %v", err)
	}
	if defaultWorkspaceCount != 1 {
		t.Fatalf("default workspace count = %d, want 1", defaultWorkspaceCount)
	}

	if err := app.sessions.Delete(context.Background(), sessionCookie.Value); err != nil {
		t.Fatalf("delete platform session: %v", err)
	}
	recoveredWorkspacesResp := app.platformRequest(t, http.MethodGet, "/api/console/organizations/"+orgCookie.Value+"/workspaces", nil, []*http.Cookie{sessionCookie, orgCookie})
	defer recoveredWorkspacesResp.Body.Close()
	if recoveredWorkspacesResp.StatusCode != http.StatusOK {
		t.Fatalf("recovered workspaces status = %d, want 200: %s", recoveredWorkspacesResp.StatusCode, readAll(t, recoveredWorkspacesResp.Body))
	}
	if _, err := app.sessions.Get(context.Background(), sessionCookie.Value); err != nil {
		t.Fatalf("recovered platform session was not saved: %v", err)
	}

	envResp := app.platformRequest(t, http.MethodGet, "/v1/environments?beta=true&include_archived=false&limit=20", nil, []*http.Cookie{sessionCookie, orgCookie})
	defer envResp.Body.Close()
	if envResp.StatusCode != http.StatusOK {
		t.Fatalf("platform environments status = %d, want 200: %s", envResp.StatusCode, readAll(t, envResp.Body))
	}
	headerEnvResp := app.platformRequestWithHeaders(t, http.MethodGet, "/v1/environments?beta=true&include_archived=false&limit=5", nil, []*http.Cookie{sessionCookie, orgCookie}, map[string]string{
		"X-Organization-UUID": orgCookie.Value,
		"X-Workspace-ID":      "default",
	})
	defer headerEnvResp.Body.Close()
	if headerEnvResp.StatusCode != http.StatusOK {
		t.Fatalf("platform environments with context headers status = %d, want 200: %s", headerEnvResp.StatusCode, readAll(t, headerEnvResp.Body))
	}
	invalidOrgResp := app.platformRequestWithHeaders(t, http.MethodGet, "/v1/environments?beta=true&include_archived=false&limit=5", nil, []*http.Cookie{sessionCookie, orgCookie}, map[string]string{
		"X-Organization-UUID": "00000000-0000-4000-8000-000000000000",
	})
	defer invalidOrgResp.Body.Close()
	if invalidOrgResp.StatusCode != http.StatusForbidden {
		t.Fatalf("platform environments invalid org header status = %d, want 403: %s", invalidOrgResp.StatusCode, readAll(t, invalidOrgResp.Body))
	}

	vaultResp := app.platformRequest(t, http.MethodGet, "/v1/vaults?beta=true&include_archived=false", nil, []*http.Cookie{sessionCookie, orgCookie})
	defer vaultResp.Body.Close()
	if vaultResp.StatusCode != http.StatusOK {
		t.Fatalf("platform vaults status = %d, want 200: %s", vaultResp.StatusCode, readAll(t, vaultResp.Body))
	}

	invalidBootstrapResp := app.platformRequest(t, http.MethodGet, "/api/bootstrap", nil, []*http.Cookie{{Name: "sessionKey", Value: "missing-session"}, {Name: "lastActiveOrg", Value: orgCookie.Value}})
	defer invalidBootstrapResp.Body.Close()
	if invalidBootstrapResp.StatusCode != http.StatusOK {
		t.Fatalf("invalid bootstrap status = %d, want 200: %s", invalidBootstrapResp.StatusCode, readAll(t, invalidBootstrapResp.Body))
	}
	var invalidBootstrap map[string]any
	decodeJSON(t, invalidBootstrapResp.Body, &invalidBootstrap)
	if invalidBootstrap["account"] != nil {
		t.Fatalf("invalid bootstrap account = %#v, want logged-out account", invalidBootstrap["account"])
	}
	if cookie := responseCookie(invalidBootstrapResp.Cookies(), "sessionKey"); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("invalid bootstrap cookies = %#v, want expired sessionKey", invalidBootstrapResp.Cookies())
	}
	if cookie := responseCookie(invalidBootstrapResp.Cookies(), "lastActiveOrg"); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("invalid bootstrap cookies = %#v, want expired lastActiveOrg", invalidBootstrapResp.Cookies())
	}

	logoutResp := app.platformRequest(t, http.MethodPost, "/api/auth/logout", nil, []*http.Cookie{sessionCookie})
	defer logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200: %s", logoutResp.StatusCode, readAll(t, logoutResp.Body))
	}
	var logout map[string]bool
	decodeJSON(t, logoutResp.Body, &logout)
	if !logout["ok"] {
		t.Fatalf("logout = %#v, want ok true", logout)
	}
	if cookie := responseCookie(logoutResp.Cookies(), "sessionKey"); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("logout cookies = %#v, want expired sessionKey", logoutResp.Cookies())
	}
	if cookie := responseCookie(logoutResp.Cookies(), "lastActiveOrg"); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("logout cookies = %#v, want expired lastActiveOrg", logoutResp.Cookies())
	}
}

func TestPlatformWorkspaceHeaderScopesV1Agents(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-workspace-agents-bucket"))
	defer app.close()

	cookies := app.platformLoginCookies(t, "workspace-scope@example.com")
	orgCookie := responseCookie(cookies, "lastActiveOrg")
	if orgCookie == nil || orgCookie.Value == "" {
		t.Fatalf("platform login cookies = %#v, want lastActiveOrg", cookies)
	}

	workspaceResp := app.platformRequest(t, http.MethodPost, "/api/console/organizations/"+orgCookie.Value+"/workspaces", strings.NewReader(`{"name":"Scoped agents","display_color":"#9B87F5"}`), cookies)
	defer workspaceResp.Body.Close()
	if workspaceResp.StatusCode != http.StatusOK {
		t.Fatalf("create workspace status = %d, want 200: %s", workspaceResp.StatusCode, readAll(t, workspaceResp.Body))
	}
	var workspace map[string]any
	decodeJSON(t, workspaceResp.Body, &workspace)
	customWorkspaceID := stringValue(workspace["id"])
	if customWorkspaceID == "" {
		t.Fatalf("created workspace = %#v, want id", workspace)
	}

	defaultAgentName := "Default workspace scoped agent"
	customAgentName := "Custom workspace scoped agent"
	createPlatformAgentInWorkspace(t, app, cookies, orgCookie.Value, "default", defaultAgentName)
	createPlatformAgentInWorkspace(t, app, cookies, orgCookie.Value, customWorkspaceID, customAgentName)

	defaultAgents := listPlatformAgentsInWorkspace(t, app, cookies, orgCookie.Value, "default")
	if !hasPlatformAgentName(defaultAgents, defaultAgentName) {
		t.Fatalf("default workspace agents = %#v, want %q", defaultAgents, defaultAgentName)
	}
	if hasPlatformAgentName(defaultAgents, customAgentName) {
		t.Fatalf("default workspace agents = %#v, should not include %q", defaultAgents, customAgentName)
	}

	customAgents := listPlatformAgentsInWorkspace(t, app, cookies, orgCookie.Value, customWorkspaceID)
	if !hasPlatformAgentName(customAgents, customAgentName) {
		t.Fatalf("custom workspace agents = %#v, want %q", customAgents, customAgentName)
	}
	if hasPlatformAgentName(customAgents, defaultAgentName) {
		t.Fatalf("custom workspace agents = %#v, should not include %q", customAgents, defaultAgentName)
	}
}

func TestPlatformEmailLoginAndroidRoutes(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-email-login-android-bucket"))
	defer app.close()

	encodedEmail := base64.RawURLEncoding.EncodeToString([]byte("Mobile.Login@Example.com"))
	verifyResp := app.platformRequest(t, http.MethodPost, "/auth/verify_magic_link", strings.NewReader(`{"credentials":{"method":"nonce","nonce":"nonce-1","encoded_email_address":"`+encodedEmail+`"}}`), nil)
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("android verify status = %d, want 200: %s", verifyResp.StatusCode, readAll(t, verifyResp.Body))
	}
	var verify map[string]any
	decodeJSON(t, verifyResp.Body, &verify)
	if verify["success"] != true || !strings.HasPrefix(stringValue(verify["secret"]), "sk-ant-sid-session-key-") {
		t.Fatalf("android verify = %#v, want secret", verify)
	}
	state, ok := verify["state"].(map[string]any)
	if !ok || state["kind"] != "authenticated" {
		t.Fatalf("android state = %#v, want authenticated", verify["state"])
	}
	stateAccount, ok := state["account"].(map[string]any)
	if !ok || stateAccount["email_address"] != "mobile.login@example.com" {
		t.Fatalf("android state account = %#v, want decoded email account", state["account"])
	}

	sessionCookie := responseCookie(verifyResp.Cookies(), "sessionKey")
	if sessionCookie == nil {
		t.Fatalf("missing session cookie: %#v", verifyResp.Cookies())
	}
	logoutResp := app.platformRequest(t, http.MethodPost, "/auth/logout", nil, []*http.Cookie{sessionCookie})
	defer logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("android logout status = %d, want 200: %s", logoutResp.StatusCode, readAll(t, logoutResp.Body))
	}
	var logout map[string]bool
	decodeJSON(t, logoutResp.Body, &logout)
	if !logout["success"] {
		t.Fatalf("android logout = %#v, want success true", logout)
	}
	if cookie := responseCookie(logoutResp.Cookies(), "sessionKey"); cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("android logout cookies = %#v, want expired sessionKey", logoutResp.Cookies())
	}
}

func (a *testApp) platformRequest(t *testing.T, method string, path string, body io.Reader, cookies []*http.Cookie) *http.Response {
	t.Helper()
	return a.platformRequestWithHeaders(t, method, path, body, cookies, nil)
}

func (a *testApp) platformRequestWithHeaders(t *testing.T, method string, path string, body io.Reader, cookies []*http.Cookie, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, a.baseURL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "platform.claude.com"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func (a *testApp) platformLoginCookies(t *testing.T, email string) []*http.Cookie {
	t.Helper()
	a.ensureDefaultPlatformUser(t, email)
	body := strings.NewReader(`{"credentials":{"method":"code","code":"123456","email_address":"` + email + `"}}`)
	resp := a.platformRequest(t, http.MethodPost, "/api/auth/verify_magic_link", body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("platform login status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	sessionCookie := responseCookie(resp.Cookies(), "sessionKey")
	orgCookie := responseCookie(resp.Cookies(), "lastActiveOrg")
	if sessionCookie == nil || orgCookie == nil {
		t.Fatalf("platform login cookies = %#v, want sessionKey and lastActiveOrg", resp.Cookies())
	}
	return []*http.Cookie{sessionCookie, orgCookie}
}

func (a *testApp) ensureDefaultPlatformUser(t *testing.T, email string) {
	t.Helper()
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		normalizedEmail = "test@qq.com"
	}
	displayName, _, _ := strings.Cut(normalizedEmail, "@")
	displayName = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(displayName)
	if strings.TrimSpace(displayName) == "" {
		displayName = "Platform Test User"
	}
	if _, err := a.db.Pool.Exec(context.Background(), `
		with refs as (
			select o.id as organization_id, w.id as workspace_id, w.external_id as workspace_external_id
			from organizations o
			join workspaces w on w.organization_id = o.id and w.external_id = 'workspace_default'
			where o.external_id = 'org_default'
			limit 1
		),
		existing_user as (
			select u.id, u.external_id
			from users u
			join refs on refs.organization_id = u.organization_id
			where lower(u.email) = lower($1)
			  and u.deleted_at is null
			limit 1
		),
		new_user_uuid as (
			select gen_random_uuid() as value
		),
		inserted_user as (
			insert into users (uuid, external_id, organization_id, email, name, role)
			select new_user_uuid.value,
				'user_' || left(replace(new_user_uuid.value::text, '-', ''), 24),
				refs.organization_id,
				lower($1),
				$2,
				'admin'
			from refs, new_user_uuid
			where not exists (select 1 from existing_user)
			returning id, external_id
		),
		active_user as (
			select id, external_id from existing_user
			union all
			select id, external_id from inserted_user
			limit 1
		),
		new_member_uuid as (
			select gen_random_uuid() as value
		)
		insert into workspace_members (
			external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role
		)
		select
			'wmem_' || left(replace(new_member_uuid.value::text, '-', ''), 24),
			refs.organization_id,
			refs.workspace_id,
			refs.workspace_external_id,
			active_user.id,
			active_user.external_id,
			'workspace_admin'
		from refs, active_user, new_member_uuid
		where not exists (
			select 1
			from workspace_members wm
			where wm.workspace_id = refs.workspace_id
			  and wm.user_id = active_user.id
			  and wm.deleted_at is null
		)
	`, normalizedEmail, displayName); err != nil {
		t.Fatalf("ensure default platform user: %v", err)
	}
}

func responseCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func createPlatformAgentInWorkspace(t *testing.T, app *testApp, cookies []*http.Cookie, orgUUID, workspaceID, name string) {
	t.Helper()
	body := strings.NewReader(`{"name":` + strconv.Quote(name) + `,"model":"claude-sonnet-4-6"}`)
	resp := app.platformRequestWithHeaders(t, http.MethodPost, "/v1/agents?beta=true", body, cookies, map[string]string{
		"X-Organization-UUID": orgUUID,
		"X-Workspace-ID":      workspaceID,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create agent %q in workspace %q status = %d, want 200: %s", name, workspaceID, resp.StatusCode, readAll(t, resp.Body))
	}
	var agent map[string]any
	decodeJSON(t, resp.Body, &agent)
	if stringValue(agent["name"]) != name || stringValue(agent["id"]) == "" {
		t.Fatalf("created agent = %#v, want named agent %q", agent, name)
	}
}

func listPlatformAgentsInWorkspace(t *testing.T, app *testApp, cookies []*http.Cookie, orgUUID, workspaceID string) []map[string]any {
	t.Helper()
	resp := app.platformRequestWithHeaders(t, http.MethodGet, "/v1/agents?beta=true&include_archived=false&limit=20", nil, cookies, map[string]string{
		"X-Organization-UUID": orgUUID,
		"X-Workspace-ID":      workspaceID,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list agents in workspace %q status = %d, want 200: %s", workspaceID, resp.StatusCode, readAll(t, resp.Body))
	}
	var page struct {
		Data []map[string]any `json:"data"`
	}
	decodeJSON(t, resp.Body, &page)
	return page.Data
}

func hasPlatformAgentName(agents []map[string]any, name string) bool {
	for _, agent := range agents {
		if stringValue(agent["name"]) == name {
			return true
		}
	}
	return false
}
