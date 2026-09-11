package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestPlatformBillingCannotUseDeveloperEntrypoints(t *testing.T) {
	access := auth.WorkspaceAccess{OrganizationRole: "billing", Role: "workspace_billing", Source: "organization"}
	for _, path := range []string{"/v1/agents", "/v1/files", "/v1/sessions", "/api/org/upload_b64", "/api/organizations/org/proxy/v1/messages", "/api/organizations/org/mcp/vault-auth/start", "/web-api/sessions/sesn_test/stream", "/api/org/files/file_test/preview"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		if platformActionAllowed(request, access) {
			t.Fatalf("Billing allowed: %s", path)
		}
	}
	if !platformActionAllowed(httptest.NewRequest(http.MethodGet, "/api/organizations/org/workbench/prompts", nil), access) {
		t.Fatal("Billing 应保留 Workbench 能力")
	}
	access.Role = "workspace_admin"
	if !platformActionAllowed(httptest.NewRequest(http.MethodPost, "/v1/agents", nil), access) {
		t.Fatal("提权 Billing 未获得开发能力")
	}
	if platformActionAllowed(httptest.NewRequest(http.MethodPost, "/api/console/organizations/org/workspaces/", nil), access) {
		t.Fatal("工作区提权泄漏组织管理权限")
	}
}
