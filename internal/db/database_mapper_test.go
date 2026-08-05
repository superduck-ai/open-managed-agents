package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestDatabaseSeedMapperBuilders(t *testing.T) {
	organizationUUID := "11111111-1111-4111-8111-111111111111"
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	userUUID := "33333333-3333-4333-8333-333333333333"

	contracts := []mapperBuilderContract{
		{
			statement: adminOrganizationMapperSeedDefaultStatement,
			bound: buildAdminOrganizationMapperSeedDefault(
				yourbatis.DialectPostgres, "workspace_default", "default",
			),
			wantID:            "AdminOrganizationMapper.SeedDefault",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceExternalID", "name", "name"},
			wantSQLFragments:  []string{"WITH existing AS", "INSERT INTO organizations", "SELECT uuid FROM updated"},
		},
		{
			statement:         adminWorkspaceMapperSeedDefaultStatement,
			bound:             buildAdminWorkspaceMapperSeedDefault(yourbatis.DialectPostgres, "workspace_default", organizationUUID, "default"),
			wantID:            "AdminWorkspaceMapper.SeedDefault",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"externalID", "organizationUUID", "name"},
			wantSQLFragments:  []string{"INSERT INTO workspaces", "ON CONFLICT (external_id)"},
		},
		{
			statement:         adminUserMapperSeedDefaultStatement,
			bound:             buildAdminUserMapperSeedDefault(yourbatis.DialectPostgres, "user_default", organizationUUID, "admin@example.local", "Local Admin"),
			wantID:            "AdminUserMapper.SeedDefault",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"externalID", "organizationUUID", "email", "name"},
			wantSQLFragments:  []string{"INSERT INTO users", "ON CONFLICT (external_id)"},
		},
		{
			statement: adminWorkspaceMemberMapperSeedDefaultStatement,
			bound: buildAdminWorkspaceMemberMapperSeedDefault(yourbatis.DialectPostgres, seedAdminWorkspaceMemberParams{
				ExternalID: "wmem_default", OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID,
				WorkspaceExternalID: "workspace_default", UserUUID: userUUID, UserExternalID: "user_default",
			}),
			wantID:   "AdminWorkspaceMemberMapper.SeedDefault",
			wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.WorkspaceExternalID", "params.UserUUID", "params.UserExternalID",
			},
			wantSQLFragments: []string{"INSERT INTO workspace_members", "ON CONFLICT (external_id)"},
		},
		{
			statement: adminAPIKeyMapperSeedDefaultStatement,
			bound: buildAdminAPIKeyMapperSeedDefault(yourbatis.DialectPostgres, seedAdminAPIKeyParams{
				ExternalID: "api_key_default", WorkspaceUUID: workspaceUUID, KeyHash: "hash",
				CreatedByUserUUID: userUUID, Name: "default", PartialKeyHint: "sk-ant...test",
			}),
			wantID:   "AdminAPIKeyMapper.SeedDefault",
			wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.ExternalID", "params.WorkspaceUUID", "params.KeyHash",
				"params.CreatedByUserUUID", "params.Name", "params.PartialKeyHint",
			},
			wantSensitiveArgumentNames: []string{"params.KeyHash"},
			wantSQLFragments:           []string{"INSERT INTO api_keys", "ON CONFLICT (external_id)"},
		},
	}

	for _, contract := range contracts {
		t.Run(contract.wantID, func(t *testing.T) {
			assertMapperBuilderContract(t, contract)
		})
	}
}

func TestAPIKeyAuthenticationMapperBuilder(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:                  adminAPIKeyMapperFindActiveByKeyHashStatement,
		bound:                      buildAdminAPIKeyMapperFindActiveByKeyHash(yourbatis.DialectPostgres, "hash"),
		wantID:                     "AdminAPIKeyMapper.FindActiveByKeyHash",
		wantKind:                   yourbatis.StatementSelect,
		wantArgumentNames:          []string{"keyHash"},
		wantSensitiveArgumentNames: []string{"keyHash"},
		wantSQLFragments:           []string{"WHERE ak.key_hash = $1", "ak.status = 'active'"},
	})
}
