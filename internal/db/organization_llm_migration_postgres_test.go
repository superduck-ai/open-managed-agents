package db

import (
	"io/fs"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestOrganizationLLMProviderMigrationFromMain(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	migrationFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	mainProvider, err := goose.NewProvider(
		goose.DialectPostgres, database, migrationFS,
		goose.WithDisableGlobalRegistry(true),
		goose.WithLogger(goose.NopLogger()),
		goose.WithExcludeNames([]string{"00077_organization_llm_provider.sql", "00083_organization_llm_provider.sql"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mainProvider.UpTo(ctx, 82); err != nil {
		t.Fatalf("migrate to main schema: %v", err)
	}
	if err := runGooseMigrations(ctx, database); err != nil {
		t.Fatalf("upgrade main schema with organization provider: %v", err)
	}
	var nullable string
	if err := database.QueryRowContext(ctx, `
		select is_nullable from information_schema.columns
		where table_schema = current_schema() and table_name = 'llm_providers' and column_name = 'workspace_uuid'
	`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if nullable != "YES" {
		t.Fatal("organization providers require a nullable workspace_uuid")
	}
	var indexExists bool
	if err := database.QueryRowContext(ctx, `
		select exists(select 1 from pg_indexes
		where schemaname = current_schema() and indexname = 'llm_providers_organization_default_key')
	`).Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if !indexExists {
		t.Fatal("organization provider uniqueness index is missing")
	}
}
