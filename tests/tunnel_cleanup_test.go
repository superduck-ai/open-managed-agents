//go:build e2e

package tests

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

func TestTunnelArchiveCleanupTransactionAndRestart(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = managedTunnelDatabase(t, cfg.Database.URL)
	app := newTestAppWithStore(t, &cfg, newFakeStore("tunnel-cleanup"))
	t.Cleanup(app.close)
	connection := managedTunnelNATS(t)
	broker, err := tunnels.NewBroker(t.Context(), connection, cfg.Tunnel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	workers := river.NewWorkers()
	tunnels.RegisterCleanupWorker(workers, app.db, broker, nil)
	jobs, err := riverjobs.NewClient(app.db, nil, workers, map[string]river.QueueConfig{tunnels.CleanupQueue: {MaxWorkers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.NewServer(api.ServerDeps{Config: cfg, DB: app.db, CodeSessionCredentials: app.credentials, FilestoreCredentials: app.filestoreCredentials, Deployments: app.deployments, ObjectStore: app.store, VaultSecrets: app.vaultSecrets, TunnelBroker: broker, TunnelCleanupJobs: tunnels.NewCleanupJobs(jobs)}))
	defer server.Close()
	client := anthropic.NewClient(option.WithBaseURL(server.URL), option.WithAPIKey(defaultTestKey))
	created, err := client.Beta.Tunnels.New(t.Context(), anthropic.BetaTunnelNewParams{})
	if err != nil {
		t.Fatal(err)
	}
	ids := getDefaultDBIDs(t, app.pool)
	record, err := app.db.GetMCPTunnel(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.ActivateTokenVersion(t.Context(), record.UUID, 1); err != nil {
		t.Fatal(err)
	}
	// Force job insertion to fail after the archive writes, proving both writes
	// share the transaction. All DDL is confined to this test's disposable DB.
	if _, err := app.pool.Exec(t.Context(), `ALTER TABLE river_job ADD CONSTRAINT reject_tunnel_cleanup CHECK (kind <> 'tunnel_control_cleanup')`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Beta.Tunnels.Archive(t.Context(), created.ID, anthropic.BetaTunnelArchiveParams{}); err == nil {
		t.Fatal("archive survived failed job insertion")
	}
	record, err = app.db.GetMCPTunnel(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, created.ID)
	if err != nil || record.ArchivedAt != nil {
		t.Fatalf("archive did not roll back: %v", err)
	}
	if _, err := app.pool.Exec(t.Context(), `ALTER TABLE river_job DROP CONSTRAINT reject_tunnel_cleanup`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Beta.Tunnels.Archive(t.Context(), created.ID, anthropic.BetaTunnelArchiveParams{}); err != nil {
		t.Fatal(err)
	}
	// Duplicate archive before execution must not add another pending job.
	if _, err := client.Beta.Tunnels.Archive(t.Context(), created.ID, anthropic.BetaTunnelArchiveParams{}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM river_job WHERE kind='tunnel_control_cleanup'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable job count=%d: %v", count, err)
	}
	js, err := jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	controls, err := js.Stream(t.Context(), "KV_OMA_TUNNEL_CONTROL_V1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := controls.Info(t.Context())
	if err != nil || info.State.Msgs != 1 {
		t.Fatalf("control removed before worker: %v", err)
	}
	// Recreate the client after commit, simulating exit before cleanup begins.
	restarted, err := riverjobs.NewClient(app.db, nil, workers, map[string]river.QueueConfig{tunnels.CleanupQueue: {MaxWorkers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := restarted.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for {
		var state string
		if err := app.pool.QueryRow(t.Context(), `SELECT state FROM river_job WHERE kind='tunnel_control_cleanup'`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup state=%s", state)
		}
		time.Sleep(25 * time.Millisecond)
	}
	info, err = controls.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 0 {
		t.Fatalf("capacity not released: %d", info.State.Msgs)
	}
	record, err = app.db.GetMCPTunnel(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, created.ID)
	if err != nil || record.ArchivedAt == nil {
		t.Fatalf("archived resource disappeared: %v", err)
	}
}
