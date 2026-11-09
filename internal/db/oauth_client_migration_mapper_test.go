package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestOAuthClientMigrationMapper(t *testing.T) {
	for _, kind := range []string{"platform_oauth_providers", "env_oauth_flows"} {
		for _, after := range []string{"", "cursor"} {
			for _, transitional := range []bool{false, true} {
				for _, expanded := range []bool{false, true} {
					bound := buildOAuthClientMigrationMapperListPage(yourbatis.DialectPostgres, kind, after, transitional, expanded)
					if kind == "env_oauth_flows" && !strings.Contains(bound.SQL, "provider_key NOT IN ('builtin:feishu', 'builtin:friday')") {
						t.Fatal("维护扫描未排除两个内置 flow")
					}
					names := []string{}
					if after != "" {
						names = append(names, "after")
					}
					assertMapperBuilderContract(t, mapperBuilderContract{statement: oAuthClientMigrationMapperListPageStatement, bound: bound,
						wantID: "OAuthClientMigrationMapper.ListPage", wantKind: yourbatis.StatementSelect,
						wantArgumentNames: names, wantSQLFragments: []string{kind, "ORDER BY uuid LIMIT 100"}})
					if strings.Contains(bound.SQL, "client_id AS legacy_client_id") != transitional {
						t.Fatal("旧列读取阶段错误")
					}
					if strings.Contains(bound.SQL, "CAST(NULL AS jsonb) AS client_id_envelope") == expanded {
						t.Fatal("扩展前访问新列")
					}
				}
			}
		}
		bound := buildOAuthClientMigrationMapperBackfill(yourbatis.DialectPostgres, kind, OAuthClientMigrationRecord{}, "sealed")
		names := []string{"envelope", "expected.UUID", "expected.LegacyClientID"}
		sensitive := []string{"envelope", "expected.LegacyClientID"}
		if kind == "platform_oauth_providers" {
			names = append(names, "expected.SecretDocument")
			sensitive = append(sensitive, "expected.SecretDocument")
		} else {
			names = append(names, "expected.OrganizationUUID", "expected.WorkspaceUUID")
		}
		assertMapperBuilderContract(t, mapperBuilderContract{statement: oAuthClientMigrationMapperBackfillStatement, bound: bound,
			wantID: "OAuthClientMigrationMapper.Backfill", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: names, wantSensitiveArgumentNames: sensitive, wantSQLFragments: []string{kind, "client_id_envelope IS NULL", "WHERE uuid ="}})
	}
}

func TestPlatformOAuthProviderIdentityQueries(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{statement: platformOAuthProviderMapperFindByBuiltinKeyStatement,
		bound:  buildPlatformOAuthProviderMapperFindByBuiltinKey(yourbatis.DialectPostgres, "feishu"),
		wantID: "PlatformOAuthProviderMapper.FindByBuiltinKey", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"builtinKey"}, wantSQLFragments: []string{"WHERE builtin_key = $1"}})
	assertMapperBuilderContract(t, mapperBuilderContract{statement: platformOAuthProviderMapperFindByUUIDStatement,
		bound:  buildPlatformOAuthProviderMapperFindByUUID(yourbatis.DialectPostgres, "uuid"),
		wantID: "PlatformOAuthProviderMapper.FindByUUID", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"providerUUID"}, wantSQLFragments: []string{"WHERE uuid = $1"}})
}
