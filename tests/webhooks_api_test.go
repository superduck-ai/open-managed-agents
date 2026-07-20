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
		storedWebhook, err := app.db.GetWebhookEndpoint(context.Background(), apiKey.WorkspaceID, created.ID)
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
	sessionID := "sesn_webhook_endpoint_delivery"
	webhooks.Enqueue(ctx, app.db, app.cfg.Webhook, apiKey.WorkspaceID, apiKey.OrganizationExternalID, apiKey.WorkspaceExternalID, "session.status_idled", sessionID, nil)
	if count := webhookJobCount(t, app, "session.status_idled", sessionID); count != 1 {
		t.Fatalf("session.status_idled webhook jobs = %d, want 1", count)
	}
	if err := webhooks.RunOnce(ctx, app.db, app.cfg.Webhook, "webhook-endpoint-worker"); err != nil {
		t.Fatalf("run endpoint webhook delivery: %v", err)
	}

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
		Type string `json:"type"`
		Data struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(delivered.Body, &payload); err != nil {
		t.Fatalf("unmarshal delivered webhook: %v", err)
	}
	if event.Type != "event" || payload.Type != "event" || payload.Data.Type != "session.status_idled" || payload.Data.ID != sessionID {
		t.Fatalf("unexpected webhook event=%+v payload=%+v body=%s", event, payload, delivered.Body)
	}

	unsubscribedSessionID := "sesn_webhook_endpoint_unsubscribed"
	webhooks.Enqueue(ctx, app.db, app.cfg.Webhook, apiKey.WorkspaceID, apiKey.OrganizationExternalID, apiKey.WorkspaceExternalID, "session.status_terminated", unsubscribedSessionID, nil)
	if count := webhookJobCount(t, app, "session.status_terminated", unsubscribedSessionID); count != 0 {
		t.Fatalf("session.status_terminated webhook jobs = %d, want 0 due endpoint filter", count)
	}

	redirectReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/moved", http.StatusFound)
	}))
	defer redirectReceiver.Close()
	redirectEndpoint := createWebhook(t, app, `{"url":`+quoteJSON(redirectReceiver.URL)+`,"name":"redirect callback","enabled_events":["session.status_terminated"]}`)
	redirectSessionID := "sesn_webhook_endpoint_redirect"
	webhooks.Enqueue(ctx, app.db, app.cfg.Webhook, apiKey.WorkspaceID, apiKey.OrganizationExternalID, apiKey.WorkspaceExternalID, "session.status_terminated", redirectSessionID, nil)
	if err := webhooks.RunOnce(ctx, app.db, app.cfg.Webhook, "webhook-redirect-worker"); err != nil {
		t.Fatalf("run redirect webhook delivery: %v", err)
	}
	disabled := retrieveWebhook(t, app, redirectEndpoint.ID)
	if disabled.Status != "disabled" || disabled.DisabledReason == nil || !strings.Contains(*disabled.DisabledReason, "webhook status 302") {
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
	if _, err := app.db.Pool.Exec(context.Background(), `delete from jobs where type = 'webhook_delivery'`); err != nil {
		t.Fatalf("clear webhook jobs: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `delete from webhook_endpoints`); err != nil {
		t.Fatalf("clear webhook endpoints: %v", err)
	}
}
