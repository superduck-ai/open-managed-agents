package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

const runtimeIdentityMetadataKey = "_oma_runtime_user_uuid"

type keylessPlatformFixture struct {
	app       *testApp
	cookies   []*http.Cookie
	workspace db.AdminWorkspace
	login     platformsession.Session
	email     string
}

func newKeylessPlatformFixture(t *testing.T) keylessPlatformFixture {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("keyless-runtime"))
	t.Cleanup(app.close)
	email := "keyless-runtime-" + uuid.NewV4().String() + "@example.com"
	cookies := app.platformLoginCookies(t, email)
	login, err := app.sessions.Get(context.Background(), responseCookie(cookies, "sessionKey").Value)
	if err != nil {
		t.Fatal(err)
	}
	id := createPlatformWorkspace(t, app, cookies, login.OrganizationUUID, "Keyless "+uuid.NewV4().String())
	workspace, err := app.db.GetAdminWorkspace(context.Background(), login.OrganizationUUID, id)
	if err != nil {
		t.Fatal(err)
	}
	seedTestLLMProviderForWorkspace(t, app, workspace.OrganizationUUID, workspace.UUID,
		"Keyless provider", "https://llm.example.com", "test-keyless-provider", "claude-opus-4-6")
	return keylessPlatformFixture{app: app, cookies: cookies, workspace: workspace, login: login, email: email}
}

func (f keylessPlatformFixture) request(t *testing.T, method, path string, body io.Reader, contentType string) map[string]json.RawMessage {
	t.Helper()
	resp := f.app.platformRequestWithHeaders(t, method, path, body, f.cookies, map[string]string{
		"X-Organization-UUID": f.workspace.OrganizationUUID, "X-Workspace-ID": f.workspace.ExternalID,
		"Content-Type": contentType, "anthropic-beta": "managed-agents-2026-04-01,skills-2025-10-02,files-api-2025-04-14,webhooks-2026-03-01,message-batches-2024-09-24",
		"anthropic-version": "2023-06-01",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, readAll(t, resp.Body))
	}
	var result map[string]json.RawMessage
	decodeJSON(t, resp.Body, &result)
	if strings.Contains(string(result["metadata"]), runtimeIdentityMetadataKey) {
		t.Fatal("response exposed server-owned runtime identity")
	}
	return result
}

func (f keylessPlatformFixture) json(t *testing.T, method, path, body string) map[string]json.RawMessage {
	t.Helper()
	return f.request(t, method, path, strings.NewReader(body), "application/json")
}

func keylessResourceID(t *testing.T, result map[string]json.RawMessage) string {
	t.Helper()
	var id string
	if err := json.Unmarshal(result["id"], &id); err != nil || id == "" {
		t.Fatalf("missing resource ID: %v", err)
	}
	return id
}

func (f keylessPlatformFixture) agentEnvironment(t *testing.T) (string, string) {
	t.Helper()
	agent := f.json(t, http.MethodPost, "/v1/agents?beta=true", `{"name":"Keyless agent","model":"claude-opus-4-6"}`)
	env := f.json(t, http.MethodPost, "/v1/environments?beta=true", `{"name":"Keyless environment"}`)
	return keylessResourceID(t, agent), keylessResourceID(t, env)
}

func TestPlatformKeylessRuntimeIdentity(t *testing.T) {
	f := newKeylessPlatformFixture(t)
	ctx := context.Background()
	agentID, envID := f.agentEnvironment(t)
	// An invalid client UUID must never reach the runtime UUID cast.
	created := f.json(t, http.MethodPost, "/v1/sessions?beta=true",
		`{"agent":`+quoteJSON(agentID)+`,"environment_id":`+quoteJSON(envID)+`,"metadata":{"_oma_runtime_user_uuid":"spoofed","label":"public"}}`)
	sessionID := keylessResourceID(t, created)
	assertIdentity := func(t *testing.T) db.Session {
		t.Helper()
		session, found, err := f.app.db.GetSession(ctx, f.workspace.UUID, sessionID)
		if err != nil || !found {
			t.Fatalf("load session: %v, found %t", err, found)
		}
		if session.CreatedByAPIKeyUUID != "" || session.RuntimeUserUUID != f.login.UserUUID {
			t.Fatalf("runtime identity = key %q, user %q", session.CreatedByAPIKeyUUID, session.RuntimeUserUUID)
		}
		return session
	}

	t.Run("failure client cannot overwrite or erase runtime identity", func(t *testing.T) {
		for _, patch := range []string{
			`{"metadata":{"_oma_runtime_user_uuid":"different-user","label":"updated"}}`,
			`{"metadata":{}}`, `{"metadata":null}`,
		} {
			f.json(t, http.MethodPost, "/v1/sessions/"+sessionID+"?beta=true", patch)
			assertIdentity(t)
		}
		if _, err := f.app.db.PatchSessionMetadata(ctx, f.workspace.UUID, sessionID,
			json.RawMessage(`{"_oma_runtime_user_uuid":"another-spoof","runtime":"test"}`)); err != nil {
			t.Fatal(err)
		}
		assertIdentity(t)
	})
	t.Run("failure token scope cannot cross workspace", func(t *testing.T) {
		_, err := f.app.db.GetFilestoreTokenScopeForSessionIssue(ctx, f.login.WorkspaceUUID, sessionID)
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("cross workspace scope: %v", err)
		}
	})
	t.Run("failure deleted runtime user is not replaced by another account", func(t *testing.T) {
		if _, err := f.app.pool.Exec(ctx, "UPDATE users SET deleted_at=NOW() WHERE uuid=$1", f.login.UserUUID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, err := f.app.pool.Exec(ctx, "UPDATE users SET deleted_at=NULL WHERE uuid=$1", f.login.UserUUID)
			if err != nil {
				t.Error(err)
			}
		})
		if _, err := f.app.db.GetFilestoreTokenScopeForSessionIssue(ctx, f.workspace.UUID, sessionID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("deleted runtime user: %v", err)
		}
	})
	t.Run("success keyless session signs a user scoped filestore token", func(t *testing.T) {
		scope, err := f.app.db.GetFilestoreTokenScopeForSessionIssue(ctx, f.workspace.UUID, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if scope.AccountUUID != f.login.UserUUID {
			t.Fatalf("account = %q, want logged-in user", scope.AccountUUID)
		}
		token, err := f.app.filestoreCredentials.Issue(filestore.TokenIdentity{
			Subject: scope.AccountExternalID, OrgUUID: scope.OrganizationUUID, AccountUUID: scope.AccountUUID,
			WorkspaceUUID: scope.WorkspaceUUID, WorkspaceTaggedID: scope.WorkspaceExternalID,
			ResolvedWorkspaceTaggedID: scope.WorkspaceExternalID, FilesystemID: scope.FilesystemExternalID,
			OrgTaints: scope.OrgTaints, WorkspaceCMEKEnabled: scope.WorkspaceCMEKEnabled,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.filestoreCredentials.Verify(token); err != nil {
			t.Fatal(err)
		}
		var nullCreator bool
		if err := f.app.pool.QueryRow(ctx, "SELECT created_by_api_key_uuid IS NULL FROM filestore_filesystems WHERE workspace_uuid=$1 AND session_uuid=$2",
			f.workspace.UUID, assertIdentity(t).UUID).Scan(&nullCreator); err != nil || !nullCreator {
			t.Fatalf("filesystem creator is NULL = %t: %v", nullCreator, err)
		}
	})
	session := assertIdentity(t)
	code, err := f.app.db.CreateCodeSession(ctx, db.CreateCodeSessionInput{
		ExternalID: "codesession_keyless_" + uuid.NewV4().String(), OrganizationUUID: session.OrganizationUUID,
		WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID,
		EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID,
		Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("success code session git email comes from runtime user", func(t *testing.T) {
		credential, err := f.app.db.GetCodeSessionCredentialContextForIssue(ctx, session.OrganizationUUID, session.WorkspaceUUID, code.ExternalID)
		if err != nil || credential.AccountEmail != f.email {
			t.Fatalf("credential account email = %q: %v", credential.AccountEmail, err)
		}
	})
}

func TestPlatformKeylessResourceCreators(t *testing.T) {
	f := newKeylessPlatformFixture(t)
	agentID, envID := f.agentEnvironment(t)
	store := f.json(t, http.MethodPost, "/v1/memory_stores?beta=true", `{"name":"Keyless memory"}`)
	storeID := keylessResourceID(t, store)
	memory := f.json(t, http.MethodPost, "/v1/memory_stores/"+storeID+"/memories?beta=true&view=full", `{"path":"/note","content":"keyless"}`)
	var versionID string
	if err := json.Unmarshal(memory["memory_version_id"], &versionID); err != nil {
		t.Fatal(err)
	}
	version := f.json(t, http.MethodGet, "/v1/memory_stores/"+storeID+"/memory_versions/"+versionID+"?beta=true", "")
	if !strings.Contains(string(version["created_by"]), "user_actor") || !strings.Contains(string(version["created_by"]), f.login.UserExternalID) {
		t.Fatalf("memory actor = %s", version["created_by"])
	}
	f.json(t, http.MethodPost, "/v1/vaults?beta=true", `{"display_name":"Keyless vault"}`)
	f.json(t, http.MethodPost, "/v1/webhooks?beta=true", `{"name":"Keyless webhook","url":"https://example.com/hook","enabled_events":["session.status_idled"]}`)
	f.json(t, http.MethodPost, "/v1/messages/batches?beta=true", minimalBatchBody("keyless-batch"))
	fileBody, contentType := multipartBody(t, "keyless.txt", "text/plain", []byte("keyless"), false)
	f.request(t, http.MethodPost, "/v1/files?beta=true", fileBody, contentType)
	skillBody, contentType := skillMultipartBody(t, "Keyless Skill", []skillUploadFile{{Filename: "keyless/SKILL.md", Content: "---\nname: Keyless Skill\ndescription: keyless\n---\n# Keyless\n"}})
	f.request(t, http.MethodPost, "/v1/skills?beta=true", skillBody, contentType)
	deployment := f.json(t, http.MethodPost, "/v1/deployments?beta=true",
		deploymentBodyWithExtra(agentID, envID, `"metadata":{"_oma_runtime_user_uuid":"spoof"}`))
	deploymentID := keylessResourceID(t, deployment)
	f.json(t, http.MethodPost, "/v1/deployments/"+deploymentID+"?beta=true", `{"metadata":{}}`)
	run := f.json(t, http.MethodPost, "/v1/deployments/"+deploymentID+"/run?beta=true", `{}`)
	var sessionID string
	if err := json.Unmarshal(run["session_id"], &sessionID); err != nil || sessionID == "" {
		t.Fatalf("run has no session: %s", run["error"])
	}
	session, found, err := f.app.db.GetSession(context.Background(), f.workspace.UUID, sessionID)
	if err != nil || !found || session.RuntimeUserUUID != f.login.UserUUID {
		t.Fatalf("deployment run identity: %v, found %t, user %q", err, found, session.RuntimeUserUUID)
	}
	storedDeployment, err := f.app.db.GetDeployment(context.Background(), f.workspace.UUID, deploymentID)
	if err != nil || storedDeployment.RuntimeUserUUID != f.login.UserUUID {
		t.Fatalf("deployment identity after update: %v", err)
	}
	t.Run("another workspace member can run the deployment as themselves", func(t *testing.T) {
		other := f
		other.cookies = f.app.platformLoginCookies(t, "other-runtime-"+uuid.NewV4().String()+"@example.com")
		otherLogin, err := f.app.sessions.Get(context.Background(), responseCookie(other.cookies, "sessionKey").Value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.db.UpdateAdminUserRole(context.Background(), f.workspace.OrganizationUUID, otherLogin.UserExternalID, "user"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.db.CreateAdminWorkspaceMember(context.Background(), db.AdminWorkspaceMember{
			ExternalID: "wmem_keyless_" + uuid.NewV4().String(), OrganizationUUID: f.workspace.OrganizationUUID,
			WorkspaceUUID: f.workspace.UUID, WorkspaceExternalID: f.workspace.ExternalID,
			UserUUID: otherLogin.UserUUID, UserExternalID: otherLogin.UserExternalID, WorkspaceRole: "workspace_developer", CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		otherRun := other.json(t, http.MethodPost, "/v1/deployments/"+deploymentID+"/run?beta=true", `{}`)
		var otherSessionID string
		if err := json.Unmarshal(otherRun["session_id"], &otherSessionID); err != nil {
			t.Fatal(err)
		}
		otherSession, found, err := f.app.db.GetSession(context.Background(), f.workspace.UUID, otherSessionID)
		if err != nil || !found || otherSession.RuntimeUserUUID != otherLogin.UserUUID || otherSession.CreatedByAPIKeyUUID != "" {
			t.Fatalf("manual invocation identity: %v, found %t, user %q", err, found, otherSession.RuntimeUserUUID)
		}
	})

	for _, table := range []string{"agents", "environments", "memory_stores", "vaults", "webhook_endpoints", "message_batches", "files", "skills", "skill_versions", "deployments", "deployment_runs", "sessions"} {
		t.Run("NULL creator "+table, func(t *testing.T) {
			var count, keys int
			// Fixed test-only table whitelist, never user input.
			err := f.app.pool.QueryRow(context.Background(), "SELECT count(*), count(created_by_api_key_uuid) FROM "+table+" WHERE workspace_uuid=$1", f.workspace.UUID).Scan(&count, &keys)
			if err != nil || count == 0 || keys != 0 {
				t.Fatalf("%s rows=%d key actors=%d: %v", table, count, keys, err)
			}
		})
	}
	var keys int
	if err := f.app.pool.QueryRow(context.Background(), "SELECT count(*) FROM api_keys WHERE workspace_uuid=$1", f.workspace.UUID).Scan(&keys); err != nil || keys != 0 {
		t.Fatalf("implicit API keys=%d: %v", keys, err)
	}
	t.Run("real API key keeps its actor and scope even with a cookie", func(t *testing.T) {
		resp := f.app.platformRequestWithHeaders(t, http.MethodPost, "/v1/environments?beta=true", strings.NewReader(`{"name":"Explicit API key `+uuid.NewV4().String()+`"}`), f.cookies, map[string]string{
			"X-Api-Key": defaultTestKey, "X-Workspace-ID": f.workspace.ExternalID,
			"anthropic-beta": "managed-agents-2026-04-01", "anthropic-version": "2023-06-01",
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("API key request status = %d", resp.StatusCode)
		}
		var body map[string]json.RawMessage
		decodeJSON(t, resp.Body, &body)
		ids := getDefaultDBIDs(t, f.app.pool)
		environment, err := f.app.db.GetEnvironment(context.Background(), ids.WorkspaceUUID, keylessResourceID(t, body))
		if err != nil || environment.CreatedByAPIKeyUUID != ids.APIKeyUUID {
			t.Fatalf("actual API-key actor not retained: %v", err)
		}
	})
}
