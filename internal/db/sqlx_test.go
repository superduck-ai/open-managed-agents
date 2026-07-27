package db

import (
	"reflect"
	"testing"

	"github.com/jmoiron/sqlx"
)

type postgresRebinder struct{}

func (postgresRebinder) Rebind(query string) string {
	return sqlx.Rebind(sqlx.DOLLAR, query)
}

func TestBindNamedUsesPostgreSQLPlaceholders(t *testing.T) {
	query, arguments, err := bindNamed(postgresRebinder{}, `
		update filestore_entries
		set metadata = CAST(:metadata AS jsonb), updated_at = :now
		where workspace_uuid = :workspace_uuid and updated_at < :now
	`, map[string]any{
		"metadata":       `{"source":"sqlx"}`,
		"now":            "2026-07-23T00:00:00Z",
		"workspace_uuid": "workspace-uuid",
	})
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	wantArguments := []any{
		`{"source":"sqlx"}`,
		"2026-07-23T00:00:00Z",
		"workspace-uuid",
		"2026-07-23T00:00:00Z",
	}
	if !reflect.DeepEqual(arguments, wantArguments) {
		t.Fatalf("bindNamed() arguments = %#v, want %#v", arguments, wantArguments)
	}
	wantQuery := `
		update filestore_entries
		set metadata = CAST($1 AS jsonb), updated_at = $2
		where workspace_uuid = $3 and updated_at < $4
	`
	if query != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", query, wantQuery)
	}
}
