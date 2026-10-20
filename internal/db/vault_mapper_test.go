package db

import (
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestVaultMapperBuilders(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC)
	cursor := &VaultPageCursor{CreatedAt: createdAt, UUID: "22222222-2222-4222-8222-222222222222"}

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         vaultMapperListActiveByExternalIDsStatement,
		bound:             buildVaultMapperListActiveByExternalIDs(yourbatis.DialectPostgres, "workspace", []string{"vault_a", "vault_b"}),
		wantID:            "VaultMapper.ListActiveByExternalIDs",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"workspaceUUID", "externalID", "externalID"},
		wantSQLFragments:  []string{"workspace_uuid = $1", "external_id IN ( $2 , $3 )", "archived_at IS NULL", "ORDER BY external_id"},
	})

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
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.WorkspaceUUID", "params.Cursor.UUID", "params.Cursor.CreatedAt", "params.Limit"},
		wantSQLFragments:  []string{"archived_at IS NULL", "(created_at, uuid) < ($2, $3)", "EXISTS ( SELECT 1 FROM vaults AS cursor_vault", "cursor_vault.workspace_uuid = $4", "cursor_vault.uuid = $5", "cursor_vault.created_at = $6", "LIMIT $7"},
	})
	withoutCursor := buildVaultMapperListPage(yourbatis.DialectPostgres, listVaultsMapperParams{
		WorkspaceUUID: "workspace", Limit: 21, IncludeArchived: true,
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         vaultMapperListPageStatement,
		bound:             withoutCursor,
		wantID:            "VaultMapper.ListPage",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.Limit"},
		wantSQLFragments:  []string{"workspace_uuid = $1", "deleted_at IS NULL", "LIMIT $2"},
	})
	if strings.Contains(withoutCursor.SQL, "EXISTS") || strings.Contains(withoutCursor.SQL, "archived_at IS NULL") {
		t.Fatal("first include-archived page retained a cursor or archive predicate")
	}
}
