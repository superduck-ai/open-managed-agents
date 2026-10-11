package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestOrganizationMemberMapperBuilders(t *testing.T) {
	for _, contract := range []mapperBuilderContract{
		{
			statement:         organizationMemberMapperLockOrganizationStatement,
			bound:             buildOrganizationMemberMapperLockOrganization(yourbatis.DialectPostgres, "org"),
			wantID:            "OrganizationMemberMapper.LockOrganization",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID"},
			wantSQLFragments:  []string{"WHERE uuid = $1 FOR UPDATE"},
		},
		{
			statement:         organizationMemberMapperFindRoleStatement,
			bound:             buildOrganizationMemberMapperFindRole(yourbatis.DialectPostgres, "org", "user"),
			wantID:            "OrganizationMemberMapper.FindRole",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "userReference", "userReference", "userReference"},
			wantSQLFragments:  []string{"organization_uuid = $1", "deleted_at IS NULL", "external_id = $2", "CAST(uuid AS text) = $4"},
		},
		{
			statement:         organizationMemberMapperFindMemberByReferenceStatement,
			bound:             buildOrganizationMemberMapperFindMemberByReference(yourbatis.DialectPostgres, "org", "user"),
			wantID:            "OrganizationMemberMapper.FindMemberByReference",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "userReference", "userReference", "userReference"},
			wantSQLFragments:  []string{"organization_uuid = $1", "deleted_at IS NULL", "external_id = $2", "CAST(uuid AS text) = $4"},
		},
		{
			statement:         organizationMemberMapperCountAdminsStatement,
			bound:             buildOrganizationMemberMapperCountAdmins(yourbatis.DialectPostgres, "org"),
			wantID:            "OrganizationMemberMapper.CountAdmins",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID"},
			wantSQLFragments:  []string{"COUNT(*)", "organization_uuid = $1", "deleted_at IS NULL", "role = 'admin'"},
		},
	} {
		t.Run(contract.wantID, func(t *testing.T) { assertMapperBuilderContract(t, contract) })
	}
}
