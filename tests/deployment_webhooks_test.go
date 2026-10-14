package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

var deploymentSessionEvents = []string{
	"session.status_idled", "session.thread_created", "session.thread_idled", "session.outcome_evaluation_ended",
}

func TestWebhookDeploymentFailuresDoNotNotify(t *testing.T) {
	app, _, _ := newEventSubscription(t, deploymentSessionEvents)
	deployment := newWebhookDeployment(t, app)
	const otherKey = "sk-ant-test-deployment-webhook-other"
	seedWorkspaceKey(t, app.pool, "deployment_webhook_other_org", "deployment_webhook_other_workspace", "deployment_webhook_other_key", otherKey)
	resp := doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deployment.ID+"/run", strings.NewReader(`{}`), otherKey, true)
	assertError(t, resp, http.StatusNotFound, "not_found_error")
	assertDeploymentWebhookTotal(t, app, 0)

	// Reject the run insert after Session SQL has executed, proving rollback does not notify.
	trigger := pgx.Identifier{"test_webhook_run_failure_" + uuid.NewV4().String()}.Sanitize()
	_, err := app.pool.Exec(t.Context(), `CREATE FUNCTION `+trigger+`() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.deployment_external_id = `+("'"+strings.ReplaceAll(deployment.ID, "'", "''")+"'")+` THEN RAISE EXCEPTION 'test run failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER `+trigger+` BEFORE INSERT ON deployment_runs FOR EACH ROW EXECUTE FUNCTION `+trigger+`()`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := app.pool.Exec(context.Background(), `DROP TRIGGER `+trigger+` ON deployment_runs; DROP FUNCTION `+trigger+`() `); err != nil {
			t.Error(err)
		}
	}()
	resp = doDeploymentRequest(t, app, http.MethodPost, "/v1/deployments/"+deployment.ID+"/run", strings.NewReader(`{}`), defaultTestKey, true)
	assertError(t, resp, http.StatusInternalServerError, "api_error")
	assertDeploymentWebhookTotal(t, app, 0)
	assertDeploymentSessionCount(t, app, deployment.ID, 0)
}

func TestWebhookDeploymentScheduledNoSessionDoesNotNotify(t *testing.T) {
	for _, scenario := range []string{"failed dependency", "stale schedule", "archive branch"} {
		t.Run(scenario, func(t *testing.T) {
			app, _, _ := newEventSubscription(t, deploymentSessionEvents)
			deployment := newWebhookDeployment(t, app)
			ids := getDefaultDBIDs(t, app.pool)
			if scenario == "archive branch" {
				current, err := app.db.GetDeployment(t.Context(), ids.WorkspaceUUID, deployment.ID)
				if err != nil {
					t.Fatal(err)
				}
				// Even an input with a Session must not notify when the transaction takes its archive branch.
				err = app.deployments.ApplyScheduledOccurrence(t.Context(), db.ApplyScheduledOccurrenceInput{
					Deployment: current, ArchiveDeployment: true, Session: &db.CreateSessionInput{},
				})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if scenario == "failed dependency" {
					archiveEnvironment(t, app, deployment.EnvironmentID)
				}
				if scenario == "stale schedule" {
					updateDeployment(t, app, deployment.ID, `{"schedule":{"type":"cron","expression":"0 0 2 1 *","timezone":"UTC"}}`)
				}
				_, stop := startDeploymentScheduler(t, app)
				defer stop()
				runWebhookScheduledJob(t, app, deployment.ID, time.Now().UTC())
			}
			assertDeploymentWebhookTotal(t, app, 0)
			assertDeploymentSessionCount(t, app, deployment.ID, 0)
		})
	}
}

func TestWebhookDeploymentSubscriptionFiltering(t *testing.T) {
	app, endpoint, _ := newEventSubscription(t, []string{"session.outcome_evaluation_ended"})
	deployment := newWebhookDeployment(t, app)
	first := runDeployment(t, app, deployment.ID)
	if first.SessionID == nil {
		t.Fatal("missing first session")
	}
	defer deleteSession(t, app, *first.SessionID)
	assertDeploymentWebhookTotal(t, app, 0)
	updateWebhook(t, app, endpoint.ID, `{"status":"disabled","enabled_events":["session.status_idled"]}`)
	second := runDeployment(t, app, deployment.ID)
	if second.SessionID == nil {
		t.Fatal("missing second session")
	}
	defer deleteSession(t, app, *second.SessionID)
	assertDeploymentWebhookTotal(t, app, 0)
}

func TestWebhookDeploymentManualAndScheduledDelivery(t *testing.T) {
	for _, mode := range []string{"manual", "scheduled worker"} {
		t.Run(mode, func(t *testing.T) {
			app, endpoint, received := newEventSubscription(t, deploymentSessionEvents)
			if _, err := app.pool.Exec(t.Context(), `UPDATE webhook_endpoints SET enabled_events=enabled_events || '["session.created","session.pending"]'::jsonb WHERE external_id=$1`, endpoint.ID); err != nil {
				t.Fatal(err)
			}
			deployment := newWebhookDeployment(t, app)
			expected := map[string]int{}
			var sessionID string
			if mode == "manual" {
				for range 2 {
					run := runDeployment(t, app, deployment.ID)
					if run.SessionID == nil {
						t.Fatal("manual run did not create a session")
					}
					sessionID = *run.SessionID
					defer deleteSession(t, app, sessionID)
					expected["session.status_idled/"+sessionID]++
				}
			} else {
				_, stop := startDeploymentScheduler(t, app)
				defer stop()
				occurrence := time.Now().UTC()
				runWebhookScheduledJob(t, app, deployment.ID, occurrence)
				// A new River job for the same occurrence exercises rollback after Session insertion.
				runWebhookScheduledJob(t, app, deployment.ID, occurrence)
				ids := getDefaultDBIDs(t, app.pool)
				runs, _, err := app.db.ListDeploymentRunsPage(t.Context(), db.ListDeploymentRunsPageParams{
					WorkspaceUUID: ids.WorkspaceUUID, DeploymentExternalID: deployment.ID, Limit: 10,
				})
				if err != nil || len(runs) != 1 || runs[0].SessionExternalID == nil {
					t.Fatalf("scheduled runs = %v: %v", runs, err)
				}
				sessionID = *runs[0].SessionExternalID
				defer deleteSession(t, app, sessionID)
				assertDeploymentSessionCount(t, app, deployment.ID, 1)
				expected["session.status_idled/"+sessionID] = 1
			}
			// Subscribing to every relevant type exposes both main-thread and initial-outcome misreports.
			assertDeploymentWebhookTotal(t, app, len(expected))
			codeID := launchLocalCodeSession(t, app, sessionID)
			postCodeSessionIngressEvents(t, app, codeID, `{"events":[{"type":"span.outcome_evaluation_end","uuid":"deployment-evaluation-ended","created_at":"2026-09-20T01:00:00Z"}]}`)
			assertWebhookCount(t, app, "session.outcome_evaluation_ended", sessionID, 1)
			expected["session.outcome_evaluation_ended/"+sessionID] = 1
			assertWebhookDeliveries(t, app, endpoint, received, expected)
		})
	}
}

func newWebhookDeployment(t *testing.T, app *testApp) deploymentAPIResponse {
	t.Helper()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployment webhook"}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	environment := createEnvironment(t, app, `{"name":"deployment webhook"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	body := deploymentBodyWithInitialEvents(agent.ID, environment.ID, `[{"type":"user.define_outcome","description":"done","rubric":{"type":"text","content":"must pass"}},{"type":"user.message","content":[{"type":"text","text":"hello"}]}]`)
	deployment := createDeployment(t, app, body)
	t.Cleanup(func() { cleanupDeploymentRows(t, app, deployment.ID) })
	// A once-yearly schedule avoids incidental clock-triggered jobs during this test.
	updateDeployment(t, app, deployment.ID, `{"schedule":{"type":"cron","expression":"0 0 1 1 *","timezone":"UTC"}}`)
	return deployment
}

func runWebhookScheduledJob(t *testing.T, app *testApp, deploymentID string, occurrence time.Time) {
	t.Helper()
	client := app.deploymentJobs
	inserted, err := client.Insert(t.Context(), deploymentjobs.Args{
		WorkspaceUUID: getDefaultDBIDs(t, app.pool).WorkspaceUUID, DeploymentExternalID: deploymentID,
		Schedule: deploymentjobs.Schedule{Type: "cron", Expression: "0 0 1 1 *", Timezone: "UTC"},
	}, &river.InsertOpts{Queue: deploymentjobs.Queue, ScheduledAt: occurrence})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := client.JobDelete(context.Background(), inserted.Job.ID); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		job, err := client.JobGet(t.Context(), inserted.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == rivertype.JobStateCompleted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled job did not complete: %s", job.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertDeploymentSessionCount(t *testing.T, app *testApp, deploymentID string, want int) {
	t.Helper()
	var count int
	if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM sessions WHERE deployment_external_id=$1`, deploymentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("deployment sessions=%d want %d", count, want)
	}
}

func assertDeploymentWebhookTotal(t *testing.T, app *testApp, want int) {
	t.Helper()
	assertWebhookQueueCount(t, app, want)
}

func assertWebhookDeliveries(t *testing.T, app *testApp, endpoint webhookAPIResponse, received chan capturedWebhookRequest, expected map[string]int, inspectors ...func(*testing.T, []byte)) {
	t.Helper()
	drainWebhookQueue(t, app, webhooks.NewWorker(app.db, app.webhookQueue, app.cfg.Webhook, nil))
	sdk := anthropic.NewClient(option.WithWebhookKey(*endpoint.SigningSecret), option.WithAPIKey(defaultTestKey))
	scope, err := app.db.GetWorkspaceIdentifiers(t.Context(), getDefaultDBIDs(t, app.pool).WorkspaceUUID)
	if err != nil {
		t.Fatal(err)
	}
	for len(received) > 0 {
		delivery := <-received
		if _, err := sdk.Beta.Webhooks.Unwrap(delivery.Body, delivery.Header); err != nil {
			t.Fatal(err)
		}
		for _, inspect := range inspectors {
			inspect(t, delivery.Body)
		}
		var event webhooks.Event
		if err := json.Unmarshal(delivery.Body, &event); err != nil {
			t.Fatal(err)
		}
		if event.Data.WorkspaceID != scope.WorkspaceExternalID || event.Data.OrganizationID != scope.OrganizationUUID || event.ID != delivery.Header.Get("webhook-id") {
			t.Fatal("incorrect event scope or ID")
		}
		key := event.Data.Type + "/" + event.Data.ID
		expected[key]--
	}
	for key, remaining := range expected {
		if remaining != 0 {
			t.Errorf("delivery %s mismatch: %d", key, remaining)
		}
	}
}
