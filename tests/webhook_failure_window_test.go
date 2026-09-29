package tests

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

const sustainedFailureReason = "auto-disabled after sustained delivery failures"

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
			if tc.age != "" {
				f.exec(t, `UPDATE webhook_endpoints SET failure_started_at=clock_timestamp()-CAST($2 AS interval) WHERE uuid=$1`, f.endpoint.UUID, tc.age)
			}
			before, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			applied, err := f.app.db.RecordWebhookDeliveryFailure(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.UUID, "receiver unavailable", false, 24*time.Hour)
			if err != nil || applied != tc.disabled {
				t.Fatalf("failure=%t %v", applied, err)
			}
			after, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			if after.FailureStartedAt == nil || (before.FailureStartedAt != nil && !after.FailureStartedAt.Equal(*before.FailureStartedAt)) {
				t.Fatal("failure window changed incorrectly")
			}

			if tc.disabled {
				if after.Status != "disabled" || after.DisabledReason == nil || *after.DisabledReason != sustainedFailureReason {
					t.Fatalf("endpoint=%+v", after)
				}
			} else if after.Status != "enabled" || after.DisabledReason != nil {
				t.Fatalf("endpoint=%+v", after)
			}
		})
	}
}

func TestWebhookFailureWindowResetAndEdits(t *testing.T) {
	for _, operation := range []string{"metadata", "secret", "disable", "enable", "success", "late-success"} {
		t.Run(operation, func(t *testing.T) {
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			f.enqueue(t, 1)
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
				err = f.app.db.RecordWebhookDeliverySuccess(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.UUID)
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
	worker := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='enabled'`, 1, f.endpoint.UUID)
	f.app.cfg.Webhook.FailureDisableAfter = time.Hour
	worker = webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
	drainWebhookQueue(t, f.app, worker)
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertWebhookQueueCount(t, f.app, 0)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND status='disabled' AND disabled_reason=$2`, 1, f.endpoint.UUID, sustainedFailureReason)
	enabled := "enabled"
	if _, err := f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, db.WebhookEndpointUpdate{Status: &enabled, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status.Store(204)
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("old job revived: %d", calls.Load())
	}
	// Enqueue a fresh event through the existing producer boundary.
	webhooks.NewEnqueuer(f.app.db, f.app.webhookQueue, nil).Enqueue(t.Context(), webhooks.EnqueueInput{OccurredAt: time.Now(), WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: f.endpoint.OrganizationUUID, EventType: "session.status_idled", ResourceID: "sesn_fresh"})
	if err := worker.RunOnce(t.Context()); err != nil {
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
	f.exec(t, `UPDATE webhook_endpoints SET failure_started_at=clock_timestamp()-interval '1 hour',consecutive_failures=4 WHERE uuid=$1`, f.endpoint.UUID)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		err := f.app.db.RecordWebhookDeliverySuccess(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.UUID)
		results <- err
	}()
	go func() {
		<-start
		_, err := f.app.db.RecordWebhookDeliveryFailure(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.UUID, "temporary", false, 24*time.Hour)
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
