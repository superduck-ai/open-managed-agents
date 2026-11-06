package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestEnvOAuthFlowMapperBuilders(t *testing.T) {
	now := time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)
	createdBy := "apikey-uuid"
	params := insertEnvOAuthFlowParams{
		UUID: "flow-uuid", ExternalID: "eof_test", OrganizationUUID: "organization-uuid",
		WorkspaceUUID: "workspace-uuid", VaultUUID: "vault-uuid", VaultExternalID: "vault_test",
		ProviderKey: "builtin:feishu", SecretName: "LARKSUITE_CLI_USER_ACCESS_TOKEN",
		DisplayName: "Feishu OAuth", RedirectURL: "https://app.example.test/oauth/vault/env-success",
		AuthorizeEndpoint: "https://auth.example.test/authorize", TokenEndpoint: "https://auth.example.test/token",
		ClientID: "client", Scope: "offline_access", TokenEndpointAuthMethod: "client_secret_post",
		TokenEndpointEncoding: "json", NetworkingType: "limited", AllowedHosts: `["open.feishu.cn"]`,
		Status: "pending", CreatedByAPIKeyUUID: &createdBy, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute),
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: envOAuthFlowMapperInsertStatement,
		bound:     buildEnvOAuthFlowMapperInsert(yourbatis.DialectPostgres, params),
		wantID:    "EnvOAuthFlowMapper.Insert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
			"params.VaultUUID", "params.VaultExternalID", "params.ProviderKey", "params.SecretName",
			"params.DisplayName", "params.RedirectURL", "params.AuthorizeEndpoint", "params.TokenEndpoint",
			"params.ClientID", "params.Scope", "params.TokenEndpointAuthMethod", "params.TokenEndpointEncoding",
			"params.NetworkingType", "params.AllowedHosts", "params.Status", "params.CreatedByAPIKeyUUID",
			"params.CreatedAt", "params.ExpiresAt",
		},
		wantSQLFragments: []string{"INSERT INTO env_oauth_flows", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         envOAuthFlowMapperFindByExternalIDStatement,
		bound:             buildEnvOAuthFlowMapperFindByExternalID(yourbatis.DialectPostgres, "eof_test"),
		wantID:            "EnvOAuthFlowMapper.FindByExternalID",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"externalID"},
		wantSQLFragments:  []string{"WHERE external_id = $1"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         envOAuthFlowMapperCompleteStatement,
		bound:             buildEnvOAuthFlowMapperComplete(yourbatis.DialectPostgres, "eof_test", "vcrd_test", now),
		wantID:            "EnvOAuthFlowMapper.Complete",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"credentialExternalID", "completedAt", "completedAt", "externalID"},
		wantSQLFragments:  []string{"status = 'completed'", "WHERE external_id = $4", "status = 'pending'"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         envOAuthFlowMapperFailStatement,
		bound:             buildEnvOAuthFlowMapperFail(yourbatis.DialectPostgres, "eof_test", "token_exchange_failed", now),
		wantID:            "EnvOAuthFlowMapper.Fail",
		wantKind:          yourbatis.StatementUpdate,
		wantArgumentNames: []string{"errorCode", "failedAt", "externalID"},
		wantSQLFragments:  []string{"status = 'failed'", "WHERE external_id = $3", "status = 'pending'"},
	})
}
