package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

var subscriptionEvents = []string{
	"session.status_run_started", "session.status_rescheduled", "session.status_idled", "session.status_terminated",
	"session.thread_created", "session.thread_idled", "session.thread_terminated", "session.outcome_evaluation_ended",
	"session.updated", "session.deleted", "vault.created", "vault.archived", "vault.deleted",
	"vault_credential.created", "vault_credential.archived", "vault_credential.deleted", "vault_credential.refresh_failed",
}

func TestWebhookSubscriptionRejectsLegacyEvents(t *testing.T) {
	app, _, _ := newEventSubscription(t, subscriptionEvents)
	for _, event := range []string{"session.created", "session.pending", "session.error", "session.thread_status_idle", "session.thread_status_running", "session.thread_status_rescheduled", "session.thread_status_terminated", "session.record_updated"} {
		resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks", strings.NewReader(`{"url":"https://example.com/hook","enabled_events":[`+quoteJSON(event)+`]}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	}
}

func TestWebhookResourceEventDelivery(t *testing.T) {
	app, endpoint, received := newEventSubscription(t, subscriptionEvents)
	vault := createVault(t, app, `{"display_name":"event matrix"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	credential := createVaultCredential(t, app, vault.ID, staticBearerBody("archive directly", "https://mcp.example.com/direct", "test-secret"))
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"webhook events"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	environment := createEnvironment(t, app, `{"name":"webhook events"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`,"vault_ids":[`+quoteJSON(vault.ID)+`]}`)
	assertWebhookCount(t, app, "session.thread_created", session.ID, 0)
	assertWebhookCount(t, app, "session.thread_idled", session.ID, 0)
	// Rejected and no-op updates cannot claim a resource change.
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"?beta=true", strings.NewReader(`{"title":123}`), defaultTestKey, true)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	updateSession(t, app, session.ID, `{}`)
	assertWebhookCount(t, app, "session.updated", session.ID, 0)
	updateSession(t, app, session.ID, `{"title":"changed","metadata":{"a":"1","b":"2"}}`)
	updateSession(t, app, session.ID, `{"title":"changed","metadata":{"b":"2","a":"1"}}`)
	assertWebhookCount(t, app, "session.updated", session.ID, 1)
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.define_outcome","description":"done","rubric":{"type":"text","text":"must pass"}}]}`, defaultTestKey)
	assertWebhookCount(t, app, "session.outcome_evaluation_ended", session.ID, 0)
	codeID := launchLocalCodeSession(t, app, session.ID)
	threadID := "sthr_webhook_" + strings.TrimPrefix(session.ID, "sesn_")
	ingress := `{"events":[
 {"type":"session.status_running","uuid":"running","created_at":"2026-09-20T01:00:00Z"},
 {"type":"session.status_rescheduled","uuid":"rescheduled","created_at":"2026-09-20T01:00:01Z"},
 {"type":"session.status_idle","uuid":"idle","created_at":"2026-09-20T01:00:02Z"},
 {"type":"session.thread_created","uuid":"child-created","session_thread_id":` + quoteJSON(threadID) + `,"created_at":"2026-09-20T01:00:03Z"},
 {"type":"session.thread_status_running","uuid":"child-running","session_thread_id":` + quoteJSON(threadID) + `,"created_at":"2026-09-20T01:00:03.5Z"},
 {"type":"session.thread_status_idle","uuid":"child-idle","session_thread_id":` + quoteJSON(threadID) + `,"created_at":"2026-09-20T01:00:04Z"},
 {"type":"span.outcome_evaluation_end","uuid":"outcome-ended","created_at":"2026-09-20T01:00:05Z"}
 ]}`
	postCodeSessionIngressEvents(t, app, codeID, ingress)
	postCodeSessionIngressEvents(t, app, codeID, ingress)
	matches := 0
	for _, event := range queuedWebhookEvents(t, app) {
		if event.Data.ID == session.ID && event.Data.Type == "session.status_run_started" && event.CreatedAt == "2026-09-20T01:00:00Z" {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("persisted occurrence matches=%d", matches)
	}
	assertWebhookCount(t, app, "session.status_run_started", session.ID, 2)
	assertWebhookCount(t, app, "session.thread_created", session.ID, 1)
	assertWebhookCount(t, app, "session.thread_idled", session.ID, 1)
	assertWebhookCount(t, app, "session.outcome_evaluation_ended", session.ID, 1)
	key, err := app.db.GetAPIKey(t.Context(), auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatal(err)
	}
	primary, found, err := app.db.GetPrimarySessionThread(t.Context(), key.WorkspaceUUID.String(), session.ID)
	if err != nil || !found {
		t.Fatal("primary thread not found")
	}
	postCodeSessionIngressEvents(t, app, codeID, `{"events":[{"type":"session.thread_status_idle","uuid":"primary-idle","session_thread_id":`+quoteJSON(primary.ExternalID)+`,"created_at":"2026-09-20T01:00:06Z"}]}`)
	assertWebhookCount(t, app, "session.thread_idled", session.ID, 1)

	archivedThread := archiveSessionThread(t, app, session.ID, threadID)
	repeatedThread := archiveSessionThread(t, app, session.ID, threadID)
	if archivedThread.ArchivedAt == nil || repeatedThread.ArchivedAt == nil || *archivedThread.ArchivedAt != *repeatedThread.ArchivedAt {
		t.Fatal("repeat thread archive changed its timestamp")
	}
	threadHistory := listThreadEvents(t, app, session.ID, threadID, defaultTestKey)
	terminationCount := 0
	for _, event := range threadHistory.Data {
		if sessionEventStringField(t, event, "type") == "session.thread_status_terminated" {
			terminationCount++
		}
	}
	if terminationCount != 1 {
		t.Fatalf("thread archive termination history: %s", threadHistory.Data)
	}
	assertWebhookCount(t, app, "session.thread_terminated", session.ID, 1)
	triggerOAuthRefreshFailure(t, app, vault.ID, codeID)
	archiveVaultCredential(t, app, vault.ID, credential.ID)
	archiveVaultCredential(t, app, vault.ID, credential.ID)
	assertWebhookCount(t, app, "vault_credential.archived", credential.ID, 1)
	archiveVault(t, app, vault.ID)
	archiveVault(t, app, vault.ID)
	assertWebhookCount(t, app, "vault.archived", vault.ID, 1)
	assertWebhookCount(t, app, "vault_credential.archived", credential.ID, 1)
	deleteVault(t, app, vault.ID)
	assertWebhookCount(t, app, "vault_credential.deleted", credential.ID, 1)
	archiveSession(t, app, session.ID)
	archiveSession(t, app, session.ID)
	assertWebhookCount(t, app, "session.status_terminated", session.ID, 1)
	assertWebhookCount(t, app, "session.thread_terminated", session.ID, 1)
	deleteSession(t, app, session.ID)
	assertWebhookCount(t, app, "session.deleted", session.ID, 1)
	assertDeliveredEventMatrix(t, app, endpoint, received)
}

func triggerOAuthRefreshFailure(t *testing.T, app *testApp, vaultID, codeID string) {
	t.Helper()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer tokenServer.Close()
	target := "https://mcp.example.com/oauth"
	credential := createVaultCredential(t, app, vaultID, `{"display_name":"expired oauth","auth":{"type":"mcp_oauth","mcp_server_url":`+quoteJSON(target)+`,"access_token":"expired-test-token","expires_at":"2020-01-01T00:00:00Z","refresh":{"token_endpoint":`+quoteJSON(tokenServer.URL)+`,"client_id":"test-client","refresh_token":"invalid-test-token","token_endpoint_auth":{"type":"none"}}}}`)
	key, err := app.db.GetAPIKey(t.Context(), auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatal(err)
	}
	injector := vaults.NewInjector(app.db, app.vaultSecrets, nil).WithWebhooks(webhooks.NewEnqueuer(app.db, app.webhookQueue, nil))
	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	transport := injector.WrapTransport(t.Context(), codeID, key.OrganizationUUID.String(), key.WorkspaceUUID.String(), targetURL, http.DefaultTransport)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := transport.RoundTrip(request); err == nil {
		response.Body.Close()
		t.Fatal("expired credential must reject the MCP request")
	}
	assertWebhookCount(t, app, "vault_credential.refresh_failed", credential.ID, 1)
}

func TestWebhookVaultDeleteIncludesAllArchivedCredentials(t *testing.T) {
	app, _, _ := newEventSubscription(t, []string{"vault_credential.deleted"})
	vault := createVault(t, app, `{"display_name":"large archived vault"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	// Archived credentials can accumulate beyond the 20 active-credential limit.
	_, err := app.pool.Exec(t.Context(), `INSERT INTO vault_credentials (external_id, organization_uuid, workspace_uuid, vault_uuid, vault_external_id, display_name, auth_type, credential_key, auth, metadata, archived_at)
 SELECT 'vcrd_matrix_' || replace(v.uuid::text,'-','') || '_' || n, v.organization_uuid,v.workspace_uuid,v.uuid,v.external_id,'archived','static_bearer','archived-' || n,'{}'::jsonb,'{}'::jsonb,NOW()
 FROM vaults v CROSS JOIN generate_series(1,1001) n WHERE v.external_id=$1`, vault.ID)
	if err != nil {
		t.Fatal(err)
	}
	deleteVault(t, app, vault.ID)
	count := 0
	for _, event := range queuedWebhookEvents(t, app) {
		if event.Data.VaultID != nil && *event.Data.VaultID == vault.ID {
			count++
		}
	}
	if count != 1001 {
		t.Fatalf("cascade messages=%d want 1001", count)
	}

}

func newEventSubscription(t *testing.T, events []string) (*testApp, webhookAPIResponse, chan capturedWebhookRequest) {
	t.Helper()
	return newEventSubscriptionWithStore(t, events, newFakeStore("webhook-event-matrix"))
}

func newEventSubscriptionWithStore(t *testing.T, events []string, store storage.ObjectStore) (*testApp, webhookAPIResponse, chan capturedWebhookRequest) {
	t.Helper()
	received := make(chan capturedWebhookRequest, 128)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		received <- capturedWebhookRequest{Header: r.Header.Clone(), Body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Webhook.WorkerEnabled {
		t.Fatal("database subscriptions require worker enabled by default")
	}

	cfg.Webhook.AllowInsecure = true
	cfg.Webhook.Timeout = time.Second
	app := newTestAppWithStore(t, &cfg, store)
	t.Cleanup(app.close)
	clearWebhookState(t, app)
	t.Cleanup(func() { clearWebhookState(t, app) })
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := createWebhook(t, app, `{"url":`+quoteJSON(receiver.URL)+`,"enabled_events":`+string(raw)+`}`)
	return app, endpoint, received
}

func assertWebhookCount(t *testing.T, app *testApp, event, resourceID string, want int) {
	t.Helper()
	if got := webhookJobCount(t, app, event, resourceID); got != want {
		t.Fatalf("%s for %s: jobs=%d want %d", event, resourceID, got, want)
	}
}

func assertDeliveredEventMatrix(t *testing.T, app *testApp, endpoint webhookAPIResponse, received chan capturedWebhookRequest) {
	t.Helper()
	worker := webhooks.NewWorker(app.db, app.webhookQueue, app.cfg.Webhook, nil)
	drainWebhookQueue(t, app, worker)

	sdk := anthropic.NewClient(option.WithWebhookKey(*endpoint.SigningSecret), option.WithAPIKey(defaultTestKey))
	seen := map[string]bool{}
	for len(received) > 0 {
		delivery := <-received
		if _, err := sdk.Beta.Webhooks.Unwrap(delivery.Body, delivery.Header); err != nil {
			t.Fatalf("SDK verification: %v", err)
		}
		var event webhooks.Event
		if err := json.Unmarshal(delivery.Body, &event); err != nil {
			t.Fatal(err)
		}
		if event.ID != delivery.Header.Get("webhook-id") || event.Data.ID == "" || event.Data.WorkspaceID == "" || event.Data.OrganizationID == "" {
			t.Fatal("missing or inconsistent envelope metadata")
		}
		if strings.HasPrefix(event.Data.Type, "session.thread_") && event.Data.SessionThreadID == nil {
			t.Fatal("thread event missing thread ID")
		}
		if strings.HasPrefix(event.Data.Type, "vault_credential.") && event.Data.VaultID == nil {
			t.Fatal("credential event missing vault ID")
		}
		seen[event.Data.Type] = true
	}
	for _, event := range subscriptionEvents {
		if !seen[event] {
			t.Errorf("no verified delivery for %s", event)
		}
	}
}

func TestWebhookSubscriptionWorkerStart(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			app, _, received := newEventSubscription(t, []string{"vault.created"})
			vault := createVault(t, app, `{"display_name":"worker start"}`)
			defer cleanupVaultRows(t, app, vault.ID)

			cfg := app.cfg.Webhook
			cfg.WorkerEnabled = enabled
			stopWorker := startWebhookWorker(t, webhooks.NewWorker(app.db, app.webhookQueue, cfg, nil))
			defer stopWorker()
			wait := 100 * time.Millisecond
			if enabled {
				wait = 3 * time.Second
			}
			select {
			case <-received:
				if !enabled {
					t.Fatal("disabled worker sent a notification")
				}
			case <-time.After(wait):
				if enabled {
					t.Fatal("default worker did not deliver database subscription")
				}
			}
		})
	}
}

func TestWebhookSubscriptionsFilterResourceEvents(t *testing.T) {
	app, endpoint, _ := newEventSubscription(t, []string{"vault.archived"})
	vault := createVault(t, app, `{"display_name":"filtered events"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	assertWebhookCount(t, app, "vault.created", vault.ID, 0)
	const otherKey = "sk-ant-test-webhook-event-other"
	seedWorkspaceKey(t, app.pool, "webhook_event_other_org", "webhook_event_other_workspace", "webhook_event_other_key", otherKey)
	denied := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/archive?beta=true", nil, otherKey, true)
	assertError(t, denied, http.StatusNotFound, "not_found_error")
	assertWebhookCount(t, app, "vault.archived", vault.ID, 0)
	updateWebhook(t, app, endpoint.ID, `{"status":"disabled"}`)
	archiveVault(t, app, vault.ID)
	assertWebhookCount(t, app, "vault.archived", vault.ID, 0)
	updateWebhook(t, app, endpoint.ID, `{"status":"enabled","enabled_events":["vault.created","vault.archived"]}`)
	// Enabling a subscription does not backfill an earlier state transition.
	archiveVault(t, app, vault.ID)
	assertWebhookCount(t, app, "vault.archived", vault.ID, 0)
	response := doVaultRequest(t, app, http.MethodPost, "/v1/vaults?beta=true", strings.NewReader(`{"display_name":"other workspace"}`), otherKey, true)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("other workspace vault creation failed")
	}
	var otherVault vaultAPIResponse
	decodeJSON(t, response.Body, &otherVault)
	defer cleanupVaultRows(t, app, otherVault.ID)
	assertWebhookCount(t, app, "vault.created", otherVault.ID, 0)
}

func TestWebhookConcurrentArchiveNotifiesOnce(t *testing.T) {
	app, _, _ := newEventSubscription(t, []string{"vault.archived", "vault_credential.archived"})
	vault := createVault(t, app, `{"display_name":"concurrent archive"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	credential := createVaultCredential(t, app, vault.ID, staticBearerBody("concurrent credential", "https://mcp.example.com/concurrent", "test-token"))
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() { archiveVault(t, app, vault.ID) })
		workers.Go(func() { archiveVaultCredential(t, app, vault.ID, credential.ID) })
	}
	workers.Wait()
	assertWebhookCount(t, app, "vault.archived", vault.ID, 1)
	assertWebhookCount(t, app, "vault_credential.archived", credential.ID, 1)
}
