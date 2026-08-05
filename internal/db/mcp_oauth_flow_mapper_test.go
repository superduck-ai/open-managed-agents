package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestMCPOAuthFlowMapperBuilders(t *testing.T) {
	now := time.Date(2026, time.August, 5, 11, 0, 0, 0, time.UTC)
	params := insertMCPOAuthFlowParams{
		UUID: "flow-uuid", ExternalID: "mcpoauth_test", OrganizationUUID: "organization-uuid",
		WorkspaceUUID: "workspace-uuid", VaultUUID: "vault-uuid", VaultExternalID: "vault_test",
		MCPServerURL: "https://mcp.example.test", RedirectURL: "https://app.example.test/callback",
		DisplayName: "Test MCP", Source: "manual", AuthorizationEndpoint: "https://auth.example.test/authorize",
		TokenEndpoint: "https://auth.example.test/token", Resource: "https://mcp.example.test",
		ClientID: "client_test", ClientSecret: "secret", TokenEndpointAuthMethod: "client_secret_post",
		CodeVerifier: "verifier", CodeChallengeMethod: "S256", Status: "pending",
		CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: mCPOAuthFlowMapperInsertStatement,
		bound:     buildMCPOAuthFlowMapperInsert(yourbatis.DialectPostgres, params),
		wantID:    "MCPOAuthFlowMapper.Insert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
			"params.VaultUUID", "params.VaultExternalID", "params.UserUUID", "params.UserExternalID",
			"params.PlatformSessionExternalID", "params.MCPServerURL", "params.RedirectURL",
			"params.DisplayName", "params.Source", "params.AuthorizationEndpoint", "params.TokenEndpoint",
			"params.RegistrationEndpoint", "params.Issuer", "params.Resource", "params.Scope",
			"params.ClientID", "params.ClientSecret", "params.TokenEndpointAuthMethod", "params.CodeVerifier",
			"params.CodeChallengeMethod", "params.Status", "params.CreatedAt", "params.CreatedAt", "params.ExpiresAt",
		},
		wantSensitiveArgumentNames: []string{"params.ClientSecret", "params.CodeVerifier"},
		wantSQLFragments:           []string{"INSERT INTO mcp_oauth_flows", "NULLIF($21, '')", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         mCPOAuthFlowMapperCompleteStatement,
		bound:             buildMCPOAuthFlowMapperComplete(yourbatis.DialectPostgres, "mcpoauth_test", "vaultcred_test", now),
		wantID:            "MCPOAuthFlowMapper.Complete",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"credentialExternalID", "completedAt", "completedAt", "externalID"},
		wantSQLFragments:  []string{"status = 'completed'", "WHERE external_id = $4", "status = 'pending'"},
	})
}
