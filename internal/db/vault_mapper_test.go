package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestVaultMapperBuilders(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC)
	cursor := &VaultPageCursor{CreatedAt: createdAt, UUID: "22222222-2222-4222-8222-222222222222"}

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         vaultMapperFindByIdentifierStatement,
		bound:             buildVaultMapperFindByIdentifier(yourbatis.DialectPostgres, "workspace", "vault_test", "vault-uuid"),
		wantID:            "VaultMapper.FindByIdentifier",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"workspaceUUID", "identifier", "vaultUUID"},
		wantSQLFragments:  []string{"workspace_uuid = $1", "external_id = $2", "OR uuid = $3"},
	})

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: vaultMapperListPageStatement,
		bound: buildVaultMapperListPage(yourbatis.DialectPostgres, listVaultsMapperParams{
			WorkspaceUUID: "workspace", Limit: 21, Cursor: cursor,
		}),
		wantID:            "VaultMapper.ListPage",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.Limit"},
		wantSQLFragments:  []string{"archived_at IS NULL", "(created_at, uuid) < ($2, $3)", "LIMIT $4"},
	})
}
