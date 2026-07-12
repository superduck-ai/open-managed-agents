package mcpcatalogs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

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

func TestConfiguredProbeTimeoutDefaultsOnlyForNonPositiveValues(t *testing.T) {
	if got := configuredProbeTimeout(0); got != defaultProbeTimeout {
		t.Fatalf("zero timeout = %v, want %v", got, defaultProbeTimeout)
	}
	if got := configuredProbeTimeout(-time.Second); got != defaultProbeTimeout {
		t.Fatalf("negative timeout = %v, want %v", got, defaultProbeTimeout)
	}
	if got := configuredProbeTimeout(250 * time.Millisecond); got != 250*time.Millisecond {
		t.Fatalf("configured timeout = %v, want 250ms", got)
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
