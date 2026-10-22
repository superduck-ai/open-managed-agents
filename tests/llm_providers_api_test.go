//go:build integration || e2e || e2b_integration

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"

	"github.com/google/uuid"
)

func TestWorkspaceLLMProviderSlugIsUniqueUnderConcurrentCreates(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-concurrent-slug-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	databaseIDs := getDefaultDBIDs(t, app.pool)
	providers := make([]db.LLMProvider, 0, 2)
	for index, name := range []string{"Concurrent first", "Concurrent second"} {
		externalID, err := ids.New("llmprov_concurrent_")
		if err != nil {
			t.Fatalf("generate provider ID: %v", err)
		}
		now := time.Now().UTC()
		provider := db.LLMProvider{
			UUID:             uuid.NewString(),
			ExternalID:       externalID,
			OrganizationUUID: databaseIDs.OrganizationUUID,
			WorkspaceUUID:    databaseIDs.WorkspaceUUID,
			Name:             name,
			Slug:             "shared",
			BaseURL:          fmt.Sprintf("https://concurrent-%d.example.com", index),
			APIKeyLast4:      fmt.Sprintf("key%d", index),
			ModelIDs:         []string{"shared-model"},
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		key := []byte(fmt.Sprintf("concurrent-provider-key-%d", index))
		envelope, err := app.vaultSecrets.Seal(context.Background(), llmproviders.SecretBinding(provider), key)
		clear(key)
		if err != nil {
			t.Fatalf("seal provider key: %v", err)
		}
		provider.SecretEnvelope = &envelope
		providers = append(providers, provider)
	}

	start := make(chan struct{})
	results := make(chan error, len(providers))
	for _, provider := range providers {
		go func() {
			<-start
			_, err := app.db.CreateLLMProvider(context.Background(), provider)
			results <- err
		}()
	}
	close(start)
	var successes, conflicts int
	for range providers {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, db.ErrLLMProviderSlugConflict):
			conflicts++
		default:
			t.Fatalf("concurrent create error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent creates successes=%d conflicts=%d, want 1 and 1", successes, conflicts)
	}
}

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
		"slug":"dashscope",
		"base_url":"https://dashscope.example.com/anthropic",
		"api_key":"  provider-alpha-secret  ",
		"model_ids":["kimi-k2.5"]
	}`)
	if first["api_key_last4"] != "cret" || first["has_api_key"] != true {
		t.Fatalf("first Provider key metadata = %#v", first)
	}
	assertJSONDoesNotContain(t, first, firstKey)
	if first["slug"] != "dashscope" {
		t.Fatalf("first Provider slug = %#v", first["slug"])
	}

	const secondKey = "provider-beta-secret"
	second := createConsoleLLMProvider(t, app, path, cookies, `{
		"name":"Model Studio",
		"base_url":"https://modelstudio.example.com",
		"api_key":"provider-beta-secret",
		"model_ids":["qwen-max","deepseek-ai/deepseek-v3","kimi-k2.5"]
	}`)
	assertJSONDoesNotContain(t, second, secondKey)
	if second["slug"] != "default" {
		t.Fatalf("second Provider slug = %#v, want default", second["slug"])
	}

	duplicateSlug := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{
		"name":"Duplicate slug",
		"slug":"dashscope",
		"base_url":"https://duplicate.example.com",
		"api_key":"duplicate-secret",
		"model_ids":["kimi-k2.5"]
	}`), cookies)
	defer duplicateSlug.Body.Close()
	if duplicateSlug.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate slug status = %d, want 409: %s", duplicateSlug.StatusCode, readAll(t, duplicateSlug.Body))
	}
	var duplicateError map[string]any
	decodeJSON(t, duplicateSlug.Body, &duplicateError)
	if duplicateError["code"] != "llm_provider_slug_conflict" {
		t.Fatalf("duplicate slug error = %#v", duplicateError)
	}

	invalidSlug := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{
		"name":"Invalid slug",
		"slug":"Dash/Scope",
		"base_url":"https://invalid.example.com",
		"api_key":"invalid-secret",
		"model_ids":[]
	}`), cookies)
	defer invalidSlug.Body.Close()
	if invalidSlug.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid slug status = %d, want 400: %s", invalidSlug.StatusCode, readAll(t, invalidSlug.Body))
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
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "dashscope/kimi-k2.5",
	)
	if err != nil {
		t.Fatalf("resolve first Provider: %v", err)
	}
	if firstUpstream.BaseURL != "https://dashscope.example.com/anthropic" || firstUpstream.APIKey != firstKey ||
		firstUpstream.ModelID != "kimi-k2.5" || firstUpstream.QualifiedModelID != "dashscope/kimi-k2.5" {
		t.Fatalf("first Provider resolution = %#v", firstUpstream)
	}
	secondUpstream, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "default/deepseek-ai/deepseek-v3",
	)
	if err != nil {
		t.Fatalf("resolve second Provider: %v", err)
	}
	if secondUpstream.BaseURL != "https://modelstudio.example.com" || secondUpstream.APIKey != secondKey ||
		secondUpstream.ModelID != "deepseek-ai/deepseek-v3" {
		t.Fatalf("second Provider resolution = %#v", secondUpstream)
	}
	legacyUpstream, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "qwen-max",
	)
	if err != nil || legacyUpstream.QualifiedModelID != "default/qwen-max" {
		t.Fatalf("legacy model resolution = (%#v, %v)", legacyUpstream, err)
	}
	if _, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "kimi-k2.5",
	); !errors.Is(err, llmproviders.ErrAmbiguousModel) {
		t.Fatalf("ambiguous legacy model error = %v, want ErrAmbiguousModel", err)
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
	for _, modelID := range []string{"dashscope/kimi-k2.5", "default/qwen-max", "default/deepseek-ai/deepseek-v3", "default/kimi-k2.5"} {
		if !strings.Contains(modelsBody, `"id":"`+modelID+`"`) {
			t.Fatalf("models response missing %q: %s", modelID, modelsBody)
		}
	}

	firstID, _ := first["id"].(string)
	renamed := app.platformRequest(t, http.MethodPut, path+"/"+firstID, strings.NewReader(`{
		"name":"DashScope updated",
		"slug":"renamed",
		"base_url":"https://dashscope.example.com/anthropic",
		"model_ids":["kimi-k2.5"]
	}`), cookies)
	defer renamed.Body.Close()
	if renamed.StatusCode != http.StatusBadRequest {
		t.Fatalf("rename slug status = %d, want 400: %s", renamed.StatusCode, readAll(t, renamed.Body))
	}

	updated := app.platformRequest(t, http.MethodPut, path+"/"+firstID, strings.NewReader(`{
		"name":"DashScope updated",
		"slug":"dashscope",
		"base_url":"https://dashscope.example.com/anthropic",
		"model_ids":["kimi-k2.5"]
	}`), cookies)
	defer updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update Provider status = %d, want 200: %s", updated.StatusCode, readAll(t, updated.Body))
	}
	retained, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "dashscope/kimi-k2.5",
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
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "dashscope/kimi-k2.5",
	); !errors.Is(err, llmproviders.ErrModelNotConfigured) {
		t.Fatalf("resolve deleted model error = %v, want ErrModelNotConfigured", err)
	}
	remaining, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "kimi-k2.5",
	)
	if err != nil || remaining.QualifiedModelID != "default/kimi-k2.5" {
		t.Fatalf("legacy model after delete = (%#v, %v)", remaining, err)
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
		update users set role = 'user' where organization_uuid = $1 and lower(email) = lower($2)
	`, orgUUID, email); err != nil {
		t.Fatalf("downgrade test user: %v", err)
	}
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"

	listed := app.platformRequest(t, http.MethodGet, path, nil, cookies)
	listed.Body.Close()
	if listed.StatusCode != http.StatusOK {
		t.Fatalf("用户读取模型状态 = %d", listed.StatusCode)
	}
	response := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{}`), cookies)
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
	upstream, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "default/glm-4.7")
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
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
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
		before := requests.Load()
		resp := app.platformRequest(t, http.MethodPost, path+"/"+providerID+"/models/sync", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sync status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		assertJSONDoesNotContain(t, body, liveKey)
		modelIDs, _ := body["model_ids"].([]any)
		if requests.Load() <= before || len(modelIDs) != 2 || modelIDs[0] != "kimi-k2.5" || modelIDs[1] != "glm-4.7" {
			t.Fatalf("sync models = %#v requests=%d before=%d", body, requests.Load(), before)
		}
	})
}

func TestWorkspaceLLMProviderSyncKeepsModelsSharedWithOtherProviders(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-sync-shared-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	const liveKey = "sync-shared-provider-key"
	liveModels := make([]map[string]string, 0, 101)
	shared := make([]string, 0, 100)
	for index := 0; index < 100; index++ {
		modelID := fmt.Sprintf("shared-%03d", index)
		shared = append(shared, modelID)
		liveModels = append(liveModels, map[string]string{"id": modelID})
	}
	liveModels = append(liveModels, map[string]string{"id": "available-model"})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"data": liveModels}); err != nil {
			t.Errorf("encode upstream response: %v", err)
		}
	}))
	defer upstream.Close()

	target := seedTestLLMProvider(t, app, "Sync target", upstream.URL, liveKey, "keep-model", "shared-000")
	seedTestLLMProvider(t, app, "Other provider", "https://other.example.com", "other-provider-key", shared...)
	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "llm-providers-sync-shared@example.com")
	path := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers/" + target.ExternalID + "/models/sync"

	response := app.platformRequest(t, http.MethodPost, path, nil, cookies)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("sync status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
	}
	var body map[string]json.RawMessage
	decodeJSON(t, response.Body, &body)
	if _, exists := body["skipped_model_ids"]; exists {
		t.Fatalf("sync response still reports skipped models: %s", body["skipped_model_ids"])
	}
	var modelIDs []string
	if err := json.Unmarshal(body["model_ids"], &modelIDs); err != nil {
		t.Fatalf("decode synced models: %v", err)
	}
	if len(modelIDs) != 100 || !slices.Equal(modelIDs[:3], []string{"keep-model", "shared-000", "shared-001"}) {
		t.Fatalf("synced models = %#v", modelIDs)
	}
}

func TestWorkspaceLLMProviderSameModelResolvesByProviderSlug(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-same-model-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "First", "https://first.example.com", "first-test-key", "same-model")
	seedTestLLMProvider(t, app, "Second", "https://second.example.com", "second-test-key", "same-model")

	workspaceUUID := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	orgUUID := loadDefaultOrganizationUUID(t, app)
	_, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "same-model")
	if !errors.Is(err, llmproviders.ErrAmbiguousModel) ||
		!strings.Contains(err.Error(), "default/same-model, default-2/same-model") {
		t.Fatalf("Resolve() error = %v, want ambiguous candidates", err)
	}
	second, err := llmproviders.Resolve(
		context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "default-2/same-model",
	)
	if err != nil || second.BaseURL != "https://second.example.com" || second.ModelID != "same-model" {
		t.Fatalf("Resolve(default-2/same-model) = (%#v, %v)", second, err)
	}
	modelIDs, err := llmproviders.ListModelIDs(context.Background(), app.db, orgUUID, workspaceUUID)
	if err != nil || !slices.Equal(modelIDs, []string{"default/same-model", "default-2/same-model"}) {
		t.Fatalf("ListModelIDs() = (%#v, %v)", modelIDs, err)
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

func TestOrganizationLLMProviderIsReadableByMembers(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-organization-console-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	const email = "llm-providers-organization-member@example.com"
	cookies := app.platformLoginCookies(t, email)
	orgUUID := loadDefaultOrganizationUUID(t, app)
	if _, err := app.pool.Exec(context.Background(), `
		update users set role = 'user' where organization_uuid = $1 and lower(email) = lower($2)
	`, orgUUID, email); err != nil {
		t.Fatalf("downgrade test user: %v", err)
	}

	missing := app.platformRequest(t, http.MethodGet, "/api/console/organizations/"+orgUUID+"/llm_provider", nil, cookies)
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("member organization provider status = %d, want 404: %s", missing.StatusCode, readAll(t, missing.Body))
	}

	denied := app.platformRequest(t, http.MethodPut, "/api/console/organizations/"+orgUUID+"/llm_provider", strings.NewReader(`{
		"name":"Organization default",
		"base_url":"https://organization.example.com",
		"api_key":"organization-provider-secret",
		"model_ids":["org-only"]
	}`), cookies)
	defer denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("member put status = %d, want 403: %s", denied.StatusCode, readAll(t, denied.Body))
	}
	var deniedBody map[string]any
	decodeJSON(t, denied.Body, &deniedBody)
	if deniedBody["code"] != "llm_provider_permission_denied" {
		t.Fatalf("permission error = %#v", deniedBody)
	}

	adminCookies := app.platformLoginCookies(t, "llm-providers-organization-admin@example.com")
	created := app.platformRequest(t, http.MethodPut, "/api/console/organizations/"+orgUUID+"/llm_provider", strings.NewReader(`{
		"name":"Organization default",
		"base_url":"https://organization.example.com",
		"api_key":"organization-provider-secret",
		"model_ids":["org-only"]
	}`), adminCookies)
	created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("admin create status = %d, want 201", created.StatusCode)
	}
	read := app.platformRequest(t, http.MethodGet, "/api/console/organizations/"+orgUUID+"/llm_provider", nil, cookies)
	defer read.Body.Close()
	if read.StatusCode != http.StatusOK {
		t.Fatalf("member get status = %d, want 200: %s", read.StatusCode, readAll(t, read.Body))
	}
	var body map[string]any
	decodeJSON(t, read.Body, &body)
	if body["scope"] != "organization" || body["slug"] != "organization" || body["name"] != "Organization default" || body["has_api_key"] != true || body["api_key_last4"] != "cret" {
		t.Fatalf("member organization provider = %#v", body)
	}
	models, ok := body["model_ids"].([]any)
	if !ok || len(models) != 1 || models[0] != "org-only" {
		t.Fatalf("member model IDs = %#v", body["model_ids"])
	}
	if _, exists := body["api_key"]; exists {
		t.Fatal("member response contains an API key field")
	}
	assertJSONDoesNotContain(t, body, "organization-provider-secret")
}

func TestOrganizationLLMProviderFillsWorkspaceGaps(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("llm-providers-organization-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)

	orgUUID := loadDefaultOrganizationUUID(t, app)
	workspaceUUID := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	cookies := app.platformLoginCookies(t, "llm-providers-organization@example.com")
	path := "/api/console/organizations/" + orgUUID + "/llm_provider"
	workspacePath := "/api/console/organizations/" + orgUUID + "/workspaces/default/llm_providers"
	const orgKey = "organization-provider-secret"

	if _, err := llmproviders.ListModelIDs(context.Background(), app.db, orgUUID, workspaceUUID); !errors.Is(err, llmproviders.ErrNotConfigured) {
		t.Fatalf("empty model list error = %v, want ErrNotConfigured", err)
	}
	missing := app.platformRequest(t, http.MethodGet, path, nil, cookies)
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing organization provider status = %d, want 404: %s", missing.StatusCode, readAll(t, missing.Body))
	}

	for _, invalid := range []struct {
		method string
		path   string
		body   string
		code   string
	}{
		{http.MethodPut, path, `{"name":"Organization","slug":"custom","base_url":"https://organization.example.com","api_key":"test-key","model_ids":[]}`, "llm_provider_slug_immutable"},
		{http.MethodPost, workspacePath, `{"name":"Workspace","slug":"organization","base_url":"https://workspace.example.com","api_key":"test-key","model_ids":[]}`, "llm_provider_slug_invalid"},
	} {
		response := app.platformRequest(t, invalid.method, invalid.path, strings.NewReader(invalid.body), cookies)
		var body map[string]any
		decodeJSON(t, response.Body, &body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || body["code"] != invalid.code {
			t.Fatalf("invalid slug response = %d %#v", response.StatusCode, body)
		}
	}

	createdResponse := app.platformRequest(t, http.MethodPut, path, strings.NewReader(`{
		"name":"Organization default",
		"base_url":"https://organization.example.com/anthropic",
		"api_key":"organization-provider-secret",
		"model_ids":["org-only","shared-model"]
	}`), cookies)
	defer createdResponse.Body.Close()
	if createdResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create organization provider status = %d, want 201: %s", createdResponse.StatusCode, readAll(t, createdResponse.Body))
	}
	var created map[string]any
	decodeJSON(t, createdResponse.Body, &created)
	if created["scope"] != "organization" || created["api_key_last4"] != "cret" || created["slug"] != "organization" || created["api_backend"] != llmproviders.Messages {
		t.Fatalf("organization provider = %#v", created)
	}
	assertJSONDoesNotContain(t, created, orgKey)
	createdID, _ := created["id"].(string)

	var storedWorkspace *string
	if err := app.pool.QueryRow(context.Background(), `
		select workspace_uuid from llm_providers where external_id = $1
	`, createdID).Scan(&storedWorkspace); err != nil {
		t.Fatalf("load organization provider workspace: %v", err)
	}
	if storedWorkspace != nil {
		t.Fatalf("organization provider workspace_uuid = %v, want NULL", *storedWorkspace)
	}

	workspaceList := app.platformRequest(t, http.MethodGet, workspacePath, nil, cookies)
	defer workspaceList.Body.Close()
	workspaceBody := readAll(t, workspaceList.Body)
	if workspaceList.StatusCode != http.StatusOK || bytes.Contains(workspaceBody, []byte(createdID)) {
		t.Fatalf("workspace provider list = %d %s", workspaceList.StatusCode, workspaceBody)
	}

	orgUpstream, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "org-only")
	if err != nil || orgUpstream.BaseURL != "https://organization.example.com/anthropic" || orgUpstream.APIKey != orgKey {
		t.Fatalf("organization resolution = %#v, %v", orgUpstream, err)
	}
	otherWorkspace := "00000000-0000-4000-8000-000000000077"
	if _, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, otherWorkspace, "org-only"); err != nil {
		t.Fatalf("other workspace organization resolution: %v", err)
	}

	workspaceProvider := createConsoleLLMProvider(t, app, workspacePath, cookies, `{
		"name":"Workspace",
		"base_url":"https://workspace.example.com",
		"api_key":"workspace-provider-secret",
		"model_ids":["shared-model","workspace-only"]
	}`)
	assertJSONDoesNotContain(t, workspaceProvider, "workspace-provider-secret")
	shared, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "shared-model")
	if err != nil || shared.BaseURL != "https://workspace.example.com" || shared.APIKey != "workspace-provider-secret" {
		t.Fatalf("workspace override = %#v, %v", shared, err)
	}
	if _, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "org-only"); err != nil {
		t.Fatalf("organization-only model after workspace override: %v", err)
	}
	qualified, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "organization/shared-model")
	if err != nil || qualified.BaseURL != orgUpstream.BaseURL || qualified.QualifiedModelID != "organization/shared-model" || qualified.ModelID != "shared-model" {
		t.Fatalf("explicit organization model resolution = %#v, %v", qualified, err)
	}
	modelIDs, err := llmproviders.ListModelIDs(context.Background(), app.db, orgUUID, workspaceUUID)
	if err != nil {
		t.Fatalf("list merged models: %v", err)
	}
	if !slices.Equal(modelIDs, []string{"default/shared-model", "default/workspace-only", "organization/org-only", "organization/shared-model"}) {
		t.Fatalf("merged models = %#v", modelIDs)
	}

	externalID, err := ids.New("llmprov_test_")
	if err != nil {
		t.Fatalf("generate duplicate organization provider ID: %v", err)
	}
	now := time.Now().UTC()
	duplicate := db.LLMProvider{
		UUID:             uuid.NewString(),
		ExternalID:       externalID,
		OrganizationUUID: orgUUID,
		Name:             "Second default",
		Slug:             llmproviders.OrganizationSlug,
		APIBackend:       llmproviders.Messages,
		BaseURL:          "https://second.example.com",
		APIKeyLast4:      "cond",
		ModelIDs:         []string{"second-model"},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	plaintext := []byte("second-provider-secret")
	envelope, err := app.vaultSecrets.Seal(context.Background(), llmproviders.SecretBinding(duplicate), plaintext)
	clear(plaintext)
	if err != nil {
		t.Fatalf("seal duplicate organization provider: %v", err)
	}
	duplicate.SecretEnvelope = &envelope
	if _, err := app.db.CreateLLMProvider(context.Background(), duplicate); !errors.Is(err, db.ErrOrganizationLLMProviderExists) {
		t.Fatalf("duplicate organization provider error = %v, want ErrOrganizationLLMProviderExists", err)
	}

	updated := app.platformRequest(t, http.MethodPut, path, strings.NewReader(`{
		"name":"Organization default updated",
		"api_backend":"chat_completions",
		"base_url":"https://organization.example.com/anthropic",
		"model_ids":["org-only","shared-model"]
	}`), cookies)
	defer updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update organization provider status = %d, want 200: %s", updated.StatusCode, readAll(t, updated.Body))
	}
	retained, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, otherWorkspace, "org-only")
	if err != nil || retained.APIKey != orgKey || retained.APIBackend != llmproviders.ChatCompletions {
		t.Fatalf("updated organization key = %#v, %v", retained, err)
	}

	deleted := app.platformRequest(t, http.MethodDelete, path, nil, cookies)
	defer deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete organization provider status = %d, want 204: %s", deleted.StatusCode, readAll(t, deleted.Body))
	}
	if _, err := llmproviders.Resolve(context.Background(), app.db, app.vaultSecrets, orgUUID, workspaceUUID, "org-only"); !errors.Is(err, llmproviders.ErrModelNotConfigured) {
		t.Fatalf("deleted organization model error = %v, want ErrModelNotConfigured", err)
	}
}

func TestOrganizationLLMProviderPreviewAndSync(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("organization-provider-preview"))
	defer app.close()
	clearTestLLMProviders(t, app)
	cookies := app.platformLoginCookies(t, "organization-preview@example.com")
	orgUUID := loadDefaultOrganizationUUID(t, app)
	path := "/api/console/organizations/" + orgUUID + "/llm_provider"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("X-Api-Key") != "organization-preview-key" {
			t.Errorf("unexpected upstream request: %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"discovered-model"}]}`))
	}))
	defer upstream.Close()
	preview := app.platformRequest(t, http.MethodPost, path+"/preview_models", strings.NewReader(fmt.Sprintf(`{"base_url":%q,"api_key":"organization-preview-key","api_backend":"messages"}`, upstream.URL)), cookies)
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d: %s", preview.StatusCode, readAll(t, preview.Body))
	}
	var previewBody map[string]any
	decodeJSON(t, preview.Body, &previewBody)
	preview.Body.Close()
	assertJSONDoesNotContain(t, previewBody, "organization-preview-key")
	if ids, ok := previewBody["model_ids"].([]any); !ok || len(ids) != 1 || ids[0] != "discovered-model" {
		t.Fatalf("preview models = %#v", previewBody)
	}
	created := app.platformRequest(t, http.MethodPut, path, strings.NewReader(fmt.Sprintf(`{"name":"Organization","base_url":%q,"api_key":"organization-preview-key","model_ids":["manual-model"]}`, upstream.URL)), cookies)
	created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	synced := app.platformRequest(t, http.MethodPost, path+"/models/sync", nil, cookies)
	if synced.StatusCode != http.StatusOK {
		t.Fatalf("sync status = %d: %s", synced.StatusCode, readAll(t, synced.Body))
	}
	var syncBody map[string]any
	decodeJSON(t, synced.Body, &syncBody)
	synced.Body.Close()
	assertJSONDoesNotContain(t, syncBody, "organization-preview-key")
	if syncBody["scope"] != "organization" {
		t.Fatalf("sync scope = %#v", syncBody)
	}
	provider, err := app.db.GetOrganizationLLMProvider(t.Context(), orgUUID)
	if err != nil || !slices.Equal(provider.ModelIDs, []string{"manual-model", "discovered-model"}) {
		t.Fatalf("synced provider models = %v, %v", provider.ModelIDs, err)
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE users SET role='user' WHERE organization_uuid=$1 AND email='organization-preview@example.com'`, orgUUID); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []struct{ method, suffix string }{{http.MethodPost, "/preview_models"}, {http.MethodPost, "/models/sync"}, {http.MethodDelete, ""}} {
		response := app.platformRequest(t, operation.method, path+operation.suffix, strings.NewReader(`{}`), cookies)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("member %s %s status = %d", operation.method, operation.suffix, response.StatusCode)
		}
	}
}
