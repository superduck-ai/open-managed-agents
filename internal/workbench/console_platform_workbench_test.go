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

	withWorkbenchDependencies(nil, config.AnthropicUpstreamConfig{}, handleWorkbenchGeneratePrompt)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"event: content_block_delta", `\u003cplanning\u003e`, `\u003c/planning\u003e`, `\u003cInstructions\u003e`, "Summarize support tickets into action items", `\u003c/Instructions\u003e`} {
		if !strings.Contains(body, want) {
			t.Fatalf("fallback generate prompt stream missing %q: %s", want, body)
		}
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

func TestWorkbenchGenerateTitleReturnsCompletionJSON(t *testing.T) {
	req := workbenchPostTestRequest(
		"7482d00f-2e42-478b-b2db-07c3d056a3b6",
		"/api/organizations/7482d00f-2e42-478b-b2db-07c3d056a3b6/workbench/generate_title",
		`{"message_content":"Summarize planning notes","model":"claude-opus-4-8"}`,
	)
	rec := httptest.NewRecorder()

	withWorkbenchDependencies(nil, config.AnthropicUpstreamConfig{}, handleWorkbenchGenerateTitle)(rec, req)

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
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/messages" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if r.Header.Get("X-API-Key") != "yaml-key" {
			http.Error(w, "unexpected API key", http.StatusUnauthorized)
			return
		}
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
	upstream := config.AnthropicUpstreamConfig{BaseURL: upstreamServer.URL + "/anthropic", APIKey: "yaml-key"}

	withWorkbenchDependencies(nil, upstream, handleWorkbenchGenerateTitle)(rec, req)

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
}

func TestCreateWorkbenchPromptReusesCapturedDefaultPrompt(t *testing.T) {
	orgUUID := "7482d00f-2e42-478b-b2db-07c3d056a3b6"
	store := &fakeWorkbenchPersistenceStore{
		prompts: map[string]WorkbenchPromptRecord{
			fakeWorkbenchPersistenceKey(orgUUID, workbenchDefaultPromptID): {
				OrgUUID:     orgUUID,
				PromptUUID:  workbenchDefaultPromptID,
				WorkspaceID: "wrkspc_previous",
				Name:        "Existing prompt",
			},
		},
	}

	createReq := workbenchWorkspaceTestRequest(
		http.MethodPost,
		"/api/organizations/"+orgUUID+"/workspaces/default/prompts",
		orgUUID,
		"default",
		`{}`,
		store,
	)
	createRec := httptest.NewRecorder()
	handleCreateWorkbenchPrompt(createRec, createReq)
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
		store,
	)
	listDefaultRec := httptest.NewRecorder()
	handleListWorkbenchWorkspacePrompts(listDefaultRec, listDefaultReq)
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
		store,
	)
	listPreviousRec := httptest.NewRecorder()
	handleListWorkbenchWorkspacePrompts(listPreviousRec, listPreviousReq)
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
				OrgUUID:     orgUUID,
				PromptUUID:  workbenchDefaultPromptID,
				WorkspaceID: "default",
				Name:        "Prompt to reset",
			},
		},
	}

	deleteReq := workbenchPromptTestRequest(
		http.MethodDelete,
		"/api/organizations/"+orgUUID+"/workbench/prompts/"+workbenchDefaultPromptID,
		orgUUID,
		workbenchDefaultPromptID,
		"",
		store,
	)
	deleteRec := httptest.NewRecorder()
	handleDeleteWorkbenchPrompt(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteRec.Code, deleteRec.Body.String())
	}

	getReq := workbenchPromptTestRequest(
		http.MethodGet,
		"/api/organizations/"+orgUUID+"/workbench/prompts/"+workbenchDefaultPromptID,
		orgUUID,
		workbenchDefaultPromptID,
		"",
		store,
	)
	getRec := httptest.NewRecorder()
	handleGetWorkbenchPrompt(getRec, getReq)
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
				OrgUUID:     orgUUID,
				PromptUUID:  "prompt_current",
				WorkspaceID: "default",
				Name:        "Current workspace prompt",
			},
			fakeWorkbenchPersistenceKey(orgUUID, "prompt_other_workspace"): {
				OrgUUID:     orgUUID,
				PromptUUID:  "prompt_other_workspace",
				WorkspaceID: "wrkspc_other",
				Name:        "Other workspace prompt",
			},
		},
	}
	req := workbenchPromptListTestRequest(orgUUID, "default", store)
	rec := httptest.NewRecorder()

	handleListWorkbenchPrompts(rec, req)

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
		store,
	)
	rec := httptest.NewRecorder()
	handleCreateWorkbenchPrompt(rec, req)
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

func workbenchPromptListTestRequest(orgUUID string, workspaceID string, store workbenchPersistenceStore) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/organizations/"+orgUUID+"/workbench/prompts", nil)
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	req = workbenchTestRequestWithMethod(req, orgUUID)
	ctx := context.WithValue(req.Context(), workbenchPersistenceContextKey{}, store)
	return req.WithContext(ctx)
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

func workbenchWorkspaceTestRequest(method string, path string, orgUUID string, workspaceID string, body string, store workbenchPersistenceStore) *http.Request {
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
	ctx := context.WithValue(req.Context(), workbenchPersistenceContextKey{}, store)
	return req.WithContext(ctx)
}

func workbenchPromptTestRequest(method string, path string, orgUUID string, promptID string, body string, store workbenchPersistenceStore) *http.Request {
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
	ctx := context.WithValue(req.Context(), workbenchPersistenceContextKey{}, store)
	return req.WithContext(ctx)
}

type fakeWorkbenchPersistenceStore struct {
	prompts map[string]WorkbenchPromptRecord
}

func fakeWorkbenchPersistenceKey(orgUUID string, promptUUID string) string {
	return strings.TrimSpace(orgUUID) + "\x00" + strings.TrimSpace(promptUUID)
}

func (s *fakeWorkbenchPersistenceStore) GetWorkbenchPrompt(_ context.Context, orgUUID string, promptUUID string) (*WorkbenchPromptRecord, error) {
	record, ok := s.prompts[fakeWorkbenchPersistenceKey(orgUUID, promptUUID)]
	if !ok {
		return nil, ErrNotFound
	}
	return &record, nil
}

func (s *fakeWorkbenchPersistenceStore) ListWorkbenchPrompts(_ context.Context, orgUUID string, workspaceID string) ([]WorkbenchPromptRecord, error) {
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = "default"
	}
	records := []WorkbenchPromptRecord{}
	for _, record := range s.prompts {
		if strings.TrimSpace(record.OrgUUID) != strings.TrimSpace(orgUUID) {
			continue
		}
		if strings.TrimSpace(record.WorkspaceID) != strings.TrimSpace(workspaceID) {
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
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	if record.WorkspaceID == "" {
		record.WorkspaceID = "default"
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	s.prompts[fakeWorkbenchPersistenceKey(record.OrgUUID, record.PromptUUID)] = record
	return record, nil
}

func (s *fakeWorkbenchPersistenceStore) DeleteWorkbenchPromptState(_ context.Context, orgUUID string, promptUUID string) error {
	record, ok := s.prompts[fakeWorkbenchPersistenceKey(orgUUID, promptUUID)]
	if !ok {
		record = WorkbenchPromptRecord{OrgUUID: strings.TrimSpace(orgUUID), PromptUUID: strings.TrimSpace(promptUUID), WorkspaceID: "default"}
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
