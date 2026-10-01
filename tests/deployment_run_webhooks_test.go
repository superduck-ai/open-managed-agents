package tests

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

var deploymentRunEvents = []string{"deployment_run.started", "deployment_run.succeeded", "deployment_run.failed"}

func TestWebhookDeploymentRunRollbackAndRetry(t *testing.T) {
	for _, stage := range []string{"run insert", "last run update", "schedule delete"} {
		t.Run(stage, func(t *testing.T) {
			app, endpoint, received := newEventSubscription(t, deploymentRunEvents)
			deployment := newWebhookDeployment(t, app)
			input := webhookFailureOccurrence(t, app, deployment.ID)
			var remove func()
			switch stage {
			case "run insert":
				remove = installWebhookMutationFailure(t, app, "deployment_runs", "INSERT", "NEW.deployment_external_id = '"+deployment.ID+"'")
			case "last run update":
				remove = installWebhookMutationFailure(t, app, "deployments", "UPDATE", "NEW.external_id = '"+deployment.ID+"'")
			case "schedule delete":
				input.AutoPauseReason = []byte(`{"type":"error","error":{"type":"environment_not_found_error","message":"test"}}`)
				remove = rejectDeploymentScheduleWrites(t, app, deployment.ID)
			}
			var cleanup sync.Once
			release := func() { cleanup.Do(remove) }
			defer release()
			if err := app.deployments.ApplyScheduledOccurrence(t.Context(), input); err == nil {
				t.Fatal("expected rollback")
			}
			assertDeploymentWebhookTotal(t, app, 0)
			if runs := listDeploymentRuns(t, app, "deployment_id="+deployment.ID); len(runs.Data) != 0 {
				t.Fatal("failed transaction left a Run")
			}
			current := retrieveDeployment(t, app, deployment.ID)
			if current.Status != "active" {
				t.Fatal("failed transaction paused deployment")
			}
			release()
			// A retry may allocate a different Run ID; only the committed attempt notifies.
			firstID := input.Run.ExternalID
			input.Run.ExternalID = newWebhookRunID(t)
			input.Run.UUID = uuid.NewV4().String()
			if err := app.deployments.ApplyScheduledOccurrence(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			for _, event := range deploymentRunEvents {
				assertWebhookCount(t, app, event, firstID, 0)
			}
			assertWebhookDeliveries(t, app, endpoint, received, map[string]int{
				"deployment_run.started/" + input.Run.ExternalID: 1, "deployment_run.failed/" + input.Run.ExternalID: 1,
			}, assertResourceWebhookPayload)
		})
	}
}

func TestWebhookDeploymentRunNoRunBranches(t *testing.T) {
	for _, mode := range []string{"manual success", "manual failure", "archive", "stale"} {
		t.Run(mode, func(t *testing.T) {
			app, _, _ := newEventSubscription(t, deploymentRunEvents)
			deployment := newWebhookDeployment(t, app)
			switch mode {
			case "manual success":
				run := runDeployment(t, app, deployment.ID)
				if run.SessionID == nil {
					t.Fatal("manual run missing Session")
				}
				defer deleteSession(t, app, *run.SessionID)
			case "manual failure":
				archiveEnvironment(t, app, deployment.EnvironmentID)
				run := runDeployment(t, app, deployment.ID)
				if run.SessionID != nil {
					t.Fatal("failed manual run created Session")
				}
			default:
				input := webhookFailureOccurrence(t, app, deployment.ID)
				input.ArchiveDeployment = true
				if mode == "stale" {
					pauseDeployment(t, app, deployment.ID)
				}
				err := app.deployments.ApplyScheduledOccurrence(t.Context(), input)
				if mode == "stale" {
					if !errors.Is(err, db.ErrStaleSchedule) {
						t.Fatalf("stale: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if runs := listDeploymentRuns(t, app, "deployment_id="+deployment.ID); len(runs.Data) != 0 {
					t.Fatal("archive/stale branch created Run")
				}
			}
			assertDeploymentWebhookTotal(t, app, 0)
		})
	}
}

func TestWebhookDeploymentRunScheduledWorkerDelivery(t *testing.T) {
	for _, outcome := range []string{"succeeded", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			app, endpoint, received := newEventSubscription(t, deploymentRunEvents)
			deployment := newWebhookDeployment(t, app)
			if outcome == "failed" {
				archiveEnvironment(t, app, deployment.EnvironmentID)
			}
			_, stop := startDeploymentScheduler(t, app)
			defer stop()
			occurrence := time.Now().UTC()
			runWebhookScheduledJob(t, app, deployment.ID, occurrence)
			runWebhookScheduledJob(t, app, deployment.ID, occurrence)
			runs := listDeploymentRuns(t, app, "deployment_id="+deployment.ID)
			if len(runs.Data) != 1 {
				t.Fatalf("runs=%d", len(runs.Data))
			}
			run := retrieveDeploymentRun(t, app, runs.Data[0].ID)
			if outcome == "succeeded" {
				if run.SessionID == nil {
					t.Fatal("missing Session")
				}
				defer deleteSession(t, app, *run.SessionID)
				// succeeded observes Session creation, before any model worker executes.
				session := retrieveSession(t, app, *run.SessionID, defaultTestKey)
				if session.Status != "idle" {
					t.Fatalf("new Session status=%s", session.Status)
				}
			} else {
				if run.SessionID != nil || !strings.Contains(string(run.Error), "environment_archived_error") {
					t.Fatalf("failed run=%+v", run)
				}
			}
			assertWebhookDeliveries(t, app, endpoint, received, map[string]int{
				"deployment_run.started/" + run.ID: 1, "deployment_run." + outcome + "/" + run.ID: 1,
			}, assertResourceWebhookPayload)
		})
	}
}

func TestWebhookDeploymentRunConcurrentRecoverableOccurrence(t *testing.T) {
	app, endpoint, received := newEventSubscription(t, append(append([]string{}, deploymentRunEvents...), "deployment.paused", "deployment.updated"))
	deployment := newWebhookDeployment(t, app)
	input := webhookFailureOccurrence(t, app, deployment.ID)
	var group sync.WaitGroup
	for range 6 {
		group.Go(func() {
			attempt := input
			attempt.Run.UUID = uuid.NewV4().String()
			attempt.Run.ExternalID = newWebhookRunID(t)
			err := app.deployments.ApplyScheduledOccurrence(t.Context(), attempt)
			if err != nil && !errors.Is(err, db.ErrStaleSchedule) {
				t.Errorf("occurrence: %v", err)
			}
		})
	}
	group.Wait()
	if got := retrieveDeployment(t, app, deployment.ID); got.Status != "active" {
		t.Fatal("recoverable failure paused deployment")
	}
	assertWebhookCount(t, app, "deployment.paused", deployment.ID, 0)
	runs := listDeploymentRuns(t, app, "deployment_id="+deployment.ID)
	if len(runs.Data) != 1 {
		t.Fatalf("runs=%d", len(runs.Data))
	}
	assertWebhookDeliveries(t, app, endpoint, received, map[string]int{
		"deployment.updated/" + deployment.ID:       1,
		"deployment_run.started/" + runs.Data[0].ID: 1, "deployment_run.failed/" + runs.Data[0].ID: 1,
	}, assertResourceWebhookPayload)
}

func TestWebhookDeploymentRunFiltering(t *testing.T) {
	app, endpoint, received := newEventSubscription(t, []string{"deployment_run.succeeded"})
	deployment := newWebhookDeployment(t, app)
	for _, disabled := range []bool{false, true} {
		if disabled {
			updateWebhook(t, app, endpoint.ID, `{"status":"disabled","enabled_events":["deployment_run.started","deployment_run.failed"]}`)
		}
		input := webhookFailureOccurrence(t, app, deployment.ID)
		if err := app.deployments.ApplyScheduledOccurrence(t.Context(), input); err != nil {
			t.Fatal(err)
		}
	}
	updateWebhook(t, app, endpoint.ID, `{"status":"enabled","enabled_events":["deployment_run.started","deployment_run.succeeded","deployment_run.failed"]}`)
	assertDeploymentWebhookTotal(t, app, 0)
	assertWebhookDeliveries(t, app, endpoint, received, map[string]int{})
}

func webhookFailureOccurrence(t *testing.T, app *testApp, deploymentID string) db.ApplyScheduledOccurrenceInput {
	t.Helper()
	deployment, err := app.db.GetDeployment(t.Context(), getDefaultDBIDs(t, app.pool).WorkspaceUUID, deploymentID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return db.ApplyScheduledOccurrenceInput{
		Deployment: deployment, ScheduledAt: now, Now: now,
		Run: db.DeploymentRun{UUID: uuid.NewV4().String(), ExternalID: newWebhookRunID(t), Error: []byte(`{"type":"session_rate_limited_error","message":"test rate limit"}`)},
	}
}
func newWebhookRunID(t *testing.T) string {
	t.Helper()
	id, err := ids.New("drun_")
	if err != nil {
		t.Fatal(err)
	}
	return id
}
