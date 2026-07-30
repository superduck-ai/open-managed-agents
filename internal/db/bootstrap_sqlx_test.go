package db

import (
	"testing"
)

func TestBootstrapQueriesUseNamedParametersAndCasts(t *testing.T) {
	query, arguments, err := bindNamed(postgresRebinder{}, `
		select
		cast(o.uuid as text) as uuid,
		coalesce(o.settings, CAST('{}' AS jsonb)) as settings
	from organizations o
	where o.uuid = CAST(:org_uuid AS uuid)
	limit 1
`, map[string]any{"org_uuid": "00000000-0000-0000-0000-000000000001"})
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	if len(arguments) != 1 {
		t.Fatalf("bindNamed() arguments len = %d, want 1", len(arguments))
	}
	wantQuery := `
		select
		cast(o.uuid as text) as uuid,
		coalesce(o.settings, CAST('{}' AS jsonb)) as settings
	from organizations o
	where o.uuid = CAST($1 AS uuid)
	limit 1
`
	if query != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", query, wantQuery)
	}
}
