package tests

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

const redirectDisableReason = "auto-disabled: endpoint URL returned a redirect (3xx)"
const addressDisableReason = "auto-disabled: endpoint URL resolved to an invalid address"

func TestWebhookPermanentFailureDoesNotRevive(t *testing.T) {
	for _, mode := range []string{"redirect", "address", "global"} {
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
			if mode == "global" {
				f.exec(t, `UPDATE jobs SET payload=payload-'webhook_endpoint_uuid' WHERE type='webhook_delivery'`)
				f.app.cfg.Webhook.EndpointURL = f.endpoint.URL
				f.app.cfg.Webhook.SigningKey = f.endpoint.SigningSecret
				f.app.cfg.Webhook.EventTypes = []string{"session.status_idled"}
			}
			worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
			if err := worker.RunOnce(t.Context(), "permanent"); err != nil {
				t.Fatal(err)
			}
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='failed' AND attempts=1 AND payload->>'last_error'=$1`, 1, reason)
			if mode != "global" {
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='disabled' AND disabled_reason=$2 AND consecutive_failures=1`, 1, f.endpoint.UUID, reason)
				updateWebhook(t, f.app, f.endpoint.ExternalID, `{"status":"enabled"}`)
			} else {
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=0 AND status='enabled'`, 1, f.endpoint.UUID)
			}
			before := calls.Load()
			f.exec(t, `UPDATE jobs SET run_after=NOW()-interval '1 second' WHERE type='webhook_delivery'`)
			if err := worker.RunOnce(t.Context(), "after-reenable"); err != nil {
				t.Fatal(err)
			}
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
				webhooks.NewEnqueuer(f.app.db, f.app.cfg.Webhook, nil).Enqueue(t.Context(), webhooks.EnqueueInput{
					OccurredAt: time.Now().UTC(), WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: scope.OrganizationUUID,
					WorkspaceExternalID: scope.WorkspaceExternalID, EventType: "session.status_idled", ResourceID: "sesn_after_reenable",
				})
				if err := worker.RunOnce(t.Context(), "new-event"); err != nil {
					t.Fatal(err)
				}
				if calls.Load() != before+1 {
					t.Fatal("new event was not delivered")
				}
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='failed'`, 1)
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed'`, 1)
			}
		})
	}
}

func TestWebhookRetryAttemptLimits(t *testing.T) {
	for _, limit := range []int{0, 1, 10} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			var calls atomic.Int32
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
			f.app.cfg.Webhook.MaxAttempts = limit
			f.enqueue(t, 1)
			worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
			want := limit
			if want == 0 {
				want = 3
			}
			for attempt := 1; attempt <= want; attempt++ {
				if err := worker.RunOnce(t.Context(), "retry"); err != nil {
					t.Fatal(err)
				}
				if calls.Load() != int32(attempt) {
					t.Fatalf("calls=%d attempt=%d", calls.Load(), attempt)
				}
				status := "retry"
				if attempt == want {
					status = "failed"
				}
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status=$1 AND attempts=$2`, 1, status, attempt)
				if status == "retry" {
					upper := min(120, 5<<min(attempt, 5))
					// Fail writes run_after just before the transaction's updated_at; allow clock/transaction skew.
					assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND extract(epoch FROM run_after-updated_at) BETWEEN 4 AND $1`, 1, upper+1)
				}
				f.exec(t, `UPDATE jobs SET run_after=NOW()-interval '1 second' WHERE type='webhook_delivery'`)
			}
			if err := worker.RunOnce(t.Context(), "exhausted"); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != int32(want) {
				t.Fatal("extra delivery after exhaustion")
			}
		})
	}
}

func TestWebhookHistoricalAttemptLimit(t *testing.T) {
	var calls atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	f.enqueue(t, 1)
	f.exec(t, `UPDATE jobs SET attempts=7, run_after=NOW()-interval '1 hour', payload=payload||'{"last_error":"original failure"}'::jsonb WHERE type='webhook_delivery'`)
	f.exec(t, `UPDATE webhook_endpoints SET consecutive_failures=7 WHERE uuid=$1`, f.endpoint.UUID)
	f.app.cfg.Webhook.MaxAttempts = 0
	worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
	if err := worker.RunOnce(t.Context(), "old-limit"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("sent exhausted historical job")
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='failed' AND attempts=7 AND payload->>'last_error'='original failure' AND run_after<NOW()-interval '30 minutes'`, 1)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=7 AND status='enabled'`, 1, f.endpoint.UUID)
	f.exec(t, `DELETE FROM jobs WHERE type='webhook_delivery' AND status='failed'`)
	// A not-yet-due historical job keeps its old schedule. Once due it uses the new policy.
	f.enqueue(t, 1)
	f.exec(t, `UPDATE jobs SET attempts=1,run_after=NOW()+interval '1 hour' WHERE type='webhook_delivery' AND status='pending'`)
	if err := worker.RunOnce(t.Context(), "not-due"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("rewrote existing schedule")
	}
	f.exec(t, `UPDATE jobs SET run_after=NOW()-interval '1 second' WHERE type='webhook_delivery' AND status='pending'`)
	if err := worker.RunOnce(t.Context(), "old-due"); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='retry' AND attempts=2 AND run_after<NOW()+interval '21 seconds'`, 1)
}

func TestWebhookTerminalResultRollbackAndFencing(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	f.enqueue(t, 1)
	job := f.lease(t)
	failure := db.WebhookDeliveryFailure{Reason: redirectDisableReason, Terminal: true, MaxAttempts: 3, DisableAfter: 24 * time.Hour}
	remove := installWebhookMutationFailure(t, f.app, "webhook_endpoints", "UPDATE", "NEW.uuid = '"+f.endpoint.UUID+"'")
	if applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), job, failure); err == nil || applied {
		t.Fatalf("rollback=%t %v", applied, err)
	}
	f.assertState(t, job, "running", 0, 0)
	remove()
	f.exec(t, `UPDATE jobs SET locked_until=NOW()-interval '1 second' WHERE uuid=$1`, job.UUID)
	if applied, err := f.app.db.ExhaustWebhookDeliveryJob(t.Context(), job); err != nil || applied {
		t.Fatalf("stale exhaust=%t %v", applied, err)
	}
	fresh := f.lease(t)
	if applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), job, failure); err != nil || applied {
		t.Fatalf("stale terminal=%t %v", applied, err)
	}
	if applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), fresh, failure); err != nil || !applied {
		t.Fatalf("terminal=%t %v", applied, err)
	}
	f.assertState(t, fresh, "failed", 1, 1)
	if applied, err := f.app.db.ExhaustWebhookDeliveryJob(t.Context(), fresh); err != nil || applied {
		t.Fatalf("duplicate exhaust=%t %v", applied, err)
	}
	if applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), fresh, failure); err != nil || applied {
		t.Fatalf("duplicate terminal=%t %v", applied, err)
	}
}

func TestWebhookRetryRealWorkerPolling(t *testing.T) {
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
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil).Start(ctx)
	for range 2 {
		select {
		case <-delivered:
		case <-time.After(20 * time.Second):
			t.Fatal("worker did not deliver/retry")
		}
	}
	// The receiver notification precedes the result transaction; wait for that commit.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var count int
		err := f.app.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed' AND attempts=1`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND failure_started_at IS NULL AND consecutive_failures=0`, 1, f.endpoint.UUID)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker result not committed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if calls.Load() != 2 {
		t.Fatalf("requests=%d", calls.Load())
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=0 AND status='enabled'`, 1, f.endpoint.UUID)
}
