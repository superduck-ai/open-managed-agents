package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestOrganizationMemberGuardMapperBuilders(t *testing.T) {
	for _, contract := range []mapperBuilderContract{
		{
			statement:         organizationMemberGuardMapperLockOrganizationStatement,
			bound:             buildOrganizationMemberGuardMapperLockOrganization(yourbatis.DialectPostgres, "org"),
			wantID:            "OrganizationMemberGuardMapper.LockOrganization",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID"},
			wantSQLFragments:  []string{"WHERE uuid = $1 FOR UPDATE"},
		},
		{
			statement:         organizationMemberGuardMapperFindRoleStatement,
			bound:             buildOrganizationMemberGuardMapperFindRole(yourbatis.DialectPostgres, "org", "user"),
			wantID:            "OrganizationMemberGuardMapper.FindRole",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "userReference", "userReference", "userReference"},
			wantSQLFragments:  []string{"organization_uuid = $1", "deleted_at IS NULL", "external_id = $2", "CAST(uuid AS text) = $4"},
		},
		{
			statement:         organizationMemberGuardMapperFindMemberByReferenceStatement,
			bound:             buildOrganizationMemberGuardMapperFindMemberByReference(yourbatis.DialectPostgres, "org", "user"),
			wantID:            "OrganizationMemberGuardMapper.FindMemberByReference",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "userReference", "userReference", "userReference"},
			wantSQLFragments:  []string{"organization_uuid = $1", "deleted_at IS NULL", "external_id = $2", "CAST(uuid AS text) = $4"},
		},
		{
			statement:         organizationMemberGuardMapperCountAdminsStatement,
			bound:             buildOrganizationMemberGuardMapperCountAdmins(yourbatis.DialectPostgres, "org"),
			wantID:            "OrganizationMemberGuardMapper.CountAdmins",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID"},
			wantSQLFragments:  []string{"COUNT(*)", "organization_uuid = $1", "deleted_at IS NULL", "role = 'admin'"},
		},
	} {
		t.Run(contract.wantID, func(t *testing.T) { assertMapperBuilderContract(t, contract) })
	}
}
