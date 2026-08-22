package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"

	"github.com/go-chi/chi/v5"
)

func TestNewWorkbenchAnthropicRequestUsesCompatibleProviderAuthentication(t *testing.T) {
	request, err := newWorkbenchAnthropicRequest(
		context.Background(),
		"https://provider.example.com/v1/messages",
		llmproviders.Upstream{APIKey: "provider-key"},
		map[string]any{"model": "test-model"},
		"application/json",
	)
	if err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("X-Api-Key") != "provider-key" || request.Header.Get("Authorization") != "Bearer provider-key" {
		t.Fatalf("provider authentication headers = %#v", request.Header)
	}
}

func TestWorkbenchModelsErrorDistinguishesMissingProviderFromLoadFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{name: "missing provider", err: llmproviders.ErrNotConfigured, wantCode: "workspace_llm_provider_not_configured"},
		{name: "load failure", err: errors.New("database unavailable"), wantCode: "workspace_model_configuration_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeWorkbenchModelsError(recorder, test.err)
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if recorder.Code != http.StatusServiceUnavailable || response["error"] != test.wantCode {
				t.Fatalf("response status=%d body=%#v", recorder.Code, response)
			}
		})
	}
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
	handler := newWorkbenchHandler(store, nil, nil)

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
	handler := newWorkbenchHandler(store, nil, nil)

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
	handler := newWorkbenchHandler(store, nil, nil)
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
	handler := newWorkbenchHandler(store, nil, nil)
	body := `{
		"name": "Copied prompt",
		"latest_revision": {
			"id": "workbench-revision-copied",
			"model_name": "kimi-k2.5",
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
