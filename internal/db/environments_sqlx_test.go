package db

import (
	"strings"
	"testing"
	"time"
)

func TestEnvironmentQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	environmentQuery, environmentArguments := listEnvironmentsQuery(ListEnvironmentsPageParams{
		WorkspaceUUID: "00000000-0000-0000-0000-000000000042",
		Limit:         20,
		Cursor:        &EnvironmentPageCursor{CreatedAt: now, UUID: "00000000-0000-0000-0000-000000000007"},
	})
	workQuery, workArguments := listEnvironmentWorkQuery(ListEnvironmentWorkPageParams{
		WorkspaceUUID:         "00000000-0000-0000-0000-000000000042",
		EnvironmentExternalID: "env_test",
		Limit:                 20,
		Cursor:                &EnvironmentWorkPageCursor{CreatedAt: now, UUID: "00000000-0000-0000-0000-000000000008"},
	})

	t.Run("rejects a missing named argument", func(t *testing.T) {
		incompleteArguments := make(map[string]any, len(environmentArguments)-1)
		for name, value := range environmentArguments {
			if name != "cursor_uuid" {
				incompleteArguments[name] = value
			}
		}
		if _, _, err := bindNamed(postgresRebinder{}, environmentQuery, incompleteArguments); err == nil {
			t.Fatal("bindNamed() error = nil, want missing argument error")
		}
	})

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "list environments",
			query:        environmentQuery,
			arguments:    environmentArguments,
			wantArgCount: 5,
		},
		{
			name:         "get environment",
			query:        environmentSelectSQL() + ` where workspace_uuid = :workspace_uuid and external_id = :external_id`,
			arguments:    environmentLookupArguments("00000000-0000-0000-0000-000000000042", "env_test"),
			wantArgCount: 2,
		},
		{
			name:         "list environment work",
			query:        workQuery,
			arguments:    workArguments,
			wantArgCount: 6,
		},
		{
			name:         "get environment work",
			query:        environmentWorkSelectSQL() + ` where workspace_uuid = :workspace_uuid and environment_external_id = :environment_external_id and external_id = :work_external_id`,
			arguments:    environmentWorkLookupArguments("00000000-0000-0000-0000-000000000042", "env_test", "work_test"),
			wantArgCount: 3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains named parameter syntax: %q", query)
			}
			if strings.Contains(test.query, "::") {
				t.Fatalf("query uses PostgreSQL cast shorthand: %q", test.query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}

func TestEnvironmentArgumentsPreserveJSONBoundaries(t *testing.T) {
	env := Environment{
		Config:   []byte(`{"provider":"test"}`),
		Metadata: []byte(`{"team":"platform"}`),
	}
	arguments := environmentArguments(env)

	if got := string(arguments["config"].([]byte)); got != string(env.Config) {
		t.Fatalf("config argument = %q, want %q", got, env.Config)
	}
	if got := string(arguments["metadata"].([]byte)); got != string(env.Metadata) {
		t.Fatalf("metadata argument = %q, want %q", got, env.Metadata)
	}
}
