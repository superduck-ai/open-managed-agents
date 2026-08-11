package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"

	"github.com/go-chi/chi/v5"
	"github.com/samber/lo"
)

func handleWorkbenchModels(w http.ResponseWriter, r *http.Request) {
	newWorkbenchTestHandler(r).handleWorkbenchModels(w, r)
}

func handleWorkbenchCompletions(w http.ResponseWriter, r *http.Request) {
	newWorkbenchTestHandler(r).handleWorkbenchCompletions(w, r)
}

func handleWorkbenchGeneratePrompt(w http.ResponseWriter, r *http.Request) {
	newWorkbenchTestHandler(r).handleWorkbenchGeneratePrompt(w, r)
}

func handleWorkbenchGenerateTitle(w http.ResponseWriter, r *http.Request) {
	newWorkbenchTestHandler(r).handleWorkbenchGenerateTitle(w, r)
}

func handleWorkbenchGenerateTestCase(w http.ResponseWriter, r *http.Request) {
	newWorkbenchTestHandler(r).handleWorkbenchGenerateTestCase(w, r)
}

func handleWorkbenchModelCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	newWorkbenchTestHandler(r).handleWorkbenchModelCatalogRefresh(w, r)
}

type workbenchPersistenceContextKey struct{}
type workbenchAnthropicUpstreamContextKey struct{}
type workbenchModelCatalogContextKey struct{}
type workbenchModelCatalogUserStoreContextKey struct{}

func newWorkbenchTestHandler(r *http.Request) *workbenchHandler {
	h := newWorkbenchHandlerWithCatalog(
		workbenchPersistenceFromRequest(r),
		workbenchAnthropicUpstreamFromRequest(r),
		workbenchModelCatalogFromRequest(r),
		nil,
	)
	h.userStore = workbenchModelCatalogUserStoreFromRequest(r)
	return h
}

func withWorkbenchDependenciesAndCatalog(
	store workbenchPersistenceStore,
	upstream config.AnthropicUpstreamConfig,
	catalog modelcatalog.Reader,
	handler http.HandlerFunc,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), workbenchAnthropicUpstreamContextKey{}, upstream)
		if store != nil {
			ctx = context.WithValue(ctx, workbenchPersistenceContextKey{}, store)
		}
		if catalog != nil {
			ctx = context.WithValue(ctx, workbenchModelCatalogContextKey{}, catalog)
		}
		if userStore, ok := store.(workbenchModelCatalogUserStore); ok {
			ctx = context.WithValue(ctx, workbenchModelCatalogUserStoreContextKey{}, userStore)
		}
		handler(w, r.WithContext(ctx))
	}
}

func workbenchPersistenceFromRequest(r *http.Request) workbenchPersistenceStore {
	store, _ := r.Context().Value(workbenchPersistenceContextKey{}).(workbenchPersistenceStore)
	return store
}

func workbenchAnthropicUpstreamFromRequest(r *http.Request) config.AnthropicUpstreamConfig {
	upstream, _ := r.Context().Value(workbenchAnthropicUpstreamContextKey{}).(config.AnthropicUpstreamConfig)
	return upstream
}

func workbenchModelCatalogFromRequest(r *http.Request) modelcatalog.Reader {
	catalog, _ := r.Context().Value(workbenchModelCatalogContextKey{}).(modelcatalog.Reader)
	return catalog
}

func workbenchModelCatalogUserStoreFromRequest(r *http.Request) workbenchModelCatalogUserStore {
	store, _ := r.Context().Value(workbenchModelCatalogUserStoreContextKey{}).(workbenchModelCatalogUserStore)
	return store
}

func TestWorkbenchCreatorUsesPrincipalWhenCookiePresent(t *testing.T) {
	bootstrap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("bootstrap should not be called when a verified principal is present")
	}))
	defer bootstrap.Close()
	t.Setenv("PLATFORM_BOOTSTRAP_BASE_URL", bootstrap.URL)

	req := workbenchCreatorTestRequest("7482d00f-2e42-478b-b2db-07c3d056a3b6")
	req.Header.Set("Cookie", "sessionKey=test-session")

	creator := workbenchCreator(req)
	if creator["tagged_id"] != "user_default" {
		t.Fatalf("creator tagged_id = %#v", creator["tagged_id"])
	}
	if creator["uuid"] != "user_default" {
		t.Fatalf("creator uuid = %#v", creator["uuid"])
	}
}

func TestWorkbenchCreatorFallsBackToPrincipalWithoutCookie(t *testing.T) {
	bootstrap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("bootstrap should not be called without a cookie")
	}))
	defer bootstrap.Close()
	t.Setenv("PLATFORM_BOOTSTRAP_BASE_URL", bootstrap.URL)

	creator := workbenchCreator(workbenchCreatorTestRequest("7482d00f-2e42-478b-b2db-07c3d056a3b6"))
	if creator["tagged_id"] != "user_default" {
		t.Fatalf("creator tagged_id = %#v", creator["tagged_id"])
	}
}

func TestWorkbenchGeneratePromptFailsWithoutConfiguredGateway(t *testing.T) {
	t.Setenv("ANTHROPIC_UPSTREAM_API_KEY", "ignored-environment-key")

	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_prompt",
		`{"task":"Summarize support tickets into action items"}`,
	)
	rec := httptest.NewRecorder()

	withWorkbenchTestDependencies(config.AnthropicUpstreamConfig{}, handleWorkbenchGeneratePrompt)(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "AI gateway is not configured") || strings.Contains(body, "content_block_delta") {
		t.Fatalf("body = %s, want explicit gateway error without generated content", body)
	}
}

func TestWorkbenchGeneratePromptForwardsGatewayError(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "rate_limit_error", "message": "gateway quota exceeded"},
		})
	}))
	defer upstreamServer.Close()

	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_prompt",
		`{"task":"Summarize support tickets into action items"}`,
	)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{BaseURL: upstreamServer.URL, APIKey: "yaml-key"}

	withWorkbenchTestDependencies(upstream, handleWorkbenchGeneratePrompt)(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "gateway quota exceeded") || strings.Contains(body, "content_block_delta") {
		t.Fatalf("body = %s, want upstream gateway error without generated content", body)
	}
}

func TestWorkbenchGeneratePromptUsesMappedUpstreamModel(t *testing.T) {
	var upstreamModel string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		upstreamModel = body.Model
		http.Error(w, "force fallback", http.StatusBadGateway)
	}))
	defer upstreamServer.Close()

	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_prompt",
		`{"task":"Summarize support tickets into action items"}`,
	)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{
		BaseURL: upstreamServer.URL,
		APIKey:  "yaml-key",
		ModelMappings: map[string]string{
			"claude-sonnet-4-6": "glm-5-turbo",
		},
	}

	withWorkbenchDependenciesAndCatalog(nil, upstream, mappedWorkbenchTestCatalog(), handleWorkbenchGeneratePrompt)(rec, req)

	if upstreamModel != "glm-5-turbo" {
		t.Fatalf("upstream model = %q, want glm-5-turbo", upstreamModel)
	}
}

func TestWorkbenchCompletionsUseMappedUpstreamModel(t *testing.T) {
	var upstreamModel string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		upstreamModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer upstreamServer.Close()

	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/completions",
		`{"model_name":"claude-sonnet-4-6","messages":[{"role":"user","content":"Hello"}]}`,
	)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{
		BaseURL: upstreamServer.URL,
		APIKey:  "yaml-key",
		ModelMappings: map[string]string{
			"claude-sonnet-4-6": "glm-5-turbo",
		},
	}

	withWorkbenchDependenciesAndCatalog(nil, upstream, mappedWorkbenchTestCatalog(), handleWorkbenchCompletions)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if upstreamModel != "glm-5-turbo" {
		t.Fatalf("upstream model = %q, want glm-5-turbo", upstreamModel)
	}
}

func TestWorkbenchAnthropicTextResolvesUpstreamModelAtRequestBoundary(t *testing.T) {
	var upstreamModel string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		upstreamModel = body.Model
		writeJSON(w, http.StatusOK, map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "Generated value"}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer upstreamServer.Close()

	req := workbenchCreatorTestRequest("7482d00f-2e42-478b-b2db-07c3d056a3b6")
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{
		BaseURL: upstreamServer.URL,
		APIKey:  "yaml-key",
		ModelMappings: map[string]string{
			"claude-sonnet-4-6": "glm-5-turbo",
		},
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		if _, _, _, err := newWorkbenchHandler(nil, upstream, nil).workbenchAnthropicTextFromBody(r, map[string]any{
			"model":    "claude-sonnet-4-6",
			"messages": []any{},
		}); err != nil {
			t.Fatalf("workbenchAnthropicTextFromBody() error = %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}

	withWorkbenchDependenciesAndCatalog(nil, upstream, mappedWorkbenchTestCatalog(), handler)(rec, req)
	if upstreamModel != "glm-5-turbo" {
		t.Fatalf("upstream model = %q, want glm-5-turbo", upstreamModel)
	}
}

func TestWorkbenchGeneratePromptSystemPromptRequestsXMLSections(t *testing.T) {
	prompt := workbenchGeneratePromptSystemPrompt(true)
	for _, want := range []string{"<planning>...</planning>", "<Instructions>...</Instructions>", "Do not include markdown fences or any text outside those tags"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("generate prompt system prompt missing %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"Do not include a preface", "or <Instructions> tags"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("generate prompt system prompt still contains forbidden text %q: %s", forbidden, prompt)
		}
	}
}

func TestWorkbenchAnthropicEndpointUsesConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_UPSTREAM_BASE_URL", "https://ignored.example.test")
	upstream := config.AnthropicUpstreamConfig{BaseURL: "https://api.kimi.com/coding/", APIKey: "yaml-key"}

	endpoint, err := anthropicMessagesEndpoint(upstream)
	if err != nil {
		t.Fatalf("anthropicMessagesEndpoint error: %v", err)
	}
	if endpoint != "https://api.kimi.com/coding/v1/messages" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestWorkbenchAnthropicTokenUsesConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_UPSTREAM_API_KEY", "ignored-environment-key")
	upstream := config.AnthropicUpstreamConfig{APIKey: "yaml-key"}

	if token := proxyMessagesAnthropicToken(upstream); token != "yaml-key" {
		t.Fatalf("token = %q", token)
	}
}

func TestWorkbenchModelsExposeEffectiveModelMappings(t *testing.T) {
	orgUUID := "7482d00f-2e42-478b-b2db-07c3d056a3b6"
	req := workbenchCreatorTestRequest(orgUUID)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{
		ModelMappings: map[string]string{
			"claude-sonnet-4-6": "glm-5-turbo",
			"claude-opus-4-8":   "glm-5.2",
		},
	}

	catalog := workbenchTestCatalog{snapshot: modelcatalog.Snapshot{
		Models: []modelcatalog.Model{
			{ID: "glm-5-turbo", DisplayName: "GLM 5 Turbo"},
			{ID: "glm-5.2", DisplayName: "GLM 5.2"},
		},
		DefaultModelID:   "glm-5-turbo",
		DefaultAvailable: true,
	}}
	withWorkbenchDependenciesAndCatalog(nil, upstream, catalog, handleWorkbenchModels)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ModelMappings map[string]string `json:"model_mappings"`
		Models        []struct {
			ModelName string `json:"model_name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ModelMappings["claude-sonnet-4-6"] != "glm-5-turbo" {
		t.Fatalf("model_mappings = %#v", body.ModelMappings)
	}
	modelNames := make([]string, 0, len(body.Models))
	for _, model := range body.Models {
		modelNames = append(modelNames, model.ModelName)
	}
	for _, want := range []string{"glm-5-turbo", "glm-5.2"} {
		if !lo.Contains(modelNames, want) {
			t.Fatalf("models = %#v, missing %q", modelNames, want)
		}
	}
}

func TestWorkbenchRevisionModelUsesMappingAtWriteAndReadBoundaries(t *testing.T) {
	orgUUID := "3458f354-f4ba-4bcd-95ef-ef48b2534447"
	promptID := "prompt_model_mapping"
	revisionID := "revision_model_mapping"
	req := workbenchCreatorTestRequest(orgUUID)
	handler := newWorkbenchHandler(nil, config.AnthropicUpstreamConfig{
		ModelMappings: map[string]string{"claude-sonnet-4-6": "glm-5-turbo"},
	}, nil)

	created := handler.revisionFromBody(
		req,
		map[string]any{"model_name": "claude-sonnet-4-6"},
		revisionID,
		false,
		false,
	)
	if created["model_name"] != "glm-5-turbo" {
		t.Fatalf("created revision model = %#v, want glm-5-turbo", created["model_name"])
	}

	key := workbenchRevisionStoreKey(req, promptID, revisionID)
	workbenchLocalRevisions.Store(key, map[string]any{"id": revisionID, "model_name": "claude-sonnet-4-6"})
	defer workbenchLocalRevisions.Delete(key)
	stored, ok := handler.storedRevision(req, promptID, revisionID, false, false)
	if !ok || stored["model_name"] != "glm-5-turbo" {
		t.Fatalf("stored revision = %#v, want mapped model", stored)
	}
}

func TestWorkbenchGenerateTitleFailsWithoutConfiguredGateway(t *testing.T) {
	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_title",
		`{"message_content":"Summarize planning notes","model":"provider/model"}`,
	)
	rec := httptest.NewRecorder()

	withWorkbenchTestDependencies(config.AnthropicUpstreamConfig{}, handleWorkbenchGenerateTitle)(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("content-type = %q, want application/json", contentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, "AI gateway is not configured") || strings.Contains(body, "completion") {
		t.Fatalf("body = %s, want explicit gateway error without local title", body)
	}
}

func TestWorkbenchGenerateTestCaseFailsWithoutConfiguredGateway(t *testing.T) {
	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/evaluations/generate_test_case",
		`{"model_name":"provider/model","variables":[{"name":"TOPIC"}]}`,
	)
	rec := httptest.NewRecorder()

	withWorkbenchTestDependencies(config.AnthropicUpstreamConfig{}, handleWorkbenchGenerateTestCase)(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "AI gateway is not configured") || strings.Contains(body, "Generated TOPIC example") {
		t.Fatalf("body = %s, want explicit gateway error without generated values", body)
	}
}

func TestWorkbenchGenerateTestCaseUsesFirstCatalogModelWhenDefaultIsUnset(t *testing.T) {
	var upstreamModel string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		upstreamModel = requestBody.Model
		writeJSON(w, http.StatusOK, map[string]any{
			"content": []any{map[string]any{
				"type": "text",
				"text": "<planning>Use a concrete city.</planning><TOPIC>Beijing</TOPIC>",
			}},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer upstreamServer.Close()

	orgUUID := "7482d00f-2e42-478b-b2db-07c3d056a3b6"
	req := workbenchPostTestRequest(
		orgUUID,
		"/api/organizations/"+orgUUID+"/workbench/evaluations/generate_test_case",
		`{"variables":[{"name":"TOPIC"}]}`,
	)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{BaseURL: upstreamServer.URL, APIKey: "yaml-key"}
	catalog := workbenchTestCatalog{snapshot: modelcatalog.Snapshot{
		Models: []modelcatalog.Model{{ID: "provider/first"}, {ID: "provider/second"}},
	}}

	withWorkbenchDependenciesAndCatalog(nil, upstream, catalog, handleWorkbenchGenerateTestCase)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if upstreamModel != "provider/first" {
		t.Fatalf("upstream model = %q, want first catalog model", upstreamModel)
	}
}

func TestWorkbenchGenerateTitleUsesConfiguredAnthropicUpstream(t *testing.T) {
	var upstreamModel string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/messages" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if r.Header.Get("X-API-Key") != "yaml-key" {
			http.Error(w, "unexpected API key", http.StatusUnauthorized)
			return
		}
		var requestBody struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		upstreamModel = requestBody.Model
		writeJSON(w, http.StatusOK, map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "Configured YAML title"}},
			"usage":   map[string]any{"input_tokens": 7, "output_tokens": 3},
		})
	}))
	defer upstreamServer.Close()

	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_title",
		`{"message_content":"Summarize planning notes","model":"provider/model"}`,
	)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{
		BaseURL: upstreamServer.URL + "/anthropic",
		APIKey:  "yaml-key",
		ModelMappings: map[string]string{
			"provider/model": "glm-5.2",
		},
	}

	catalog := workbenchTestCatalog{snapshot: modelcatalog.Snapshot{
		Models: []modelcatalog.Model{{ID: "glm-5.2", DisplayName: "GLM 5.2"}},
	}}
	withWorkbenchDependenciesAndCatalog(nil, upstream, catalog, handleWorkbenchGenerateTitle)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["completion"] != "Configured YAML title" || body["input_tokens"] != float64(7) || body["output_tokens"] != float64(3) {
		t.Fatalf("unexpected configured upstream response: %#v", body)
	}
	if upstreamModel != "glm-5.2" {
		t.Fatalf("upstream model = %q, want glm-5.2", upstreamModel)
	}
}

func TestWorkbenchCompletionRejectsUnknownCatalogModel(t *testing.T) {
	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/completions",
		`{"model_name":"provider/unknown","messages":[{"role":"human","content":"hello"}]}`,
	)
	rec := httptest.NewRecorder()

	withWorkbenchTestDependencies(config.AnthropicUpstreamConfig{}, handleWorkbenchCompletions)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not available") {
		t.Fatalf("body = %s, want catalog validation error", rec.Body.String())
	}
}

func TestWorkbenchCompletionReportsUnavailableCatalog(t *testing.T) {
	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/completions",
		`{"model_name":"provider/model","messages":[{"role":"human","content":"hello"}]}`,
	)
	rec := httptest.NewRecorder()
	handler := withWorkbenchDependenciesAndCatalog(
		nil,
		config.AnthropicUpstreamConfig{},
		workbenchTestCatalog{err: modelcatalog.ErrUnavailable},
		handleWorkbenchCompletions,
	)

	handler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestWorkbenchModelsExposeStaleCatalogMetadata(t *testing.T) {
	now := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	var capabilities modelcatalog.Capabilities
	if err := json.Unmarshal([]byte(`{
		"code_execution":{"supported":true},
		"context_management":{"supported":true,"compact_20260112":{"supported":true}},
		"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":false},"high":{"supported":true},"xhigh":{"supported":false},"max":{"supported":true}},
		"thinking":{"supported":true,"types":{"enabled":{"supported":true},"adaptive":{"supported":false}}},
		"tool_use":{"supported":true},
		"image_input":{"supported":true},
		"pdf_input":{"supported":false},
		"structured_outputs":{"supported":true}
	}`), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	req := workbenchCreatorTestRequest("7482d00f-2e42-478b-b2db-07c3d056a3b6")
	rec := httptest.NewRecorder()
	handler := withWorkbenchDependenciesAndCatalog(
		nil,
		config.AnthropicUpstreamConfig{},
		workbenchTestCatalog{snapshot: modelcatalog.Snapshot{
			Models: []modelcatalog.Model{{
				ID:           "provider/model",
				DisplayName:  "Provider Model",
				Capabilities: capabilities,
			}},
			DefaultModelID:   "provider/model",
			DefaultAvailable: true,
			LastAttemptAt:    &now,
			LastSuccessAt:    &now,
			Stale:            true,
		}},
		handleWorkbenchModels,
	)

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	catalogState, _ := body["model_catalog"].(map[string]any)
	if catalogState["stale"] != true || catalogState["default_available"] != true {
		t.Fatalf("model_catalog = %#v", catalogState)
	}
	models, _ := body["models"].([]any)
	model, _ := models[0].(map[string]any)
	for _, field := range []string{
		"supports_code_execution",
		"supports_compact_context",
		"supports_thinking",
		"supports_thinking_enabled",
		"supports_tool_use",
		"supports_images",
		"supports_structured_outputs",
	} {
		if model[field] != true {
			t.Fatalf("model[%q] = %#v, want true", field, model[field])
		}
	}
	for _, field := range []string{"supports_auto_thinking", "supports_documents"} {
		if model[field] != false {
			t.Fatalf("model[%q] = %#v, want false", field, model[field])
		}
	}
	wantEffortLevels := []any{"low", "high", "max"}
	if !reflect.DeepEqual(model["supported_effort_levels"], wantEffortLevels) {
		t.Fatalf("supported effort levels = %#v, want %#v", model["supported_effort_levels"], wantEffortLevels)
	}
	rawCapabilities, _ := model["capabilities"].(map[string]any)
	if _, ok := rawCapabilities["image_input"]; !ok {
		t.Fatalf("capabilities = %#v, want complete provider payload", rawCapabilities)
	}
}

func TestWorkbenchModelCatalogRefreshRequiresOrganizationAdmin(t *testing.T) {
	for _, role := range []string{"user", "developer", "billing", "claude_code_user", "owner", "primary_owner", "membership_admin"} {
		t.Run(role, func(t *testing.T) {
			catalog := &workbenchRefreshTestCatalog{snapshot: modelcatalog.Snapshot{
				Models: []modelcatalog.Model{{ID: "provider/model"}},
			}}
			req := workbenchModelCatalogRefreshRequest(role)
			rec := httptest.NewRecorder()

			withWorkbenchDependenciesAndCatalog(nil, config.AnthropicUpstreamConfig{}, catalog, handleWorkbenchModelCatalogRefresh)(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if catalog.refreshes != 0 {
				t.Fatalf("refreshes = %d, want 0", catalog.refreshes)
			}
		})
	}
}

func TestWorkbenchModelCatalogRefreshRejectsMissingOrganizationUser(t *testing.T) {
	catalog := &workbenchRefreshTestCatalog{}
	req := workbenchModelCatalogRefreshRequest("admin")
	ctx := context.WithValue(
		req.Context(),
		workbenchModelCatalogUserStoreContextKey{},
		workbenchModelCatalogUserTestStore{err: db.ErrNotFound},
	)
	ctx = context.WithValue(ctx, workbenchModelCatalogContextKey{}, catalog)
	rec := httptest.NewRecorder()

	handleWorkbenchModelCatalogRefresh(rec, req.WithContext(ctx))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if catalog.refreshes != 0 {
		t.Fatalf("refreshes = %d, want 0", catalog.refreshes)
	}
}

func TestWorkbenchModelCatalogRefreshAcceptsOrganizationAdmin(t *testing.T) {
	catalog := &workbenchRefreshTestCatalog{snapshot: modelcatalog.Snapshot{
		Models: []modelcatalog.Model{{ID: "provider/model"}},
	}}
	req := workbenchModelCatalogRefreshRequest("admin")
	rec := httptest.NewRecorder()

	withWorkbenchDependenciesAndCatalog(nil, config.AnthropicUpstreamConfig{}, catalog, handleWorkbenchModelCatalogRefresh)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestWorkbenchModelCatalogRefreshReportsConcurrentRefresh(t *testing.T) {
	catalog := &workbenchRefreshTestCatalog{refreshErr: modelcatalog.ErrRefreshInProgress}
	req := workbenchModelCatalogRefreshRequest("admin")
	rec := httptest.NewRecorder()

	withWorkbenchDependenciesAndCatalog(nil, config.AnthropicUpstreamConfig{}, catalog, handleWorkbenchModelCatalogRefresh)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestWorkbenchModelCatalogRefreshReturnsUpdatedCatalog(t *testing.T) {
	catalog := &workbenchRefreshTestCatalog{snapshot: modelcatalog.Snapshot{
		Models:           []modelcatalog.Model{{ID: "provider/model", DisplayName: "Provider Model"}},
		DefaultModelID:   "provider/model",
		DefaultAvailable: true,
	}}
	req := workbenchModelCatalogRefreshRequest("admin")
	rec := httptest.NewRecorder()

	withWorkbenchDependenciesAndCatalog(nil, config.AnthropicUpstreamConfig{}, catalog, handleWorkbenchModelCatalogRefresh)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if catalog.refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", catalog.refreshes)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	models, _ := body["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %#v, want one refreshed model", body["models"])
	}
}

func TestCreateWorkbenchPromptReusesCapturedDefaultPrompt(t *testing.T) {
	orgUUID := "7482d00f-2e42-478b-b2db-07c3d056a3b6"
	store := &fakeWorkbenchPersistenceStore{
		prompts: map[string]WorkbenchPromptRecord{
			fakeWorkbenchPersistenceKey(orgUUID, workbenchDefaultPromptID): {
				OrgUUID:            orgUUID,
				PromptUUID:         workbenchDefaultPromptID,
				WorkspaceUUID:      fakeWorkbenchWorkspaceUUID("wrkspc_previous"),
				WorkspaceDisplayID: "wrkspc_previous",
				Name:               "Existing prompt",
			},
		},
	}
	handler := newWorkbenchHandler(store, config.AnthropicUpstreamConfig{}, nil)

	createReq := workbenchWorkspaceTestRequest(
		http.MethodPost,
		"/api/organizations/"+orgUUID+"/workspaces/default/prompts",
		orgUUID,
		"default",
		`{}`,
	)
	createRec := httptest.NewRecorder()
	handler.handleCreateWorkbenchPrompt(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if got := created["workspace_id"]; got != "default" {
		t.Fatalf("created workspace_id = %#v, want default", got)
	}
	createdID, _ := created["id"].(string)
	if createdID == "" {
		t.Fatalf("created id missing: %#v", created)
	}
	if createdID != workbenchDefaultPromptID {
		t.Fatalf("created id = %q, want captured default prompt id %q", createdID, workbenchDefaultPromptID)
	}

	listDefaultReq := workbenchWorkspaceTestRequest(
		http.MethodGet,
		"/api/organizations/"+orgUUID+"/workspaces/default/prompts",
		orgUUID,
		"default",
		"",
	)
	listDefaultRec := httptest.NewRecorder()
	handler.handleListWorkbenchWorkspacePrompts(listDefaultRec, listDefaultReq)
	if listDefaultRec.Code != http.StatusOK {
		t.Fatalf("list default status = %d, body = %s", listDefaultRec.Code, listDefaultRec.Body.String())
	}
	var defaultPrompts []map[string]any
	if err := json.Unmarshal(listDefaultRec.Body.Bytes(), &defaultPrompts); err != nil {
		t.Fatalf("decode default list response: %v", err)
	}
	if len(defaultPrompts) != 1 || defaultPrompts[0]["id"] != createdID || defaultPrompts[0]["workspace_id"] != "default" {
		t.Fatalf("default workspace prompts = %#v", defaultPrompts)
	}

	listPreviousReq := workbenchWorkspaceTestRequest(
		http.MethodGet,
		"/api/organizations/"+orgUUID+"/workspaces/wrkspc_previous/prompts",
		orgUUID,
		"wrkspc_previous",
		"",
	)
	listPreviousRec := httptest.NewRecorder()
	handler.handleListWorkbenchWorkspacePrompts(listPreviousRec, listPreviousReq)
	if listPreviousRec.Code != http.StatusOK {
		t.Fatalf("list previous status = %d, body = %s", listPreviousRec.Code, listPreviousRec.Body.String())
	}
	var previousPrompts []map[string]any
	if err := json.Unmarshal(listPreviousRec.Body.Bytes(), &previousPrompts); err != nil {
		t.Fatalf("decode previous list response: %v", err)
	}
	if len(previousPrompts) != 0 {
		t.Fatalf("previous workspace prompts = %#v", previousPrompts)
	}
}

func TestDeleteCapturedDefaultWorkbenchPromptResetsInsteadOfHidingIt(t *testing.T) {
	orgUUID := "7482d00f-2e42-478b-b2db-07c3d056a3b6"
	store := &fakeWorkbenchPersistenceStore{
		prompts: map[string]WorkbenchPromptRecord{
			fakeWorkbenchPersistenceKey(orgUUID, workbenchDefaultPromptID): {
				OrgUUID:            orgUUID,
				PromptUUID:         workbenchDefaultPromptID,
				WorkspaceUUID:      fakeWorkbenchWorkspaceUUID("default"),
				WorkspaceDisplayID: "default",
				Name:               "Prompt to reset",
			},
		},
	}
	handler := newWorkbenchHandler(store, config.AnthropicUpstreamConfig{}, nil)

	deleteReq := workbenchPromptTestRequest(
		http.MethodDelete,
		"/api/organizations/"+orgUUID+"/workbench/prompts/"+workbenchDefaultPromptID,
		orgUUID,
		workbenchDefaultPromptID,
		"",
	)
	deleteRec := httptest.NewRecorder()
	handler.handleDeleteWorkbenchPrompt(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteRec.Code, deleteRec.Body.String())
	}

	getReq := workbenchPromptTestRequest(
		http.MethodGet,
		"/api/organizations/"+orgUUID+"/workbench/prompts/"+workbenchDefaultPromptID,
		orgUUID,
		workbenchDefaultPromptID,
		"",
	)
	getRec := httptest.NewRecorder()
	handler.handleGetWorkbenchPrompt(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	record := store.prompts[fakeWorkbenchPersistenceKey(orgUUID, workbenchDefaultPromptID)]
	if record.DeletedAt != nil {
		t.Fatalf("captured default prompt stayed deleted: %#v", record.DeletedAt)
	}
}

func TestListWorkbenchPromptsIncludesCurrentWorkspacePrompts(t *testing.T) {
	orgUUID := "7482d00f-2e42-478b-b2db-07c3d056a3b6"
	store := &fakeWorkbenchPersistenceStore{
		prompts: map[string]WorkbenchPromptRecord{
			fakeWorkbenchPersistenceKey(orgUUID, "prompt_current"): {
				OrgUUID:            orgUUID,
				PromptUUID:         "prompt_current",
				WorkspaceUUID:      fakeWorkbenchWorkspaceUUID("default"),
				WorkspaceDisplayID: "default",
				Name:               "Current workspace prompt",
			},
			fakeWorkbenchPersistenceKey(orgUUID, "prompt_other_workspace"): {
				OrgUUID:            orgUUID,
				PromptUUID:         "prompt_other_workspace",
				WorkspaceUUID:      fakeWorkbenchWorkspaceUUID("wrkspc_other"),
				WorkspaceDisplayID: "wrkspc_other",
				Name:               "Other workspace prompt",
			},
		},
	}
	handler := newWorkbenchHandler(store, config.AnthropicUpstreamConfig{}, nil)
	req := workbenchPromptListTestRequest(orgUUID, "default")
	rec := httptest.NewRecorder()

	handler.handleListWorkbenchPrompts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var prompts []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &prompts); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("prompts = %#v, want default prompt and current workspace prompt", prompts)
	}
	var foundCurrent bool
	for _, prompt := range prompts {
		if prompt["id"] == "prompt_other_workspace" {
			t.Fatalf("other workspace prompt leaked into list: %#v", prompts)
		}
		if prompt["id"] == "prompt_current" && prompt["workspace_id"] == "default" {
			foundCurrent = true
		}
	}
	if !foundCurrent {
		t.Fatalf("current workspace prompt missing: %#v", prompts)
	}
}

func TestCreateWorkbenchPromptAcceptsInitialRevision(t *testing.T) {
	orgUUID := "1a3f24b5-2f6b-4d2d-85d3-5342b67b3c1a"
	store := &fakeWorkbenchPersistenceStore{}
	handler := newWorkbenchHandler(store, config.AnthropicUpstreamConfig{}, nil)
	body := `{
		"name": "Copied prompt",
		"latest_revision": {
			"id": "workbench-revision-copied",
			"model_name": "claude-opus-4-8",
			"messages": [
				{
					"role": "human",
					"content": [{"type": "text", "text": "Copied prompt body"}]
				}
			],
			"variables": [],
			"tools": []
		}
	}`
	req := workbenchWorkspaceTestRequest(
		http.MethodPost,
		"/api/organizations/"+orgUUID+"/workspaces/default/prompts",
		orgUUID,
		"default",
		body,
	)
	rec := httptest.NewRecorder()
	handler.handleCreateWorkbenchPrompt(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if got := created["name"]; got != "Copied prompt" {
		t.Fatalf("created name = %#v", got)
	}
	latest, ok := created["latest_revision"].(map[string]any)
	if !ok {
		t.Fatalf("latest_revision missing: %#v", created)
	}
	if got := latest["id"]; got != "workbench-revision-copied" {
		t.Fatalf("latest revision id = %#v", got)
	}
	messages, _ := latest["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("latest revision messages = %#v", latest["messages"])
	}
	message, _ := messages[0].(map[string]any)
	content, _ := message["content"].([]any)
	block, _ := content[0].(map[string]any)
	if got := block["text"]; got != "Copied prompt body" {
		t.Fatalf("copied message text = %#v", got)
	}
}

func workbenchCreatorTestRequest(orgUUID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+orgUUID+"/workbench/prompts", nil)
	return workbenchTestRequestWithMethod(req, orgUUID)
}

func workbenchPromptListTestRequest(orgUUID string, workspaceID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+orgUUID+"/workbench/prompts", nil)
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	return workbenchTestRequestWithMethod(req, orgUUID)
}

func workbenchPostTestRequest(orgUUID string, path string, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return workbenchTestRequestWithMethod(req, orgUUID)
}

func withWorkbenchTestDependencies(
	upstream config.AnthropicUpstreamConfig,
	handler http.HandlerFunc,
) http.HandlerFunc {
	return withWorkbenchDependenciesAndCatalog(nil, upstream, workbenchTestCatalog{
		snapshot: modelcatalog.Snapshot{
			Models:           []modelcatalog.Model{{ID: "provider/model", DisplayName: "Provider Model"}},
			DefaultModelID:   "provider/model",
			DefaultAvailable: true,
		},
	}, handler)
}

func mappedWorkbenchTestCatalog() workbenchTestCatalog {
	return workbenchTestCatalog{snapshot: modelcatalog.Snapshot{
		Models:           []modelcatalog.Model{{ID: "glm-5-turbo", DisplayName: "GLM 5 Turbo"}},
		DefaultModelID:   "glm-5-turbo",
		DefaultAvailable: true,
	}}
}

type workbenchTestCatalog struct {
	snapshot modelcatalog.Snapshot
	err      error
}

type workbenchRefreshTestCatalog struct {
	snapshot   modelcatalog.Snapshot
	refreshErr error
	refreshes  int
}

func (c *workbenchRefreshTestCatalog) Snapshot(context.Context) (modelcatalog.Snapshot, error) {
	return c.snapshot, nil
}

func (c *workbenchRefreshTestCatalog) ValidateModel(context.Context, string) error {
	return nil
}

func (c *workbenchRefreshTestCatalog) TryRefresh(context.Context) error {
	c.refreshes++
	return c.refreshErr
}

type workbenchModelCatalogUserTestStore struct {
	role string
	err  error
}

func (s workbenchModelCatalogUserTestStore) GetAdminUser(context.Context, string, string) (db.AdminUser, error) {
	return db.AdminUser{Role: s.role}, s.err
}

func workbenchModelCatalogRefreshRequest(role string) *http.Request {
	orgUUID := "7482d00f-2e42-478b-b2db-07c3d056a3b6"
	req := workbenchPostTestRequest(orgUUID, "/api/organizations/"+orgUUID+"/models/refresh", `{}`)
	principal := auth.Principal{
		CredentialType:            auth.CredentialTypePlatformSession,
		OrganizationUUID:          orgUUID,
		UserExternalID:            "user_default",
		PlatformSessionExternalID: "platform_session_test",
	}
	ctx := auth.WithPrincipal(req.Context(), principal)
	ctx = context.WithValue(ctx, workbenchModelCatalogUserStoreContextKey{}, workbenchModelCatalogUserTestStore{role: role})
	return req.WithContext(ctx)
}

func (c workbenchTestCatalog) Snapshot(context.Context) (modelcatalog.Snapshot, error) {
	return c.snapshot, c.err
}

func (c workbenchTestCatalog) ValidateModel(_ context.Context, modelID string) error {
	if c.err != nil {
		return c.err
	}
	for _, model := range c.snapshot.Models {
		if model.ID == modelID {
			return nil
		}
	}
	return modelcatalog.ErrUnknownModel
}

func workbenchTestRequestWithMethod(req *http.Request, orgUUID string) *http.Request {
	req.Host = "platform.claude.com"
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orgUuid", orgUUID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext)
	ctx = auth.WithPrincipal(ctx, auth.Principal{UserExternalID: "user_default", OrganizationUUID: orgUUID})
	return req.WithContext(ctx)
}

func workbenchWorkspaceTestRequest(method string, path string, orgUUID string, workspaceID string, body string) *http.Request {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = workbenchTestRequestWithMethod(req, orgUUID)
	chi.RouteContext(req.Context()).URLParams.Add("workspaceId", workspaceID)
	return req
}

func workbenchPromptTestRequest(method string, path string, orgUUID string, promptID string, body string) *http.Request {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = workbenchTestRequestWithMethod(req, orgUUID)
	chi.RouteContext(req.Context()).URLParams.Add("promptUuid", promptID)
	return req
}

type fakeWorkbenchPersistenceStore struct {
	prompts map[string]WorkbenchPromptRecord
}

func fakeWorkbenchPersistenceKey(orgUUID string, promptUUID string) string {
	return strings.TrimSpace(orgUUID) + "\x00" + strings.TrimSpace(promptUUID)
}

func fakeWorkbenchWorkspaceUUID(displayID string) string {
	switch strings.TrimSpace(displayID) {
	case "default":
		return "00000000-0000-4000-8000-000000000001"
	case "wrkspc_previous":
		return "00000000-0000-4000-8000-000000000002"
	case "wrkspc_other":
		return "00000000-0000-4000-8000-000000000003"
	default:
		return "00000000-0000-4000-8000-000000000004"
	}
}

func (s *fakeWorkbenchPersistenceStore) ListConsoleWorkspaces(
	_ context.Context,
	_ string,
	_ bool,
) ([]ConsoleWorkspace, error) {
	workspaces := map[string]ConsoleWorkspace{
		"default": {
			UUID:       fakeWorkbenchWorkspaceUUID("default"),
			ExternalID: "workspace_default",
			Name:       "Default",
		},
		"wrkspc_previous": {
			UUID:       fakeWorkbenchWorkspaceUUID("wrkspc_previous"),
			ExternalID: "wrkspc_previous",
			Name:       "Previous",
		},
		"wrkspc_other": {
			UUID:       fakeWorkbenchWorkspaceUUID("wrkspc_other"),
			ExternalID: "wrkspc_other",
			Name:       "Other",
		},
	}
	for _, record := range s.prompts {
		displayID := strings.TrimSpace(record.WorkspaceDisplayID)
		if displayID == "" || displayID == "default" {
			continue
		}
		workspaces[displayID] = ConsoleWorkspace{
			UUID:       fakeWorkbenchWorkspaceUUID(displayID),
			ExternalID: displayID,
			Name:       displayID,
		}
	}
	result := make([]ConsoleWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		result = append(result, workspace)
	}
	return result, nil
}

func (s *fakeWorkbenchPersistenceStore) GetWorkbenchPrompt(_ context.Context, orgUUID string, promptUUID string) (*WorkbenchPromptRecord, error) {
	record, ok := s.prompts[fakeWorkbenchPersistenceKey(orgUUID, promptUUID)]
	if !ok {
		return nil, ErrNotFound
	}
	return &record, nil
}

func (s *fakeWorkbenchPersistenceStore) ListWorkbenchPrompts(_ context.Context, orgUUID string, workspaceUUID string) ([]WorkbenchPromptRecord, error) {
	records := []WorkbenchPromptRecord{}
	for _, record := range s.prompts {
		if strings.TrimSpace(record.OrgUUID) != strings.TrimSpace(orgUUID) {
			continue
		}
		if strings.TrimSpace(record.WorkspaceUUID) != strings.TrimSpace(workspaceUUID) {
			continue
		}
		if record.DeletedAt != nil {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *fakeWorkbenchPersistenceStore) UpsertWorkbenchPrompt(_ context.Context, record WorkbenchPromptRecord) (WorkbenchPromptRecord, error) {
	if s.prompts == nil {
		s.prompts = map[string]WorkbenchPromptRecord{}
	}
	record.OrgUUID = strings.TrimSpace(record.OrgUUID)
	record.PromptUUID = strings.TrimSpace(record.PromptUUID)
	record.WorkspaceUUID = strings.TrimSpace(record.WorkspaceUUID)
	record.WorkspaceDisplayID = strings.TrimSpace(record.WorkspaceDisplayID)
	if record.WorkspaceDisplayID == "" {
		record.WorkspaceDisplayID = record.WorkspaceUUID
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	s.prompts[fakeWorkbenchPersistenceKey(record.OrgUUID, record.PromptUUID)] = record
	return record, nil
}

func (s *fakeWorkbenchPersistenceStore) DeleteWorkbenchPromptState(
	_ context.Context,
	orgUUID string,
	promptUUID string,
	workspaceUUID string,
	workspaceDisplayID string,
) error {
	record, ok := s.prompts[fakeWorkbenchPersistenceKey(orgUUID, promptUUID)]
	if !ok {
		record = WorkbenchPromptRecord{
			OrgUUID:            strings.TrimSpace(orgUUID),
			PromptUUID:         strings.TrimSpace(promptUUID),
			WorkspaceUUID:      strings.TrimSpace(workspaceUUID),
			WorkspaceDisplayID: strings.TrimSpace(workspaceDisplayID),
		}
	}
	now := time.Now().UTC()
	record.DeletedAt = &now
	record.UpdatedAt = now
	s.prompts[fakeWorkbenchPersistenceKey(orgUUID, promptUUID)] = record
	return nil
}

func (s *fakeWorkbenchPersistenceStore) GetWorkbenchRevision(context.Context, string, string, string) (*WorkbenchRevisionRecord, error) {
	return nil, ErrNotFound
}

func (s *fakeWorkbenchPersistenceStore) UpsertWorkbenchRevision(context.Context, WorkbenchRevisionRecord) error {
	return nil
}

func (s *fakeWorkbenchPersistenceStore) ListWorkbenchEvaluationRevisionIDs(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *fakeWorkbenchPersistenceStore) GetWorkbenchKV(context.Context, string, string, string) (*WorkbenchKVRecord, error) {
	return nil, ErrNotFound
}

func (s *fakeWorkbenchPersistenceStore) UpsertWorkbenchKV(context.Context, WorkbenchKVRecord) error {
	return nil
}

func (s *fakeWorkbenchPersistenceStore) DeleteWorkbenchKV(context.Context, string, string, string) error {
	return nil
}

func (s *fakeWorkbenchPersistenceStore) ListWorkbenchEvaluations(context.Context, string, string) ([]WorkbenchEvaluationRecord, error) {
	return nil, nil
}

func (s *fakeWorkbenchPersistenceStore) GetWorkbenchEvaluation(context.Context, string, string) (*WorkbenchEvaluationRecord, error) {
	return nil, ErrNotFound
}

func (s *fakeWorkbenchPersistenceStore) UpsertWorkbenchEvaluation(context.Context, WorkbenchEvaluationRecord) error {
	return nil
}

func (s *fakeWorkbenchPersistenceStore) DeleteWorkbenchEvaluation(context.Context, string, string) (*WorkbenchEvaluationRecord, error) {
	return nil, ErrNotFound
}

func (s *fakeWorkbenchPersistenceStore) AppendWorkbenchGeneratedTestCase(context.Context, string, map[string]any) error {
	return nil
}

func (s *fakeWorkbenchPersistenceStore) TakeWorkbenchGeneratedTestCase(context.Context, string, map[string]any) (map[string]any, bool, error) {
	return nil, false, nil
}
