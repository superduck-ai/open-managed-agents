//go:build integration

package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLLMProviderCreatorAuthorizationAcrossRoutes(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("provider-ownership"))
	defer app.close()
	clearTestLLMProviders(t, app)
	org := loadDefaultOrganizationUUID(t, app)
	root := "/api/console/organizations/" + org
	admin := app.platformLoginCookies(t, "provider-admin@example.com")
	owner := app.platformLoginCookies(t, "provider-owner@example.com")
	member := app.platformLoginCookies(t, "provider-member@example.com")
	if _, err := app.pool.Exec(t.Context(), `UPDATE users SET role='user' WHERE organization_uuid=$1 AND email IN ('provider-owner@example.com','provider-member@example.com')`, org); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"discovered"}],"has_more":false}`))
	}))
	defer upstream.Close()
	payload := fmt.Sprintf(`{"name":"Owned","base_url":%q,"api_key":"test-provider-secret","model_ids":["owned-model"]}`, upstream.URL)
	normal := createConsoleLLMProvider(t, app, root+"/llm_providers", owner, payload)
	reservedResponse := app.platformRequest(t, http.MethodPut, root+"/llm_provider", strings.NewReader(payload), owner)
	var reserved map[string]any
	decodeJSON(t, reservedResponse.Body, &reserved)
	reservedResponse.Body.Close()
	if reservedResponse.StatusCode != http.StatusCreated {
		t.Fatalf("reserved create = %d %#v", reservedResponse.StatusCode, reserved)
	}
	normalID := stringValue(normal["id"])
	paths := []string{root + "/llm_providers/" + normalID, root + "/workspaces/default/llm_providers/" + normalID, root + "/llm_provider"}
	for _, path := range paths {
		for _, action := range []struct{ method, suffix string }{
			{http.MethodPut, ""}, {http.MethodDelete, ""}, {http.MethodPost, "/models/sync"}, {http.MethodPost, "/models/preview"},
		} {
			response := app.platformRequest(t, action.method, path+action.suffix, strings.NewReader(payload), member)
			body := readAll(t, response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("other member %s %s = %d %s", action.method, path+action.suffix, response.StatusCode, body)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("denied saved credentials reached upstream %d times", calls.Load())
	}
	for _, path := range paths {
		response := app.platformRequest(t, http.MethodPut, strings.Replace(path, org, "00000000-0000-4000-8000-000000000099", 1), strings.NewReader(payload), member)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("cross org = %d", response.StatusCode)
		}
	}
	listResponse := app.platformRequest(t, http.MethodGet, root+"/llm_providers", nil, member)
	var list []map[string]any
	decodeJSON(t, listResponse.Body, &list)
	listResponse.Body.Close()
	if len(list) != 2 {
		t.Fatalf("member catalogue = %#v", list)
	}
	for _, provider := range list {
		creator, ok := provider["created_by"].(map[string]any)
		if !ok || creator["account_uuid"] == "" || creator["name"] == "" || provider["can_manage"] != false {
			t.Fatalf("creator metadata = %#v", provider)
		}
	}
	for _, cookies := range [][]*http.Cookie{owner, admin} {
		for _, path := range paths {
			response := app.platformRequest(t, http.MethodPut, path, strings.NewReader(payload), cookies)
			var updated map[string]any
			decodeJSON(t, response.Body, &updated)
			response.Body.Close()
			if response.StatusCode != http.StatusOK || updated["can_manage"] != true {
				t.Fatalf("authorized update = %d %#v", response.StatusCode, updated)
			}
			original := normal
			if path == root+"/llm_provider" {
				original = reserved
			}
			if fmt.Sprint(updated["created_by"]) != fmt.Sprint(original["created_by"]) {
				t.Fatalf("creator changed: %#v", updated)
			}
		}
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE llm_providers SET created_by_account_uuid=NULL WHERE organization_uuid=$1`, org); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		for _, action := range []struct{ method, suffix string }{
			{http.MethodPut, ""}, {http.MethodDelete, ""}, {http.MethodPost, "/models/sync"}, {http.MethodPost, "/models/preview"},
		} {
			response := app.platformRequest(t, action.method, path+action.suffix, strings.NewReader(payload), owner)
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("legacy owner %s = %d", action.method, response.StatusCode)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("denied legacy credentials reached upstream %d times", calls.Load())
	}
	preview := app.platformRequest(t, http.MethodPost, root+"/llm_providers/preview_models", strings.NewReader(fmt.Sprintf(`{"base_url":%q,"api_key":"own-unsaved-secret"}`, upstream.URL)), member)
	preview.Body.Close()
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("member unsaved preview = %d", preview.StatusCode)
	}
	for _, path := range paths {
		response := app.platformRequest(t, http.MethodPost, path+"/models/sync", nil, admin)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("admin legacy sync = %d", response.StatusCode)
		}
	}

	for _, path := range []string{paths[2], paths[0]} {
		response := app.platformRequest(t, http.MethodDelete, path, nil, admin)
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("admin legacy delete = %d", response.StatusCode)
		}
		if path == paths[2] {
			missing := app.platformRequest(t, http.MethodGet, root+"/llm_provider", nil, admin)
			missing.Body.Close()
			if missing.StatusCode != http.StatusNotFound {
				t.Fatalf("singular route selected a nonreserved provider = %d", missing.StatusCode)
			}
		}
	}
}
