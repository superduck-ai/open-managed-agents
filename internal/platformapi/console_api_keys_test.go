package platformapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestListConsoleWorkspaceAPIKeysRejectsUnknownWorkspaceBeforeStoreQuery(t *testing.T) {
	store := &consoleAPIKeyScopeStore{}
	request := consoleWorkspaceAPIKeyTestRequest("workspace_missing")
	recorder := httptest.NewRecorder()

	handleListConsoleWorkspaceAPIKeys(store).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	if store.apiKeyListCalls != 0 {
		t.Fatalf("API key list calls = %d, want 0", store.apiKeyListCalls)
	}
}

func TestListConsoleWorkspaceAPIKeysResolvesExternalIDOnceAtBoundary(t *testing.T) {
	workspaceUUID := "00000000-0000-4000-8000-000000000001"
	store := &consoleAPIKeyScopeStore{
		workspaces: []ConsoleWorkspace{{
			UUID:       workspaceUUID,
			ExternalID: "workspace_test",
		}},
	}
	request := consoleWorkspaceAPIKeyTestRequest("workspace_test")
	recorder := httptest.NewRecorder()

	handleListConsoleWorkspaceAPIKeys(store).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if store.workspaceListCalls != 1 {
		t.Fatalf("workspace list calls = %d, want 1", store.workspaceListCalls)
	}
	if store.apiKeyListCalls != 1 || store.workspaceUUID == nil || *store.workspaceUUID != workspaceUUID {
		t.Fatalf("API key query scope = %#v after %d calls, want %q", store.workspaceUUID, store.apiKeyListCalls, workspaceUUID)
	}
}

func TestCreateConsoleWorkspaceAPIKeyUsesPrincipalUserUUID(t *testing.T) {
	const (
		organizationUUID = "00000000-0000-4000-8000-000000000002"
		workspaceUUID    = "00000000-0000-4000-8000-000000000001"
		userUUID         = "00000000-0000-4000-8000-000000000003"
	)
	store := &consoleAPIKeyScopeStore{
		workspaces: []ConsoleWorkspace{{
			UUID:       workspaceUUID,
			ExternalID: "workspace_test",
		}},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/organizations/"+organizationUUID+"/workspaces/workspace_test/api_keys",
		strings.NewReader(`{"name":"test key"}`),
	)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orgUuid", organizationUUID)
	routeContext.URLParams.Add("workspaceId", "workspace_test")
	requestContext := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	requestContext = auth.WithPrincipal(requestContext, auth.Principal{
		OrganizationUUID: organizationUUID,
		UserUUID:         userUUID,
		UserExternalID:   "user_external",
	})
	recorder := httptest.NewRecorder()

	handleCreateConsoleWorkspaceAPIKey(store).ServeHTTP(recorder, request.WithContext(requestContext))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if store.createInput == nil || store.createInput.CreatedByUserUUID == nil ||
		*store.createInput.CreatedByUserUUID != userUUID {
		t.Fatalf("created-by UUID = %#v, want internal user UUID %q", store.createInput, userUUID)
	}
}

type consoleAPIKeyScopeStore struct {
	workspaces         []ConsoleWorkspace
	workspaceListCalls int
	apiKeyListCalls    int
	workspaceUUID      *string
	createInput        *CreateConsoleAPIKeyInput
	apiKeyCounts       map[string]int
}

func (s *consoleAPIKeyScopeStore) ListConsoleWorkspaces(
	context.Context,
	string,
	bool,
) ([]ConsoleWorkspace, error) {
	s.workspaceListCalls++
	return s.workspaces, nil
}

func (s *consoleAPIKeyScopeStore) ListConsoleAPIKeys(
	_ context.Context,
	_ string,
	workspaceUUID *string,
) ([]ConsoleAPIKey, error) {
	s.apiKeyListCalls++
	s.workspaceUUID = workspaceUUID
	return []ConsoleAPIKey{}, nil
}

func (s *consoleAPIKeyScopeStore) CreateConsoleAPIKey(
	_ context.Context,
	input CreateConsoleAPIKeyInput,
) (CreateConsoleAPIKeyResult, error) {
	s.createInput = &input
	return CreateConsoleAPIKeyResult{}, nil
}

func (s *consoleAPIKeyScopeStore) UpdateConsoleAPIKeyStatus(
	context.Context,
	UpdateConsoleAPIKeyStatusInput,
) (ConsoleAPIKey, error) {
	return ConsoleAPIKey{}, nil
}

func (s *consoleAPIKeyScopeStore) CountConsoleAPIKeysByOrganization(
	context.Context,
	string,
) (map[string]int, error) {
	return s.apiKeyCounts, nil
}

func (s *consoleAPIKeyScopeStore) ListUserWorkspaceRoles(
	context.Context,
	string,
	string,
) ([]db.WorkspaceRoleFact, error) {
	return []db.WorkspaceRoleFact{}, nil
}

func TestListConsoleWorkspacesReturnsCreatedAtAndAPIKeyCounts(t *testing.T) {
	workspaceUUID := "00000000-0000-4000-8000-000000000001"
	store := &consoleAPIKeyScopeStore{
		workspaces: []ConsoleWorkspace{{
			UUID:       workspaceUUID,
			ExternalID: "workspace_test",
			Name:       "test workspace",
			OrgUUID:    "org_test",
			CreatedAt:  time.Date(2026, 2, 3, 7, 12, 0, 0, time.UTC),
		}},
		apiKeyCounts: map[string]int{workspaceUUID: 3},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/organizations/org_test/workspaces", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orgUuid", "org_test")
	requestContext := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	requestContext = auth.WithPrincipal(requestContext, auth.Principal{
		OrganizationUUID: "org_test",
		UserUUID:         "00000000-0000-4000-8000-000000000003",
		UserExternalID:   "user_external",
		WorkspaceAccess:  auth.WorkspaceAccess{OrganizationRole: "admin"},
	})
	request = request.WithContext(requestContext)
	recorder := httptest.NewRecorder()

	handleListConsoleWorkspaces(store).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var entries []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal response %s: %v", recorder.Body.String(), err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0]["id"] != "workspace_test" {
		t.Fatalf("id = %v, want workspace_test", entries[0]["id"])
	}
	if entries[0]["created_at"] != "2026-02-03T07:12:00Z" {
		t.Fatalf("created_at = %v, want 2026-02-03T07:12:00Z", entries[0]["created_at"])
	}
	if entries[0]["api_keys_count"].(float64) != 3 {
		t.Fatalf("api_keys_count = %v, want 3", entries[0]["api_keys_count"])
	}
}

func (s *consoleAPIKeyScopeStore) CountConsoleAPIKeys(context.Context, string, string) (int, error) {
	return 0, nil
}

func consoleWorkspaceAPIKeyTestRequest(workspaceReference string) *http.Request {
	const orgUUID = "00000000-0000-4000-8000-000000000002"
	request := httptest.NewRequest(http.MethodGet, "/api/organizations/"+orgUUID+"/workspaces/"+workspaceReference+"/api_keys", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orgUuid", orgUUID)
	routeContext.URLParams.Add("workspaceId", workspaceReference)
	contextWithRoute := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	contextWithPrincipal := auth.WithPrincipal(contextWithRoute, auth.Principal{OrganizationUUID: orgUUID})
	return request.WithContext(contextWithPrincipal)
}

func (s *consoleAPIKeyScopeStore) GetAdminUser(_ context.Context, orgUUID, userID string) (db.AdminUser, error) {
	return db.AdminUser{UUID: "00000000-0000-4000-8000-000000000003", ExternalID: userID, OrganizationUUID: orgUUID, Role: "admin"}, nil
}
func (s *consoleAPIKeyScopeStore) GetAdminWorkspace(_ context.Context, orgUUID, workspaceID string) (db.AdminWorkspace, error) {
	for _, workspace := range s.workspaces {
		if workspace.UUID == workspaceID || workspace.ExternalID == workspaceID {
			return db.AdminWorkspace{UUID: workspace.UUID, ExternalID: workspace.ExternalID, OrganizationUUID: orgUUID, IsDefault: workspace.IsDefault}, nil
		}
	}
	return db.AdminWorkspace{}, db.ErrNotFound
}
func (s *consoleAPIKeyScopeStore) GetAdminWorkspaceMember(context.Context, string, string, string) (db.AdminWorkspaceMember, error) {
	return db.AdminWorkspaceMember{}, db.ErrNotFound
}

type consoleWorkspaceMutationStore struct {
	updated      ConsoleWorkspace
	updateErr    error
	archived     ConsoleWorkspace
	archiveErr   error
	updateCalls  int
	updateOrg    string
	updateID     string
	updateName   string
	updateColor  string
	archiveCalls int
	archiveOrg   string
	archiveID    string
}

func (s *consoleWorkspaceMutationStore) UpdateConsoleWorkspace(
	_ context.Context, orgUUID, workspaceID, name, displayColor string,
) (ConsoleWorkspace, error) {
	s.updateCalls++
	s.updateOrg, s.updateID, s.updateName, s.updateColor = orgUUID, workspaceID, name, displayColor
	if s.updateErr != nil {
		return ConsoleWorkspace{}, s.updateErr
	}
	return s.updated, nil
}

func (s *consoleWorkspaceMutationStore) ArchiveConsoleWorkspace(
	_ context.Context, orgUUID, workspaceID string,
) (ConsoleWorkspace, error) {
	s.archiveCalls++
	s.archiveOrg, s.archiveID = orgUUID, workspaceID
	if s.archiveErr != nil {
		return ConsoleWorkspace{}, s.archiveErr
	}
	return s.archived, nil
}

func consoleWorkspaceMutationRequest(method, workspaceID, body string) *http.Request {
	const organizationUUID = "00000000-0000-4000-8000-000000000002"
	request := httptest.NewRequest(
		method,
		"/api/organizations/"+organizationUUID+"/workspaces/"+workspaceID,
		strings.NewReader(body),
	)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orgUuid", organizationUUID)
	routeContext.URLParams.Add("workspaceId", workspaceID)
	contextWithRoute := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	return request.WithContext(auth.WithPrincipal(contextWithRoute, auth.Principal{
		OrganizationUUID: organizationUUID,
		UserUUID:         "00000000-0000-4000-8000-000000000003",
		UserExternalID:   "user_external",
		WorkspaceAccess:  auth.WorkspaceAccess{OrganizationRole: "admin"},
	}))
}

func TestUpdateConsoleWorkspaceRenamesAndRecolors(t *testing.T) {
	updatedAt := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	store := &consoleWorkspaceMutationStore{
		updated: ConsoleWorkspace{
			UUID: "00000000-0000-4000-8000-000000000001", ExternalID: "wrkspc_test", OrgUUID: "org_test",
			Name: "renamed", DisplayColor: "#123456", Color: "#123456", UpdatedAt: updatedAt,
		},
	}

	recorder := httptest.NewRecorder()
	handleUpdateConsoleWorkspace(store).ServeHTTP(
		recorder,
		consoleWorkspaceMutationRequest(http.MethodPatch, "wrkspc_test", `{"name":"renamed","display_color":"#123456"}`),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if store.updateCalls != 1 || store.updateOrg != "00000000-0000-4000-8000-000000000002" || store.updateID != "wrkspc_test" ||
		store.updateName != "renamed" || store.updateColor != "#123456" {
		t.Fatalf("update input = %v/%v/%v/%v, unexpected", store.updateOrg, store.updateID, store.updateName, store.updateColor)
	}
	var entry map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal response %s: %v", recorder.Body.String(), err)
	}
	if entry["name"] != "renamed" || entry["display_color"] != "#123456" {
		t.Fatalf("entry = %v, want renamed workspace", entry)
	}
}

func TestUpdateConsoleWorkspaceReservesDefaultName(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleUpdateConsoleWorkspace(&consoleWorkspaceMutationStore{}).ServeHTTP(
		recorder,
		consoleWorkspaceMutationRequest(http.MethodPatch, "wrkspc_test", `{"name":"Default"}`),
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestUpdateConsoleWorkspaceReturnsNotFoundForMissingWorkspace(t *testing.T) {
	store := &consoleWorkspaceMutationStore{updateErr: db.ErrNotFound}

	recorder := httptest.NewRecorder()
	handleUpdateConsoleWorkspace(store).ServeHTTP(
		recorder,
		consoleWorkspaceMutationRequest(http.MethodPatch, "wrkspc_missing", `{"name":"renamed"}`),
	)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestArchiveConsoleWorkspaceReturnsArchivedWorkspace(t *testing.T) {
	archivedAt := time.Date(2026, 9, 11, 9, 30, 0, 0, time.UTC)
	store := &consoleWorkspaceMutationStore{
		archived: ConsoleWorkspace{
			UUID: "00000000-0000-4000-8000-000000000001", ExternalID: "wrkspc_test", OrgUUID: "org_test",
			Name: "legacy", ArchivedAt: &archivedAt, UpdatedAt: archivedAt,
		},
	}

	recorder := httptest.NewRecorder()
	handleArchiveConsoleWorkspace(store).ServeHTTP(
		recorder,
		consoleWorkspaceMutationRequest(http.MethodPost, "wrkspc_test", ""),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if store.archiveCalls != 1 || store.archiveOrg != "00000000-0000-4000-8000-000000000002" || store.archiveID != "wrkspc_test" {
		t.Fatalf("archive input = %v/%v/%v, unexpected", store.archiveOrg, store.archiveID, store.archiveCalls)
	}
	var entry map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal response %s: %v", recorder.Body.String(), err)
	}
	if entry["archived_at"] != "2026-09-11T09:30:00Z" {
		t.Fatalf("archived_at = %v, want 2026-09-11T09:30:00Z", entry["archived_at"])
	}
}

func TestArchiveConsoleWorkspaceRejectsDefaultWorkspace(t *testing.T) {
	store := &consoleWorkspaceMutationStore{archiveErr: db.ErrNotFound}

	recorder := httptest.NewRecorder()
	handleArchiveConsoleWorkspace(store).ServeHTTP(
		recorder,
		consoleWorkspaceMutationRequest(http.MethodPost, "default", ""),
	)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	if store.archiveCalls != 1 {
		t.Fatalf("archive calls = %d, want 1", store.archiveCalls)
	}
}
