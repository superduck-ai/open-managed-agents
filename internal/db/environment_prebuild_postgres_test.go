//go:build integration

package db

import (
	"context"
	"database/sql"
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
	seedPrebuildEnvironment(t, ctx, database, prebuildReadyEnvironment, "env_ready", "tpl-ready", 41)
	seedPrebuildEnvironment(t, ctx, database, prebuildPendingEnv, "env_pending", "", 42)
	seedPrebuildEnvironment(t, ctx, database, prebuildUnbuiltEnv, "env_unbuilt", "managed-agent-sandbox", 0)
	if _, err := migrations.UpTo(ctx, 166); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		uuid     string
		template string
		resolved sql.NullInt64
	}{
		{prebuildReadyEnvironment, "tpl-ready", sql.NullInt64{Int64: 41, Valid: true}},
		{prebuildPendingEnv, "", sql.NullInt64{}},
		{prebuildUnbuiltEnv, "managed-agent-sandbox", sql.NullInt64{}},
	} {
		template, resolved := readPrebuildEnvironment(t, ctx, database, tc.uuid)
		if template != tc.template || resolved != tc.resolved {
			t.Fatalf("environment %s backfill = (%q, %+v), want (%q, %+v)", tc.uuid, template, resolved, tc.template, tc.resolved)
		}
	}

	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	if _, err := database.ExecContext(ctx, `UPDATE environments SET build_job_id = 43 WHERE uuid = $1`, prebuildReadyEnvironment); err != nil {
		t.Fatal(err)
	}
	err := store.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		return store.ResolveEnvironmentPrebuildTx(ctx, tx, prebuildTestWorkspace, prebuildReadyEnvironment, 41, "tpl-stale")
	})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale job resolve error = %v", err)
	}
	if template, resolved := readPrebuildEnvironment(t, ctx, database, prebuildReadyEnvironment); template != "tpl-ready" || resolved.Int64 != 41 {
		t.Fatalf("stale job changed the active template to (%q, %+v)", template, resolved)
	}
	env, err := store.GetEnvironmentByUUID(ctx, prebuildTestWorkspace, prebuildReadyEnvironment)
	if err != nil || env.ResolvedBuildJobID == nil || *env.ResolvedBuildJobID != 41 || env.BuildJobID == nil || *env.BuildJobID != 43 {
		t.Fatalf("transition scan = %+v, %v", env, err)
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
	previous, latest := int64(41), int64(42)
	created, err := store.CreateEnvironment(ctx, Environment{
		UUID: prebuildReadyEnvironment, ExternalID: "env_resolve", OrganizationUUID: prebuildTestOrganization,
		WorkspaceUUID: prebuildTestWorkspace, Name: "resolve", Provider: "e2b", Config: []byte(`{"type":"cloud"}`),
		Metadata: []byte(`{}`), ResolvedTemplate: "tpl-old", ResolvedBuildJobID: &previous, BuildJobID: &latest,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil || created.ResolvedBuildJobID == nil || *created.ResolvedBuildJobID != previous {
		t.Fatalf("create = %+v, %v", created, err)
	}
	resolve := func(jobID int64, template string) error {
		return store.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
			return store.ResolveEnvironmentPrebuildTx(ctx, tx, prebuildTestWorkspace, prebuildReadyEnvironment, jobID, template)
		})
	}
	if err := resolve(previous, "tpl-superseded"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("superseded resolve error = %v", err)
	}
	if err := resolve(latest, "tpl-new"); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetEnvironmentByUUID(ctx, prebuildTestWorkspace, prebuildReadyEnvironment)
	if err != nil || current.ResolvedTemplate != "tpl-new" || current.ResolvedBuildJobID == nil || *current.ResolvedBuildJobID != latest {
		t.Fatalf("resolved environment = %+v, %v", current, err)
	}
	current.ResolvedBuildJobID, current.UpdatedAt = nil, time.Now().UTC()
	err = store.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		_, changed, err := store.UpdateEnvironmentTx(ctx, tx, current)
		if err == nil && !changed {
			err = errors.New("clearing the resolved build was not detected as a change")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, resolved := readPrebuildEnvironment(t, ctx, database, prebuildReadyEnvironment); resolved.Valid {
		t.Fatalf("resolved build job = %+v, want NULL", resolved)
	}
}

func seedPrebuildEnvironment(t *testing.T, ctx context.Context, database *sql.DB, uuid, externalID, template string, jobID int64) {
	t.Helper()
	var job any
	if jobID != 0 {
		job = jobID
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO environments (uuid, external_id, organization_uuid, workspace_uuid, name, config, metadata, provider, resolved_template, build_job_id)
		VALUES ($1, $2, $3, $4, $2, '{"type":"cloud"}', '{}', 'e2b', $5, $6)`,
		uuid, externalID, prebuildTestOrganization, prebuildTestWorkspace, template, job); err != nil {
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
