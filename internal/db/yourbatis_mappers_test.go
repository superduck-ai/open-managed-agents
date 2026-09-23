package db

import (
	"strings"
	"testing"

	yourbatis "github.com/superduck-ai/yourbatis"
)

func TestTableMappersBuildDynamicQueries(t *testing.T) {
	workspaceUUID := "22222222-2222-4222-8222-222222222222"
	codeSessionUUID := "33333333-3333-4333-8333-333333333333"

	t.Run("active code session direct publish lock", func(t *testing.T) {
		lockBound := buildCodeSessionMapperLockCodeSessionByExternalID(
			yourbatis.DialectPostgres,
			"cse_test",
		)
		assertMapperSQLContains(t, lockBound, "WHERE external_id = $1 AND deleted_at IS NULL FOR UPDATE")
		if strings.Contains(lockBound.SQL, "status = 'initializing'") {
			t.Fatalf("direct publish lock unexpectedly restricts status in SQL: %s", lockBound.SQL)
		}
	})

	t.Run("activation code session lock", func(t *testing.T) {
		bound := buildCodeSessionMapperLockInitializingCodeSession(
			yourbatis.DialectPostgres,
			workspaceUUID,
			codeSessionUUID,
		)
		assertMapperSQLContains(t, bound, "WHERE workspace_uuid = $1 AND uuid = $2")
	})
}

func assertMapperSQLContains(
	t *testing.T,
	bound yourbatis.BoundSQL,
	want string,
) {
	t.Helper()
	compact := strings.Join(strings.Fields(bound.SQL), " ")
	if !strings.Contains(compact, want) {
		t.Fatalf("SQL does not contain %q:\n%s", want, compact)
	}
}

func assertMapperArgumentNames(
	t *testing.T,
	bound yourbatis.BoundSQL,
	want []string,
) {
	t.Helper()
	got := make([]string, len(bound.Args))
	for index, argument := range bound.Args {
		got[index] = argument.Name
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("argument names = %v, want %v", got, want)
	}
}
