package db

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFilestoreSessionTokenScopeNamedQuery(t *testing.T) {
	query, arguments, err := bindNamed(
		postgresRebinder{},
		filestoreSessionTokenScopeQuery,
		filestoreSessionTokenScopeArguments("00000000-0000-0000-0000-000000000042", "  session_test  "),
	)
	if err != nil {
		t.Fatalf("bind filestore session token scope query: %v", err)
	}
	if strings.Contains(query, ":workspace_uuid") || strings.Contains(query, ":session_external_id") {
		t.Fatalf("named parameters remain after binding: %q", query)
	}
	if want := []any{
		uuid.MustParse("00000000-0000-0000-0000-000000000042"),
		"session_test",
	}; !reflect.DeepEqual(arguments, want) {
		t.Fatalf("arguments = %#v, want %#v", arguments, want)
	}
}
