package platformapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"

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

type consoleAPIKeyScopeStore struct {
	workspaces         []ConsoleWorkspace
	workspaceListCalls int
	apiKeyListCalls    int
	workspaceUUID      *string
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
	context.Context,
	CreateConsoleAPIKeyInput,
) (CreateConsoleAPIKeyResult, error) {
	return CreateConsoleAPIKeyResult{}, nil
}

func (s *consoleAPIKeyScopeStore) UpdateConsoleAPIKeyStatus(
	context.Context,
	UpdateConsoleAPIKeyStatusInput,
) (ConsoleAPIKey, error) {
	return ConsoleAPIKey{}, nil
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
