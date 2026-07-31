package db

import (
	"testing"
)

func TestBootstrapQueriesUseNamedTypedUUIDParameters(t *testing.T) {
	query, arguments, err := bindNamed(postgresRebinder{}, `
		select
		o.uuid,
		coalesce(o.settings, CAST('{}' AS jsonb)) as settings
	from organizations o
	where o.uuid = :org_uuid
	limit 1
`, map[string]any{
		"org_uuid": dbUUID("00000000-0000-0000-0000-000000000001"),
	})
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	if len(arguments) != 1 {
		t.Fatalf("bindNamed() arguments len = %d, want 1", len(arguments))
	}
	wantQuery := `
		select
		o.uuid,
		coalesce(o.settings, CAST('{}' AS jsonb)) as settings
	from organizations o
	where o.uuid = $1
	limit 1
`
	if query != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", query, wantQuery)
	}
}
