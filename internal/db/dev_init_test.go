package db

import (
	"github.com/superduck-ai/yourbatis"
	"testing"
)

func TestDevOrganizationInsertBinding(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: adminOrganizationMapperInsertStatement,
		bound:     buildAdminOrganizationMapperInsert(yourbatis.DialectPostgres, "org-uuid", "OMA"),
		wantID:    "AdminOrganizationMapper.Insert", wantKind: yourbatis.StatementInsert,
		wantArgumentNames: []string{"organizationUUID", "name"},
		wantSQLFragments:  []string{"INSERT INTO organizations (uuid, name)", "VALUES ($1, $2)"},
	})
}
