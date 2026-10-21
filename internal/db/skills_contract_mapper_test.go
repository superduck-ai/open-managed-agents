package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSkillContractMapperBranches(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: skillMapperLockWorkspaceStatement,
		bound:     buildSkillMapperLockWorkspace(yourbatis.DialectPostgres, "workspace"),
		wantID:    "SkillMapper.LockWorkspace", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames: []string{"workspaceUUID"},
		wantSQLFragments:  []string{"pg_advisory_xact_lock", "hashtextextended", "CAST($1 AS text)"},
	})
	for _, stable := range []bool{false, true} {
		bound := buildSkillMapperListPage(yourbatis.DialectPostgres, "workspace", 21, 0, stable)
		if strings.Contains(bound.SQL, "latest_version IS NOT NULL") != stable {
			t.Fatalf("stable=%t SQL=%s", stable, bound.SQL)
		}
		for _, item := range []struct {
			bound yourbatis.BoundSQL
			order string
		}{
			{buildSkillVersionMapperListPageBySkillUUID(yourbatis.DialectPostgres, "workspace", "skill", 21, 0, !stable), "uuid"},
			{buildBuiltinSkillMapperListVersionsPage(yourbatis.DialectPostgres, 1, 21, 0, !stable), "id"},
		} {
			direction := "ASC"
			if stable {
				direction = "DESC"
			}
			normalized := strings.Join(strings.Fields(item.bound.SQL), " ")
			if !strings.Contains(normalized, "ORDER BY created_at "+direction+", "+item.order+" "+direction) {
				t.Fatalf("stable=%t SQL=%s", stable, normalized)
			}
		}
	}
}
