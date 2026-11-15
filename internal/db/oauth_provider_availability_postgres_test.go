//go:build integration

package db

import (
	"os"
	"testing"
)

func TestOAuthProviderAvailabilityMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL；跳过不能视为通过")
	}
	for _, previous := range []string{"", "CUSTOM_FRIDAY_TOKEN"} {
		t.Run(previous, func(t *testing.T) {
			database := newIsolatedSessionResourceMigrationTestDatabase(t, t.Context(), address)
			provider := newMigrationTestProvider(t, database)
			if _, err := provider.UpTo(t.Context(), 126); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(t.Context(), `UPDATE platform_oauth_providers SET default_secret_name=$1 WHERE builtin_key='friday'`, previous); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(t.Context(), 127); err != nil {
				t.Fatal(err)
			}
			var enabled bool
			var name string
			if err := database.QueryRowContext(t.Context(), `SELECT enabled, default_secret_name FROM platform_oauth_providers WHERE builtin_key='friday'`).Scan(&enabled, &name); err != nil {
				t.Fatal(err)
			}
			expected := previous
			if expected == "" {
				expected = "FRIDAY_4A_TOKEN"
			}
			if !enabled || name != expected {
				t.Fatalf("Friday 默认值错误：启用=%v，变量名=%q", enabled, name)
			}
			if _, err := database.ExecContext(t.Context(), `UPDATE platform_oauth_providers SET enabled=NULL WHERE builtin_key='friday'`); err == nil {
				t.Fatal("启用状态接受 NULL")
			}
		})
	}
}
