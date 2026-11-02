//go:build integration

package tests

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestMultipleOrganizationAndWorkspaceProviders(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("multiple-providers"))
	defer app.close()
	clearTestLLMProviders(t, app)
	ids := getDefaultDBIDs(t, app.pool)
	cookies := app.platformLoginCookies(t, "multiple-providers@example.com")
	root := "/api/console/organizations/" + ids.OrganizationUUID
	orgPath := root + "/llm_providers"
	workspacePath := root + "/workspaces/default/llm_providers"
	payload := func(slug string) string {
		return fmt.Sprintf(`{"name":"Provider","slug":%q,"base_url":"https://example.test","api_key":"test-key","models":[{"id":"shared"}]}`, slug)
	}
	first := createConsoleLLMProvider(t, app, orgPath, cookies, payload("kimi"))
	second := createConsoleLLMProvider(t, app, orgPath, cookies, payload("deepseek"))
	local := createConsoleLLMProvider(t, app, workspacePath, cookies, payload("local"))
	createConsoleLLMProvider(t, app, workspacePath, cookies, payload("local-second"))
	for _, path := range []string{orgPath, workspacePath} {
		response := app.platformRequest(t, http.MethodPost, path, strings.NewReader(payload("kimi")), cookies)
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("duplicate at %s=%d %s", path, response.StatusCode, readAll(t, response.Body))
		}
		response.Body.Close()
	}
	denied := app.platformRequest(t, http.MethodDelete, workspacePath+"/"+stringValue(first["id"]), nil, cookies)
	if denied.StatusCode != http.StatusNotFound {
		t.Fatalf("workspace must not mutate organization provider: %d", denied.StatusCode)
	}
	denied.Body.Close()
	workspaceResponse := app.platformRequest(t, http.MethodPost, root+"/workspaces", strings.NewReader(fmt.Sprintf(`{"name":%q}`, "Second provider workspace-"+uniqueAdminSuffix())), cookies)
	var workspace map[string]any
	decodeJSON(t, workspaceResponse.Body, &workspace)
	workspaceResponse.Body.Close()
	if workspaceResponse.StatusCode != http.StatusOK {
		t.Fatalf("create workspace=%d %#v", workspaceResponse.StatusCode, workspace)
	}
	otherWorkspace, err := app.db.GetAdminWorkspace(t.Context(), ids.OrganizationUUID, stringValue(workspace["id"]))
	if err != nil {
		t.Fatal(err)
	}
	otherPath := root + "/workspaces/" + otherWorkspace.ExternalID + "/llm_providers"
	conflict := app.platformRequest(t, http.MethodPost, otherPath, strings.NewReader(payload("local")), cookies)
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("cross-workspace duplicate=%d", conflict.StatusCode)
	}
	conflict.Body.Close()
	for _, workspaceUUID := range []string{ids.WorkspaceUUID, otherWorkspace.UUID} {
		for _, id := range []string{"kimi/shared", "deepseek/shared"} {
			if _, err := llmproviders.Resolve(t.Context(), app.db, app.vaultSecrets, ids.OrganizationUUID, workspaceUUID, id); err != nil {
				t.Fatalf("shared model %s: %v", id, err)
			}
		}
	}
	if _, err := llmproviders.Resolve(t.Context(), app.db, app.vaultSecrets, ids.OrganizationUUID, otherWorkspace.UUID, "local/shared"); !errors.Is(err, llmproviders.ErrModelNotConfigured) {
		t.Fatalf("workspace model escaped scope: %v", err)
	}
	read := app.platformRequest(t, http.MethodGet, orgPath, nil, cookies)
	var list []map[string]any
	decodeJSON(t, read.Body, &list)
	read.Body.Close()
	if len(list) != 2 {
		t.Fatalf("organization list=%#v", list)
	}
	for _, item := range list {
		if item["scope"] != "organization" {
			t.Fatal("missing scope")
		}
		assertJSONDoesNotContain(t, item, "test-key")
	}
	update := app.platformRequest(t, http.MethodPut, orgPath+"/"+stringValue(first["id"]), strings.NewReader(payload("kimi")), cookies)
	if update.StatusCode != http.StatusOK {
		t.Fatalf("update organization=%d %s", update.StatusCode, readAll(t, update.Body))
	}
	update.Body.Close()
	remove := app.platformRequest(t, http.MethodDelete, orgPath+"/"+stringValue(second["id"]), nil, cookies)
	if remove.StatusCode != http.StatusNoContent {
		t.Fatalf("delete organization=%d", remove.StatusCode)
	}
	remove.Body.Close()
	if _, err := llmproviders.Resolve(t.Context(), app.db, app.vaultSecrets, ids.OrganizationUUID, ids.WorkspaceUUID, "deepseek/shared"); !errors.Is(err, llmproviders.ErrModelNotConfigured) {
		t.Fatalf("deleted provider still resolved: %v", err)
	}
	if _, err := app.db.GetLLMProvider(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, stringValue(local["id"])); err != nil {
		t.Fatal(err)
	}
	memberEmail := "multiple-provider-member@example.com"
	memberCookies := app.platformLoginCookies(t, memberEmail)
	if _, err := app.pool.Exec(t.Context(), `UPDATE users SET role='user' WHERE organization_uuid=$1 AND email=$2`, ids.OrganizationUUID, memberEmail); err != nil {
		t.Fatal(err)
	}
	memberRead := app.platformRequest(t, http.MethodGet, orgPath, nil, memberCookies)
	if memberRead.StatusCode != http.StatusOK {
		t.Fatalf("member read=%d", memberRead.StatusCode)
	}
	memberRead.Body.Close()
	for _, action := range []struct{ method, path string }{
		{http.MethodPost, orgPath},
		{http.MethodPut, orgPath + "/" + stringValue(first["id"])},
		{http.MethodDelete, orgPath + "/" + stringValue(first["id"])},
		{http.MethodPost, orgPath + "/preview_models"},
		{http.MethodPost, orgPath + "/" + stringValue(first["id"]) + "/models/sync"},
	} {
		response := app.platformRequest(t, action.method, action.path, strings.NewReader(payload("forbidden")), memberCookies)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("member %s %s=%d", action.method, action.path, response.StatusCode)
		}
		response.Body.Close()
	}

}
