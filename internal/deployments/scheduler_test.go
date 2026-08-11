package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDeploymentSchedulerIgnoresStaleInactiveUpdate(t *testing.T) {
	scheduler := newRegistryTestScheduler(t)
	deployment := scheduledDeploymentForRegistryTest(2)
	scheduler.Update(context.Background(), deployment)

	deployment.Status = "paused"
	deployment.ScheduleRevision = 1
	scheduler.Update(context.Background(), deployment)

	if revision := scheduler.registered[deployment.ExternalID]; revision != 2 {
		t.Fatalf("registered revision = %d, want 2", revision)
	}
	if !scheduler.client.PeriodicJobs().RemoveByID(deployment.ExternalID) {
		t.Fatal("newer periodic job was removed by stale inactive update")
	}
}

func TestDeploymentSchedulerIgnoresStaleActiveUpdate(t *testing.T) {
	scheduler := newRegistryTestScheduler(t)
	deployment := scheduledDeploymentForRegistryTest(2)
	scheduler.Update(context.Background(), deployment)

	deployment.ScheduleRevision = 1
	scheduler.Update(context.Background(), deployment)

	if revision := scheduler.registered[deployment.ExternalID]; revision != 2 {
		t.Fatalf("registered revision = %d, want 2", revision)
	}
}

func TestDeploymentSchedulerRemovesInactiveDeployment(t *testing.T) {
	scheduler := newRegistryTestScheduler(t)
	deployment := scheduledDeploymentForRegistryTest(1)
	scheduler.Update(context.Background(), deployment)

	deployment.Status = "paused"
	deployment.ScheduleRevision = 2
	scheduler.Update(context.Background(), deployment)

	if _, ok := scheduler.registered[deployment.ExternalID]; ok {
		t.Fatal("inactive deployment remains registered")
	}
	if scheduler.client.PeriodicJobs().RemoveByID(deployment.ExternalID) {
		t.Fatal("inactive deployment periodic job remains configured")
	}
}

func newRegistryTestScheduler(t *testing.T) *DeploymentScheduler {
	t.Helper()
	config, err := pgx.ParseConfig("postgres://localhost/unused")
	if err != nil {
		t.Fatalf("parse test database config: %v", err)
	}
	sqlDB := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = sqlDB.Close() })
	workers := river.NewWorkers()
	river.AddWorker(workers, &scheduledDeploymentWorker{})
	client, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{
		Queues:  map[string]river.QueueConfig{deploymentScheduleQueue: {MaxWorkers: 1}},
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("new River client: %v", err)
	}
	return &DeploymentScheduler{
		client: client, logger: slog.Default(), registered: make(map[string]int64),
	}
}

func scheduledDeploymentForRegistryTest(revision int64) db.Deployment {
	return db.Deployment{
		WorkspaceUUID: "workspace", ExternalID: "depl_registry", Status: "active",
		Schedule:         json.RawMessage(`{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`),
		ScheduleRevision: revision,
	}
}

func TestClassifyReferenceFailureRetriesInfrastructureErrors(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	failure, err := classifyReferenceFailure("agent", databaseErr, false)
	if !errors.Is(err, databaseErr) || failure != nil {
		t.Fatalf("classifyReferenceFailure() = (%v, %v), want (nil, database error)", failure, err)
	}

	failure, err = classifyReferenceFailure("agent", db.ErrNotFound, false)
	if err != nil || failure == nil || failure.Type != "agent_archived_error" || failure.Message != "Agent not found" {
		t.Fatalf("classifyReferenceFailure(not found) = (%v, %v)", failure, err)
	}
}

func TestShouldAutoPauseUsesOfficialAllowlist(t *testing.T) {
	autoPause := []string{
		"environment_archived_error",
		"agent_archived_error",
		"environment_not_found_error",
		"vault_not_found_error",
		"file_not_found_error",
		"session_resource_not_found_error",
		"workspace_archived_error",
		"organization_disabled_error",
		"memory_store_archived_error",
		"skill_not_found_error",
		"vault_archived_error",
		"unknown_error",
		"self_hosted_resources_unsupported_error",
		"mcp_egress_blocked_error",
	}
	for _, errorType := range autoPause {
		if !shouldAutoPause(runError(errorType, "test")) {
			t.Errorf("shouldAutoPause(%q) = false", errorType)
		}
	}
	for _, errorType := range []string{"session_rate_limited_error", "session_creation_rejected_error", "future_error"} {
		if shouldAutoPause(runError(errorType, "test")) {
			t.Errorf("shouldAutoPause(%q) = true", errorType)
		}
	}
	if shouldAutoPause(nil) {
		t.Error("shouldAutoPause(nil) = true")
	}
}
