package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestAPIKeyRuntimeUserBindingScope(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: adminAPIKeyMapperBindEmptyRuntimeUserStatement,
		bound:     buildAdminAPIKeyMapperBindEmptyRuntimeUser(yourbatis.DialectPostgres, "org", "workspace", "key", "account"),
		wantID:    "AdminAPIKeyMapper.BindEmptyRuntimeUser", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "externalID", "runtimeUserUUID"},
		wantSQLFragments:  []string{"ak.workspace_uuid = w.uuid", "w.organization_uuid = $1", "w.uuid = $2", "ak.external_id = $3", "ak.runtime_user_uuid IS NULL", "u.uuid = $4", "u.deleted_at IS NULL", "w.archived_at IS NULL", "ak.status = 'active'"},
	})
}

func TestAPIKeyAuthenticationKeepsRuntimeIdentitySeparate(t *testing.T) {
	row := apiKeyAuthRow{
		UUID: "10000000-0000-4000-8000-000000000001", OrganizationUUID: "10000000-0000-4000-8000-000000000002",
		WorkspaceUUID: "10000000-0000-4000-8000-000000000003", RuntimeUserUUID: "10000000-0000-4000-8000-000000000004",
	}
	key, err := row.apiKey()
	if err != nil || key.RuntimeUserUUID != row.RuntimeUserUUID {
		t.Fatalf("运行身份映射失败：%+v，%v", key, err)
	}
}
