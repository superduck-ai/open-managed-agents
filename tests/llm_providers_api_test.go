package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestWorkspaceLLMProvidersBYOKLifecycle(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	orgUUID := loadDefaultOrganizationUUID(t, app)
	workspaceUUID := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	cookies := app.platformLoginCookies(t, "llm-providers@example.com")
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"

	const firstKey = "provider-alpha-secret"
	first := createConsoleLLMProvider(t, app, path, cookies, `{
		"name":"DashScope",
		"base_url":"https://dashscope.example.com/anthropic",
		"api_key":"  provider-alpha-secret  ",
		"model_ids":["kimi-k2.5"]
	}`)
	if first["api_key_last4"] != "cret" || first["has_api_key"] != true {
		t.Fatalf("first Provider key metadata = %#v", first)
	}
	assertJSONDoesNotContain(t, first, firstKey)

	const secondKey = "provider-beta-secret"
	second := createConsoleLLMProvider(t, app, path, cookies, `{
		"name":"Model Studio",
		"base_url":"https://modelstudio.example.com",
		"api_key":"provider-beta-secret",
		"model_ids":["qwen-max","deepseek-v3"]
	}`)
	assertJSONDoesNotContain(t, second, secondKey)

	duplicate := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{
		"name":"Duplicate model",
		"base_url":"https://duplicate.example.com",
		"api_key":"duplicate-secret",
		"model_ids":["kimi-k2.5"]
	}`), cookies)
	defer duplicate.Body.Close()
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate model status = %d, want 409: %s", duplicate.StatusCode, readAll(t, duplicate.Body))
	}
	var duplicateError map[string]any
	decodeJSON(t, duplicate.Body, &duplicateError)
	if duplicateError["code"] != "model_conflict" || duplicateError["model_id"] != "kimi-k2.5" {
		t.Fatalf("duplicate model error = %#v", duplicateError)
	}

	list := app.platformRequest(t, http.MethodGet, path, nil, cookies)
	defer list.Body.Close()
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list Providers status = %d, want 200: %s", list.StatusCode, readAll(t, list.Body))
	}
	listBody := readAll(t, list.Body)
	if bytes.Contains(listBody, []byte(firstKey)) || bytes.Contains(listBody, []byte(secondKey)) {
		t.Fatalf("Provider list leaked plaintext key: %s", listBody)
	}
	var listed []map[string]any
	if err := json.Unmarshal(listBody, &listed); err != nil {
		t.Fatalf("decode Provider list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("Provider list = %#v, want two Providers", listed)
	}

	for _, secret := range []string{firstKey, secondKey} {
		var ciphertext []byte
		if err := app.pool.QueryRow(context.Background(), `
			select ciphertext
			from llm_providers
			where api_key_last4 = $1
		`, secret[len(secret)-4:]).Scan(&ciphertext); err != nil {
			t.Fatalf("load Provider ciphertext: %v", err)
		}
		if bytes.Contains(ciphertext, []byte(secret)) {
			t.Fatalf("Provider ciphertext contains plaintext key")
		}
	}

	firstUpstream, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "kimi-k2.5",
	)
	if err != nil {
		t.Fatalf("resolve first Provider: %v", err)
	}
	if firstUpstream.BaseURL != "https://dashscope.example.com/anthropic" || firstUpstream.APIKey != firstKey {
		t.Fatalf("first Provider resolution = %#v", firstUpstream)
	}
	secondUpstream, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "qwen-max",
	)
	if err != nil {
		t.Fatalf("resolve second Provider: %v", err)
	}
	if secondUpstream.BaseURL != "https://modelstudio.example.com" || secondUpstream.APIKey != secondKey {
		t.Fatalf("second Provider resolution = %#v", secondUpstream)
	}
	if _, err := llmproviders.ListModelIDs(
		context.Background(), app.db, "00000000-0000-4000-8000-000000000099", workspaceUUID,
	); !errors.Is(err, llmproviders.ErrNotConfigured) {
		t.Fatalf("cross-organization model list error = %v, want ErrNotConfigured", err)
	}

	models := app.do(t, http.MethodGet, "/v1/models", nil, defaultTestKey, false, "")
	defer models.Body.Close()
	if models.StatusCode != http.StatusOK {
		t.Fatalf("models status = %d, want 200: %s", models.StatusCode, readAll(t, models.Body))
	}
	modelsBody := string(readAll(t, models.Body))
	for _, modelID := range []string{"kimi-k2.5", "qwen-max", "deepseek-v3"} {
		if !strings.Contains(modelsBody, `"id":"`+modelID+`"`) {
			t.Fatalf("models response missing %q: %s", modelID, modelsBody)
		}
	}

	firstID, _ := first["id"].(string)
	updated := app.platformRequest(t, http.MethodPut, path+"/"+firstID, strings.NewReader(`{
		"name":"DashScope updated",
		"base_url":"https://dashscope.example.com/anthropic",
		"model_ids":["kimi-k2.5"]
	}`), cookies)
	defer updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update Provider status = %d, want 200: %s", updated.StatusCode, readAll(t, updated.Body))
	}
	retained, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "kimi-k2.5",
	)
	if err != nil || retained.APIKey != firstKey {
		t.Fatalf("update did not retain Provider key: (%#v, %v)", retained, err)
	}

	deleted := app.platformRequest(t, http.MethodDelete, path+"/"+firstID, nil, cookies)
	defer deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete Provider status = %d, want 204: %s", deleted.StatusCode, readAll(t, deleted.Body))
	}
	if _, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "kimi-k2.5",
	); !errors.Is(err, llmproviders.ErrModelNotConfigured) {
		t.Fatalf("resolve deleted model error = %v, want ErrModelNotConfigured", err)
	}

	clearTestLLMProviders(t, app)
	message := doMessagesRequest(t, app, defaultTestKey, `{"model":"kimi-k2.5","max_tokens":16,"messages":[]}`)
	assertError(t, message, http.StatusServiceUnavailable, "api_error")
}

func TestWorkspaceLLMProvidersRequireAdministrator(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-permission-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	const email = "llm-providers-developer@example.com"
	cookies := app.platformLoginCookies(t, email)
	orgUUID := loadDefaultOrganizationUUID(t, app)
	if _, err := app.pool.Exec(context.Background(), `
		update users set role = 'developer' where organization_uuid = $1 and lower(email) = lower($2)
	`, orgUUID, email); err != nil {
		t.Fatalf("downgrade test user: %v", err)
	}
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"

	response := app.platformRequest(t, http.MethodGet, path, nil, cookies)
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.StatusCode, readAll(t, response.Body))
	}
	var body map[string]any
	decodeJSON(t, response.Body, &body)
	if body["code"] != "llm_provider_permission_denied" {
		t.Fatalf("permission error = %#v", body)
	}
}

func TestWorkspaceLLMProviderRejectsMalformedBaseURLs(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-unsafe-url-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)
	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "llm-providers-unsafe@example.com")
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"

	for _, baseURL := range []string{
		"ftp://llm.example.com",
		"https://user:pass@llm.example.com",
		"https://llm.example.com?token=secret",
		"https://llm.example.com#fragment",
		"llm.example.com",
	} {
		body, _ := json.Marshal(map[string]any{
			"name":      "Unsafe",
			"base_url":  baseURL,
			"api_key":   "unsafe-test-key",
			"model_ids": []string{"unsafe-model"},
		})
		response := app.platformRequest(t, http.MethodPost, path, bytes.NewReader(body), cookies)
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("base URL %q status = %d, want 400: %s", baseURL, response.StatusCode, readAll(t, response.Body))
		}
	}
}

func TestWorkspaceLLMProviderAcceptsPrivateAndCustomPortURLs(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-private-url-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)
	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "llm-providers-private@example.com")
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"

	created := createConsoleLLMProvider(t, app, path, cookies, `{
		"name":"Local gateway",
		"base_url":"http://127.0.0.1:11434/anthropic",
		"api_key":"local-gateway-key",
		"model_ids":["local-model"]
	}`)
	if created["base_url"] != "http://127.0.0.1:11434/anthropic" {
		t.Fatalf("created Provider base_url = %#v", created["base_url"])
	}
}

func TestWorkspaceLLMProviderEmptyModelsLifecycle(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-empty-models-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	const providerKey = "empty-models-provider-key"
	orgUUID := loadDefaultOrganizationUUID(t, app)
	workspaceUUID := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	cookies := app.platformLoginCookies(t, "llm-providers-empty@example.com")
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"

	created := createConsoleLLMProvider(t, app, path, cookies, `{
		"name":"Empty Gateway",
		"base_url":"https://empty.example.com/anthropic",
		"api_key":"empty-models-provider-key",
		"model_ids":[]
	}`)
	providerID, _ := created["id"].(string)
	modelIDs, _ := created["model_ids"].([]any)
	if providerID == "" || len(modelIDs) != 0 {
		t.Fatalf("created empty Provider = %#v", created)
	}

	list := app.platformRequest(t, http.MethodGet, path, nil, cookies)
	defer list.Body.Close()
	var listed []map[string]any
	decodeJSON(t, list.Body, &listed)
	if list.StatusCode != http.StatusOK || len(listed) != 1 {
		t.Fatalf("list empty Provider status=%d body=%#v", list.StatusCode, listed)
	}

	updated := app.platformRequest(t, http.MethodPut, path+"/"+providerID, strings.NewReader(`{
		"name":"Empty Gateway updated",
		"base_url":"https://empty.example.com/anthropic",
		"model_ids":["glm-4.7"]
	}`), cookies)
	defer updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("add model status = %d, want 200: %s", updated.StatusCode, readAll(t, updated.Body))
	}
	upstream, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "glm-4.7")
	if err != nil || upstream.APIKey != providerKey {
		t.Fatalf("updated empty Provider key retention = (%#v, %v)", upstream, err)
	}

	emptied := app.platformRequest(t, http.MethodPut, path+"/"+providerID, strings.NewReader(`{
		"name":"Empty Gateway updated",
		"base_url":"https://empty.example.com/anthropic",
		"model_ids":[]
	}`), cookies)
	defer emptied.Body.Close()
	if emptied.StatusCode != http.StatusOK {
		t.Fatalf("empty models status = %d, want 200: %s", emptied.StatusCode, readAll(t, emptied.Body))
	}

	models := app.do(t, http.MethodGet, "/v1/models", nil, defaultTestKey, false, "")
	defer models.Body.Close()
	var modelsBody map[string]any
	decodeJSON(t, models.Body, &modelsBody)
	data, _ := modelsBody["data"].([]any)
	if models.StatusCode != http.StatusOK || len(data) != 0 {
		t.Fatalf("empty models response status=%d body=%#v", models.StatusCode, modelsBody)
	}

	deleted := app.platformRequest(t, http.MethodDelete, path+"/"+providerID, nil, cookies)
	defer deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete empty Provider status = %d, want 204: %s", deleted.StatusCode, readAll(t, deleted.Body))
	}
}

func TestWorkspaceLLMProviderModelDiscovery(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-discover-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	const liveKey = "live-provider-secret"
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("X-Api-Key") != liveKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":"model","id":"glm-4.7"},{"id":"kimi-k2.5"}]}`))
	}))
	defer upstream.Close()

	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "llm-providers-discover@example.com")
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"

	t.Run("failure preview without api key", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodPost, path+"/preview_models", strings.NewReader(`{
			"base_url":"`+upstream.URL+`",
			"api_key":"   "
		}`), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("preview status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("success preview lists upstream models", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodPost, path+"/preview_models", strings.NewReader(`{
			"base_url":"`+upstream.URL+`",
			"api_key":"`+liveKey+`"
		}`), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("preview status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		assertJSONDoesNotContain(t, body, liveKey)
		modelIDs, _ := body["model_ids"].([]any)
		if len(modelIDs) != 2 || modelIDs[0] != "glm-4.7" || modelIDs[1] != "kimi-k2.5" {
			t.Fatalf("preview models = %#v", body)
		}
	})

	t.Run("success sync merges live models into provider", func(t *testing.T) {
		created := createConsoleLLMProvider(t, app, path, cookies, `{
			"name":"Live Gateway",
			"base_url":"`+upstream.URL+`",
			"api_key":"`+liveKey+`",
			"model_ids":["kimi-k2.5"]
		}`)
		providerID, _ := created["id"].(string)
		before := requests
		resp := app.platformRequest(t, http.MethodPost, path+"/"+providerID+"/models/sync", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sync status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		assertJSONDoesNotContain(t, body, liveKey)
		modelIDs, _ := body["model_ids"].([]any)
		if requests <= before || len(modelIDs) != 2 || modelIDs[0] != "kimi-k2.5" || modelIDs[1] != "glm-4.7" {
			t.Fatalf("sync models = %#v requests=%d before=%d", body, requests, before)
		}
	})
}

func TestWorkspaceLLMProviderSyncPreservesExistingModelsAndFiltersBeforeLimit(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-sync-conflicts-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	const liveKey = "sync-conflicts-provider-key"
	liveModels := make([]map[string]string, 0, 101)
	conflicts := make([]string, 0, 101)
	conflicts = append(conflicts, "dirty-shared")
	for index := 0; index < 100; index++ {
		modelID := fmt.Sprintf("conflict-%03d", index)
		conflicts = append(conflicts, modelID)
		liveModels = append(liveModels, map[string]string{"id": modelID})
	}
	liveModels = append(liveModels, map[string]string{"id": "available-model"})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"data": liveModels}); err != nil {
			t.Errorf("encode upstream response: %v", err)
		}
	}))
	defer upstream.Close()

	target := seedTestLLMProvider(t, app, "Sync target", upstream.URL, liveKey, "keep-model", "dirty-shared")
	seedTestLLMProvider(t, app, "Conflicting provider", "https://conflicts.example.com", "conflicting-key", conflicts...)
	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "llm-providers-sync-conflicts@example.com")
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers/" + target.ExternalID + "/models/sync"

	response := app.platformRequest(t, http.MethodPost, path, nil, cookies)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("sync status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
	}
	var body struct {
		ModelIDs        []string `json:"model_ids"`
		SkippedModelIDs []string `json:"skipped_model_ids"`
	}
	decodeJSON(t, response.Body, &body)
	wantModels := []string{"keep-model", "dirty-shared", "available-model"}
	if !slices.Equal(body.ModelIDs, wantModels) {
		t.Fatalf("synced models = %#v, want %#v", body.ModelIDs, wantModels)
	}
	if len(body.SkippedModelIDs) != 100 || body.SkippedModelIDs[0] != "conflict-000" {
		t.Fatalf("skipped models = %#v", body.SkippedModelIDs)
	}
}

func TestWorkspaceLLMProviderResolutionFailsClosedOnAmbiguousModel(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-ambiguous-model-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "First", "https://first.example.com", "first-test-key", "same-model")
	seedTestLLMProvider(t, app, "Second", "https://second.example.com", "second-test-key", "same-model")

	workspaceUUID := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	orgUUID := loadDefaultOrganizationUUID(t, app)
	if _, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "same-model",
	); !errors.Is(err, llmproviders.ErrAmbiguousModel) {
		t.Fatalf("Resolve() error = %v, want ErrAmbiguousModel", err)
	}
	if _, err := llmproviders.ListModelIDs(
		context.Background(), app.db, orgUUID, workspaceUUID,
	); !errors.Is(err, llmproviders.ErrAmbiguousModel) {
		t.Fatalf("ListModelIDs() error = %v, want ErrAmbiguousModel", err)
	}
}

func createConsoleLLMProvider(t *testing.T, app *testApp, path string, cookies []*http.Cookie, body string) map[string]any {
	t.Helper()
	response := app.platformRequest(t, http.MethodPost, path, strings.NewReader(body), cookies)
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create Provider status = %d, want 201: %s", response.StatusCode, readAll(t, response.Body))
	}
	var created map[string]any
	decodeJSON(t, response.Body, &created)
	return created
}

func assertJSONDoesNotContain(t *testing.T, value any, secret string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("response leaked plaintext key: %s", encoded)
	}
}
