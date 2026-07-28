package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestEnvironmentQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	environmentQuery, environmentArguments := listEnvironmentsQuery(ListEnvironmentsPageParams{
		WorkspaceID: 42,
		Limit:       20,
		Cursor:      &EnvironmentPageCursor{CreatedAt: now, ID: 7},
	})
	workQuery, workArguments := listEnvironmentWorkQuery(ListEnvironmentWorkPageParams{
		WorkspaceID:           42,
		EnvironmentExternalID: "env_test",
		Limit:                 20,
		Cursor:                &EnvironmentWorkPageCursor{CreatedAt: now, ID: 8},
	})

	t.Run("rejects a missing named argument", func(t *testing.T) {
		incompleteArguments := make(map[string]any, len(environmentArguments)-1)
		for name, value := range environmentArguments {
			if name != "cursor_id" {
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
			query:        environmentSelectSQL() + ` where workspace_id = :workspace_id and external_id = :external_id`,
			arguments:    environmentLookupArguments(42, "env_test"),
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
			query:        environmentWorkSelectSQL() + ` where workspace_id = :workspace_id and environment_external_id = :environment_external_id and external_id = :work_external_id`,
			arguments:    environmentWorkLookupArguments(42, "env_test", "work_test"),
			wantArgCount: 3,
		},
		{
			name:         "merge environment work metadata",
			query:        mergeEnvironmentWorkMetadataQuery,
			arguments:    mergeEnvironmentWorkMetadataTestArguments(),
			wantArgCount: 4,
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

func mergeEnvironmentWorkMetadataTestArguments() map[string]any {
	arguments := environmentWorkLookupArguments(42, "env_test", "work_test")
	arguments["metadata_patch"] = jsonArg(json.RawMessage(`{"provider_sandbox_id":"sbx_test"}`))
	return arguments
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

func TestPackagedEnvironmentTemplateMigrationAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires config: %v", err)
	}
	database, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	tx, err := database.sql.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		create temporary table environments (
			external_id text primary key,
			resolved_template text not null,
			config jsonb not null,
			updated_at timestamptz not null default now()
		) on commit drop;
		insert into environments (external_id, resolved_template, config) values
			('migrate', 'claude-code-interpreter', '{"type":"cloud","packages":{"type":"packages","pip":["numpy"]}}'),
			('empty', 'claude-code-interpreter', '{"type":"cloud","packages":{"type":"packages","pip":[]}}'),
			('self_hosted', 'claude-code-interpreter', '{"type":"self_hosted","packages":{"type":"packages","pip":["numpy"]}}'),
			('custom', 'custom-template', '{"type":"cloud","packages":{"type":"packages","pip":["numpy"]}}'),
			('current', 'managed-agent-sandbox', '{"type":"cloud","packages":{"type":"packages","pip":["numpy"]}}');
	`); err != nil {
		t.Fatalf("seed migration environments: %v", err)
	}

	migration, err := embeddedMigrations.ReadFile("migrations/00035_migrate_packaged_environment_template.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	upSQL := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
	if _, err := tx.ExecContext(ctx, upSQL); err != nil {
		t.Fatalf("apply packaged Environment migration: %v", err)
	}

	rows := []struct {
		ExternalID       string `db:"external_id"`
		ResolvedTemplate string `db:"resolved_template"`
	}{}
	if err := tx.SelectContext(ctx, &rows, `select external_id, resolved_template from environments order by external_id`); err != nil {
		t.Fatalf("read migrated environments: %v", err)
	}
	want := map[string]string{
		"migrate":     "managed-agent-sandbox",
		"empty":       "claude-code-interpreter",
		"self_hosted": "claude-code-interpreter",
		"custom":      "custom-template",
		"current":     "managed-agent-sandbox",
	}
	for _, row := range rows {
		if row.ResolvedTemplate != want[row.ExternalID] {
			t.Fatalf("Environment %q template = %q, want %q", row.ExternalID, row.ResolvedTemplate, want[row.ExternalID])
		}
	}
}
