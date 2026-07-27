package db

import (
	"strings"
	"testing"
	"time"
)

func TestMCPOAuthFlowQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	flow := MCPOAuthFlow{
		UUID:                    "11111111-1111-4111-8111-111111111111",
		ExternalID:              "mcpoauth_test",
		OrganizationID:          1,
		WorkspaceID:             2,
		VaultID:                 3,
		VaultExternalID:         "vault_test",
		MCPServerURL:            "https://mcp.example.test",
		RedirectURL:             "https://app.example.test/callback",
		DisplayName:             "Test MCP",
		AuthorizationEndpoint:   "https://auth.example.test/authorize",
		TokenEndpoint:           "https://auth.example.test/token",
		Resource:                "https://mcp.example.test",
		ClientID:                "client_test",
		TokenEndpointAuthMethod: "none",
		CodeVerifier:            "verifier",
		CodeChallengeMethod:     "S256",
		Status:                  "pending",
		CreatedAt:               now,
		ExpiresAt:               now.Add(10 * time.Minute),
	}
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "create",
			query:        createMCPOAuthFlowQuery,
			arguments:    mcpOAuthFlowArguments(flow),
			wantArgCount: 28,
		},
		{
			name:         "get",
			query:        getMCPOAuthFlowQuery,
			arguments:    map[string]any{"external_id": flow.ExternalID},
			wantArgCount: 1,
		},
		{
			name:  "complete",
			query: completeMCPOAuthFlowQuery,
			arguments: map[string]any{
				"external_id":            flow.ExternalID,
				"credential_external_id": "vaultcred_test",
				"completed_at":           now,
			},
			wantArgCount: 4,
		},
		{
			name:  "fail",
			query: failMCPOAuthFlowQuery,
			arguments: map[string]any{
				"external_id": flow.ExternalID,
				"error_code":  "authorization_failed",
				"failed_at":   now,
			},
			wantArgCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, tt.query, tt.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if len(arguments) != tt.wantArgCount {
				t.Fatalf("bindNamed() arguments = %#v, want %d arguments", arguments, tt.wantArgCount)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("bound query still contains a named parameter: %s", query)
			}
		})
	}
}
