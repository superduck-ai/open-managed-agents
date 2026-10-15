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
			wantArgumentNames:          []string{"params.AgentSnapshot", "params.Title", "params.Metadata", "params.UpdatedAt", "params.WorkspaceUUID", "params.ExternalID", "params.Title", "params.AgentSnapshot", "params.Metadata"},
			wantSensitiveArgumentNames: []string{"params.AgentSnapshot", "params.Metadata", "params.AgentSnapshot", "params.Metadata"},
			wantSQLFragments:           []string{"workspace_uuid = $5", "external_id = $6", "title IS DISTINCT FROM $7", "agent_snapshot IS DISTINCT FROM CAST($8 AS jsonb)", "(metadata - '_oma_runtime_user_uuid') IS DISTINCT FROM"}},
	} {
		t.Run(tc.wantID, func(t *testing.T) { assertMapperBuilderContract(t, tc) })
	}
}
