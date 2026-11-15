package db

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
)

func TestLLMProviderMapperTenantScope(t *testing.T) {
	const (
		organizationUUID = "00000000-0000-4000-8000-000000000001"
		workspaceUUID    = "00000000-0000-4000-8000-000000000002"
		externalID       = "llmprov_test"
	)
	params := llmProviderWriteParams{
		OrganizationUUID:    organizationUUID,
		SecretWorkspaceUUID: nullableString(workspaceUUID),
		ExternalID:          externalID,
		APIBackend:          "chat_completions",
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement:                  lLMProviderMapperInsertStatement,
			bound:                      buildLLMProviderMapperInsert(yourbatis.DialectPostgres, params),
			wantID:                     "LLMProviderMapper.Insert",
			wantKind:                   yourbatis.StatementInsert,
			wantArgumentNames:          []string{"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.SecretWorkspaceUUID", "params.CreatedByAccountUUID", "params.Name", "params.Slug", "params.APIBackend", "params.BaseURL", "params.APIKeyLast4", "params.Models", "params.Ciphertext", "params.Nonce", "params.WrappedDEK", "params.FormatVersion", "params.KeyProvider", "params.KeyVersion", "params.CreatedAt", "params.UpdatedAt"},
			wantSensitiveArgumentNames: []string{"params.Ciphertext", "params.Nonce", "params.WrappedDEK"},
			wantSQLFragments:           []string{"name, slug, api_backend, base_url", "$6, $7, $8, $9", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement:         lLMProviderMapperFindByExternalIDStatement,
			bound:             buildLLMProviderMapperFindByExternalID(yourbatis.DialectPostgres, organizationUUID, externalID),
			wantID:            "LLMProviderMapper.FindByExternalID",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "external_id = $2"},
		}},
		{"update", mapperBuilderContract{
			statement: lLMProviderMapperUpdateByExternalIDStatement,
			bound:     buildLLMProviderMapperUpdateByExternalID(yourbatis.DialectPostgres, params),
			wantID:    "LLMProviderMapper.UpdateByExternalID",
			wantKind:  yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.Name", "params.APIBackend", "params.BaseURL", "params.APIKeyLast4", "params.Models",
				"params.Ciphertext", "params.Nonce", "params.WrappedDEK", "params.FormatVersion",
				"params.KeyProvider", "params.KeyVersion", "params.UpdatedAt", "params.OrganizationUUID",
				"params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.Ciphertext", "params.Nonce", "params.WrappedDEK"},
			wantSQLFragments:           []string{"organization_uuid = $13", "external_id = $14"},
		}},
		{"find organization default", mapperBuilderContract{
			statement:         lLMProviderMapperFindOrganizationDefaultStatement,
			bound:             buildLLMProviderMapperFindOrganizationDefault(yourbatis.DialectPostgres, organizationUUID),
			wantID:            "LLMProviderMapper.FindOrganizationDefault",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "slug = 'organization'"},
		}},
		{"delete", mapperBuilderContract{
			statement:         lLMProviderMapperDeleteByExternalIDStatement,
			bound:             buildLLMProviderMapperDeleteByExternalID(yourbatis.DialectPostgres, organizationUUID, externalID),
			wantID:            "LLMProviderMapper.DeleteByExternalID",
			wantKind:          yourbatis.StatementDelete,
			wantArgumentNames: []string{"organizationUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "external_id = $2"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}

func TestLLMProviderMapperBackendBinding(t *testing.T) {
	params := llmProviderWriteParams{APIBackend: "chat_completions"}
	for _, bound := range []yourbatis.BoundSQL{buildLLMProviderMapperInsert(yourbatis.DialectPostgres, params), buildLLMProviderMapperUpdateByExternalID(yourbatis.DialectPostgres, params)} {
		found := false
		for _, argument := range bound.Args {
			if argument.Name == "params.APIBackend" {
				found = true
				if argument.Value != "chat_completions" || argument.Sensitive {
					t.Fatalf("backend argument=%+v", argument)
				}
			}
		}
		if !found {
			t.Fatal("backend was not bound")
		}
	}
}

func TestLLMProviderEmptyModelsRoundTrip(t *testing.T) {
	for _, models := range []LLMModels{nil, {}} {
		params, err := llmProviderParams(LLMProvider{Models: models, SecretEnvelope: &secrets.Envelope{
			Ciphertext: []byte("x"), Nonce: []byte("x"), WrappedDEK: []byte("x"), FormatVersion: 1, KeyProvider: "local", KeyVersion: 1,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if string(params.Models) != "[]" {
			t.Fatalf("models encoded as %s", params.Models)
		}
		provider, err := (llmProviderRow{Models: params.Models}).provider()
		if err != nil {
			t.Fatal(err)
		}
		if provider.Models == nil || len(provider.Models) != 0 {
			t.Fatalf("decoded models: %#v", provider.Models)
		}
	}
}

func TestLLMProviderOrganizationListAndNullableScopeBinding(t *testing.T) {
	organization := "00000000-0000-4000-8000-000000000001"
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: lLMProviderMapperListByOrganizationStatement,
		bound:     buildLLMProviderMapperListByOrganization(yourbatis.DialectPostgres, organization),
		wantID:    "LLMProviderMapper.ListByOrganization", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID"}, wantSQLFragments: []string{"organization_uuid = $1", "ORDER BY created_at, uuid"},
	})
	bound := buildLLMProviderMapperFindByExternalID(yourbatis.DialectPostgres, organization, "provider")
	if len(bound.Args) != 2 {
		t.Fatal("missing organization and identity bindings")
	}
}

func TestLLMProviderCreatorProjection(t *testing.T) {
	for _, creator := range []*string{nil, new("00000000-0000-4000-8000-000000000002")} {
		row := llmProviderRow{Models: []byte("[]"), CreatedByAccountUUID: creator, CreatedByName: new("Creator")}
		provider, err := row.provider()
		if err != nil || provider.CreatedByAccountUUID != stringFromNullable(creator) || provider.CreatedByName != "Creator" {
			t.Fatalf("creator projection = %+v, %v", provider, err)
		}
	}
}
