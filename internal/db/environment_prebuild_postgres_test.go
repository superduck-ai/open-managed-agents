//go:build integration

package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

const (
	prebuildTestOrganization = "89000000-0000-0000-0000-000000000001"
	prebuildTestWorkspace    = "89000000-0000-0000-0000-000000000002"
	prebuildReadyEnvironment = "89000000-0000-0000-0000-000000000011"
	prebuildPendingEnv       = "89000000-0000-0000-0000-000000000012"
	prebuildUnbuiltEnv       = "89000000-0000-0000-0000-000000000013"
	prebuildBareEnvironment  = "89000000-0000-0000-0000-000000000014"
	emptyPackagesSnapshot    = `{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]}`
)

func TestEnvironmentResolvedBuildJobMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 165); err != nil {
		t.Fatal(err)
	}
	readyConfig := `{"type":"cloud","packages":{"type":"packages","pip":["b","a"],"npm":null,"go":["example.com/tool@v1"]}}`
	seedPrebuildEnvironment(t, ctx, database, prebuildReadyEnvironment, "env_ready", readyConfig, "tpl-ready", 41)
	seedPrebuildEnvironment(t, ctx, database, prebuildBareEnvironment, "env_bare", `{"type":"cloud","init_script":"make"}`, "tpl-bare", 44)
	seedPrebuildEnvironment(t, ctx, database, prebuildPendingEnv, "env_pending", readyConfig, "", 42)
	seedPrebuildEnvironment(t, ctx, database, prebuildUnbuiltEnv, "env_unbuilt", `{"type":"cloud"}`, "managed-agent-sandbox", 0)
	if _, err := migrations.UpTo(ctx, 166); err != nil {
		t.Fatal(err)
	}
	readyPackages := `{"type":"packages","apt":[],"cargo":[],"gem":[],"go":["example.com/tool@v1"],"npm":[],"pip":["b","a"]}`
	for _, tc := range []struct {
		uuid     string
		template string
		resolved sql.NullInt64
		packages sql.NullString
	}{
		{prebuildReadyEnvironment, "tpl-ready", sql.NullInt64{Int64: 41, Valid: true}, sql.NullString{String: readyPackages, Valid: true}},
		{prebuildBareEnvironment, "tpl-bare", sql.NullInt64{Int64: 44, Valid: true}, sql.NullString{String: emptyPackagesSnapshot, Valid: true}},
		{prebuildPendingEnv, "", sql.NullInt64{}, sql.NullString{}},
		{prebuildUnbuiltEnv, "managed-agent-sandbox", sql.NullInt64{}, sql.NullString{}},
	} {
		template, resolved := readPrebuildEnvironment(t, ctx, database, tc.uuid)
		var matches sql.NullBool
		if err := database.QueryRowContext(ctx, `SELECT resolved_packages = $2::jsonb FROM environments WHERE uuid = $1`, tc.uuid, tc.packages).Scan(&matches); err != nil {
			t.Fatal(err)
		}
		if template != tc.template || resolved != tc.resolved || matches.Valid != tc.packages.Valid || (matches.Valid && !matches.Bool) {
			t.Fatalf("environment %s backfill = (%q, %+v, packages match %+v), want (%q, %+v, %+v)", tc.uuid, template, resolved, matches, tc.template, tc.resolved, tc.packages)
		}
	}

	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	if _, err := database.ExecContext(ctx, `UPDATE environments SET build_job_id = 43 WHERE uuid = $1`, prebuildReadyEnvironment); err != nil {
		t.Fatal(err)
	}
	err := store.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		return store.ResolveEnvironmentPrebuildTx(ctx, tx, prebuildTestWorkspace, prebuildReadyEnvironment, 41, "tpl-stale", json.RawMessage(emptyPackagesSnapshot))
	})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale job resolve error = %v", err)
	}
	if template, resolved := readPrebuildEnvironment(t, ctx, database, prebuildReadyEnvironment); template != "tpl-ready" || resolved.Int64 != 41 {
		t.Fatalf("stale job changed the active template to (%q, %+v)", template, resolved)
	}

	if _, err := migrations.DownTo(ctx, 165); err != nil {
		t.Fatal(err)
	}
	var template string
	if err := database.QueryRowContext(ctx, `SELECT resolved_template FROM environments WHERE uuid = $1`, prebuildReadyEnvironment).Scan(&template); err != nil || template != "" {
		t.Fatalf("down migration kept transition template %q: %v", template, err)
	}
}

func TestEnvironmentResolvePrebuildPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	latest := int64(42)
	if _, err := store.CreateEnvironment(ctx, Environment{
		UUID: prebuildReadyEnvironment, ExternalID: "env_resolve", OrganizationUUID: prebuildTestOrganization,
		WorkspaceUUID: prebuildTestWorkspace, Name: "resolve", Provider: "e2b", Config: []byte(`{"type":"cloud"}`),
		Metadata: []byte(`{}`), BuildJobID: &latest, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	err := store.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		return store.ResolveEnvironmentPrebuildTx(ctx, tx, prebuildTestWorkspace, prebuildReadyEnvironment, latest, "tpl-new", json.RawMessage(emptyPackagesSnapshot))
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.GetEnvironmentByUUID(ctx, prebuildTestWorkspace, prebuildReadyEnvironment)
	if err != nil || current.ResolvedTemplate != "tpl-new" || current.ResolvedBuildJobID == nil || *current.ResolvedBuildJobID != latest {
		t.Fatalf("resolved environment = %+v, %v", current, err)
	}
	var packages map[string]json.RawMessage
	if err := json.Unmarshal(current.ResolvedPackages, &packages); err != nil || len(packages) != 7 {
		t.Fatalf("resolved packages = %s, %v", current.ResolvedPackages, err)
	}
}

func seedPrebuildEnvironment(t *testing.T, ctx context.Context, database *sql.DB, uuid, externalID, config, template string, jobID int64) {
	t.Helper()
	var job any
	if jobID != 0 {
		job = jobID
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO environments (uuid, external_id, organization_uuid, workspace_uuid, name, config, metadata, provider, resolved_template, build_job_id)
		VALUES ($1, $2, $3, $4, $2, $5::jsonb, '{}', 'e2b', $6, $7)`,
		uuid, externalID, prebuildTestOrganization, prebuildTestWorkspace, config, template, job); err != nil {
		t.Fatalf("seed environment %s: %v", externalID, err)
	}
}

func readPrebuildEnvironment(t *testing.T, ctx context.Context, database *sql.DB, uuid string) (string, sql.NullInt64) {
	t.Helper()
	var template string
	var resolved sql.NullInt64
	if err := database.QueryRowContext(ctx, `SELECT resolved_template, resolved_build_job_id FROM environments WHERE uuid = $1`, uuid).Scan(&template, &resolved); err != nil {
		t.Fatal(err)
	}
	return template, resolved
}
