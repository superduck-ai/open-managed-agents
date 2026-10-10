package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSecretMigrationMapper(t *testing.T) {
	for _, kind := range SecretMigrationKinds() {
		t.Run(kind, func(t *testing.T) {
			for _, after := range []string{"", "00000000-0000-0000-0000-000000000001"} {
				bound := buildSecretMigrationMapperListPage(yourbatis.DialectPostgres, kind, after, 100)
				args := []string{"limit"}
				if after != "" {
					args = []string{"after", "limit"}
				}
				assertMapperBuilderContract(t, mapperBuilderContract{
					statement: secretMigrationMapperListPageStatement, bound: bound, wantID: "SecretMigrationMapper.ListPage", wantKind: yourbatis.StatementSelect,
					wantArgumentNames: args, wantSQLFragments: []string{kind, "ORDER BY records.uuid", "LIMIT"},
				})
			}
			bound := buildSecretMigrationMapperReplace(yourbatis.DialectPostgres, kind, SecretMigrationRecord{}, nil)
			if !strings.Contains(bound.SQL, "organization_uuid") || !strings.Contains(bound.SQL, "workspace_uuid") || !strings.Contains(bound.SQL, "m.uuid =") {
				t.Fatal("update lost tenant scope")
			}
			sensitive := 0
			for _, arg := range bound.Args {
				if arg.Name == "next" || arg.Name == "expected.Document" {
					sensitive++
					if !arg.Sensitive {
						t.Fatal("envelope argument is not sensitive")
					}
				}
			}
			if sensitive < 2 {
				t.Fatal("missing envelope compare-and-swap bindings")
			}
			if kind == "vault_credentials" && !strings.Contains(bound.SQL, "version = version + 1") {
				t.Fatal("OAuth CAS version not incremented")
			}
		})
	}
}
