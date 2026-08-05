package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
		&pagePosition{CreatedAt: cursorTime, UUID: cursorUUID},
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
