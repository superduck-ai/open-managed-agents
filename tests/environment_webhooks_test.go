package tests

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var environmentWebhookEvents = []string{"environment.created", "environment.updated", "environment.archived", "environment.deleted"}

func TestWebhookEnvironmentRejectedOperations(t *testing.T) {
	app, _, _ := newEventSubscription(t, environmentWebhookEvents)
	environment := createWebhookEnvironment(t, app, "environment notifications")
	const otherKey = "sk-ant-test-environment-webhook-other"
	seedWorkspaceKey(t, app.pool, "environment_webhook_other_org", "environment_webhook_other_workspace", "environment_webhook_other_key", otherKey)
	base := "/v1/environments/" + environment.ID
	for _, tc := range []struct {
		name, method, path, body, key, errorType string
		status                                   int
	}{
		{"invalid create", "POST", "/v1/environments", `{"name":"bad","config":{"type":"invalid"}}`, defaultTestKey, "invalid_request_error", 400},
		{"duplicate create", "POST", "/v1/environments", `{"name":"environment notifications"}`, defaultTestKey, "conflict_error", 409},
		{"invalid update", "POST", base, `{"name":123}`, defaultTestKey, "invalid_request_error", 400},
		{"missing update", "POST", "/v1/environments/env_missing", `{}`, defaultTestKey, "not_found_error", 404},
		{"missing archive", "POST", "/v1/environments/env_missing/archive", `{}`, defaultTestKey, "not_found_error", 404},
		{"missing delete", "DELETE", "/v1/environments/env_missing", `{}`, defaultTestKey, "not_found_error", 404},
		{"foreign update", "POST", base, `{"description":"blocked"}`, otherKey, "not_found_error", 404},
		{"foreign archive", "POST", base + "/archive", `{}`, otherKey, "not_found_error", 404},
		{"foreign delete", "DELETE", base, `{}`, otherKey, "not_found_error", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doEnvironmentRequest(t, app, tc.method, tc.path+"?beta=true", strings.NewReader(tc.body), tc.key, true)
			assertError(t, resp, tc.status, tc.errorType)
			assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 0, 0, 0)
		})
	}
	another := createWebhookEnvironment(t, app, "other environment")
	resp := doEnvironmentRequest(t, app, "POST", base+"?beta=true", strings.NewReader(`{"name":`+quoteJSON(another.Name)+`}`), defaultTestKey, true)
	assertError(t, resp, 409, "conflict_error")
	assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 0, 0, 0)
}

func TestWebhookEnvironmentDatabaseFailures(t *testing.T) {
	app, _, _ := newEventSubscription(t, environmentWebhookEvents)
	environment := createWebhookEnvironment(t, app, "environment transaction")
	before := loadWebhookEnvironment(t, app, environment.ID)
	trigger := pgx.Identifier{"test_environment_failure_" + uuid.NewV4().String()}.Sanitize()
	literal := "'" + strings.ReplaceAll(environment.ID, "'", "''") + "'"
	_, err := app.pool.Exec(t.Context(), `CREATE FUNCTION `+trigger+`() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.external_id = `+literal+` OR NEW.name = 'rejected webhook create' THEN RAISE EXCEPTION 'test write failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER `+trigger+` BEFORE INSERT OR UPDATE ON environments FOR EACH ROW EXECUTE FUNCTION `+trigger+`()`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := app.pool.Exec(context.Background(), `DROP TRIGGER `+trigger+` ON environments; DROP FUNCTION `+trigger+`() `); err != nil {
			t.Error(err)
		}
	}()
	base := "/v1/environments/" + environment.ID
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/v1/environments", `{"name":"rejected webhook create"}`},
		{"POST", base, `{"description":"not committed"}`},
		{"POST", base + "/archive", `{}`},
		{"DELETE", base, `{}`},
	} {
		resp := doEnvironmentRequest(t, app, tc.method, tc.path+"?beta=true", strings.NewReader(tc.body), defaultTestKey, true)
		assertError(t, resp, 500, "api_error")
	}
	after := loadWebhookEnvironment(t, app, environment.ID)
	if !before.UpdatedAt.Equal(after.UpdatedAt) || after.ArchivedAt != nil || before.Description != after.Description {
		t.Fatal("failed writes changed environment")
	}
	assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 0, 0, 0)
	assertWebhookQueueCount(t, app, 1)
}

func TestWebhookEnvironmentNoopAndAttributeChanges(t *testing.T) {
	app, _, _ := newEventSubscription(t, environmentWebhookEvents)
	environment := createWebhookEnvironment(t, app, "environment attributes")
	before := loadWebhookEnvironment(t, app, environment.ID)
	for _, body := range []string{`{}`, `{"name":"environment attributes","description":"","metadata":{},"scope":null}`, `{"config":{}}`, `{"config":null}`} {
		updateEnvironment(t, app, environment.ID, body, 200)
	}
	after := loadWebhookEnvironment(t, app, environment.ID)
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("no-op update changed timestamp")
	}
	assertWebhookCount(t, app, "environment.updated", environment.ID, 0)
	for index, body := range []string{
		`{"name":"environment attributes renamed"}`, `{"description":"updated"}`,
		`{"metadata":{"a":"1","b":"2"}}`, `{"scope":"account"}`, `{"scope":null}`,
		`{"config":{"packages":{"pip":["requests"]}}}`,
	} {
		updateEnvironment(t, app, environment.ID, body, 200)
		assertWebhookCount(t, app, "environment.updated", environment.ID, index+1)
		timestamp := loadWebhookEnvironment(t, app, environment.ID).UpdatedAt
		updateEnvironment(t, app, environment.ID, body, 200)
		if !timestamp.Equal(loadWebhookEnvironment(t, app, environment.ID).UpdatedAt) {
			t.Fatal("repeated update changed timestamp")
		}
		assertWebhookCount(t, app, "environment.updated", environment.ID, index+1)
	}
	updateEnvironment(t, app, environment.ID, `{"metadata":{"b":"2","a":"1"},"config":{"packages":{"pip":["requests"],"apt":[],"npm":[],"go":[],"cargo":[],"gem":[]},"type":"cloud"}}`, 200)
	assertWebhookCount(t, app, "environment.updated", environment.ID, 6)
	after = loadWebhookEnvironment(t, app, environment.ID)
	if after.Scope != nil {
		t.Fatal("scope:null must remain NULL in storage")
	}
	// A deployment template change still follows the existing config re-resolution rule.
	if _, err := app.pool.Exec(t.Context(), `UPDATE environments SET resolved_template='previous-template' WHERE uuid=$1`, after.UUID); err != nil {
		t.Fatal(err)
	}
	updateEnvironment(t, app, environment.ID, `{"config":{}}`, 200)
	restored := loadWebhookEnvironment(t, app, environment.ID)
	if restored.ResolvedTemplate != before.ResolvedTemplate {
		t.Fatal("config update did not re-resolve the template")
	}
	assertWebhookCount(t, app, "environment.updated", environment.ID, 7)
	archiveEnvironment(t, app, environment.ID)
	updateEnvironment(t, app, environment.ID, `{"description":"archived environments retain existing update behavior"}`, 200)
	assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 8, 1, 0)
}

func TestWebhookEnvironmentConcurrentArchiveAndUpdate(t *testing.T) {
	app, _, _ := newEventSubscription(t, environmentWebhookEvents)
	environment := createWebhookEnvironment(t, app, "concurrent environment")
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() { updateEnvironment(t, app, environment.ID, `{"description":"same update"}`, 200) })
	}
	group.Wait()
	assertWebhookCount(t, app, "environment.updated", environment.ID, 1)
	for range 8 {
		group.Go(func() { archiveEnvironment(t, app, environment.ID) })
	}
	group.Wait()
	before := loadWebhookEnvironment(t, app, environment.ID)
	archiveEnvironment(t, app, environment.ID)
	after := loadWebhookEnvironment(t, app, environment.ID)
	if !before.UpdatedAt.Equal(after.UpdatedAt) || before.ArchivedAt == nil || !before.ArchivedAt.Equal(*after.ArchivedAt) {
		t.Fatal("duplicate archive changed timestamps")
	}
	assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 1, 1, 0)
}

func TestWebhookEnvironmentWorkAndSandboxDoNotNotify(t *testing.T) {
	app, _, _ := newEventSubscription(t, environmentWebhookEvents)
	environment := createWebhookEnvironment(t, app, "environment work boundary")
	record := loadWebhookEnvironment(t, app, environment.ID)
	const envKey = "sk-ant-env-webhook-work"
	err := app.db.CreateEnvironmentKey(t.Context(), db.EnvironmentKey{ExternalID: "envkey_webhook_work", OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, EnvironmentUUID: record.UUID, EnvironmentExternalID: record.ExternalID}, auth.HashAPIKey(envKey))
	if err != nil {
		t.Fatal(err)
	}
	workID, _ := createEnvironmentWork(t, app, record)
	defer cleanupEnvironmentWorkRows(t, app.pool, workID)
	resp := doEnvironmentRequest(t, app, "DELETE", "/v1/environments/"+environment.ID+"?beta=true", nil, defaultTestKey, true)
	assertError(t, resp, 400, "invalid_request_error")
	pollEnvironmentWork(t, app, environment.ID, envKey)
	postEnvironmentWork(t, app, environment.ID, workID, "ack", nil, envKey)
	heartbeatEnvironmentWork(t, app, environment.ID, workID, "NO_HEARTBEAT", envKey)
	updateEnvironmentWork(t, app, environment.ID, workID, `{"metadata":{"state":"seen"}}`, envKey)
	sandbox, err := app.db.CreateEnvironmentSandbox(t.Context(), db.EnvironmentSandbox{
		UUID: uuid.NewV4().String(), ExternalID: "sandbox_" + uuid.NewV4().String(), OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID,
		EnvironmentUUID: record.UUID, EnvironmentExternalID: record.ExternalID, Provider: "e2b", Template: record.ResolvedTemplate, State: "creating", Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.db.UpdateEnvironmentSandboxState(t.Context(), record.WorkspaceUUID, sandbox.ExternalID, "running", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 0, 0, 0)
	if !record.UpdatedAt.Equal(loadWebhookEnvironment(t, app, environment.ID).UpdatedAt) {
		t.Fatal("work changed environment resource timestamp")
	}
	postEnvironmentWork(t, app, environment.ID, workID, "stop", strings.NewReader(`{"force":true}`), envKey)
	deleteEnvironment(t, app, environment.ID)
	assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 0, 0, 1)
}

func TestWebhookFormerSDKEnvironmentMissingOperationsDoNotNotify(t *testing.T) {
	app, _, _ := newEventSubscription(t, environmentWebhookEvents)
	if err := app.db.Seed(t.Context(), []config.SeedAPIKey{{ExternalID: "api_key_official_sdk_resource_tests", Key: formerSDKKey}}); err != nil {
		t.Fatal(err)
	}
	// The former SDK identity now follows ordinary resource lookup and notification rules.
	const base = "/v1/environments/env_011CZkZ9X2dpNyB7HsEFoRfW"
	for _, tc := range []struct{ method, path string }{{"POST", base}, {"POST", base + "/archive"}, {"DELETE", base}} {
		response := doEnvironmentRequest(t, app, tc.method, tc.path+"?beta=true", strings.NewReader(`{}`), formerSDKKey, true)
		assertError(t, response, 404, "not_found_error")
	}
	assertWebhookQueueCount(t, app, 0)
}

func TestWebhookEnvironmentSubscriptionFiltering(t *testing.T) {
	app, endpoint, _ := newEventSubscription(t, []string{"environment.archived"})
	environment := createWebhookEnvironment(t, app, "filtered environment")
	assertWebhookCount(t, app, "environment.created", environment.ID, 0)
	updateWebhook(t, app, endpoint.ID, `{"status":"disabled"}`)
	archiveEnvironment(t, app, environment.ID)
	updateWebhook(t, app, endpoint.ID, `{"status":"enabled","enabled_events":["environment.created","environment.archived"]}`)
	archiveEnvironment(t, app, environment.ID)
	assertEnvironmentWebhookCounts(t, app, environment.ID, 0, 0, 0, 0)
	const otherKey = "sk-ant-test-environment-webhook-foreign"
	seedWorkspaceKey(t, app.pool, "environment_webhook_foreign_org", "environment_webhook_foreign_workspace", "environment_webhook_foreign_key", otherKey)
	response := doEnvironmentRequest(t, app, "POST", "/v1/environments?beta=true", strings.NewReader(`{"name":"foreign environment"}`), otherKey, true)
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("foreign create: %s", readAll(t, response.Body))
	}
	var other environmentAPIResponse
	decodeJSON(t, response.Body, &other)
	defer cleanupEnvironmentRows(t, app.pool, other.ID)
	assertWebhookCount(t, app, "environment.created", other.ID, 0)
}

func TestWebhookEnvironmentEventDelivery(t *testing.T) {
	app, endpoint, received := newEventSubscription(t, environmentWebhookEvents)
	environment := createWebhookEnvironment(t, app, "verified environment")
	updateEnvironment(t, app, environment.ID, `{"description":"updated"}`, 200)
	archiveEnvironment(t, app, environment.ID)
	deleteEnvironment(t, app, environment.ID)
	response := doEnvironmentRequest(t, app, "DELETE", "/v1/environments/"+environment.ID+"?beta=true", nil, defaultTestKey, true)
	assertError(t, response, 404, "not_found_error")
	assertEnvironmentWebhookCounts(t, app, environment.ID, 1, 1, 1, 1)
	expected := map[string]int{}
	for _, event := range environmentWebhookEvents {
		expected[event+"/"+environment.ID] = 1
	}
	assertWebhookDeliveries(t, app, endpoint, received, expected)
}

func createWebhookEnvironment(t *testing.T, app *testApp, name string) environmentAPIResponse {
	t.Helper()
	environment := createEnvironment(t, app, `{"name":`+quoteJSON(name)+`}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	return environment
}

func loadWebhookEnvironment(t *testing.T, app *testApp, id string) db.Environment {
	t.Helper()
	environment, err := app.db.GetEnvironment(t.Context(), getDefaultDBIDs(t, app.pool).WorkspaceUUID, id)
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func assertEnvironmentWebhookCounts(t *testing.T, app *testApp, id string, created, updated, archived, deleted int) {
	t.Helper()
	for index, count := range []int{created, updated, archived, deleted} {
		assertWebhookCount(t, app, environmentWebhookEvents[index], id, count)
	}
}
