//go:build integration

package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
)

func telemetryDestinationTestDB(t *testing.T) (context.Context, *sql.DB, *DB) {
	t.Helper()
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		databaseURL = cfg.Database.URL
	}
	ctx, standardDB, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, standardDB, &DB{mapperDB: yourbatis.NewDB(standardDB, yourbatis.DialectPostgres)}
}

func TestTelemetryDestinationsPostgres(t *testing.T) {
	ctx, standardDB, database := telemetryDestinationTestDB(t)
	organization := "96000000-0000-0000-0000-000000000001"
	workspaceA, workspaceB := "96000000-0000-0000-0000-00000000000a", "96000000-0000-0000-0000-00000000000b"
	if _, err := standardDB.ExecContext(ctx, `INSERT INTO organizations (uuid, name) VALUES ($1, 'telemetry')`, organization); err != nil {
		t.Fatal(err)
	}
	envelope := &secrets.Envelope{Ciphertext: []byte("c"), Nonce: []byte("n"), WrappedDEK: []byte("k"), FormatVersion: 1, KeyProvider: "local", KeyVersion: 1}
	base := TelemetryDestination{OrganizationUUID: organization, EndpointURL: "https://collector.example.com", TracesEnabled: true,
		TracesEndpointURL: "https://collector.example.com/v1/traces", TracesContentMode: "metadata_only", TracesMaxContentBytes: 1000, State: "enabled"}
	create := func(id, scope string, workspaces []string, headers *secrets.Envelope) TelemetryDestination {
		t.Helper()
		destination := base
		destination.UUID, destination.ExternalID, destination.WorkspaceScope, destination.WorkspaceUUIDs, destination.Headers = "", id, scope, workspaces, headers
		destination.UUID = strings.Replace("96000000-0000-0000-0000-0000000000XX", "XX", id[len(id)-2:], 1)
		if headers != nil {
			destination.HeaderNames = []string{"Authorization"}
		}
		created, err := database.CreateTelemetryDestination(ctx, destination, 3)
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	all := create("tdst_01", "all", nil, envelope)
	include := create("tdst_02", "include", []string{workspaceA}, nil)
	exclude := create("tdst_03", "exclude", []string{workspaceA}, nil)
	if _, err := database.CreateTelemetryDestination(ctx, TelemetryDestination{OrganizationUUID: organization, ExternalID: "tdst_04"}, 3); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
	if all.Headers == nil || all.HeaderNames[0] != "Authorization" || include.WorkspaceUUIDs[0] != workspaceA || include.Headers != nil {
		t.Fatalf("created = %+v %+v", all, include)
	}

	ids := func(destinations []TelemetryDestination) string {
		names := []string{}
		for _, destination := range destinations {
			names = append(names, destination.ExternalID)
		}
		return strings.Join(names, ",")
	}
	for workspace, want := range map[string]string{workspaceA: "tdst_01,tdst_02", workspaceB: "tdst_01,tdst_03"} {
		active, err := database.ListTelemetryDestinationsForWorkspace(ctx, organization, workspace)
		if err != nil || ids(active) != want {
			t.Fatalf("active for %s = %s %v", workspace, ids(active), err)
		}
		listed, err := database.ListTelemetryDestinations(ctx, organization, []string{workspace}, 10, 0)
		if err != nil || ids(listed) != want {
			t.Fatalf("listed for %s = %s %v", workspace, ids(listed), err)
		}
	}
	if listed, err := database.ListTelemetryDestinations(ctx, organization, nil, 2, 1); err != nil || ids(listed) != "tdst_02,tdst_03" {
		t.Fatalf("page = %s %v", ids(listed), err)
	}

	include.WorkspaceUUIDs = []string{workspaceB}
	include.TracesContentMode = "content"
	if _, err := database.UpdateTelemetryDestination(ctx, include, include.UpdatedAt.Add(-time.Second)); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update = %v", err)
	}
	updated, err := database.UpdateTelemetryDestination(ctx, include, include.UpdatedAt)
	if err != nil || updated.TracesContentMode != "content" || len(updated.WorkspaceUUIDs) != 1 || updated.WorkspaceUUIDs[0] != workspaceB {
		t.Fatalf("updated = %+v %v", updated, err)
	}
	missing := include
	missing.ExternalID = "tdst_missing"
	if _, err := database.UpdateTelemetryDestination(ctx, missing, include.UpdatedAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := database.MarkTelemetryDestinationTested(ctx, organization, "tdst_01", now, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkTelemetryDestinationTested(ctx, organization, "tdst_01", now.Add(5*time.Second), 10*time.Second); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("rate limit = %v", err)
	}
	if err := database.MarkTelemetryDestinationTested(ctx, organization, "tdst_01", now.Add(11*time.Second), 10*time.Second); err != nil {
		t.Fatal(err)
	}

	disabled, err := database.RecordTelemetryDeliveryFailure(ctx, all.UUID, "status 503", "", 24*time.Hour, now)
	if err != nil || disabled {
		t.Fatalf("first failure disabled=%v err=%v", disabled, err)
	}
	disabled, err = database.RecordTelemetryDeliveryFailure(ctx, all.UUID, "status 503", "", 24*time.Hour, now.Add(25*time.Hour))
	if err != nil || !disabled {
		t.Fatalf("sustained failure disabled=%v err=%v", disabled, err)
	}
	staleEdit := all
	staleEdit.TracesContentMode = "content"
	if _, err := database.UpdateTelemetryDestination(ctx, staleEdit, all.UpdatedAt); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("edit read before auto-disable = %v", err)
	}
	stored, err := database.GetTelemetryDestination(ctx, organization, "tdst_01")
	if err != nil || stored.State != "disabled" || *stored.DisabledReason != "sustained_failure" || !stored.FailureStartedAt.Equal(now) || *stored.LastError != "status 503" {
		t.Fatalf("stored = %+v %v", stored, err)
	}
	if again, err := database.RecordTelemetryDeliveryFailure(ctx, all.UUID, "late", "", time.Hour, now); err != nil || again {
		t.Fatalf("disabled destination recorded again = %v %v", again, err)
	}
	if disabled, err := database.RecordTelemetryDeliveryFailure(ctx, exclude.UUID, "redirected", "redirect", 24*time.Hour, now); err != nil || !disabled {
		t.Fatalf("immediate = %v %v", disabled, err)
	}
	if err := database.RecordTelemetryDeliverySuccess(ctx, include.UUID, now); err != nil {
		t.Fatal(err)
	}
	reenabled := stored
	reenabled.State, reenabled.DisabledReason = "enabled", nil
	reenabled, err = database.UpdateTelemetryDestination(ctx, reenabled, stored.UpdatedAt)
	if err != nil || reenabled.State != "enabled" || reenabled.FailureStartedAt != nil || reenabled.LastError == nil {
		t.Fatalf("re-enabled = %+v %v", reenabled, err)
	}
	if err := database.DeleteTelemetryDestination(ctx, organization, "tdst_02"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetTelemetryDestination(ctx, organization, "tdst_02"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted lookup = %v", err)
	}
	if err := database.DeleteTelemetryDestination(ctx, organization, "tdst_02"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
	var links int
	if err := standardDB.QueryRowContext(ctx, `SELECT count(*) FROM telemetry_destination_workspaces WHERE destination_uuid = $1`, include.UUID).Scan(&links); err != nil || links != 0 {
		t.Fatalf("links = %d %v", links, err)
	}
	if _, err := database.GetTelemetryDestinationByUUID(ctx, all.UUID); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteTelemetryDestination(ctx, organization, "tdst_01"); err != nil {
		t.Fatal(err)
	}
	var retained bool
	if err := standardDB.QueryRowContext(ctx, `SELECT headers_ciphertext IS NOT NULL OR headers_nonce IS NOT NULL OR headers_wrapped_dek IS NOT NULL FROM telemetry_destinations WHERE uuid = $1`, all.UUID).Scan(&retained); err != nil || retained {
		t.Fatalf("deleted destination kept header ciphertext = %v %v", retained, err)
	}

	if _, err := standardDB.ExecContext(ctx, `INSERT INTO workspaces (uuid, external_id, organization_uuid, name, archived_at) VALUES ($1, 'wrkspc_a', $3, 'a', NULL), ($2, 'wrkspc_b', $3, 'b', now())`, workspaceA, workspaceB, organization); err != nil {
		t.Fatal(err)
	}
	visible, err := database.TelemetryWorkspaceExternalIDs(ctx, organization, []string{workspaceA, workspaceB})
	if err != nil || len(visible) != 1 || visible[workspaceA] != "wrkspc_a" {
		t.Fatalf("workspace identities = %v %v", visible, err)
	}
	resolved, err := database.ResolveTelemetryWorkspaces(ctx, organization, []string{"wrkspc_a", "wrkspc_b"})
	if err != nil || len(resolved) != 1 || resolved["wrkspc_a"] != workspaceA {
		t.Fatalf("resolved workspaces = %v %v", resolved, err)
	}
}
