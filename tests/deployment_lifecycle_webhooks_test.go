package tests

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var deploymentLifecycleEvents = []string{"deployment.created", "deployment.updated", "deployment.paused", "deployment.unpaused", "deployment.archived"}

func TestWebhookDeploymentLifecycleRejectedOperations(t *testing.T) {
	app, _, _ := newEventSubscription(t, deploymentLifecycleEvents)
	deployment := newWebhookDeployment(t, app)
	const foreignKey = "sk-ant-deployment-events-other"
	seedWorkspaceKey(t, app.pool, "deployment_events_org", "deployment_events_workspace", "deployment_events_key", foreignKey)
	for _, tc := range []struct {
		path, body, key string
		status          int
	}{
		{"/v1/deployments", `{}`, defaultTestKey, 400},
		{"/v1/deployments/" + deployment.ID, `{"name":123}`, defaultTestKey, 400},
		{"/v1/deployments/" + deployment.ID, `{"schedule":{"type":"cron","expression":"invalid"}}`, defaultTestKey, 400},
		{"/v1/deployments/" + deployment.ID, `{"name":"foreign"}`, foreignKey, 404},
	} {
		response := doDeploymentRequest(t, app, "POST", tc.path, strings.NewReader(tc.body), tc.key, true)
		assertError(t, response, tc.status, map[int]string{400: "invalid_request_error", 404: "not_found_error"}[tc.status])
	}
	for _, suffix := range []string{"", "/archive", "/pause", "/unpause"} {
		for _, target := range []struct{ id, key string }{{"depl_missing", defaultTestKey}, {deployment.ID, foreignKey}} {
			response := doDeploymentRequest(t, app, "POST", "/v1/deployments/"+target.id+suffix, strings.NewReader(`{}`), target.key, true)
			assertError(t, response, 404, "not_found_error")
		}
	}
	assertDeploymentWebhookTotal(t, app, 2)
	archiveDeployment(t, app, deployment.ID)
	response := doDeploymentRequest(t, app, "POST", "/v1/deployments/"+deployment.ID, strings.NewReader(`{"name":"archived"}`), defaultTestKey, true)
	assertError(t, response, 400, "invalid_request_error")
	assertDeploymentWebhookTotal(t, app, 3)
}

func TestWebhookDeploymentLifecycleRollback(t *testing.T) {
	for _, operation := range []string{"create", "update", "pause", "unpause", "archive", "agent archive", "automatic pause"} {
		t.Run(operation, func(t *testing.T) {
			app, _, _ := newEventSubscription(t, deploymentLifecycleEvents)
			deployment := newWebhookDeployment(t, app)
			scope := getDefaultDBIDs(t, app.pool).WorkspaceUUID
			current, err := app.db.GetDeployment(t.Context(), scope, deployment.ID)
			if err != nil {
				t.Fatal(err)
			}
			expected := 2
			if operation == "unpause" {
				pauseDeployment(t, app, deployment.ID)
				expected++
			}
			before := retrieveDeployment(t, app, deployment.ID)
			schedule, scheduleErr := app.deploymentJobs.DurablePeriodicJobGet(t.Context(), deployment.ID)
			var remove func()
			if operation == "create" {
				remove = installWebhookMutationFailure(t, app, "deployments", "INSERT", "NEW.name = 'rejected webhook deployment'")
			} else {
				remove = rejectDeploymentScheduleWrites(t, app, deployment.ID)
			}
			defer remove()
			path := "/v1/deployments/" + deployment.ID
			body := `{}`
			switch operation {
			case "create":
				path = "/v1/deployments"
				body = deploymentBodyWithExtra(current.AgentExternalID, current.EnvironmentExternalID, `"name":"rejected webhook deployment"`)
			case "update":
				body = `{"schedule":{"type":"cron","expression":"0 0 2 1 *","timezone":"UTC"}}`
			case "agent archive":
				path = "/v1/agents/" + current.AgentExternalID + "/archive?beta=true"
			case "automatic pause":
				err = app.deployments.ApplyScheduledOccurrence(t.Context(), db.ApplyScheduledOccurrenceInput{
					Deployment: current, ScheduledAt: time.Now().UTC(), Now: time.Now().UTC(),
					Run:             db.DeploymentRun{ExternalID: "drun_rollback", UUID: "f69cb9ac-f0e5-4c94-aef5-dcba119bb48a", Error: []byte(`{"type":"unknown_error"}`)},
					AutoPauseReason: []byte(`{"type":"error"}`),
				})
				if err == nil {
					t.Fatal("expected schedule rollback")
				}
			default:
				path += "/" + operation
			}
			if operation != "automatic pause" {
				response := doDeploymentRequest(t, app, "POST", path, strings.NewReader(body), defaultTestKey, true)
				assertError(t, response, 500, "api_error")
			}
			if after := retrieveDeployment(t, app, deployment.ID); !reflect.DeepEqual(before, after) {
				t.Fatal("rolled back operation changed deployment")
			}
			afterSchedule, afterErr := app.deploymentJobs.DurablePeriodicJobGet(t.Context(), deployment.ID)
			if !reflect.DeepEqual(schedule, afterSchedule) || !errors.Is(afterErr, scheduleErr) {
				t.Fatalf("schedule changed: %v", afterErr)
			}
			if operation == "agent archive" && retrieveAgent(t, app, current.AgentExternalID, "").ArchivedAt != nil {
				t.Fatal("Agent archive escaped rollback")
			}
			if runs := listDeploymentRuns(t, app, "deployment_id="+deployment.ID); len(runs.Data) != 0 {
				t.Fatal("run escaped rollback")
			}
			assertDeploymentWebhookTotal(t, app, expected)
		})
	}
}

func TestWebhookDeploymentLifecycleConcurrentDelivery(t *testing.T) {
	app, endpoint, received := newEventSubscription(t, deploymentLifecycleEvents)
	deployment := newWebhookDeployment(t, app)
	deployment = updateDeployment(t, app, deployment.ID, `{"metadata":{"a":"1","b":"2"}}`)
	for _, body := range []string{`{}`, `{"metadata":{"b":"2","a":"1"}}`, `{"name":"initial events deployment"}`, `{"schedule":{"timezone":"UTC","expression":"0 0 1 1 *","type":"cron"}}`} {
		if got := updateDeployment(t, app, deployment.ID, body); !reflect.DeepEqual(deployment, got) {
			t.Fatal("no-op update changed deployment")
		}
	}
	if got := unpauseDeployment(t, app, deployment.ID); !reflect.DeepEqual(deployment, got) {
		t.Fatal("no-op unpause changed deployment")
	}
	for _, operation := range []struct {
		event string
		call  func(*testing.T, *testApp, string) deploymentAPIResponse
	}{
		{"paused", pauseDeployment}, {"unpaused", unpauseDeployment}, {"archived", archiveDeployment},
	} {
		var group sync.WaitGroup
		for range 6 {
			group.Go(func() { operation.call(t, app, deployment.ID) })
		}
		group.Wait()
		before := retrieveDeployment(t, app, deployment.ID)
		if after := operation.call(t, app, deployment.ID); !reflect.DeepEqual(before, after) {
			t.Fatal("repeat operation changed timestamp")
		}
		assertWebhookCount(t, app, "deployment."+operation.event, deployment.ID, 1)
	}
	if _, err := app.deploymentJobs.DurablePeriodicJobGet(t.Context(), deployment.ID); !errors.Is(err, rivertype.ErrNotFound) {
		t.Fatalf("archived schedule remains: %v", err)
	}
	assertWebhookDeliveries(t, app, endpoint, received, map[string]int{
		"deployment.created/" + deployment.ID: 1, "deployment.updated/" + deployment.ID: 2,
		"deployment.paused/" + deployment.ID: 1, "deployment.unpaused/" + deployment.ID: 1, "deployment.archived/" + deployment.ID: 1,
	}, assertResourceWebhookPayload)
}

func TestWebhookDeploymentGitTokenChanges(t *testing.T) {
	app, _, _ := newEventSubscription(t, deploymentLifecycleEvents)
	deployment := newWebhookDeployment(t, app)
	scope := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	wantUpdates := 1
	for _, tc := range []struct {
		name, fields string
		changed      bool
	}{
		{"initial token", `,"authorization_token":"first-test-token"`, true},
		{"same token", `,"authorization_token":"first-test-token"`, false},
		{"new token", `,"authorization_token":"second-test-token"`, true},
		{"omitted token removes credential", "", true},
		{"still absent", "", false},
		{"restore token", `,"authorization_token":"second-test-token"`, true},
		{"empty token removes credential", `,"authorization_token":""`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := app.db.GetDeployment(t.Context(), scope, deployment.ID)
			if err != nil {
				t.Fatal(err)
			}
			updateDeployment(t, app, deployment.ID, `{"resources":[{"type":"github_repository","url":"https://github.com/example/repo"`+tc.fields+`}]}`)
			after, err := app.db.GetDeployment(t.Context(), scope, deployment.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.changed {
				wantUpdates++
				if before.UpdatedAt.Equal(after.UpdatedAt) || string(before.ResourceSecrets) == string(after.ResourceSecrets) {
					t.Fatal("credential change was not persisted")
				}
			} else if !reflect.DeepEqual(before, after) {
				t.Fatal("same credential changed the stored deployment")
			}
			assertWebhookCount(t, app, "deployment.updated", deployment.ID, wantUpdates)
		})
	}
}

func TestWebhookDeploymentLifecycleCascade(t *testing.T) {
	app, endpoint, received := newEventSubscription(t, deploymentLifecycleEvents)
	agent := newWebhookAgent(t, app)
	deployments := newAgentWebhookDeployments(t, app, agent.ID)
	// Direct archive followed by root archive must not notify the same child twice.
	archiveDeployment(t, app, deployments[1].ID)
	archiveAgent(t, app, agent.ID)
	archiveAgent(t, app, agent.ID)
	expected := map[string]int{}
	for _, deployment := range deployments {
		expected["deployment.created/"+deployment.ID] = 1
		expected["deployment.archived/"+deployment.ID] = 1
	}
	assertAgentWebhookDeployments(t, app, deployments, true)
	assertWebhookDeliveries(t, app, endpoint, received, expected, assertResourceWebhookPayload)
}

func TestWebhookDeploymentLifecycleScheduledTransitions(t *testing.T) {
	for _, mode := range []string{"automatic pause worker", "archive occurrence", "stale occurrence"} {
		t.Run(mode, func(t *testing.T) {
			app, endpoint, received := newEventSubscription(t, deploymentLifecycleEvents)
			deployment := newWebhookDeployment(t, app)
			expected := map[string]int{"deployment.created/" + deployment.ID: 1, "deployment.updated/" + deployment.ID: 1}
			if mode == "automatic pause worker" {
				archiveEnvironment(t, app, deployment.EnvironmentID)
				_, stop := startDeploymentScheduler(t, app)
				defer stop()
				occurrence := time.Now().UTC()
				runWebhookScheduledJob(t, app, deployment.ID, occurrence)
				runWebhookScheduledJob(t, app, deployment.ID, occurrence)
				expected["deployment.paused/"+deployment.ID] = 1
				// Changing the reason on an already paused deployment is not another pause transition.
				pauseDeployment(t, app, deployment.ID)
				assertWebhookCount(t, app, "deployment.paused", deployment.ID, 1)
			} else {
				scope := getDefaultDBIDs(t, app.pool).WorkspaceUUID
				current, err := app.db.GetDeployment(t.Context(), scope, deployment.ID)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "stale occurrence" {
					pauseDeployment(t, app, deployment.ID)
					expected["deployment.paused/"+deployment.ID] = 1
				}
				err = app.deployments.ApplyScheduledOccurrence(t.Context(), db.ApplyScheduledOccurrenceInput{Deployment: current, ArchiveDeployment: true})
				if mode == "archive occurrence" {
					if err != nil {
						t.Fatal(err)
					}
					expected["deployment.archived/"+deployment.ID] = 1
				} else if !errors.Is(err, db.ErrStaleSchedule) {
					t.Fatalf("stale=%v", err)
				}
				if err = app.deployments.ApplyScheduledOccurrence(t.Context(), db.ApplyScheduledOccurrenceInput{Deployment: current, ArchiveDeployment: true}); !errors.Is(err, db.ErrStaleSchedule) {
					t.Fatalf("repeat=%v", err)
				}
			}
			assertWebhookDeliveries(t, app, endpoint, received, expected, assertResourceWebhookPayload)
		})
	}
}

func TestWebhookDeploymentLifecycleFilteringAndRunBoundary(t *testing.T) {
	app, endpoint, received := newEventSubscription(t, []string{"session.updated"})
	deployment := newWebhookDeployment(t, app)
	for _, status := range []string{"disabled", "enabled"} {
		events := `["deployment.created"]`
		if status == "disabled" {
			events = `["deployment.created","deployment.updated","deployment.paused","deployment.unpaused","deployment.archived"]`
		}
		updateWebhook(t, app, endpoint.ID, `{"status":"`+status+`","enabled_events":`+events+`}`)
		updateDeployment(t, app, deployment.ID, `{"name":"`+status+`"}`)
		pauseDeployment(t, app, deployment.ID)
		unpauseDeployment(t, app, deployment.ID)
	}
	updateWebhook(t, app, endpoint.ID, `{"enabled_events":["deployment.created","deployment.updated","deployment.paused","deployment.unpaused","deployment.archived"]}`)
	before := retrieveDeployment(t, app, deployment.ID)
	run := runDeployment(t, app, deployment.ID)
	if run.SessionID == nil {
		t.Fatal("manual session missing")
	}
	defer deleteSession(t, app, *run.SessionID)
	retrieveDeployment(t, app, deployment.ID)
	listDeploymentRuns(t, app, "deployment_id="+deployment.ID)
	for _, event := range deploymentLifecycleEvents {
		assertWebhookCount(t, app, event, deployment.ID, 0)
	}
	// Filtered mutations are not replayed when enabling the subscription.
	assertWebhookDeliveries(t, app, endpoint, received, map[string]int{})
	if before.Status != "active" {
		t.Fatal("unexpected status")
	}
}

func TestWebhookDeploymentLifecycleChangedProperties(t *testing.T) {
	app, _, _ := newEventSubscription(t, deploymentLifecycleEvents)
	deployment := newWebhookDeployment(t, app)
	agent := newWebhookAgent(t, app)
	environment := createEnvironment(t, app, `{"name":"deployment replacement"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	for i, body := range []string{
		`{"name":"changed"}`, `{"description":"changed"}`, `{"description":null}`,
		`{"metadata":{"key":"value"}}`, `{"agent":"` + agent.ID + `"}`,
		`{"environment_id":"` + environment.ID + `"}`,
		`{"initial_events":[{"type":"user.message","content":[{"type":"text","text":"changed"}]}]}`,
		`{"schedule":null}`,
	} {
		before := updateDeployment(t, app, deployment.ID, body)
		if after := updateDeployment(t, app, deployment.ID, body); !reflect.DeepEqual(before, after) {
			t.Fatal("same property value changed timestamp")
		}
		assertWebhookCount(t, app, "deployment.updated", deployment.ID, i+2)
	}
	var group sync.WaitGroup
	for range 6 {
		group.Go(func() { updateDeployment(t, app, deployment.ID, `{"name":"concurrent same value"}`) })
	}
	group.Wait()
	assertWebhookCount(t, app, "deployment.updated", deployment.ID, 10)
}

func TestWebhookDeploymentLifecycleOtherWorkspaceDoesNotNotify(t *testing.T) {
	app, _, _ := newEventSubscription(t, deploymentLifecycleEvents)
	const key = "sk-ant-deployment-webhook-foreign"
	org, workspace := seedWorkspaceKey(t, app.pool, "deployment_notify_org", "deployment_notify_workspace", "deployment_notify_key", key)
	seedTestLLMProviderForWorkspace(t, app, org, workspace, "Deployment webhook provider", "https://llm.example.com", "test-provider-key", "claude-opus-4-6")
	response := doAgentRequest(t, app, "POST", "/v1/agents?beta=true", strings.NewReader(`{"name":"foreign","model":"claude-opus-4-6"}`), key, true)
	if response.StatusCode != 200 {
		t.Fatalf("agent: %s", readAll(t, response.Body))
	}
	var agent agentAPIResponse
	decodeJSON(t, response.Body, &agent)
	response.Body.Close()
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	response = doEnvironmentRequest(t, app, "POST", "/v1/environments?beta=true", strings.NewReader(`{"name":"foreign"}`), key, true)
	if response.StatusCode != 200 {
		t.Fatalf("environment: %s", readAll(t, response.Body))
	}
	var environment environmentAPIResponse
	decodeJSON(t, response.Body, &environment)
	response.Body.Close()
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	response = doDeploymentRequest(t, app, "POST", "/v1/deployments", strings.NewReader(minimalDeploymentBody(agent.ID, environment.ID)), key, true)
	if response.StatusCode != 200 {
		t.Fatalf("deployment: %s", readAll(t, response.Body))
	}
	var deployment deploymentAPIResponse
	decodeJSON(t, response.Body, &deployment)
	response.Body.Close()
	t.Cleanup(func() { cleanupDeploymentRows(t, app, deployment.ID) })
	for _, op := range []struct{ suffix, body string }{{"", `{"name":"changed"}`}, {"/pause", `{}`}, {"/unpause", `{}`}, {"/archive", `{}`}} {
		response = doDeploymentRequest(t, app, "POST", "/v1/deployments/"+deployment.ID+op.suffix, strings.NewReader(op.body), key, true)
		if response.StatusCode != 200 {
			t.Errorf("operation status=%d", response.StatusCode)
		}
		response.Body.Close()
	}
	assertDeploymentWebhookTotal(t, app, 0)
}
