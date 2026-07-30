package db

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAppendAdminCursorFilterBindsNamedParameters(t *testing.T) {
	cursorTime := time.Date(2026, time.July, 23, 10, 30, 0, 0, time.UTC)
	arguments := map[string]any{
		"organization_uuid": "22222222-2222-4222-8222-222222222222",
		"limit":             11,
	}
	query := appendCursorFilter(
		adminAPIKeySelectSQL()+` where w.organization_uuid = CAST(:organization_uuid AS uuid)`,
		arguments,
		"ak.created_at",
		"apikey_after",
		"",
		&AdminCursor{CreatedAt: cursorTime, ID: 99},
	)
	query += " order by ak.created_at desc, ak.id desc limit :limit"

	boundQuery, values, err := bindNamed(postgresRebinder{}, query, arguments)
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	wantQuery := adminAPIKeySelectSQL() + ` where w.organization_uuid = CAST($1 AS uuid)` +
		" and (ak.created_at < $2 or (ak.created_at = $3 and ak.id < $4))" +
		" order by ak.created_at desc, ak.id desc limit $5"
	if boundQuery != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", boundQuery, wantQuery)
	}
	wantValues := []any{"22222222-2222-4222-8222-222222222222", cursorTime, cursorTime, int64(99), 11}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("bindNamed() values = %#v, want %#v", values, wantValues)
	}
}

func TestAppendAdminCursorFilterBuildsBeforeCondition(t *testing.T) {
	cursorTime := time.Date(2026, time.July, 23, 11, 0, 0, 0, time.UTC)
	arguments := map[string]any{}
	query := appendCursorFilter(
		"select id from users where organization_id = :organization_id",
		arguments,
		"added_at",
		"",
		"user_before",
		&AdminCursor{CreatedAt: cursorTime, ID: 7},
	)
	arguments["organization_id"] = int64(9)

	boundQuery, values, err := bindNamed(postgresRebinder{}, query, arguments)
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	wantQuery := "select id from users where organization_id = $1" +
		" and (added_at > $2 or (added_at = $3 and id > $4))"
	if boundQuery != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", boundQuery, wantQuery)
	}
	wantValues := []any{int64(9), cursorTime, cursorTime, int64(7)}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("bindNamed() values = %#v, want %#v", values, wantValues)
	}
}

func TestGetAdminOrganizationQueryUsesUUID(t *testing.T) {
	organizationUUID := "22222222-2222-4222-8222-222222222222"
	query, arguments, err := bindNamed(postgresRebinder{}, getAdminOrganizationQuery, map[string]any{
		"organization_uuid": organizationUUID,
	})
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	if len(arguments) != 1 || arguments[0] != organizationUUID {
		t.Fatalf("bindNamed() arguments = %#v, want organization UUID", arguments)
	}
	if want := "where uuid = CAST($1 AS uuid)"; !strings.Contains(query, want) {
		t.Fatalf("bound query = %q, want %q", query, want)
	}
}
