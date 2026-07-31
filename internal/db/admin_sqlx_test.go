package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAppendAdminCursorFilterBindsNamedParameters(t *testing.T) {
	cursorTime := time.Date(2026, time.July, 23, 10, 30, 0, 0, time.UTC)
	organizationUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	cursorUUID := uuid.MustParse("99999999-9999-4999-8999-999999999999")
	arguments := map[string]any{
		"organization_uuid": organizationUUID,
		"limit":             11,
	}
	query := appendCursorFilter(
		adminAPIKeySelectSQL()+` where w.organization_uuid = :organization_uuid`,
		arguments,
		"ak.created_at",
		"apikey_after",
		"",
		&AdminCursor{CreatedAt: cursorTime, UUID: cursorUUID},
	)
	query += " order by ak.created_at desc, ak.uuid desc limit :limit"

	boundQuery, values, err := bindNamed(postgresRebinder{}, query, arguments)
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	wantQuery := adminAPIKeySelectSQL() + ` where w.organization_uuid = $1` +
		" and (ak.created_at < $2 or (ak.created_at = $3 and ak.uuid < $4))" +
		" order by ak.created_at desc, ak.uuid desc limit $5"
	if boundQuery != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", boundQuery, wantQuery)
	}
	wantValues := []any{
		organizationUUID,
		cursorTime,
		cursorTime,
		cursorUUID,
		11,
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("bindNamed() values = %#v, want %#v", values, wantValues)
	}
}

func TestAppendAdminCursorFilterBuildsBeforeCondition(t *testing.T) {
	cursorTime := time.Date(2026, time.July, 23, 11, 0, 0, 0, time.UTC)
	organizationUUID := uuid.MustParse("99999999-9999-4999-8999-999999999999")
	cursorUUID := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	arguments := map[string]any{}
	query := appendCursorFilter(
		"select uuid from users where organization_uuid = :organization_uuid",
		arguments,
		"added_at",
		"",
		"user_before",
		&AdminCursor{CreatedAt: cursorTime, UUID: cursorUUID},
	)
	arguments["organization_uuid"] = organizationUUID

	boundQuery, values, err := bindNamed(postgresRebinder{}, query, arguments)
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	wantQuery := "select uuid from users where organization_uuid = $1" +
		" and (added_at > $2 or (added_at = $3 and uuid > $4))"
	if boundQuery != wantQuery {
		t.Fatalf("bindNamed() query = %q, want %q", boundQuery, wantQuery)
	}
	wantValues := []any{
		organizationUUID,
		cursorTime,
		cursorTime,
		cursorUUID,
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("bindNamed() values = %#v, want %#v", values, wantValues)
	}
}

func TestGetAdminOrganizationQueryUsesUUID(t *testing.T) {
	organizationUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	query, arguments, err := bindNamed(postgresRebinder{}, getAdminOrganizationQuery, map[string]any{
		"organization_uuid": organizationUUID,
	})
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	if len(arguments) != 1 || arguments[0] != organizationUUID {
		t.Fatalf("bindNamed() arguments = %#v, want organization UUID", arguments)
	}
	if want := "where uuid = $1"; !strings.Contains(query, want) {
		t.Fatalf("bound query = %q, want %q", query, want)
	}
	if strings.Contains(query, "CAST(") {
		t.Fatalf("bound query contains UUID cast ceremony: %q", query)
	}
}
