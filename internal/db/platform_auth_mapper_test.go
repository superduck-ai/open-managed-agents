package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformAuthMapperBuilders(t *testing.T) {
	userUUID := "22222222-2222-4222-8222-222222222222"
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: platformAuthUserMapperResolveSessionIdentityStatement,
		bound: buildPlatformAuthUserMapperResolveSessionIdentity(
			yourbatis.DialectPostgres,
			"11111111-1111-4111-8111-111111111111",
			"user_test",
			&userUUID,
		),
		wantID:   "PlatformAuthUserMapper.ResolveSessionIdentity",
		wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{
			"organizationUUID", "userID", "userUUID", "userID",
		},
		wantSQLFragments: []string{
			"u.organization_uuid = $1", "u.external_id = $2", "OR u.uuid = $3",
		},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: platformAuthUserMapperResolveSessionIdentityStatement,
		bound: buildPlatformAuthUserMapperResolveSessionIdentity(
			yourbatis.DialectPostgres, "11111111-1111-4111-8111-111111111111", "user_test", nil,
		),
		wantID:            "PlatformAuthUserMapper.ResolveSessionIdentity",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "userID", "userID"},
		wantSQLFragments: []string{
			"u.organization_uuid = $1", "u.external_id = $2",
		},
	})

	params := insertPlatformAuthAPIKeyParams{
		ExternalID: "api_key_test", WorkspaceUUID: "workspace-uuid", KeyHash: "hash",
		Status: "active", CreatedByUserUUID: "user-uuid", Name: "default", PartialKeyHint: "sk-ant...test",
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: platformAuthAPIKeyMapperInsertStatement,
		bound:     buildPlatformAuthAPIKeyMapperInsert(yourbatis.DialectPostgres, params),
		wantID:    "PlatformAuthAPIKeyMapper.Insert",
		wantKind:  yourbatis.StatementInsert,
		wantArgumentNames: []string{
			"params.ExternalID", "params.WorkspaceUUID", "params.KeyHash", "params.Status",
			"params.CreatedByUserUUID", "params.Name", "params.PartialKeyHint",
		},
		wantSensitiveArgumentNames: []string{"params.KeyHash", "params.PartialKeyHint"},
		wantSQLFragments:           []string{"INSERT INTO api_keys", "workspace_uuid"},
	})
}

func TestPlatformSessionIdentityRowMapsStringUUIDs(t *testing.T) {
	row := platformSessionIdentityRow{
		OrganizationUUID:    "11111111-1111-4111-8111-111111111111",
		WorkspaceUUID:       "22222222-2222-4222-8222-222222222222",
		WorkspaceExternalID: "workspace_test",
		UserUUID:            "33333333-3333-4333-8333-333333333333",
		UserExternalID:      "user_test",
	}

	session := row.session()
	if session.OrganizationUUID != row.OrganizationUUID || session.WorkspaceUUID != row.WorkspaceUUID ||
		session.UserUUID != row.UserUUID || session.APIKeyUUID != "" {
		t.Fatalf("session = %#v, want values from row %#v", session, row)
	}
}
