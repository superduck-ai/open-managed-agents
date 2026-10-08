package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type webhookAPIResponse struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	URL            string   `json:"url"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	EnabledEvents  []string `json:"enabled_events"`
	Status         string   `json:"status"`
	DisabledReason *string  `json:"disabled_reason"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	SigningSecret  *string  `json:"signing_secret"`
}

type webhookPageAPIResponse struct {
	Data     []webhookAPIResponse `json:"data"`
	NextPage *string              `json:"next_page"`
}

type webhookSigningSecretAPIResponse struct {
	SigningSecret string `json:"signing_secret"`
}

func TestWebhooksAPI(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Webhook.AllowInsecure = false
	app := newTestAppWithStore(t, &cfg, newFakeStore("webhooks-api-bucket"))
	defer app.close()
	clearWebhookState(t, app)
	defer clearWebhookState(t, app)

	t.Run("failure missing beta header", func(t *testing.T) {
		resp := doWebhookRequest(t, app, http.MethodGet, "/v1/webhooks", nil, defaultTestKey, false)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure private url", func(t *testing.T) {
		resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks", strings.NewReader(`{"url":"https://localhost/webhook","name":"bad","enabled_events":["session.status_idled"]}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure unsupported event", func(t *testing.T) {
		resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks", strings.NewReader(`{"url":"https://webhook.example.com","name":"bad","enabled_events":["session.created"]}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing enabled events", func(t *testing.T) {
		resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks", strings.NewReader(`{"url":"https://webhook.example.com","name":"bad"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure regenerate missing webhook", func(t *testing.T) {
		resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks/wh_missing/regenerate_signing_secret", strings.NewReader(`{}`), defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure regenerate body fields", func(t *testing.T) {
		created := createWebhook(t, app, `{"url":"https://webhook.example.com","name":"bad body","enabled_events":["session.status_idled"]}`)
		resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks/"+created.ID+"/regenerate_signing_secret", strings.NewReader(`{"name":"nope"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		clearWebhookState(t, app)
	})

	t.Run("failure invalid optional fields", func(t *testing.T) {
		for _, body := range []string{
			`{"url":"https://webhook.example.com","name":null,"enabled_events":["session.updated"]}`,
			`{"url":"https://webhook.example.com","description":null,"enabled_events":["session.updated"]}`,
			`{"url":"https://webhook.example.com","name":123,"enabled_events":["session.updated"]}`,
			`{"url":"https://webhook.example.com/path#fragment","enabled_events":["session.updated"]}`,
		} {
			resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks", strings.NewReader(body), defaultTestKey, true)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		}
	})

	t.Run("failure cross workspace access", func(t *testing.T) {
		const otherKey = "sk-ant-test-webhook-other"
		seedWorkspaceKey(t, app.pool, "webhook_other_org", "webhook_other_workspace", "webhook_other_key", otherKey)
		created := createWebhook(t, app, `{"url":"https://webhook.example.com","enabled_events":["session.updated"]}`)
		for _, request := range []struct{ method, suffix, body string }{
			{http.MethodGet, "", ""},
			{http.MethodPost, "", `{"name":"other workspace"}`},
			{http.MethodPost, "/regenerate_signing_secret", `{}`},
			{http.MethodDelete, "", ""},
		} {
			resp := doWebhookRequest(t, app, request.method, "/v1/webhooks/"+created.ID+request.suffix, strings.NewReader(request.body), otherKey, true)
			assertError(t, resp, http.StatusNotFound, "not_found_error")
		}
		resp := doWebhookRequest(t, app, http.MethodGet, "/v1/webhooks", nil, otherKey, true)
		defer resp.Body.Close()
		var page webhookPageAPIResponse
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("other workspace list status=%d", resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		if len(page.Data) != 0 {
			t.Fatal("other workspace can list endpoint")
		}
		if retrieved := retrieveWebhook(t, app, created.ID); retrieved.Name != "" {
			t.Fatal("cross workspace update changed endpoint")
		}
		clearWebhookState(t, app)
	})

	t.Run("success omitted and empty optional fields", func(t *testing.T) {
		created := createWebhook(t, app, `{"url":"https://webhook.example.com","enabled_events":["session.updated","session.deleted"]}`)
		if created.Name != "" || created.Description != "" {
			t.Fatal("omitted optional fields must be empty")
		}
		updateWebhook(t, app, created.ID, `{"name":"temporary","description":"temporary"}`)
		updated := updateWebhook(t, app, created.ID, `{"url":"https://webhook.example.com/new","name":"","description":""}`)
		if updated.Name != "" || updated.Description != "" || updated.URL != "https://webhook.example.com/new" || updated.SigningSecret != nil {
			t.Fatal("update did not clear optional fields, change URL, or hide secret")
		}
		for _, body := range []string{`{"name":null}`, `{"description":null}`, `{"url":""}`} {
			resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks/"+created.ID, strings.NewReader(body), defaultTestKey, true)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		}
		if retrieved := retrieveWebhook(t, app, created.ID); retrieved.URL != updated.URL || retrieved.Name != "" || retrieved.SigningSecret != nil {
			t.Fatal("updated endpoint not persisted")
		}
		clearWebhookState(t, app)
	})

	t.Run("success lifecycle", func(t *testing.T) {
		created := createWebhook(t, app, `{"url":"https://webhook.example.com","name":"docs callback","description":"created from console","enabled_events":["session.status_idled"]}`)
		if created.Type != "webhook" || created.ID == "" || !strings.HasPrefix(created.ID, "wh_") {
			t.Fatalf("unexpected created webhook: %+v", created)
		}
		if created.SigningSecret == nil || !strings.HasPrefix(*created.SigningSecret, "whsec_") {
			t.Fatalf("created webhook signing secret = %v, want whsec_ value", created.SigningSecret)
		}
		createdSecret := *created.SigningSecret

		retrieved := retrieveWebhook(t, app, created.ID)
		if retrieved.ID != created.ID || retrieved.SigningSecret != nil {
			t.Fatalf("retrieve webhook = %+v, want same id and hidden secret", retrieved)
		}
		page := listWebhooks(t, app)
		if len(page.Data) != 1 || page.Data[0].ID != created.ID || page.Data[0].SigningSecret != nil {
			t.Fatalf("unexpected webhooks page: %+v", page)
		}

		updated := updateWebhook(t, app, created.ID, `{"name":"docs callback updated","description":"","enabled_events":["session.status_idled","vault.created"]}`)
		if updated.Name != "docs callback updated" || updated.Description != "" || len(updated.EnabledEvents) != 2 || updated.SigningSecret != nil {
			t.Fatalf("unexpected updated webhook: %+v", updated)
		}

		regenerated := regenerateWebhookSigningSecret(t, app, created.ID)
		if !strings.HasPrefix(regenerated.SigningSecret, "whsec_") || regenerated.SigningSecret == createdSecret {
			t.Fatalf("regenerated signing secret = %q, want new whsec_ value", regenerated.SigningSecret)
		}
		apiKey, err := app.db.GetAPIKey(context.Background(), auth.HashAPIKey(defaultTestKey))
		if err != nil {
			t.Fatalf("load api key: %v", err)
		}
		storedWebhook, err := app.db.GetWebhookEndpoint(context.Background(), apiKey.WorkspaceUUID.String(), created.ID)
		if err != nil {
			t.Fatalf("load stored webhook: %v", err)
		}
		if storedWebhook.SigningSecret != regenerated.SigningSecret {
			t.Fatalf("stored signing secret = %q, want regenerated value", storedWebhook.SigningSecret)
		}
		retrievedAfterRegenerate := retrieveWebhook(t, app, created.ID)
		if retrievedAfterRegenerate.SigningSecret != nil {
			t.Fatalf("retrieve after regenerate exposed signing secret: %+v", retrievedAfterRegenerate)
		}

		deleted := deleteWebhook(t, app, created.ID)
		if deleted.ID != created.ID || deleted.Type != "webhook_deleted" {
			t.Fatalf("unexpected delete response: %+v", deleted)
		}
		resp := doWebhookRequest(t, app, http.MethodGet, "/v1/webhooks/"+created.ID, nil, defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})
}

func TestWebhookEndpointDelivery(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []capturedWebhookRequest
	)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read webhook body: %v", err)
		}
		mu.Lock()
		requests = append(requests, capturedWebhookRequest{Header: r.Header.Clone(), Body: append([]byte(nil), body...)})
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Webhook.AllowInsecure = true
	cfg.Webhook.WorkerEnabled = true
	cfg.Webhook.Timeout = time.Second
	cfg.Webhook.MaxAttempts = 3
	app := newTestAppWithStore(t, &cfg, newFakeStore("webhooks-delivery-bucket"))
	defer app.close()
	clearWebhookState(t, app)
	defer clearWebhookState(t, app)

	endpoint := createWebhook(t, app, `{"url":`+quoteJSON(receiver.URL)+`,"name":"local callback","enabled_events":["session.status_idled","vault.created"]}`)
	if endpoint.SigningSecret == nil {
		t.Fatalf("created webhook signing secret is nil")
	}

	ctx := context.Background()
	apiKey, err := app.db.GetAPIKey(ctx, auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("load api key: %v", err)
	}
	enqueuer := webhooks.NewEnqueuer(app.db, app.webhookQueue, nil)
	occurredAt := time.Date(2020, 1, 2, 3, 4, 5, 123456789, time.UTC)
	enqueue := func(eventType, resourceID string) {
		enqueuer.Enqueue(ctx, webhooks.EnqueueInput{
			OccurredAt:          occurredAt,
			WorkspaceUUID:       apiKey.WorkspaceUUID.String(),
			OrganizationUUID:    apiKey.OrganizationUUID.String(),
			WorkspaceExternalID: apiKey.WorkspaceExternalID,
			EventType:           eventType,
			ResourceID:          resourceID,
		})
	}
	sessionID := "sesn_webhook_endpoint_delivery"
	enqueue("session.status_idled", sessionID)
	if count := webhookJobCount(t, app, "session.status_idled", sessionID); count != 1 {
		t.Fatalf("session.status_idled webhook jobs = %d, want 1", count)
	}
	drainWebhookQueue(t, app, webhooks.NewWorker(app.db, app.webhookQueue, app.cfg.Webhook, nil))

	mu.Lock()
	if len(requests) != 1 {
		t.Fatalf("webhook receiver saw %d requests, want 1", len(requests))
	}
	delivered := requests[0]
	mu.Unlock()
	if delivered.Header.Get("X-Webhook-Signature") == "" {
		t.Fatalf("X-Webhook-Signature header is empty: %+v", delivered.Header)
	}
	client := anthropic.NewClient(option.WithWebhookKey(*endpoint.SigningSecret), option.WithAPIKey(defaultTestKey))
	event, err := client.Beta.Webhooks.Unwrap(delivered.Body, delivered.Header)
	if err != nil {
		t.Fatalf("SDK failed to unwrap webhook: %v", err)
	}
	var payload struct {
		CreatedAt string `json:"created_at"`
		Type      string `json:"type"`
		Data      struct {
			ID             string `json:"id"`
			OrganizationID string `json:"organization_id"`
			Type           string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(delivered.Body, &payload); err != nil {
		t.Fatalf("unmarshal delivered webhook: %v", err)
	}
	if payload.CreatedAt != occurredAt.Format(time.RFC3339Nano) || event.Type != "event" ||
		payload.Type != "event" ||
		payload.Data.Type != "session.status_idled" ||
		payload.Data.ID != sessionID ||
		payload.Data.OrganizationID != apiKey.OrganizationUUID.String() {
		t.Fatalf("unexpected webhook event=%+v payload=%+v body=%s", event, payload, delivered.Body)
	}

	unsubscribedSessionID := "sesn_webhook_endpoint_unsubscribed"
	enqueue("session.status_terminated", unsubscribedSessionID)
	if count := webhookJobCount(t, app, "session.status_terminated", unsubscribedSessionID); count != 0 {
		t.Fatalf("session.status_terminated webhook jobs = %d, want 0 due endpoint filter", count)
	}

	redirectReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/moved", http.StatusFound)
	}))
	defer redirectReceiver.Close()
	redirectEndpoint := createWebhook(t, app, `{"url":`+quoteJSON(redirectReceiver.URL)+`,"name":"redirect callback","enabled_events":["session.status_terminated"]}`)
	redirectSessionID := "sesn_webhook_endpoint_redirect"
	enqueue("session.status_terminated", redirectSessionID)
	drainWebhookQueue(t, app, webhooks.NewWorker(app.db, app.webhookQueue, app.cfg.Webhook, nil))
	disabled := retrieveWebhook(t, app, redirectEndpoint.ID)
	if disabled.Status != "disabled" || disabled.DisabledReason == nil || *disabled.DisabledReason != "auto-disabled: endpoint URL returned a redirect (3xx)" {
		t.Fatalf("redirect endpoint = %+v, want disabled with status reason", disabled)
	}
}

func doWebhookRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new webhook request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "webhooks-2026-03-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do webhook request: %v", err)
	}
	return resp
}

func createWebhook(t *testing.T, app *testApp, body string) webhookAPIResponse {
	t.Helper()
	resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create webhook status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var webhook webhookAPIResponse
	decodeJSON(t, resp.Body, &webhook)
	return webhook
}

func regenerateWebhookSigningSecret(t *testing.T, app *testApp, webhookID string) webhookSigningSecretAPIResponse {
	t.Helper()
	resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks/"+webhookID+"/regenerate_signing_secret", strings.NewReader(`{}`), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("regenerate webhook signing secret status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var regenerated webhookSigningSecretAPIResponse
	decodeJSON(t, resp.Body, &regenerated)
	return regenerated
}

func retrieveWebhook(t *testing.T, app *testApp, webhookID string) webhookAPIResponse {
	t.Helper()
	resp := doWebhookRequest(t, app, http.MethodGet, "/v1/webhooks/"+webhookID, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve webhook status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var webhook webhookAPIResponse
	decodeJSON(t, resp.Body, &webhook)
	return webhook
}

func updateWebhook(t *testing.T, app *testApp, webhookID, body string) webhookAPIResponse {
	t.Helper()
	resp := doWebhookRequest(t, app, http.MethodPost, "/v1/webhooks/"+webhookID, strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update webhook status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var webhook webhookAPIResponse
	decodeJSON(t, resp.Body, &webhook)
	return webhook
}

func listWebhooks(t *testing.T, app *testApp) webhookPageAPIResponse {
	t.Helper()
	resp := doWebhookRequest(t, app, http.MethodGet, "/v1/webhooks", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list webhooks status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page webhookPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func deleteWebhook(t *testing.T, app *testApp, webhookID string) struct {
	ID   string `json:"id"`
	Type string `json:"type"`
} {
	t.Helper()
	resp := doWebhookRequest(t, app, http.MethodDelete, "/v1/webhooks/"+webhookID, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete webhook status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deleted struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	decodeJSON(t, resp.Body, &deleted)
	return deleted
}

func clearWebhookState(t *testing.T, app *testApp) {
	t.Helper()
	if err := app.webhookStream.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(context.Background(), `delete from webhook_endpoints`); err != nil {
		t.Fatalf("clear webhook endpoints: %v", err)
	}
}
