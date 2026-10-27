package mcpcatalogs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestProbeServerRoutesCanonicalTunnelThroughInjectedProber(t *testing.T) {
	t.Parallel()
	const endpoint = "https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/secondary"

	t.Run("recognized Tunnel failure does not fall back to anonymous HTTP", func(t *testing.T) {
		handler := NewHandler(nil, nil).WithTunnelProber(nil, func(
			context.Context,
			string,
			string,
			string,
		) ([]CatalogTool, bool, error) {
			return nil, true, apperr.New(apperr.Unavailable, "No tunnel connector is available", nil)
		})
		_, err := handler.probeServer(context.Background(), "org", "workspace", endpoint)
		var probeErr *ProbeError
		if !errors.As(err, &probeErr) || probeErr.Message != "No tunnel connector is available" {
			t.Fatalf("probe error = %#v, want safe Tunnel availability error", err)
		}
	})

	t.Run("successful Tunnel probe uses authenticated workspace scope and normalizes tools", func(t *testing.T) {
		var gotOrganization, gotWorkspace, gotEndpoint string
		handler := NewHandler(nil, nil).WithTunnelProber(nil, func(
			_ context.Context,
			organizationUUID string,
			workspaceUUID string,
			probeEndpoint string,
		) ([]CatalogTool, bool, error) {
			gotOrganization, gotWorkspace, gotEndpoint = organizationUUID, workspaceUUID, probeEndpoint
			return []CatalogTool{{Name: " search ", Title: " Search ", Description: " Find records. "}}, true, nil
		})
		result, err := handler.probeServer(context.Background(), "org-uuid", "workspace-uuid", endpoint)
		if err != nil {
			t.Fatalf("probe Tunnel server: %v", err)
		}
		if gotOrganization != "org-uuid" || gotWorkspace != "workspace-uuid" || gotEndpoint != endpoint {
			t.Fatalf("Tunnel probe scope = (%q, %q, %q)", gotOrganization, gotWorkspace, gotEndpoint)
		}
		if len(result.Tools) != 1 || result.Tools[0].Name != "search" || result.Tools[0].Title != "Search" || result.Tools[0].Description != "Find records." {
			t.Fatalf("Tunnel probe tools = %#v", result.Tools)
		}
	})
}

func TestReadCatalogRejectsTunnelOutsideAuthenticatedScope(t *testing.T) {
	t.Parallel()
	handler := NewHandler(nil, nil).WithTunnelProber(func(
		_ context.Context,
		organizationUUID string,
		workspaceUUID string,
		endpoint string,
	) (bool, error) {
		if organizationUUID != "org" || workspaceUUID != "workspace" || endpoint == "" {
			t.Fatalf("resolve scope = (%q, %q, %q)", organizationUUID, workspaceUUID, endpoint)
		}
		return true, apperr.New(apperr.NotFound, "Tunnel not found", nil)
	}, nil)
	response, err := handler.readCatalog(
		context.Background(),
		"org",
		"workspace",
		AgentServer{Name: "private", URL: "https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"},
	)
	if err != nil {
		t.Fatalf("read out-of-scope Tunnel catalog: %v", err)
	}
	if response.Status != "error" || response.Tools != nil {
		t.Fatalf("out-of-scope Tunnel catalog = %#v, want error/null", response)
	}
}

func TestCatalogResponsePreservesToolListKnowledge(t *testing.T) {
	tests := []struct {
		name      string
		response  catalogResponse
		wantTools string
	}{
		{
			name:      "unknown catalog serializes null",
			response:  catalogResponse{ServerName: "weather", Status: "unknown", Tools: nil},
			wantTools: "null",
		},
		{
			name: "known empty catalog serializes empty array",
			response: mapCatalog("weather", db.MCPToolCatalog{
				Tools: []db.MCPToolCatalogItem{},
			}),
			wantTools: "[]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.response)
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			var payload struct {
				Tools json.RawMessage `json:"tools"`
			}
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if string(payload.Tools) != test.wantTools {
				t.Fatalf("tools JSON = %s, want %s", payload.Tools, test.wantTools)
			}
		})
	}
}

func TestProbeHTTPErrorUsesGatewayStatuses(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "timeout", err: &ProbeError{Code: "timeout", Message: "timeout"}, wantStatus: http.StatusGatewayTimeout},
		{name: "authentication", err: &ProbeError{Code: "auth_required", Message: "auth"}, wantStatus: http.StatusBadGateway},
		{name: "network", err: &ProbeError{Code: "unreachable", Message: "network"}, wantStatus: http.StatusBadGateway},
		{name: "unknown", err: errors.New("boom"), wantStatus: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, _ := probeHTTPError(test.err)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
		})
	}
}

func TestPrincipalCanSeeRecoveredPlatformOrganizationAliasOnlyOnTrustedHost(t *testing.T) {
	principal := auth.Principal{OrganizationUUID: "local-org"}
	ctx := auth.WithPlatformMirrorOrganizationAlias(context.Background(), "official-org")

	trusted := httptest.NewRequest(http.MethodGet, "https://platform.claude.com/test", nil).WithContext(ctx)
	if !principalCanSeeOrganization(trusted, principal, "official-org") {
		t.Fatal("recovered alias on platform.claude.com was not visible")
	}

	untrusted := httptest.NewRequest(http.MethodGet, "https://attacker.example/test", nil).WithContext(ctx)
	if principalCanSeeOrganization(untrusted, principal, "official-org") {
		t.Fatal("recovered alias was visible on an untrusted host")
	}
}
