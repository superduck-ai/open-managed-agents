package tests

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

var agentWebhookEvents = []string{"agent.created", "agent.updated", "agent.archived"}

func TestWebhookAgentRejectedOperations(t *testing.T) {
	app, _, _ := newEventSubscription(t, agentWebhookEvents)
	agent := newWebhookAgent(t, app)
	const foreignKey = "sk-ant-agent-webhook-other"
	seedWorkspaceKey(t, app.pool, "agent_hook_org", "agent_hook_workspace", "agent_hook_key", foreignKey)
	base := "/v1/agents/" + agent.ID
	for _, tc := range []struct {
		name, path, body, key string
		status                int
	}{
		{"invalid create", "/v1/agents", `{"model":"claude-opus-4-6","name":""}`, defaultTestKey, 400},
		{"invalid field", base, `{"version":1,"name":123}`, defaultTestKey, 400},
		{"invalid tools", base, `{"version":1,"tools":[{"type":"mcp_toolset","mcp_server_name":"missing"}]}`, defaultTestKey, 400},
		{"missing version", base, `{"name":"changed"}`, defaultTestKey, 400},
		{"missing update", "/v1/agents/agent_missing", `{"version":1}`, defaultTestKey, 404},
		{"missing archive", "/v1/agents/agent_missing/archive", `{}`, defaultTestKey, 404},
		{"foreign update", base, `{"version":1,"name":"changed"}`, foreignKey, 404},
		{"foreign archive", base + "/archive", `{}`, foreignKey, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := doAgentRequest(t, app, "POST", tc.path+"?beta=true", strings.NewReader(tc.body), tc.key, true)
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d want %d: %s", response.StatusCode, tc.status, readAll(t, response.Body))
			}
			assertAgentWebhookCounts(t, app, agent.ID, 1, 0, 0)
		})
	}
	archived := archiveAgent(t, app, agent.ID)
	updateAgent(t, app, agent.ID, `{"version":1,"name":"cannot update"}`, 400)
	if got := retrieveAgent(t, app, agent.ID, ""); !reflect.DeepEqual(got, archived) {
		t.Fatal("rejected update changed archived Agent")
	}
	assertAgentWebhookCounts(t, app, agent.ID, 1, 0, 1)
	assertAgentWebhookTotal(t, app, 2)
}

func TestWebhookAgentVersionRollback(t *testing.T) {
	app, _, _ := newEventSubscription(t, agentWebhookEvents)
	agent := newWebhookAgent(t, app)
	remove := installWebhookMutationFailure(t, app, "agent_versions", "INSERT", "NEW.name = 'agent webhook rejected version'")
	defer remove()
	response := doAgentRequest(t, app, "POST", "/v1/agents?beta=true", strings.NewReader(`{"name":"agent webhook rejected version","model":"claude-opus-4-6"}`), defaultTestKey, true)
	assertError(t, response, 500, "api_error")
	updateAgent(t, app, agent.ID, `{"version":1,"name":"agent webhook rejected version"}`, 500)
	if after := retrieveAgent(t, app, agent.ID, ""); !reflect.DeepEqual(after, agent) {
		t.Fatal("failed version insert changed Agent")
	}
	var rows int
	if err := app.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM agents WHERE name=$1)+(SELECT count(*) FROM agent_versions WHERE name=$1)`, "agent webhook rejected version").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("partial writes=%d: %v", rows, err)
	}
	if versions := listAgentVersions(t, app, agent.ID, ""); len(versions.Data) != 1 {
		t.Fatalf("versions after rollback=%d", len(versions.Data))
	}
	assertAgentWebhookCounts(t, app, agent.ID, 1, 0, 0)
	assertAgentWebhookTotal(t, app, 1)
}

func TestWebhookAgentArchiveRollback(t *testing.T) {
	for _, stage := range []string{"deployment", "schedule"} {
		t.Run(stage, func(t *testing.T) {
			app, _, _ := newEventSubscription(t, agentWebhookEvents)
			agent := newWebhookAgent(t, app)
			deployments := newAgentWebhookDeployments(t, app, agent.ID)
			before, err := app.deploymentJobs.DurablePeriodicJobGet(t.Context(), deployments[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			var remove func()
			if stage == "deployment" {
				remove = installWebhookMutationFailure(t, app, "deployments", "UPDATE", "NEW.agent_external_id = '"+agent.ID+"'")
			} else {
				remove = rejectDeploymentScheduleWrites(t, app, deployments[0].ID)
			}
			defer remove()
			response := doAgentRequest(t, app, "POST", "/v1/agents/"+agent.ID+"/archive?beta=true", nil, defaultTestKey, true)
			assertError(t, response, 500, "api_error")
			if after := retrieveAgent(t, app, agent.ID, ""); !reflect.DeepEqual(after, agent) {
				t.Fatal("failed cascade changed Agent")
			}
			assertAgentWebhookDeployments(t, app, deployments, false)
			after, err := app.deploymentJobs.DurablePeriodicJobGet(t.Context(), deployments[0].ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed archive changed schedule: %v", err)
			}
			assertAgentWebhookCounts(t, app, agent.ID, 1, 0, 0)
		})
	}
}

func TestWebhookAgentNoopAndConcurrentUpdate(t *testing.T) {
	app, _, _ := newEventSubscription(t, agentWebhookEvents)
	agent := newWebhookAgent(t, app)
	for _, body := range []string{`{"version":1}`, `{"version":1,"metadata":{"b":"2","a":"1"}}`, `{"version":1,"name":"agent webhook"}`} {
		if got := updateAgent(t, app, agent.ID, body, 200); !reflect.DeepEqual(got, agent) {
			t.Fatal("no-op changed Agent or timestamp")
		}
	}
	assertAgentWebhookCounts(t, app, agent.ID, 1, 0, 0)
	var group sync.WaitGroup
	var published, conflicts atomic.Int32
	for i := range 2 {
		group.Go(func() {
			response := doAgentRequest(t, app, "POST", "/v1/agents/"+agent.ID+"?beta=true", strings.NewReader(fmt.Sprintf(`{"version":1,"name":"concurrent %d"}`, i)), defaultTestKey, true)
			defer response.Body.Close()
			switch response.StatusCode {
			case 200:
				published.Add(1)
			case 409:
				conflicts.Add(1)
			default:
				t.Errorf("unexpected update status=%d", response.StatusCode)
			}
		})
	}
	group.Wait()
	if published.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("published=%d conflicts=%d", published.Load(), conflicts.Load())
	}
	updateAgent(t, app, agent.ID, `{"version":1}`, 409)
	if versions := listAgentVersions(t, app, agent.ID, ""); len(versions.Data) != 2 {
		t.Fatalf("versions=%d", len(versions.Data))
	}
	assertAgentWebhookCounts(t, app, agent.ID, 1, 1, 0)
}

func TestWebhookAgentChangedFields(t *testing.T) {
	app, _, _ := newEventSubscription(t, agentWebhookEvents)
	agent := newWebhookAgent(t, app)
	child := createAgent(t, app, `{"name":"agent child","model":"claude-opus-4-6"}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, child.ID) })
	fields := []string{
		`"name":"renamed"`, `"description":"description"`, `"system":"instruction"`, `"metadata":{"a":"changed"}`,
		`"model":{"id":"claude-opus-4-6","speed":"fast"}`,
		`"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"web_fetch","enabled":false}]}]`,
		`"skills":[{"type":"anthropic","skill_id":"xlsx","version":"1"}]`,
		`"multiagent":{"type":"coordinator","agents":[{"id":"` + child.ID + `","type":"agent","version":1}]}`,
		`"mcp_servers":[{"type":"url","name":"server","url":"https://mcp.example.com"}],"tools":[{"type":"mcp_toolset","mcp_server_name":"server"}]`,
	}
	for i, field := range fields {
		updated := updateAgent(t, app, agent.ID, fmt.Sprintf(`{"version":%d,%s}`, i+1, field), 200)
		if updated.Version != i+2 {
			t.Fatalf("field %s version=%d", field, updated.Version)
		}
		assertAgentWebhookCounts(t, app, agent.ID, 1, i+1, 0)
	}
}

func TestWebhookAgentConcurrentArchiveAndDelivery(t *testing.T) {
	for _, withDeployments := range []bool{false, true} {
		t.Run(fmt.Sprintf("deployments=%t", withDeployments), func(t *testing.T) {
			app, endpoint, received := newEventSubscription(t, agentWebhookEvents)
			agent := newWebhookAgent(t, app)
			var deployments []deploymentAPIResponse
			if withDeployments {
				deployments = newAgentWebhookDeployments(t, app, agent.ID)
			}
			updateAgent(t, app, agent.ID, `{"version":1,"name":"published"}`, 200)
			var group sync.WaitGroup
			for range 8 {
				group.Go(func() { archiveAgent(t, app, agent.ID) })
			}
			group.Wait()
			before := retrieveAgent(t, app, agent.ID, "")
			if after := archiveAgent(t, app, agent.ID); !reflect.DeepEqual(before, after) {
				t.Fatal("repeated archive changed Agent")
			}
			listAgentVersions(t, app, agent.ID, "")
			assertAgentWebhookDeployments(t, app, deployments, true)
			assertAgentWebhookCounts(t, app, agent.ID, 1, 1, 1)
			assertAgentWebhookTotal(t, app, 3)
			expected := map[string]int{}
			for _, event := range agentWebhookEvents {
				expected[event+"/"+agent.ID] = 1
			}
			assertWebhookDeliveries(t, app, endpoint, received, expected, assertResourceWebhookPayload)
		})
	}
}

func TestWebhookAgentRepeatedArchiveKeepsCascade(t *testing.T) {
	app, _, _ := newEventSubscription(t, agentWebhookEvents)
	agent := newWebhookAgent(t, app)
	deployments := newAgentWebhookDeployments(t, app, agent.ID)
	before := archiveAgent(t, app, agent.ID)
	// Simulate an existing inconsistent child row: repeated Agent archival must retain
	// the existing cascade behavior even though the root itself is already archived.
	if _, err := app.pool.Exec(t.Context(), `UPDATE deployments SET archived_at=NULL WHERE workspace_uuid=$1 AND external_id=$2`, getDefaultDBIDs(t, app.pool).WorkspaceUUID, deployments[1].ID); err != nil {
		t.Fatal(err)
	}
	after := archiveAgent(t, app, agent.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("repeated archive changed Agent")
	}
	assertAgentWebhookDeployments(t, app, deployments, true)
	assertAgentWebhookCounts(t, app, agent.ID, 1, 0, 1)
	assertAgentWebhookTotal(t, app, 2)
}

func TestWebhookAgentFilteringAndFormerSDKIdentity(t *testing.T) {
	app, endpoint, _ := newEventSubscription(t, []string{"agent.archived"})
	agent := newWebhookAgent(t, app)
	updateAgent(t, app, agent.ID, `{"version":1,"description":"not subscribed"}`, 200)
	updateWebhook(t, app, endpoint.ID, `{"status":"disabled"}`)
	archiveAgent(t, app, agent.ID)
	updateWebhook(t, app, endpoint.ID, `{"status":"enabled","enabled_events":["agent.created","agent.updated","agent.archived"]}`)
	archiveAgent(t, app, agent.ID)
	assertAgentWebhookTotal(t, app, 0)
	const otherKey = "sk-ant-agent-webhook-filter"
	otherOrg, otherWorkspace := seedWorkspaceKey(t, app.pool, "agent_filter_org", "agent_filter_workspace", "agent_filter_key", otherKey)
	seedTestLLMProviderForWorkspace(t, app, otherOrg, otherWorkspace, "Agent webhook provider", "https://llm.example.com", "test-provider-key", "claude-opus-4-6")
	response := doAgentRequest(t, app, "POST", "/v1/agents?beta=true", strings.NewReader(`{"name":"foreign","model":"claude-opus-4-6"}`), otherKey, true)
	var other agentAPIResponse
	if response.StatusCode != 200 {
		t.Fatalf("foreign create: %s", readAll(t, response.Body))
	}
	decodeJSON(t, response.Body, &other)
	response.Body.Close()
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, other.ID) })
	for _, tc := range []struct{ id, path, body, key string }{
		{other.ID, "", `{"version":1,"name":"changed"}`, otherKey},
		{other.ID, "/archive", `{}`, otherKey},
	} {
		response = doAgentRequest(t, app, "POST", "/v1/agents/"+tc.id+tc.path+"?beta=true", strings.NewReader(tc.body), tc.key, true)
		if response.StatusCode != 200 {
			t.Errorf("operation status=%d", response.StatusCode)
		}
		response.Body.Close()
	}
	if err := app.db.Seed(t.Context(), []config.SeedAPIKey{{ExternalID: "api_key_official_sdk_resource_tests", Key: formerSDKKey}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "/archive"} {
		response = doAgentRequest(t, app, "POST", "/v1/agents/agent_011CZkYpogX7uDKUyvBTophP"+path+"?beta=true", strings.NewReader(`{"version":1}`), formerSDKKey, true)
		assertError(t, response, 404, "not_found_error")
	}
	assertAgentWebhookTotal(t, app, 0)
}

func newWebhookAgent(t *testing.T, app *testApp) agentAPIResponse {
	t.Helper()
	agent := createAgent(t, app, `{"name":"agent webhook","model":"claude-opus-4-6","metadata":{"a":"1","b":"2"}}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	return agent
}
func assertAgentWebhookCounts(t *testing.T, app *testApp, id string, created, updated, archived int) {
	t.Helper()
	for i, want := range []int{created, updated, archived} {
		assertWebhookCount(t, app, agentWebhookEvents[i], id, want)
	}
}
func assertAgentWebhookTotal(t *testing.T, app *testApp, want int) {
	t.Helper()
	var got int
	if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE type='webhook_delivery'`).Scan(&got); err != nil || got != want {
		t.Fatalf("jobs=%d want %d: %v", got, want, err)
	}
}
func newAgentWebhookDeployments(t *testing.T, app *testApp, agentID string) []deploymentAPIResponse {
	t.Helper()
	env := createEnvironment(t, app, `{"name":"agent webhook deployment"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, env.ID) })
	var result []deploymentAPIResponse
	for _, extra := range []string{`"schedule":{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`, `"name":"no schedule"`} {
		deployment := createDeployment(t, app, deploymentBodyWithExtra(agentID, env.ID, extra))
		t.Cleanup(func() { cleanupDeploymentRows(t, app, deployment.ID) })
		result = append(result, deployment)
	}
	return result
}
func assertAgentWebhookDeployments(t *testing.T, app *testApp, deployments []deploymentAPIResponse, archived bool) {
	t.Helper()
	scope := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	for i, deployment := range deployments {
		row, err := app.db.GetDeployment(context.Background(), scope, deployment.ID)
		if err != nil || (row.ArchivedAt != nil) != archived {
			t.Fatalf("deployment archived=%v error=%v", row.ArchivedAt, err)
		}
		if !archived && !reflect.DeepEqual(retrieveDeployment(t, app, deployment.ID), deployment) {
			t.Fatal("rolled-back deployment changed")
		}
		_, err = app.deploymentJobs.DurablePeriodicJobGet(t.Context(), deployment.ID)
		if i == 0 && !archived {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, rivertype.ErrNotFound) {
			t.Fatalf("unexpected schedule: %v", err)
		}
	}
}
