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
		where cast(o.uuid as text) = :org_uuid
		limit 1
	`, map[string]any{"org_uuid": "org_123"})
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
		where cast(o.uuid as text) = $1
		limit 1
	`
	if query != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", query, wantQuery)
	}
}
