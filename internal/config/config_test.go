package config

import (
	"os"
	"testing"
)

func TestLoadDatabaseAutoMigrateDefaultDevelopment(t *testing.T) {
	prepareLoadTest(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Fatalf("AppEnv = %q, want development", cfg.AppEnv)
	}
	if !cfg.DatabaseAutoMigrate {
		t.Fatal("DatabaseAutoMigrate = false, want true")
	}
}

func TestLoadDatabaseAutoMigrateProductionDefault(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("APP_ENV", "production")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DatabaseAutoMigrate {
		t.Fatal("DatabaseAutoMigrate = true, want false")
	}
}

func TestLoadDatabaseAutoMigrateProdDefault(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("APP_ENV", "prod")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DatabaseAutoMigrate {
		t.Fatal("DatabaseAutoMigrate = true, want false")
	}
}

func TestLoadDatabaseAutoMigrateOverride(t *testing.T) {
	t.Run("production enabled explicitly", func(t *testing.T) {
		prepareLoadTest(t)
		t.Setenv("APP_ENV", "production")
		t.Setenv("DB_AUTO_MIGRATE", "true")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !cfg.DatabaseAutoMigrate {
			t.Fatal("DatabaseAutoMigrate = false, want true")
		}
	})

	t.Run("development disabled explicitly", func(t *testing.T) {
		prepareLoadTest(t)
		t.Setenv("APP_ENV", "development")
		t.Setenv("DB_AUTO_MIGRATE", "false")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.DatabaseAutoMigrate {
			t.Fatal("DatabaseAutoMigrate = true, want false")
		}
	})
}

func prepareLoadTest(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, key := range []string{
		"APP_ENV",
		"DB_AUTO_MIGRATE",
		"DATABASE_URL",
		"S3_ENDPOINT",
		"S3_BUCKET",
		"S3_ACCESS_KEY_ID",
		"S3_SECRET_ACCESS_KEY",
	} {
		unsetEnv(t, key)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	oldValue, hadValue := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
