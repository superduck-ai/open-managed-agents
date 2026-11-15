package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformOAuthProviderMapperBuilders(t *testing.T) {
	now := time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)
	params := upsertPlatformOAuthProviderParams{
		UUID: "provider-uuid", ExternalID: "poap_test", Name: "Example", LogoURL: "https://example.test/logo.png",
		AuthorizeEndpoint: "https://auth.example.test/authorize",
		TokenEndpoint:     "https://auth.example.test/token", Scope: "read",
		DefaultSecretName: "EXAMPLE_TOKEN", Enabled: true,
		ClientIDEnvelope: new(`{"ciphertext":"id"}`), ClientSecretEnvelope: new(`{"ciphertext":"cipher"}`),
		TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "form",
		CreatedByPlatformAdminUUID: "admin-uuid", CreatedAt: now, UpdatedAt: now,
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: platformOAuthProviderMapperInsertStatement,
		bound:     buildPlatformOAuthProviderMapperInsert(yourbatis.DialectPostgres, params),
		wantID:    "PlatformOAuthProviderMapper.Insert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.UUID", "params.ExternalID", "params.Name", "params.LogoURL", "params.AuthorizeEndpoint",
			"params.TokenEndpoint", "params.Scope", "params.ClientIDEnvelope", "params.ClientSecretEnvelope",
			"params.TokenEndpointAuthMethod", "params.TokenEndpointEncoding",
			"params.CreatedByPlatformAdminUUID", "params.CreatedAt", "params.UpdatedAt", "params.DefaultSecretName", "params.Enabled", "params.Archived",
		},
		wantSensitiveArgumentNames: []string{"params.ClientIDEnvelope", "params.ClientSecretEnvelope"},
		wantSQLFragments:           []string{"INSERT INTO platform_oauth_providers", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         platformOAuthProviderMapperListStatement,
		bound:             buildPlatformOAuthProviderMapperList(yourbatis.DialectPostgres),
		wantID:            "PlatformOAuthProviderMapper.List",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{},
		wantSQLFragments:  []string{"FROM platform_oauth_providers", "ORDER BY created_at DESC"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         platformOAuthProviderMapperFindByExternalIDStatement,
		bound:             buildPlatformOAuthProviderMapperFindByExternalID(yourbatis.DialectPostgres, "poap_test"),
		wantID:            "PlatformOAuthProviderMapper.FindByExternalID",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"externalID"},
		wantSQLFragments:  []string{"WHERE external_id = $1"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: platformOAuthProviderMapperUpdateByUUIDStatement,
		bound:     buildPlatformOAuthProviderMapperUpdateByUUID(yourbatis.DialectPostgres, params),
		wantID:    "PlatformOAuthProviderMapper.UpdateByUUID",
		wantKind:  yourbatis.StatementUpdate,
		wantArgumentNames: []string{
			"params.Name", "params.LogoURL", "params.AuthorizeEndpoint", "params.TokenEndpoint", "params.Scope", "params.DefaultSecretName", "params.Enabled", "params.Archived",
			"params.ClientIDEnvelope", "params.ClientSecretEnvelope", "params.TokenEndpointAuthMethod",
			"params.TokenEndpointEncoding", "params.UpdatedAt", "params.UUID", "params.Version",
		},
		wantSensitiveArgumentNames: []string{"params.ClientIDEnvelope", "params.ClientSecretEnvelope"},
		wantSQLFragments:           []string{"UPDATE platform_oauth_providers", "RETURNING"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         platformOAuthProviderMapperDeleteByUUIDStatement,
		bound:             buildPlatformOAuthProviderMapperDeleteByUUID(yourbatis.DialectPostgres, "provider-uuid", 1),
		wantID:            "PlatformOAuthProviderMapper.DeleteByUUID",
		wantKind:          yourbatis.StatementDelete,
		wantArgumentNames: []string{"providerUUID", "expectedVersion"},
		wantSQLFragments:  []string{"version = $2", "DELETE FROM platform_oauth_providers"},
	})
}
