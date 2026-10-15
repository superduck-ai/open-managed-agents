package tests

import (
	"reflect"
	"testing"
)

func TestWebhookReservedDeletionsDoNotEmit(t *testing.T) {
	events := []string{"agent.deleted", "deployment.deleted"}
	app, endpoint, received := newEventSubscription(t, events)
	if got := retrieveWebhook(t, app, endpoint.ID); !reflect.DeepEqual(got.EnabledEvents, events) {
		t.Fatalf("reserved subscriptions not preserved: %v", got.EnabledEvents)
	}
	updated := updateWebhook(t, app, endpoint.ID, `{"enabled_events":["agent.deleted"]}`)
	if !reflect.DeepEqual(updated.EnabledEvents, events[:1]) {
		t.Fatalf("reserved subscription update: %v", updated.EnabledEvents)
	}
	updateWebhook(t, app, endpoint.ID, `{"enabled_events":["agent.deleted","deployment.deleted"]}`)
	if got := retrieveWebhook(t, app, endpoint.ID); !reflect.DeepEqual(got.EnabledEvents, events) {
		t.Fatalf("reserved subscription edit not persisted: %v", got.EnabledEvents)
	}

	agent := newWebhookAgent(t, app)
	updateAgent(t, app, agent.ID, `{"version":1,"name":"reserved deletion"}`, 200)
	deployments := newAgentWebhookDeployments(t, app, agent.ID)
	archiveDeployment(t, app, deployments[0].ID)
	archiveDeployment(t, app, deployments[0].ID)
	archiveAgent(t, app, agent.ID)
	archiveAgent(t, app, agent.ID)
	assertAgentWebhookDeployments(t, app, deployments, true)
	assertAgentWebhookTotal(t, app, 0)
	assertWebhookDeliveries(t, app, endpoint, received, map[string]int{})
}

func TestWebhookReservedBudgetDoesNotEmit(t *testing.T) {
	events := []string{"session.budget_reached"}
	app, endpoint, received := newEventSubscription(t, events)
	if got := retrieveWebhook(t, app, endpoint.ID); !reflect.DeepEqual(got.EnabledEvents, events) {
		t.Fatalf("reserved budget subscription not preserved: %v", got.EnabledEvents)
	}
	updateWebhook(t, app, endpoint.ID, `{"enabled_events":["agent.deleted"]}`)
	updated := updateWebhook(t, app, endpoint.ID, `{"enabled_events":["session.budget_reached"]}`)
	if got := retrieveWebhook(t, app, endpoint.ID); !reflect.DeepEqual(got.EnabledEvents, events) || !reflect.DeepEqual(updated.EnabledEvents, events) {
		t.Fatal("reserved budget subscription edit not persisted")
	}
	agent := newWebhookAgent(t, app)
	environment := createEnvironment(t, app, `{"name":"reserved budget"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	updateSession(t, app, session.ID, `{"title":"reserved budget"}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	postCodeSessionIngressEvents(t, app, codeID, `{"events":[{"type":"session.status_idle","uuid":"reserved-budget-idle","stop_reason":"budget_reached","created_at":"2026-09-23T01:00:00Z"}]}`)
	assertAgentWebhookTotal(t, app, 0)
	assertWebhookDeliveries(t, app, endpoint, received, map[string]int{})
}
