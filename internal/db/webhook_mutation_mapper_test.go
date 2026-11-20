package db

import (
	"github.com/superduck-ai/yourbatis"
	"testing"
)

func TestWebhookMutationMapperContracts(t *testing.T) {
	for _, tc := range []mapperBuilderContract{
		{statement: vaultCredentialMapperArchiveByVaultUUIDStatement,
			bound:  buildVaultCredentialMapperArchiveByVaultUUID(yourbatis.DialectPostgres, "workspace", "vault"),
			wantID: "VaultCredentialMapper.ArchiveByVaultUUID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "vaultUUID"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "vault_uuid = $2", "archived_at IS NULL", "RETURNING external_id"}},
		{statement: vaultCredentialMapperDeleteByVaultUUIDStatement,
			bound:  buildVaultCredentialMapperDeleteByVaultUUID(yourbatis.DialectPostgres, "workspace", "vault"),
			wantID: "VaultCredentialMapper.DeleteByVaultUUID", wantKind: yourbatis.StatementDelete,
			wantArgumentNames: []string{"workspaceUUID", "vaultUUID"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "vault_uuid = $2", "RETURNING external_id"}},
		{statement: vaultMapperArchiveByExternalIDStatement,
			bound:  buildVaultMapperArchiveByExternalID(yourbatis.DialectPostgres, "workspace", "vault"),
			wantID: "VaultMapper.ArchiveByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "externalID"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "external_id = $2", "archived_at IS NULL"}},
		{statement: vaultCredentialMapperArchiveByExternalIDStatement,
			bound:  buildVaultCredentialMapperArchiveByExternalID(yourbatis.DialectPostgres, "workspace", "vault", "credential"),
			wantID: "VaultCredentialMapper.ArchiveByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "vaultExternalID", "credentialExternalID"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "vault_external_id = $2", "external_id = $3", "archived_at IS NULL"}},
		{statement: sessionMapperArchiveStatement,
			bound:  buildSessionMapperArchive(yourbatis.DialectPostgres, "workspace", "session"),
			wantID: "SessionMapper.Archive", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "sessionExternalID"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "external_id = $2", "archived_at IS NULL"}},
		{statement: sessionThreadMapperArchiveStatement,
			bound:  buildSessionThreadMapperArchive(yourbatis.DialectPostgres, "workspace", "session", "thread"),
			wantID: "SessionThreadMapper.Archive", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "sessionExternalID", "threadExternalID"},
			wantSQLFragments:  []string{"workspace_uuid = $1", "session_external_id = $2", "external_id = $3", "archived_at IS NULL"}},
		{statement: sessionMapperUpdateByExternalIDStatement,
			bound:  buildSessionMapperUpdateByExternalID(yourbatis.DialectPostgres, sessionUpdateParams{WorkspaceUUID: "workspace", ExternalID: "session", AgentSnapshot: []byte(`{}`), Metadata: []byte(`{}`)}),
			wantID: "SessionMapper.UpdateByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.AgentSnapshot", "params.Title", "params.Metadata", "params.Budget", "params.BudgetReachedAt", "params.BudgetRemovedAt", "params.WorkspaceUUID", "params.ExternalID", "params.PreviousStatus", "params.PreviousUpdatedAt"},
			wantSensitiveArgumentNames: []string{"params.AgentSnapshot", "params.Title", "params.Metadata", "params.Budget"},
			wantSQLFragments:           []string{"title = $2", "workspace_uuid = $7", "external_id = $8", "archived_at IS NULL", "status = $9", "updated_at = $10"}},
	} {
		t.Run(tc.wantID, func(t *testing.T) { assertMapperBuilderContract(t, tc) })
	}
}
