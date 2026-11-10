package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSkillCatalogBindings(t *testing.T) {
	for _, source := range []string{"", "custom", "anthropic"} {
		for _, sort := range []string{"", "name", "updated"} {
			query := SkillCatalogQuery{WorkspaceUUID: "workspace", Query: "%_ report", Source: source, Sort: sort, Limit: 12, Offset: 24}
			bound := buildSkillCatalogMapperList(yourbatis.DialectPostgres, query)
			if strings.Contains(bound.SQL, "%_ report") {
				t.Fatal("query interpolated into SQL")
			}
			for _, fragment := range []string{"v.workspace_uuid = $1", "s.workspace_uuid = $2", "s.listed", "v.state = 'published'", "s.deleted_at IS NULL", "LIMIT", "OFFSET"} {
				if !strings.Contains(bound.SQL, fragment) {
					t.Fatalf("missing %s: %s", fragment, bound.SQL)
				}
			}
			if bound.Args[0].Value != "workspace" || bound.Args[1].Value != "workspace" {
				t.Fatal("workspace scope not bound")
			}
			if bound.Args[len(bound.Args)-2].Value != 12 || bound.Args[len(bound.Args)-1].Value != 24 {
				t.Fatal("pagination not bound")
			}
			counts := buildSkillCatalogMapperCounts(yourbatis.DialectPostgres, query)
			if len(counts.Args) != 3 {
				t.Fatal("source hides other tab counts")
			}
		}
	}
	bound := buildSkillMapperUpdateIcon(yourbatis.DialectPostgres, "workspace", "skill_demo", "file-text")
	for _, fragment := range []string{"workspace_uuid = $2", "external_id = $3", "source = 'custom'", "deleted_at IS NULL", "RETURNING"} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("missing icon update scope %s", fragment)
		}
	}
}
