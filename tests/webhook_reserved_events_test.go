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
