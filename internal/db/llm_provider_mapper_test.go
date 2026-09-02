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
		WorkspaceUUID:    workspaceUUID,
		ExternalID:       externalID,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"lock workspace", mapperBuilderContract{
			statement:         lLMProviderMapperLockWorkspaceStatement,
			bound:             buildLLMProviderMapperLockWorkspace(yourbatis.DialectPostgres, organizationUUID, workspaceUUID),
			wantID:            "LLMProviderMapper.LockWorkspace",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID"},
			wantSQLFragments:  []string{"pg_advisory_xact_lock", "CAST($1 AS text)", "CAST($2 AS text)"},
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
				"params.Name", "params.BaseURL", "params.APIKeyLast4", "params.ModelIDs",
				"params.Ciphertext", "params.Nonce", "params.WrappedDEK", "params.FormatVersion",
				"params.KeyProvider", "params.KeyVersion", "params.UpdatedAt", "params.OrganizationUUID",
				"params.WorkspaceUUID", "params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.Ciphertext", "params.Nonce", "params.WrappedDEK"},
			wantSQLFragments:           []string{"organization_uuid = $12", "workspace_uuid = $13", "external_id = $14"},
		}},
		{"delete", mapperBuilderContract{
			statement:         lLMProviderMapperDeleteByExternalIDStatement,
			bound:             buildLLMProviderMapperDeleteByExternalID(yourbatis.DialectPostgres, organizationUUID, workspaceUUID, externalID),
			wantID:            "LLMProviderMapper.DeleteByExternalID",
			wantKind:          yourbatis.StatementDelete,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "external_id = $3"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
		})
	}
}
