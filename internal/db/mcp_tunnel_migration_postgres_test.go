package db

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"strings"
	"testing"
)

const tunnelMigrationFixtureSQL = `
	insert into organizations (uuid, name)
	values ('53000000-0000-0000-0000-000000000001', 'Tunnel migration');

	insert into workspaces (uuid, external_id, organization_uuid, name)
	values (
		'53000000-0000-0000-0000-000000000002', 'workspace_tunnel_migration',
		'53000000-0000-0000-0000-000000000001', 'Tunnel migration'
	);

	insert into mcp_tunnels (
		uuid, external_id, organization_uuid, workspace_uuid, workspace_external_id,
		display_name, domain, token_id, tunnel_token
	) values (
		'53000000-0000-0000-0000-000000000003', 'tnl_legacy',
		'53000000-0000-0000-0000-000000000001', '53000000-0000-0000-0000-000000000002',
		'workspace_tunnel_migration', 'Legacy tunnel', 'legacy.tunnel.invalid',
		'ttkn_legacy', 'legacy-plaintext-token'
	);

	insert into mcp_tunnel_certificates (
		uuid, external_id, organization_uuid, tunnel_uuid, tunnel_external_id,
		ca_certificate_pem, fingerprint
	) values (
		'53000000-0000-0000-0000-000000000004', 'tcrt_legacy',
		'53000000-0000-0000-0000-000000000001', '53000000-0000-0000-0000-000000000003',
		'tnl_legacy', 'legacy certificate', 'legacy-fingerprint'
	);
`

func TestMCPTunnelMigrationFollowsCurrentMain(t *testing.T) {
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("embedded migration directory is empty")
	}
	var previousFound, tunnelFound bool
	for _, entry := range entries {
		switch entry.Name() {
		case "00058_simplify_sandbox_reclamation.sql":
			previousFound = true
		case "00059_rebuild_mcp_tunnels.sql":
			tunnelFound = true
			if !previousFound {
				t.Fatal("MCP Tunnel migration must follow upstream migration 58")
			}
		}
	}
	if !tunnelFound {
		t.Fatal("embedded migrations do not include 00059_rebuild_mcp_tunnels.sql")
	}
}

func TestRebuildMCPTunnelsMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}

	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 58); err != nil {
		t.Fatalf("migrate fixture database to 58: %v", err)
	}
	if _, err := database.ExecContext(ctx, tunnelMigrationFixtureSQL); err != nil {
		t.Fatalf("seed legacy Tunnel fixture: %v", err)
	}
	if _, err := provider.UpTo(ctx, 59); err != nil {
		t.Fatalf("rebuild MCP Tunnels at migration 59: %v", err)
	}

	assertTunnelMigration59State(t, ctx, database)
	assertTunnelIDConstraint(t, ctx, database)

	if _, err := provider.Down(ctx); err != nil {
		t.Fatalf("roll back MCP Tunnel migration 59: %v", err)
	}
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "token_id", true)
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "tunnel_token", true)
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "workspace_external_id", true)
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "workspace_uuid", true)
	assertMigrationTableExists(t, ctx, database, "mcp_tunnel_token_versions", false)
	assertMigrationColumnNullable(t, ctx, database, "mcp_tunnels", "workspace_uuid", "YES")
	assertTableRowCount(t, ctx, database, "mcp_tunnels", 0)
	assertTableRowCount(t, ctx, database, "mcp_tunnel_certificates", 0)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("reapply MCP Tunnel migration 59: %v", err)
	}
	assertTunnelMigration59State(t, ctx, database)
}

func assertTunnelMigration59State(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, tableName := range []string{"mcp_tunnels", "mcp_tunnel_token_versions", "mcp_tunnel_certificates"} {
		assertTableRowCount(t, ctx, database, tableName, 0)
	}
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "token_id", false)
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "tunnel_token", false)
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "workspace_external_id", false)
	assertMigrationColumnExists(t, ctx, database, "mcp_tunnels", "workspace_uuid", true)

	assertMigrationColumnNullable(t, ctx, database, "mcp_tunnels", "workspace_uuid", "NO")

	var constraintDefinition string
	if err := database.QueryRowContext(ctx, `
		select pg_get_constraintdef(oid)
		from pg_constraint
		where connamespace = current_schema()::regnamespace
			and conname = 'mcp_tunnels_external_id_format_check'
	`).Scan(&constraintDefinition); err != nil {
		t.Fatalf("load MCP Tunnel external ID constraint: %v", err)
	}
	if !strings.Contains(constraintDefinition, "^tunnel_[0-9a-f]{32}$") {
		t.Fatalf("MCP Tunnel external ID constraint = %q", constraintDefinition)
	}

	var foreignKeyCount int
	if err := database.QueryRowContext(ctx, `
		select count(*)
		from information_schema.table_constraints
		where constraint_schema = current_schema()
			and table_name in ('mcp_tunnels', 'mcp_tunnel_token_versions', 'mcp_tunnel_certificates')
			and constraint_type = 'FOREIGN KEY'
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("count MCP Tunnel foreign keys: %v", err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("MCP Tunnel foreign key count = %d, want 0", foreignKeyCount)
	}

	var activeIndexDefinition string
	if err := database.QueryRowContext(ctx, `
		select indexdef
		from pg_indexes
		where schemaname = current_schema()
			and indexname = 'mcp_tunnel_token_versions_one_active_v1_idx'
	`).Scan(&activeIndexDefinition); err != nil {
		t.Fatalf("load active Tunnel token index: %v", err)
	}
	if !strings.Contains(activeIndexDefinition, "UNIQUE") ||
		!strings.Contains(activeIndexDefinition, "WHERE ((retired_at IS NULL) AND (archived_at IS NULL))") {
		t.Fatalf("active Tunnel token index = %q", activeIndexDefinition)
	}
}

func assertMigrationColumnNullable(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	tableName string,
	columnName string,
	want string,
) {
	t.Helper()
	var nullable string
	if err := database.QueryRowContext(ctx, `
		select is_nullable
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = $1
			and column_name = $2
	`, tableName, columnName).Scan(&nullable); err != nil {
		t.Fatalf("check migration column %s.%s nullability: %v", tableName, columnName, err)
	}
	if nullable != want {
		t.Fatalf("migration column %s.%s is_nullable = %q, want %q", tableName, columnName, nullable, want)
	}
}

func assertMigrationTableExists(t *testing.T, ctx context.Context, database *sql.DB, tableName string, want bool) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `select to_regclass($1) is not null`, tableName).Scan(&exists); err != nil {
		t.Fatalf("check migration table %s: %v", tableName, err)
	}
	if exists != want {
		t.Fatalf("migration table %s exists = %t, want %t", tableName, exists, want)
	}
}

func assertTunnelIDConstraint(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const insertSQL = `
		insert into mcp_tunnels (
			uuid, external_id, organization_uuid, workspace_uuid, display_name, domain
		) values (
			'53000000-0000-0000-0000-000000000005', $1,
			'53000000-0000-0000-0000-000000000001', '53000000-0000-0000-0000-000000000002',
			'Tunnel ID constraint', $2
		)
	`
	if _, err := database.ExecContext(
		ctx,
		insertSQL,
		"tnl_0123456789abcdef0123456789abcdef",
		"invalid-id.tunnel.invalid",
	); err == nil {
		t.Fatal("migration 59 accepted a legacy tnl_ Tunnel ID")
	}
	if _, err := database.ExecContext(
		ctx,
		insertSQL,
		"tunnel_g123456789abcdef0123456789abcde",
		"non-hex-id.tunnel.invalid",
	); err == nil {
		t.Fatal("migration 59 accepted a non-hexadecimal Tunnel ID")
	}
	if _, err := database.ExecContext(
		ctx,
		insertSQL,
		"tunnel_0123456789abcdef0123456789abcdef",
		"valid-id.tunnel.invalid",
	); err != nil {
		t.Fatalf("migration 59 rejected a valid Tunnel ID: %v", err)
	}
	if _, err := database.ExecContext(ctx, `delete from mcp_tunnels`); err != nil {
		t.Fatalf("clear Tunnel ID constraint fixture: %v", err)
	}
}

func assertTableRowCount(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	tableName string,
	want int,
) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(ctx, "select count(*) from "+tableName).Scan(&got); err != nil {
		t.Fatalf("count %s rows: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", tableName, got, want)
	}
}
