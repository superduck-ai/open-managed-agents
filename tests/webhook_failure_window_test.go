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

const sustainedFailureReason = "auto-disabled after sustained delivery failures"

func windowFailure() db.WebhookDeliveryFailure {
	return db.WebhookDeliveryFailure{Reason: "receiver unavailable", RetryDelay: time.Minute, MaxAttempts: 3, DisableAfter: 24 * time.Hour}
}

func TestWebhookFailureWindowResultRollback(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	f.enqueue(t, 1)
	job := f.lease(t)
	// Fail the task write after the endpoint has already been changed in the transaction.
	f.exec(t, `UPDATE webhook_endpoints SET failure_started_at=clock_timestamp()-interval '25 hours' WHERE uuid=$1`, f.endpoint.UUID)
	remove := installWebhookMutationFailure(t, f.app, "jobs", "UPDATE", "NEW.uuid = '"+job.UUID+"'")
	applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), job, windowFailure())
	if err == nil || applied {
		t.Fatalf("write failure=%t %v", applied, err)
	}
	f.assertState(t, job, "running", 0, 0)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='enabled' AND disabled_reason IS NULL AND failure_started_at<clock_timestamp()-interval '24 hours'`, 1, f.endpoint.UUID)
	remove()
	applied, err = f.app.db.FailWebhookDeliveryJob(t.Context(), job, windowFailure())
	if err != nil || !applied {
		t.Fatalf("retry=%t %v", applied, err)
	}
	f.assertState(t, job, "failed", 1, 1)
}

func TestWebhookFailureWindowExpiredWhileWaiting(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	f.enqueue(t, 1)
	job := f.lease(t)
	blocker, err := f.app.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	var id string
	if err := blocker.QueryRow(t.Context(), `SELECT uuid FROM webhook_endpoints WHERE uuid=$1 FOR UPDATE`, f.endpoint.UUID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE jobs SET locked_until=clock_timestamp()+interval '500 milliseconds' WHERE uuid=$1`, job.UUID)
	type result struct {
		applied bool
		err     error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		applied, err := f.app.db.FailWebhookDeliveryJob(ctx, job, windowFailure())
		done <- result{applied, err}
	}()
	// Observe an actual lock wait before allowing the lease to expire.
	waitWindowSQL(t, f, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE cardinality(pg_blocking_pids(pid))>0 AND query LIKE '%WITH locked_endpoint%')`)
	waitWindowSQL(t, f, `SELECT locked_until<=clock_timestamp() FROM jobs WHERE uuid=$1`, job.UUID)
	if err := blocker.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.applied {
		t.Fatalf("expired result=%+v", r)
	}
	f.assertState(t, job, "running", 0, 0)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND failure_started_at IS NULL AND status='enabled'`, 1, f.endpoint.UUID)
}

func waitWindowSQL(t *testing.T, f deliveryFixture, query string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var ready bool
		if err := f.app.pool.QueryRow(t.Context(), query, args...).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for database condition")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebhookFailureWindowBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, age string
		disabled  bool
	}{
		{"first failure", "", false},
		{"before threshold", "23 hours", false},
		{"at threshold", "24 hours", true},
		{"after threshold", "25 hours", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
			f.enqueue(t, 1)
			job := f.lease(t)
			if tc.age != "" {
				f.exec(t, `UPDATE webhook_endpoints SET failure_started_at=clock_timestamp()-CAST($2 AS interval) WHERE uuid=$1`, f.endpoint.UUID, tc.age)
			}
			before, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), job, windowFailure())
			if err != nil || !applied {
				t.Fatalf("failure=%t %v", applied, err)
			}
			after, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			if after.FailureStartedAt == nil || (before.FailureStartedAt != nil && !after.FailureStartedAt.Equal(*before.FailureStartedAt)) {
				t.Fatal("failure window changed incorrectly")
			}
			status := "retry"
			if tc.disabled {
				status = "failed"
				if after.Status != "disabled" || after.DisabledReason == nil || *after.DisabledReason != sustainedFailureReason {
					t.Fatalf("endpoint=%+v", after)
				}
			} else if after.Status != "enabled" || after.DisabledReason != nil {
				t.Fatalf("endpoint=%+v", after)
			}
			f.assertState(t, job, status, 1, 1)
			if tc.disabled {
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE uuid=$1 AND run_after<=clock_timestamp() AND payload->>'last_error'='receiver unavailable'`, 1, job.UUID)
			}
		})
	}
}

func TestWebhookFailureWindowResetAndEdits(t *testing.T) {
	for _, operation := range []string{"metadata", "secret", "disable", "enable", "success", "late-success"} {
		t.Run(operation, func(t *testing.T) {
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			f.enqueue(t, 1)
			job := f.lease(t)
			f.exec(t, `UPDATE webhook_endpoints SET failure_started_at=clock_timestamp()-interval '25 hours',consecutive_failures=21 WHERE uuid=$1`, f.endpoint.UUID)
			before, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "metadata":
				name := "new name"
				_, err = f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, db.WebhookEndpointUpdate{Name: &name, UpdatedAt: time.Now()})
			case "secret":
				err = f.app.db.RegenerateWebhookEndpointSigningSecret(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, "new-secret", time.Now())
			case "enable", "disable":
				status := "enabled"
				if operation == "disable" {
					status = "disabled"
				}
				_, err = f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, db.WebhookEndpointUpdate{Status: &status, UpdatedAt: time.Now()})
			case "success", "late-success":
				if operation == "late-success" {
					f.exec(t, `UPDATE webhook_endpoints SET status='disabled',disabled_reason=$2 WHERE uuid=$1`, f.endpoint.UUID, sustainedFailureReason)
				}
				var applied bool
				applied, err = f.app.db.CompleteWebhookDeliveryJob(t.Context(), job, true)
				if err == nil && !applied {
					t.Fatal("success not applied")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "success" || operation == "enable" {
				if after.FailureStartedAt != nil || after.ConsecutiveFailures != 0 || after.DisabledReason != nil {
					t.Fatalf("not reset: %+v", after)
				}
			} else if after.FailureStartedAt == nil || !after.FailureStartedAt.Equal(*before.FailureStartedAt) || after.ConsecutiveFailures != 21 {
				t.Fatalf("lost window: %+v", after)
			}
			if operation == "late-success" && after.Status != "disabled" {
				t.Fatal("late success re-enabled subscription")
			}
		})
	}
}

func TestWebhookFailureWindowWorkerAndReenable(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(500)
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(int(status.Load())) })
	f.enqueue(t, 1)
	f.exec(t, `UPDATE webhook_endpoints SET failure_started_at=clock_timestamp()-interval '2 hours' WHERE uuid=$1`, f.endpoint.UUID)
	// The programmatic default is 24h; a two-hour-old window remains enabled.
	worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
	if err := worker.RunOnce(t.Context(), "default-window"); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='enabled'`, 1, f.endpoint.UUID)
	f.app.cfg.Webhook.FailureDisableAfter = time.Hour
	worker = webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
	f.exec(t, `UPDATE jobs SET run_after=clock_timestamp()-interval '1 second' WHERE status='retry'`)
	if err := worker.RunOnce(t.Context(), "custom-window"); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='failed' AND attempts=2`, 1)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='disabled' AND disabled_reason=$2`, 1, f.endpoint.UUID, sustainedFailureReason)
	enabled := "enabled"
	if _, err := f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, db.WebhookEndpointUpdate{Status: &enabled, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status.Store(204)
	if err := worker.RunOnce(t.Context(), "no-revival"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("old job revived: %d", calls.Load())
	}
	// Enqueue a fresh event through the existing producer boundary.
	webhooks.NewEnqueuer(f.app.db, f.app.cfg.Webhook, nil).Enqueue(t.Context(), webhooks.EnqueueInput{OccurredAt: time.Now(), WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: f.endpoint.OrganizationUUID, EventType: "session.status_idled", ResourceID: "sesn_fresh"})
	if err := worker.RunOnce(t.Context(), "fresh-event"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("fresh delivery count=%d", calls.Load())
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='enabled' AND failure_started_at IS NULL AND consecutive_failures=0`, 1, f.endpoint.UUID)
}

func TestWebhookFailureWindowConcurrentSuccessAndFailure(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	f.enqueue(t, 2)
	first, second := f.lease(t), f.lease(t)
	f.exec(t, `UPDATE webhook_endpoints SET failure_started_at=clock_timestamp()-interval '1 hour',consecutive_failures=4 WHERE uuid=$1`, f.endpoint.UUID)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		applied, err := f.app.db.CompleteWebhookDeliveryJob(t.Context(), first, true)
		if err == nil && !applied {
			err = fmt.Errorf("success not applied")
		}
		results <- err
	}()
	go func() {
		<-start
		applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), second, windowFailure())
		if err == nil && !applied {
			err = fmt.Errorf("failure not applied")
		}
		results <- err
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	endpoint, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Status != "enabled" || endpoint.DisabledReason != nil {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}
	switch endpoint.ConsecutiveFailures {
	case 0:
		if endpoint.FailureStartedAt != nil {
			t.Fatal("success did not clear window")
		}
	case 1:
		if endpoint.FailureStartedAt == nil {
			t.Fatal("failure did not restart window")
		}
		assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND failure_started_at>clock_timestamp()-interval '1 minute'`, 1, endpoint.UUID)
	default:
		t.Fatalf("lost update: %+v", endpoint)
	}
}
