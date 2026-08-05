package db

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestConsoleMemberMapperBuilders(t *testing.T) {
	orgUUID := "11111111-1111-4111-8111-111111111111"
	userUUID := "22222222-2222-4222-8222-222222222222"
	identifier := consoleUserIdentifierParams{OrgUUID: orgUUID, UserID: "user_console", UserUUID: userUUID}
	role := updateConsoleUserRoleParams{OrgUUID: orgUUID, UserID: identifier.UserID, UserUUID: userUUID, Role: "developer"}

	contracts := []mapperBuilderContract{
		{
			statement:         consoleUserMapperExistsActiveByUUIDStatement,
			bound:             buildConsoleUserMapperExistsActiveByUUID(yourbatis.DialectPostgres, orgUUID, userUUID),
			wantID:            "ConsoleUserMapper.ExistsActiveByUUID",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"orgUUID", "userUUID"},
			wantSQLFragments:  []string{"SELECT EXISTS", "organization_uuid = $1", "uuid = $2"},
		},
		{
			statement:         consoleUserMapperListOrganizationMembersStatement,
			bound:             buildConsoleUserMapperListOrganizationMembers(yourbatis.DialectPostgres, orgUUID, 100),
			wantID:            "ConsoleUserMapper.ListOrganizationMembers",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"orgUUID", "limit"},
			wantSQLFragments:  []string{"FROM users u", "LIMIT $2"},
		},
		{
			statement:         consoleUserMapperUpdateOrganizationRoleStatement,
			bound:             buildConsoleUserMapperUpdateOrganizationRole(yourbatis.DialectPostgres, role),
			wantID:            "ConsoleUserMapper.UpdateOrganizationRole",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.Role", "params.OrgUUID", "params.UserID", "params.UserID", "params.UserUUID"},
			wantSQLFragments:  []string{"UPDATE users u", "OR u.uuid = $5", "RETURNING"},
		},
		{
			statement:         consoleUserMapperSoftDeleteOrganizationMemberStatement,
			bound:             buildConsoleUserMapperSoftDeleteOrganizationMember(yourbatis.DialectPostgres, identifier),
			wantID:            "ConsoleUserMapper.SoftDeleteOrganizationMember",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.OrgUUID", "params.UserID", "params.UserID", "params.UserUUID"},
			wantSQLFragments:  []string{"UPDATE users u", "OR u.uuid = $4"},
		},
		{
			statement:         consoleUserMapperFindBootstrapContextStatement,
			bound:             buildConsoleUserMapperFindBootstrapContext(yourbatis.DialectPostgres, orgUUID),
			wantID:            "ConsoleUserMapper.FindBootstrapContext",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"preferredOrgUUID"},
			wantSQLFragments:  []string{"u.organization_uuid = $1", "LIMIT 1"},
		},
		{
			statement:         consoleUserMapperFindBootstrapUserStatement,
			bound:             buildConsoleUserMapperFindBootstrapUser(yourbatis.DialectPostgres, identifier.UserID, userUUID),
			wantID:            "ConsoleUserMapper.FindBootstrapUser",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"userExternalID", "userExternalID", "userUUID"},
			wantSQLFragments:  []string{"u.external_id = $1", "OR u.uuid = $3"},
		},
		{
			statement:         consoleUserMapperListBootstrapOrganizationsStatement,
			bound:             buildConsoleUserMapperListBootstrapOrganizations(yourbatis.DialectPostgres, identifier.UserID, userUUID, orgUUID),
			wantID:            "ConsoleUserMapper.ListBootstrapOrganizations",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"userExternalID", "userExternalID", "userUUID", "preferredOrgUUID"},
			wantSQLFragments:  []string{"JOIN organizations o", "OR u.uuid = $3", "CASE WHEN o.uuid = $4"},
		},
		{
			statement:         consoleWorkspaceMemberMapperSoftDeleteByOrganizationUserStatement,
			bound:             buildConsoleWorkspaceMemberMapperSoftDeleteByOrganizationUser(yourbatis.DialectPostgres, identifier),
			wantID:            "ConsoleWorkspaceMemberMapper.SoftDeleteByOrganizationUser",
			wantKind:          yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.OrgUUID", "params.UserID", "params.UserID", "params.UserUUID", "params.OrgUUID"},
			wantSQLFragments:  []string{"WITH target_user", "UPDATE workspace_members wm", "wm.organization_uuid = $5"},
		},
	}

	for _, contract := range contracts {
		t.Run(contract.wantID, func(t *testing.T) {
			assertMapperBuilderContract(t, contract)
		})
	}
}

func TestConsoleMemberMapperOptionalUUIDBindings(t *testing.T) {
	orgUUID := "11111111-1111-4111-8111-111111111111"
	identifier := consoleUserIdentifierParams{OrgUUID: orgUUID, UserID: "user_console"}

	for name, bound := range map[string]yourbatis.BoundSQL{
		"update":        buildConsoleUserMapperUpdateOrganizationRole(yourbatis.DialectPostgres, updateConsoleUserRoleParams{OrgUUID: orgUUID, UserID: identifier.UserID}),
		"delete user":   buildConsoleUserMapperSoftDeleteOrganizationMember(yourbatis.DialectPostgres, identifier),
		"delete member": buildConsoleWorkspaceMemberMapperSoftDeleteByOrganizationUser(yourbatis.DialectPostgres, identifier),
		"find user":     buildConsoleUserMapperFindBootstrapUser(yourbatis.DialectPostgres, identifier.UserID, ""),
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(bound.SQL, "OR u.uuid =") || strings.Contains(bound.SQL, "OR uuid =") {
				t.Fatalf("generated SQL binds an empty UUID: %q", bound.SQL)
			}
		})
	}

	bound := buildConsoleUserMapperListBootstrapOrganizations(yourbatis.DialectPostgres, identifier.UserID, "", "")
	if strings.Contains(bound.SQL, "CASE WHEN o.uuid") || !reflect.DeepEqual(bound.Values(), []any{identifier.UserID, identifier.UserID}) {
		t.Fatalf("optional bootstrap bindings = (%q, %#v)", bound.SQL, bound.Values())
	}
}

func TestConsoleMemberMapperExecution(t *testing.T) {
	addedAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{"user_uuid", "email", "full_name", "role", "added_at"},
		rows: [][]driver.Value{{
			"22222222-2222-4222-8222-222222222222",
			"console@example.com",
			"Console User",
			"developer",
			addedAt,
		}},
	})

	rows, err := NewConsoleUserMapper(executor).ListOrganizationMembers(
		context.Background(),
		"11111111-1111-4111-8111-111111111111",
		100,
	)
	if err != nil || len(rows) != 1 || rows[0].UserUUID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("ListOrganizationMembers() = (%+v, %v)", rows, err)
	}

	rowsExecutor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
	rowsAffected, err := NewConsoleUserMapper(rowsExecutor).SoftDeleteOrganizationMember(
		context.Background(),
		consoleUserIdentifierParams{OrgUUID: "org", UserID: "user"},
	)
	if err != nil || rowsAffected != 1 {
		t.Fatalf("SoftDeleteOrganizationMember() = (%d, %v)", rowsAffected, err)
	}

	execExecutor := newMapperTestExecutor(t, mapperTestResponse{})
	if err := NewConsoleWorkspaceMemberMapper(execExecutor).SoftDeleteByOrganizationUser(
		context.Background(),
		consoleUserIdentifierParams{OrgUUID: "org", UserID: "user"},
	); err != nil {
		t.Fatalf("SoftDeleteByOrganizationUser() error = %v", err)
	}
}
