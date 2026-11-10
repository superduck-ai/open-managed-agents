//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type platformResourceFixture struct {
	keylessPlatformFixture
	rawKey string
}

func newPlatformResourceFixture(t *testing.T) platformResourceFixture {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("platform-admin-runtime"))
	cookies := app.platformLoginCookies(t, "operator@example.com")
	login, err := app.sessions.Get(t.Context(), responseCookie(cookies, "sessionKey").Value)
	if err != nil {
		t.Fatal(err)
	}
	ownerCookies := app.platformLoginCookies(t, "organization-owner@example.com")
	owner, err := app.sessions.Get(t.Context(), responseCookie(ownerCookies, "sessionKey").Value)
	if err != nil {
		t.Fatal(err)
	}
	service := platformadmin.New(app.db, nil)
	if err := service.SetAdministrator(t.Context(), login.UserUUID, "回归测试", "", true); err != nil {
		t.Fatal(err)
	}
	org, _, err := service.CreateOrganization(t.Context(), platformadmin.Actor{AccountUUID: login.UserUUID}, platformadmin.CreateOrganization{Name: "目标组织", AdminAccountUUIDs: []string{owner.UserUUID}}, "resource-runtime")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := app.db.GetAdminWorkspace(t.Context(), org.UUID, "default")
	if err != nil {
		t.Fatal(err)
	}
	seedTestLLMProviderForWorkspace(t, app, org.UUID, workspace.UUID, "测试模型", "https://llm.example.com", "test-provider", "claude-opus-4-6")
	return platformResourceFixture{keylessPlatformFixture: keylessPlatformFixture{app: app, cookies: cookies, workspace: workspace, login: login, email: "operator@example.com"}}
}

func (f platformResourceFixture) call(t *testing.T, method, path string, body io.Reader, contentType string, status int) []byte {
	t.Helper()
	cookies := f.cookies
	headers := map[string]string{"X-Organization-UUID": f.workspace.OrganizationUUID, "X-Workspace-ID": f.workspace.ExternalID, "Content-Type": contentType, "anthropic-beta": "managed-agents-2026-04-01,skills-2025-10-02,files-api-2025-04-14,webhooks-2026-03-01,message-batches-2024-09-24", "anthropic-version": "2023-06-01"}
	if f.rawKey != "" {
		cookies = nil
		headers["X-Api-Key"] = f.rawKey
	}
	resp := f.app.platformRequestWithHeaders(t, method, path, body, cookies, headers)
	defer resp.Body.Close()
	payload := readAll(t, resp.Body)
	if resp.StatusCode != status {
		t.Fatalf("%s %s：状态 %d，预期 %d：%s", method, path, resp.StatusCode, status, payload)
	}
	return payload
}

func (f platformResourceFixture) resource(t *testing.T, method, path, body string) string {
	t.Helper()
	payload := f.call(t, method, path, strings.NewReader(body), "application/json", http.StatusOK)
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	return result.ID
}

func TestPlatformAdminResourceRuntime(t *testing.T) {
	f := newPlatformResourceFixture(t)
	keysPath := "/api/console/organizations/" + f.workspace.OrganizationUUID + "/workspaces/" + f.workspace.ExternalID + "/api_keys"
	f.call(t, http.MethodPost, keysPath, strings.NewReader(`{"name":"伪造身份","runtime_user_uuid":"客户端不能指定"}`), "application/json", 400)
	payload := f.call(t, http.MethodPost, keysPath, strings.NewReader(`{"name":"跨组织运行账号"}`), "application/json", 200)
	var key struct {
		ID     string `json:"id"`
		RawKey string `json:"raw_key"`
	}
	if err := json.Unmarshal(payload, &key); err != nil || key.RawKey == "" {
		t.Fatalf("创建密钥失败：%v", err)
	}
	authenticated, err := f.app.db.GetAPIKey(t.Context(), auth.HashAPIKey(key.RawKey))
	if err != nil || authenticated.RuntimeUserUUID != f.login.UserUUID {
		t.Fatalf("密钥运行账号错误：%v", err)
	}
	var creator *string
	if err := f.app.pool.QueryRow(t.Context(), "SELECT created_by_user_uuid::text FROM api_keys WHERE external_id=$1", key.ID).Scan(&creator); err != nil || creator != nil {
		t.Fatalf("跨组织账号被写为成员创建者：%v", err)
	}
	api := f
	api.rawKey = key.RawKey
	api.call(t, http.MethodGet, "/api/platform-admin/organizations", nil, "application/json", http.StatusForbidden)
	api.call(t, http.MethodGet, "/api/console/organizations/"+f.workspace.OrganizationUUID+"/members", nil, "application/json", http.StatusUnauthorized)
	if err := f.app.deploymentJobs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.app.deploymentJobs.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	for _, mode := range []string{"平台会话", "API密钥"} {
		t.Run(mode, func(t *testing.T) {
			scoped := f
			if mode == "API密钥" {
				scoped = api
			}
			testPlatformResourceUse(t, scoped)
		})
	}
	if _, err := f.app.db.GetActivePlatformUserByEmail(t.Context(), f.workspace.OrganizationUUID, f.email); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("不应创建组织成员：%v", err)
	}
	t.Run("新建渠道凭据保存相同运行账号", func(t *testing.T) {
		raw, err := f.app.db.EnsureChannelWorkspaceCredential(t.Context(), platform.CreateConsoleAPIKeyInput{OrgUUID: f.workspace.OrganizationUUID, WorkspaceUUID: f.workspace.UUID, RuntimeUserUUID: f.login.UserUUID}, f.app.vaultSecrets)
		if err != nil {
			t.Fatal(err)
		}
		channel := f
		channel.rawKey = raw
		testPlatformResourceUse(t, channel)
	})
	t.Run("失效账号导致新建密钥事务回滚", func(t *testing.T) {
		before, err := f.app.db.CountConsoleAPIKeys(t.Context(), f.workspace.OrganizationUUID, f.workspace.UUID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.app.db.CreateConsoleAPIKey(t.Context(), platform.CreateConsoleAPIKeyInput{OrgUUID: f.workspace.OrganizationUUID, WorkspaceUUID: f.workspace.UUID, RuntimeUserUUID: uuid.NewV4().String(), Name: "不可创建"})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("失效运行账号应拒绝创建：%v", err)
		}
		after, err := f.app.db.CountConsoleAPIKeys(t.Context(), f.workspace.OrganizationUUID, f.workspace.UUID)
		if err != nil || before != after {
			t.Fatalf("密钥创建未回滚：%d -> %d：%v", before, after, err)
		}
	})
	f.call(t, http.MethodPost, keysPath, strings.NewReader(`{"name":"过期但允许创建","expires_at":"2000-01-01T00:00:00Z"}`), "application/json", 200)
	t.Run("运行账号删除后拒绝签发", func(t *testing.T) {
		var sessionID string
		if err := f.app.pool.QueryRow(t.Context(), "SELECT external_id FROM sessions WHERE workspace_uuid=$1 ORDER BY id LIMIT 1", f.workspace.UUID).Scan(&sessionID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.pool.Exec(t.Context(), "UPDATE users SET deleted_at=NOW() WHERE uuid=$1", f.login.UserUUID); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := f.app.pool.Exec(t.Context(), "UPDATE users SET deleted_at=NULL WHERE uuid=$1", f.login.UserUUID); err != nil {
				t.Error(err)
			}
		}()
		if _, err := f.app.db.GetFilestoreTokenScopeForSessionIssue(t.Context(), f.workspace.UUID, sessionID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("失效账号不应被替换：%v", err)
		}
	})
	f.call(t, http.MethodPost, keysPath+"/"+key.ID, strings.NewReader(`{"status":"inactive"}`), "application/json", 200)
	api.call(t, http.MethodGet, "/v1/agents?beta=true", nil, "application/json", 401)
}

func testPlatformResourceUse(t *testing.T, f platformResourceFixture) {
	t.Helper()
	agent := f.resource(t, "POST", "/v1/agents?beta=true", `{"model":"claude-opus-4-6","name":"运行测试","tools":[{"type":"agent_toolset_20260401"}]}`)
	env := f.resource(t, "POST", "/v1/environments?beta=true", `{"name":"运行测试-`+uuid.NewV4().String()+`"}`)
	storeName := "memory-" + uuid.NewV4().String()
	store := f.resource(t, "POST", "/v1/memory_stores?beta=true", `{"name":`+quoteJSON(storeName)+`}`)
	f.resource(t, "POST", "/v1/memory_stores/"+store+"/memories?beta=true&view=full", `{"path":"/note","content":"跨组织记忆"}`)
	f.resource(t, "GET", "/v1/memory_stores/"+store+"/memories?beta=true", "")
	vault := f.resource(t, "POST", "/v1/vaults?beta=true", `{"display_name":"运行凭据"}`)
	credential := f.resource(t, "POST", "/v1/vaults/"+vault+"/credentials?beta=true", staticBearerBody("测试凭据", "https://mcp.example.com/mcp", "test-secret"))
	f.resource(t, "GET", "/v1/vaults/"+vault+"/credentials/"+credential+"?beta=true", "")
	webhook := f.resource(t, "POST", "/v1/webhooks?beta=true", `{"name":"运行回调","url":"https://example.com/hook","enabled_events":["session.status_idled"]}`)
	f.resource(t, "GET", "/v1/webhooks/"+webhook+"?beta=true", "")
	batch := f.resource(t, "POST", "/v1/messages/batches?beta=true", minimalBatchBody("platform-admin"))
	f.resource(t, "GET", "/v1/messages/batches/"+batch+"?beta=true", "")
	fileBody, contentType := multipartBody(t, "test.txt", "text/plain", []byte("文件内容"), false)
	var file struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(f.call(t, "POST", "/v1/files?beta=true", fileBody, contentType, 200), &file); err != nil {
		t.Fatal(err)
	}
	f.resource(t, "GET", "/v1/files/"+file.ID+"?beta=true", "")
	skillName := "platform-" + uuid.NewV4().String()
	skillBody, contentType := skillMultipartBody(t, skillName, []skillUploadFile{{Filename: "platform/SKILL.md", Content: "---\nname: " + skillName + "\ndescription: regression\n---\n# 技能\n"}})
	var skill struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(f.call(t, "POST", "/v1/skills?beta=true", skillBody, contentType, 200), &skill); err != nil {
		t.Fatal(err)
	}
	f.resource(t, "GET", "/v1/skills/"+skill.ID+"?beta=true", "")
	f.resource(t, "POST", "/v1/agents/"+agent+"?beta=true", `{"skills":[{"type":"custom","skill_id":`+quoteJSON(skill.ID)+`,"version":"latest"}]}`)
	resources := `"resources":[{"type":"file","file_id":` + quoteJSON(file.ID) + `,"mount_path":"/workspace/test.txt"},{"type":"memory_store","memory_store_id":` + quoteJSON(store) + `}],"vault_ids":[` + quoteJSON(vault) + `]`
	sessionID := f.resource(t, "POST", "/v1/sessions?beta=true", `{"agent":`+quoteJSON(agent)+`,"environment_id":`+quoteJSON(env)+`,`+resources+`,"metadata":{"_oma_runtime_user_uuid":"不能伪造"}}`)
	assertPlatformSessionRuntime(t, f, sessionID, storeName)
	schedule := deploymentjobs.Schedule{Type: "cron", Expression: "0 0 1 1 *", Timezone: "UTC"}
	deployment := f.resource(t, "POST", "/v1/deployments?beta=true", deploymentBodyWithExtra(agent, env, resources+`,"schedule":{"type":"cron","expression":"0 0 1 1 *","timezone":"UTC"}`))
	runPayload := f.call(t, "POST", "/v1/deployments/"+deployment+"/run?beta=true", strings.NewReader(`{}`), "application/json", 200)
	var run struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(runPayload, &run); err != nil {
		t.Fatal(err)
	}
	assertPlatformSessionRuntime(t, f, run.SessionID, storeName)
	now := time.Now().UTC()
	if _, err := f.app.deploymentJobs.Insert(t.Context(), deploymentjobs.Args{WorkspaceUUID: f.workspace.UUID, DeploymentExternalID: deployment, Schedule: schedule}, &river.InsertOpts{Queue: deploymentjobs.Queue, ScheduledAt: now, MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	var scheduledID string
	deploymentWait(t, "平台账号定时运行", 10*time.Second, func() bool {
		err := f.app.pool.QueryRow(t.Context(), `SELECT r.session_external_id FROM deployment_runs r JOIN deployments d ON d.uuid=r.deployment_uuid WHERE d.external_id=$1 AND r.trigger_type='schedule'`, deployment).Scan(&scheduledID)
		return err == nil
	})
	assertPlatformSessionRuntime(t, f, scheduledID, storeName)
}

func assertPlatformSessionRuntime(t *testing.T, f platformResourceFixture, sessionID, storeName string) {
	t.Helper()
	session, found, err := f.app.db.GetSession(t.Context(), f.workspace.UUID, sessionID)
	if err != nil || !found || session.RuntimeUserUUID != f.login.UserUUID {
		t.Fatalf("会话运行账号错误：%s：%v", sessionID, err)
	}
	if f.rawKey != "" {
		if _, err := f.app.pool.Exec(t.Context(), "UPDATE sessions SET metadata=metadata-'_oma_runtime_user_uuid' WHERE uuid=$1", session.UUID); err != nil {
			t.Fatal(err)
		}
	}
	scope, err := f.app.db.GetFilestoreTokenScopeForSessionIssue(t.Context(), f.workspace.UUID, sessionID)
	if err != nil || scope.AccountUUID != f.login.UserUUID {
		t.Fatalf("Filestore 签发身份错误：%v", err)
	}
	token, err := f.app.filestoreCredentials.Issue(filestore.TokenIdentity{Subject: scope.AccountExternalID, OrgUUID: scope.OrganizationUUID, AccountUUID: scope.AccountUUID, WorkspaceUUID: scope.WorkspaceUUID, WorkspaceTaggedID: scope.WorkspaceExternalID, ResolvedWorkspaceTaggedID: scope.WorkspaceExternalID, FilesystemID: scope.FilesystemExternalID, OrgTaints: scope.OrgTaints, WorkspaceCMEKEnabled: scope.WorkspaceCMEKEnabled})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(struct {
		FilesystemID string `json:"filesystemId"`
		Path         string `json:"path"`
	}{scope.FilesystemExternalID, "/"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.app.baseURL+"/v1/filestore/fs/listDirectory", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("Filestore 运行令牌回查失败：%d %s", resp.StatusCode, readAll(t, resp.Body))
	}
	for _, resource := range []struct{ path, content string }{{"/memory/" + storeName + "/note", "跨组织记忆"}} {
		encoded, err := json.Marshal(struct {
			FilesystemID string `json:"filesystemId"`
			Path         string `json:"path"`
		}{scope.FilesystemExternalID, resource.path})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), "POST", f.app.baseURL+"/v1/filestore/fs/readFile", bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, err := f.app.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		content := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || string(content) != resource.content {
			t.Fatalf("挂载资源内容错误：%s：状态 %d：%s", resource.path, response.StatusCode, content)
		}
	}
	_, err = f.app.db.ResolveFilestoreTokenScope(t.Context(), scope.OrganizationUUID, f.login.UserUUID, f.login.WorkspaceUUID, scope.WorkspaceExternalID, scope.WorkspaceExternalID, scope.FilesystemExternalID)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("跨工作区令牌应拒绝：%v", err)
	}
	code, err := f.app.db.CreateCodeSession(t.Context(), db.CreateCodeSessionInput{ExternalID: "codesession_" + uuid.NewV4().String(), OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID, Metadata: []byte(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := f.app.db.GetCodeSessionCredentialContextForIssue(t.Context(), session.OrganizationUUID, session.WorkspaceUUID, code.ExternalID)
	if err != nil || credential.AccountEmail != f.email {
		t.Fatalf("运行 Git 邮箱错误：%v", err)
	}
	request, err := http.NewRequestWithContext(t.Context(), "POST", "https://mcp.example.com/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &kmsUpstream{}
	transport := vaults.NewInjector(f.app.db, f.app.vaultSecrets, nil).WrapMCPTransport(t.Context(), code.ExternalID, session.OrganizationUUID, session.WorkspaceUUID, request.URL, recorder)
	response, err := transport.RoundTrip(request)
	if response != nil {
		response.Body.Close()
	}
	if err != nil || recorder.authorization != "Bearer test-secret" {
		t.Fatalf("Vault 运行凭据注入失败：%v", err)
	}
	if f.rawKey != "" {
		if _, err := f.app.pool.Exec(t.Context(), "UPDATE sessions SET metadata=jsonb_set(metadata,'{_oma_runtime_user_uuid}','\"invalid\"') WHERE uuid=$1", session.UUID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.db.GetFilestoreTokenScopeForSessionIssue(t.Context(), f.workspace.UUID, sessionID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("非法身份不能回退换人：%v", err)
		}
		invalid, err := f.app.db.GetCodeSessionCredentialContextForIssue(t.Context(), session.OrganizationUUID, session.WorkspaceUUID, code.ExternalID)
		if err != nil || invalid.AccountEmail != "" {
			t.Fatalf("非法身份不能回退邮箱：%v", err)
		}
	}
}
