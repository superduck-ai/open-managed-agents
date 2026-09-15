package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPlatformWorkspaceAPIKeyLifecycle(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-key-lifecycle-bucket"))
	defer app.close()
	cookies := app.platformLoginCookies(t, "key-lifecycle@example.com")
	orgUUID := responseCookie(cookies, "lastActiveOrg").Value
	workspaceID := createPlatformWorkspace(t, app, cookies, orgUUID, "Key lifecycle "+strconv.FormatInt(time.Now().UnixNano(), 10))
	consolePath := "/api/console/organizations/" + orgUUID
	keysPath := consolePath + "/workspaces/" + workspaceID + "/api_keys"
	headers := map[string]string{"X-Organization-UUID": orgUUID, "X-Workspace-ID": workspaceID}
	request := func(t *testing.T, method, path, body string, wantStatus int) string {
		t.Helper()
		resp := app.platformRequestWithHeaders(t, method, path, strings.NewReader(body), cookies, headers)
		defer resp.Body.Close()
		payload := readAll(t, resp.Body)
		if resp.StatusCode != wantStatus {
			t.Fatalf("%s %s status = %d, want %d: %s", method, path, resp.StatusCode, wantStatus, payload)
		}
		if cookie := responseCookie(resp.Cookies(), "sessionKey"); cookie != nil && cookie.MaxAge < 0 {
			t.Fatal("workspace request cleared the valid platform session")
		}
		return string(payload)
	}

	t.Run("failure keyless workspace still requires membership", func(t *testing.T) {
		session, err := app.sessions.Get(context.Background(), responseCookie(cookies, "sessionKey").Value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.db.UpdateAdminUserRole(context.Background(), orgUUID, session.UserExternalID, "user"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := app.db.UpdateAdminUserRole(context.Background(), orgUUID, session.UserExternalID, "admin"); err != nil {
				t.Fatal(err)
			}
		})
		request(t, http.MethodGet, keysPath, "", http.StatusForbidden)
		request(t, http.MethodPost, keysPath, `{"name":"Unauthorized key"}`, http.StatusForbidden)
		request(t, http.MethodPost, "/v1/environments?beta=true", `{}`, http.StatusForbidden)
	})

	t.Run("success resource writes without a key", func(t *testing.T) {
		request(t, http.MethodPost, "/v1/environments?beta=true", `{"name":"Keyless environment"}`, http.StatusOK)
	})

	t.Run("success empty workspace stays accessible", func(t *testing.T) {
		body := request(t, http.MethodGet, keysPath, "", http.StatusOK)
		var keys []json.RawMessage
		if err := json.Unmarshal([]byte(body), &keys); err != nil || len(keys) != 0 {
			t.Fatalf("new workspace keys = %s (%v), want empty list without implicit key creation", body, err)
		}
		request(t, http.MethodGet, consolePath+"/workspaces", "", http.StatusOK)
		request(t, http.MethodGet, "/v1/agents?beta=true", "", http.StatusOK)
		request(t, http.MethodPost, "/v1/agents:search?beta=true", `{"name":"No matching agent"}`, http.StatusOK)
		request(t, http.MethodGet, "/v1/environments?beta=true", "", http.StatusOK)
		bootstrap := request(t, http.MethodGet, "/api/bootstrap", "", http.StatusOK)
		if !strings.Contains(bootstrap, "key-lifecycle@example.com") {
			t.Fatal("bootstrap lost the authenticated account in a workspace without keys")
		}
		request(t, http.MethodPost, consolePath+"/workspaces", `{"name":"Another workspace"}`, http.StatusOK)
	})

	keyBody := request(t, http.MethodPost, keysPath, `{"name":"First key"}`, http.StatusOK)
	var key struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(keyBody), &key); err != nil || key.ID == "" {
		t.Fatalf("decode created key ID: %v", err)
	}
	for _, status := range []string{"inactive", "archived"} {
		t.Run("success restore last "+status+" key", func(t *testing.T) {
			request(t, http.MethodPost, keysPath+"/"+key.ID, `{"status":`+strconv.Quote(status)+`}`, http.StatusOK)
			request(t, http.MethodGet, keysPath, "", http.StatusOK)
			request(t, http.MethodGet, "/v1/agents?beta=true", "", http.StatusOK)
			request(t, http.MethodPost, "/v1/environments?beta=true", `{"name":`+strconv.Quote("Key "+status+" does not block console")+`}`, http.StatusOK)
			request(t, http.MethodPost, keysPath+"/"+key.ID, `{"status":"active"}`, http.StatusOK)
			request(t, http.MethodPost, "/v1/environments?beta=true", `{"name":`+strconv.Quote("Restored from "+status)+`}`, http.StatusOK)
		})
	}
	t.Run("success replace expired last key", func(t *testing.T) {
		request(t, http.MethodPost, keysPath+"/"+key.ID, `{"status":"inactive"}`, http.StatusOK)
		request(t, http.MethodPost, keysPath, `{"name":"Expired key","expires_at":"2000-01-01T00:00:00Z"}`, http.StatusOK)
		request(t, http.MethodGet, keysPath, "", http.StatusOK)
		request(t, http.MethodGet, "/v1/agents?beta=true", "", http.StatusOK)
		request(t, http.MethodPost, "/v1/environments?beta=true", `{"name":"Expired key does not block console"}`, http.StatusOK)
		request(t, http.MethodPost, keysPath, `{"name":"Replacement key"}`, http.StatusOK)
		request(t, http.MethodPost, "/v1/environments?beta=true", `{"name":"Replaced expired key"}`, http.StatusOK)
	})
}

func TestPlatformLoginWithoutActiveAPIKey(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-keyless-login-bucket"))
	defer app.close()
	// Use an isolated signup organization; never deactivate the shared fixture key.
	email := "keyless-login-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com"
	login := func() []*http.Cookie {
		body := `{"credentials":{"method":"code","code":"123456","email_address":` + strconv.Quote(email) + `}}`
		resp := app.platformRequest(t, http.MethodPost, "/api/auth/verify_magic_link", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		return resp.Cookies()
	}
	cookies := login()
	session, err := app.sessions.Get(context.Background(), responseCookie(cookies, "sessionKey").Value)
	if err != nil {
		t.Fatal(err)
	}
	keys, _, err := app.db.ListAdminAPIKeysPage(context.Background(), db.ListAdminAPIKeysParams{OrganizationUUID: session.OrganizationUUID, WorkspaceExternalID: session.WorkspaceExternalID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if err := app.db.UpdateAdminAPIKey(context.Background(), session.OrganizationUUID, key.ExternalID, false, "", true, "inactive"); err != nil {
			t.Fatal(err)
		}
	}
	cookies = login()
	session, err = app.sessions.Get(context.Background(), responseCookie(cookies, "sessionKey").Value)
	if err != nil {
		t.Fatal(err)
	}
	if session.APIKeyUUID != "" || session.UserUUID == "" || session.WorkspaceUUID == "" {
		t.Fatal("keyless login must resolve user and workspace without an API key")
	}
	for _, workspaceID := range []string{"", "default", session.WorkspaceExternalID} {
		t.Run("success keyless session scope "+workspaceID, func(t *testing.T) {
			resp := app.platformRequestWithHeaders(t, http.MethodGet, "/api/console/organizations/"+session.OrganizationUUID+"/workspaces", nil, cookies, map[string]string{"X-Workspace-ID": workspaceID})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("keyless session status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
			}
		})
	}
	createPlatformWorkspaceAPIKey(t, app, cookies, session.OrganizationUUID, "default")
}
