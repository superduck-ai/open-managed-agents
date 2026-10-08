package tests

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

const redirectDisableReason = "auto-disabled: endpoint URL returned a redirect (3xx)"
const addressDisableReason = "auto-disabled: endpoint URL resolved to an invalid address"

func TestWebhookPermanentFailureDoesNotRevive(t *testing.T) {
	for _, mode := range []string{"redirect", "address"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			var responseStatus atomic.Int32
			responseStatus.Store(302)
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(int(responseStatus.Load())) })
			f.enqueue(t, 1)
			reason := redirectDisableReason
			if mode == "address" {
				f.exec(t, `UPDATE webhook_endpoints SET url='https://[::ffff:127.0.0.1]/hook' WHERE uuid=$1`, f.endpoint.UUID)
				f.app.cfg.Webhook.AllowInsecure = false
				reason = addressDisableReason
			}
			worker := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
			drainWebhookQueue(t, f.app, worker)
			assertWebhookQueueCount(t, f.app, 0)
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='disabled' AND disabled_reason=$2 AND consecutive_failures=1`, 1, f.endpoint.UUID, reason)
			updateWebhook(t, f.app, f.endpoint.ExternalID, `{"status":"enabled"}`)
			before := calls.Load()
			drainWebhookQueue(t, f.app, worker)
			if calls.Load() != before {
				t.Fatal("terminal job revived")
			}
			wantCalls := int32(1)
			if mode == "address" {
				wantCalls = 0
			}
			if before != wantCalls {
				t.Fatalf("permanent failure sent %d requests, want %d", before, wantCalls)
			}
			if mode == "redirect" {
				responseStatus.Store(204)
				scope, err := f.app.db.GetWorkspaceIdentifiers(t.Context(), f.endpoint.WorkspaceUUID)
				if err != nil {
					t.Fatal(err)
				}
				webhooks.NewEnqueuer(f.app.db, f.app.webhookQueue, nil).Enqueue(t.Context(), webhooks.EnqueueInput{
					OccurredAt: time.Now().UTC(), WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: scope.OrganizationUUID,
					WorkspaceExternalID: scope.WorkspaceExternalID, EventType: "session.status_idled", ResourceID: "sesn_after_reenable",
				})
				drainWebhookQueue(t, f.app, worker)
				if calls.Load() != before+1 {
					t.Fatal("new event was not delivered")
				}
				assertWebhookQueueCount(t, f.app, 0)
				assertWebhookQueueCount(t, f.app, 0)
			}
		})
	}
}

func TestWebhookRetryContinuousConsumer(t *testing.T) {
	delivered := make(chan struct{}, 2)
	var calls atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(204)
		}
		delivered <- struct{}{}
	})
	f.enqueue(t, 1)
	f.app.cfg.Webhook.MaxAttempts = 2

	stopWorker := startWebhookWorker(t, webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil))
	defer stopWorker()
	for range 2 {
		select {
		case <-delivered:
		case <-time.After(20 * time.Second):
			t.Fatal("worker did not deliver/retry")
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		info, err := f.app.webhookStream.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if info.State.Msgs == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("successful retry not acknowledged")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopWorker()
	assertWebhookQueueCount(t, f.app, 0)

	if calls.Load() != 2 {
		t.Fatalf("requests=%d", calls.Load())
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=0 AND status='enabled'`, 1, f.endpoint.UUID)
}
