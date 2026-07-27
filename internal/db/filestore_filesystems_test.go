package db

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilestoreSessionTokenScopeNamedQuery(t *testing.T) {
	query, arguments, err := bindNamed(
		postgresRebinder{},
		filestoreSessionTokenScopeQuery,
		filestoreSessionTokenScopeArguments(42, "  session_test  "),
	)
	if err != nil {
		t.Fatalf("bind filestore session token scope query: %v", err)
	}
	if strings.Contains(query, ":workspace_id") || strings.Contains(query, ":session_external_id") {
		t.Fatalf("named parameters remain after binding: %q", query)
	}
	if want := []any{int64(42), "session_test"}; !reflect.DeepEqual(arguments, want) {
		t.Fatalf("arguments = %#v, want %#v", arguments, want)
	}
}
