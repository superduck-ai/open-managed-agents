package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestLLMProviderMapperTenantScope(t *testing.T) {
	const (
		organizationUUID = "00000000-0000-4000-8000-000000000001"
		workspaceUUID    = "00000000-0000-4000-8000-000000000002"
		externalID       = "llmprov_test"
	)
	params := llmProviderWriteParams{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    nullableString(workspaceUUID),
		ExternalID:       externalID,
		APIBackend:       "chat_completions",
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
			wantArgumentNames:          []string{"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.Name", "params.Slug", "params.APIBackend", "params.BaseURL", "params.APIKeyLast4", "params.Models", "params.Ciphertext", "params.Nonce", "params.WrappedDEK", "params.FormatVersion", "params.KeyProvider", "params.KeyVersion", "params.CreatedAt", "params.UpdatedAt"},
			wantSensitiveArgumentNames: []string{"params.Ciphertext", "params.Nonce", "params.WrappedDEK"},
			wantSQLFragments:           []string{"name, slug, api_backend, base_url", "$5, $6, $7, $8", "RETURNING"},
		}},
		{"list", mapperBuilderContract{
			statement:         lLMProviderMapperListByWorkspaceStatement,
			bound:             buildLLMProviderMapperListByWorkspace(yourbatis.DialectPostgres, organizationUUID, workspaceUUID),
			wantID:            "LLMProviderMapper.ListByWorkspace",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2"},
		}},
		{"find", mapperBuilderContract{
			statement:         lLMProviderMapperFindByExternalIDStatement,
			bound:             buildLLMProviderMapperFindByExternalID(yourbatis.DialectPostgres, organizationUUID, workspaceUUID, externalID),
			wantID:            "LLMProviderMapper.FindByExternalID",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "external_id = $3"},
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
				"params.WorkspaceUUID", "params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.Ciphertext", "params.Nonce", "params.WrappedDEK"},
			wantSQLFragments:           []string{"organization_uuid = $13", "workspace_uuid = $14", "external_id = $15"},
		}},
		{"find organization default", mapperBuilderContract{
			statement:         lLMProviderMapperFindOrganizationDefaultStatement,
			bound:             buildLLMProviderMapperFindOrganizationDefault(yourbatis.DialectPostgres, organizationUUID),
			wantID:            "LLMProviderMapper.FindOrganizationDefault",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid IS NULL"},
		}},
		{"update organization default", mapperBuilderContract{
			statement: lLMProviderMapperUpdateOrganizationDefaultStatement,
			bound:     buildLLMProviderMapperUpdateOrganizationDefault(yourbatis.DialectPostgres, params),
			wantID:    "LLMProviderMapper.UpdateOrganizationDefault",
			wantKind:  yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.Name", "params.APIBackend", "params.BaseURL", "params.APIKeyLast4", "params.Models",
				"params.Ciphertext", "params.Nonce", "params.WrappedDEK", "params.FormatVersion",
				"params.KeyProvider", "params.KeyVersion", "params.UpdatedAt", "params.OrganizationUUID",
				"params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.Ciphertext", "params.Nonce", "params.WrappedDEK"},
			wantSQLFragments:           []string{"organization_uuid = $13", "workspace_uuid IS NULL", "external_id = $14"},
		}},
		{"delete", mapperBuilderContract{
			statement:         lLMProviderMapperDeleteByExternalIDStatement,
			bound:             buildLLMProviderMapperDeleteByExternalID(yourbatis.DialectPostgres, organizationUUID, workspaceUUID, externalID),
			wantID:            "LLMProviderMapper.DeleteByExternalID",
			wantKind:          yourbatis.StatementDelete,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "external_id = $3"},
		}},
		{"delete organization default", mapperBuilderContract{
			statement:         lLMProviderMapperDeleteOrganizationDefaultStatement,
			bound:             buildLLMProviderMapperDeleteOrganizationDefault(yourbatis.DialectPostgres, organizationUUID, externalID),
			wantID:            "LLMProviderMapper.DeleteOrganizationDefault",
			wantKind:          yourbatis.StatementDelete,
			wantArgumentNames: []string{"organizationUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid IS NULL", "external_id = $2"},
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
	for _, bound := range []yourbatis.BoundSQL{buildLLMProviderMapperInsert(yourbatis.DialectPostgres, params), buildLLMProviderMapperUpdateByExternalID(yourbatis.DialectPostgres, params), buildLLMProviderMapperUpdateOrganizationDefault(yourbatis.DialectPostgres, params)} {
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
