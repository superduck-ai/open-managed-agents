package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"

	"github.com/go-chi/chi/v5"
)

func TestPlatformMCPVaultAuthStartRejectsTrailingJSON(t *testing.T) {
	const orgUUID = "00000000-0000-4000-8000-000000000001"
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/organizations/"+orgUUID+"/mcp/vault-auth/start",
		strings.NewReader(`{"mcp_server_url":"https://mcp.example.com"} {}`),
	)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orgUuid", orgUUID)
	requestContext := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	request = request.WithContext(auth.WithPrincipal(requestContext, auth.Principal{OrganizationUUID: orgUUID}))
	recorder := httptest.NewRecorder()

	(&Server{}).handlePlatformMCPVaultAuthStart(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var response platformMCPVaultAuthErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ErrorCode != platformMCPVaultAuthVerificationRequestFailed {
		t.Fatalf("error_code = %q, want %q", response.ErrorCode, platformMCPVaultAuthVerificationRequestFailed)
	}
}
