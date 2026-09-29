package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

func TestWebhookNoMatchingSubscriptionDoesNotEnqueue(t *testing.T) {
	for _, mode := range []string{"none", "disabled", "unselected", "other-workspace", "deleted-last"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeliveryFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected delivery") })
			workspace := f.endpoint.WorkspaceUUID
			switch mode {
			case "none":
				f.exec(t, `DELETE FROM webhook_endpoints WHERE uuid=$1`, f.endpoint.UUID)
			case "disabled":
				updateWebhook(t, f.app, f.endpoint.ExternalID, `{"status":"disabled"}`)
			case "unselected":
				updateWebhook(t, f.app, f.endpoint.ExternalID, `{"enabled_events":["agent.created"]}`)
			case "other-workspace":
				workspace = uuid.NewV4().String()
			case "deleted-last":
				if err := f.app.db.DeleteWebhookEndpoint(t.Context(), workspace, f.endpoint.ExternalID); err != nil {
					t.Fatal(err)
				}
			}
			webhooks.NewEnqueuer(f.app.db, f.app.webhookQueue, nil).Enqueue(t.Context(), webhooks.EnqueueInput{
				OccurredAt: time.Now(), WorkspaceUUID: workspace, OrganizationUUID: f.endpoint.OrganizationUUID,
				EventType: "session.status_idled", ResourceID: "sesn_unmatched",
			})
			assertWebhookQueueCount(t, f.app, 0)
		})
	}
}

func TestWebhookSubscriptionFanoutWithHistoricalJob(t *testing.T) {
	received := make(chan capturedWebhookRequest, 3)
	f := newDeliveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		received <- capturedWebhookRequest{Header: r.Header.Clone(), Body: body}
		w.WriteHeader(204)
	})
	second := createWebhook(t, f.app, `{"url":`+quoteJSON(f.endpoint.URL)+`,"enabled_events":["session.status_idled"]}`)
	scope, err := f.app.db.GetWorkspaceIdentifiers(t.Context(), f.endpoint.WorkspaceUUID)
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2020, 1, 2, 3, 4, 5, 123456789, time.UTC)
	webhooks.NewEnqueuer(f.app.db, f.app.webhookQueue, nil).Enqueue(t.Context(), webhooks.EnqueueInput{
		OccurredAt: occurredAt, WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: scope.OrganizationUUID,
		WorkspaceExternalID: scope.WorkspaceExternalID, EventType: "session.status_idled", ResourceID: "sesn_fanout",
	})
	assertWebhookQueueCount(t, f.app, 2)
	f.exec(t, `INSERT INTO jobs(external_id,workspace_uuid,type,status,payload) VALUES('job_legacy_mixed',$1,'webhook_delivery','pending','{"event_type":"session.status_idled","event":{"id":"wevt_legacy"}}')`, f.endpoint.WorkspaceUUID)
	if err := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 {
		t.Fatalf("deliveries=%d want 2", len(received))
	}
	firstDelivery, secondDelivery := <-received, <-received
	if !bytes.Equal(firstDelivery.Body, secondDelivery.Body) {
		t.Fatal("fanout payload changed")
	}
	seen := map[string]bool{}
	for _, delivery := range []capturedWebhookRequest{firstDelivery, secondDelivery} {
		matched := false
		for _, key := range []string{f.endpoint.SigningSecret, *second.SigningSecret} {
			sdk := anthropic.NewClient(option.WithWebhookKey(key), option.WithAPIKey(defaultTestKey))
			if _, err := sdk.Beta.Webhooks.Unwrap(delivery.Body, delivery.Header); err == nil {
				seen[key] = true
				matched = true
			}
		}
		if !matched {
			t.Fatal("SDK signature verification failed")
		}
	}
	if len(seen) != 2 {
		t.Fatal("subscriptions did not use separate secrets")
	}
	var event webhooks.Event
	if err := json.Unmarshal(firstDelivery.Body, &event); err != nil {
		t.Fatal(err)
	}
	if event.CreatedAt != occurredAt.Format(time.RFC3339Nano) || event.Data.ID != "sesn_fanout" || event.Data.WorkspaceID != scope.WorkspaceExternalID || event.Data.OrganizationID != scope.OrganizationUUID {
		t.Fatalf("incorrect envelope: %+v", event)
	}
	assertWebhookQueueCount(t, f.app, 0)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE external_id='job_legacy_mixed' AND status='pending'`, 1)
	f.exec(t, `DELETE FROM jobs WHERE external_id='job_legacy_mixed'`)
}
