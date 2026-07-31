package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/go-chi/chi/v5"
	"github.com/samber/lo"
)

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

func TestWorkbenchGeneratePromptFallsBackWithoutAnthropicToken(t *testing.T) {
	t.Setenv("ANTHROPIC_UPSTREAM_API_KEY", "ignored-environment-key")

	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_prompt",
		`{"task":"Summarize support tickets into action items"}`,
	)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{ModelMappings: map[string]string{
		"claude-sonnet-4-6": "glm-5-turbo",
	}}

	handler := newWorkbenchHandler(nil, upstream, nil)
	handler.handleWorkbenchGeneratePrompt(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"model":"glm-5-turbo"`, "event: content_block_delta", `\u003cplanning\u003e`, `\u003c/planning\u003e`, `\u003cInstructions\u003e`, "Summarize support tickets into action items", `\u003c/Instructions\u003e`} {
		if !strings.Contains(body, want) {
			t.Fatalf("fallback generate prompt stream missing %q: %s", want, body)
		}
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

	handler := newWorkbenchHandler(nil, upstream, nil)
	handler.handleWorkbenchGeneratePrompt(rec, req)

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

	handler := newWorkbenchHandler(nil, upstream, nil)
	handler.handleWorkbenchCompletions(rec, req)

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
	upstream := config.AnthropicUpstreamConfig{
		BaseURL: upstreamServer.URL,
		APIKey:  "yaml-key",
		ModelMappings: map[string]string{
			"claude-sonnet-4-6": "glm-5-turbo",
		},
	}
	handler := newWorkbenchHandler(nil, upstream, nil)
	if _, _, _, ok := handler.anthropicTextFromBody(req, map[string]any{
		"model":    "claude-sonnet-4-6",
		"messages": []any{},
	}); !ok {
		t.Fatal("anthropicTextFromBody() failed")
	}

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
	upstream := config.AnthropicUpstreamConfig{BaseURL: "https://api.kimi.com/coding/"}

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

	handler := newWorkbenchHandler(nil, upstream, nil)
	handler.handleWorkbenchModels(rec, req)

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

func TestWorkbenchGenerateTitleReturnsCompletionJSON(t *testing.T) {
	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_title",
		`{"message_content":"Summarize planning notes","model":"claude-opus-4-8"}`,
	)
	rec := httptest.NewRecorder()

	handler := newWorkbenchHandler(nil, config.AnthropicUpstreamConfig{}, nil)
	handler.handleWorkbenchGenerateTitle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("content-type = %q, want application/json", contentType)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["completion"] != "Summarize planning notes" {
		t.Fatalf("completion = %#v", body["completion"])
	}
	if strings.Contains(rec.Body.String(), "event:") {
		t.Fatalf("generate_title returned SSE body: %s", rec.Body.String())
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
		`{"message_content":"Summarize planning notes","model":"claude-opus-4-8"}`,
	)
	rec := httptest.NewRecorder()
	upstream := config.AnthropicUpstreamConfig{
		BaseURL: upstreamServer.URL + "/anthropic",
		APIKey:  "yaml-key",
		ModelMappings: map[string]string{
			"claude-opus-4-8": "glm-5.2",
		},
	}

	handler := newWorkbenchHandler(nil, upstream, nil)
	handler.handleWorkbenchGenerateTitle(rec, req)

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
