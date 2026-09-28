package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
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
			webhooks.NewEnqueuer(f.app.db, nil).Enqueue(t.Context(), webhooks.EnqueueInput{
				OccurredAt: time.Now(), WorkspaceUUID: workspace, OrganizationUUID: f.endpoint.OrganizationUUID,
				EventType: "session.status_idled", ResourceID: "sesn_unmatched",
			})
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery'`, 0)
		})
	}
}

func TestWebhookHistoricalUntargetedJobs(t *testing.T) {
	var calls atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) })
	failureStart := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	f.exec(t, `UPDATE webhook_endpoints SET consecutive_failures=7, failure_started_at=$2 WHERE uuid=$1`, f.endpoint.UUID, failureStart)
	cases := []struct {
		name, state, target, want string
		leaseSeconds              int
	}{
		{"pending", "pending", "", "completed", 0},
		{"retry-null", "retry", `,"webhook_endpoint_uuid":null`, "completed", 0},
		{"expired-empty", "running", `,"webhook_endpoint_uuid":""`, "completed", -60},
		{"running", "running", "", "running", 60},
		{"completed", "completed", "", "completed", 0},
		{"failed", "failed", "", "failed", 0},
	}
	expectedPayloads := make(map[string]string, len(cases))
	for _, tc := range cases {
		payload := `{"event_type":"session.status_idled","event":{"id":"wevt_history","created_at":"2020-01-01T00:00:00Z","type":"event","data":{"type":"session.status_idled"}},"last_error":"old failure"` + tc.target + `}`
		expectedPayloads[tc.name] = payload
		f.exec(t, `INSERT INTO jobs (external_id, workspace_uuid, type, status, payload, attempts, run_after, locked_by, locked_until) VALUES ($1,$2,'webhook_delivery',$3,$4::jsonb,9,NOW()-interval '1 hour','old-worker',NOW()+$5*interval '1 second')`, "job_history_"+tc.name, f.endpoint.WorkspaceUUID, tc.state, payload, tc.leaseSeconds)
	}
	cfg := f.app.cfg.Webhook
	cfg.WorkerEnabled = false
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	webhooks.NewWorker(f.app.db, cfg, nil).Start(ctx)
	time.Sleep(100 * time.Millisecond)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE external_id='job_history_pending' AND status='pending'`, 1)
	worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
	if err := worker.RunOnce(t.Context(), "history"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE external_id=$1 AND status=$2 AND attempts=9 AND payload=$3::jsonb AND run_after < NOW()`, 1, "job_history_"+tc.name, tc.want, expectedPayloads[tc.name])
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE external_id IN ('job_history_pending','job_history_retry-null','job_history_expired-empty') AND locked_by IS NULL AND locked_until IS NULL`, 3)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE external_id='job_history_running' AND locked_by='old-worker'`, 1)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=7 AND failure_started_at=$2`, 1, f.endpoint.UUID, failureStart)
	if calls.Load() != 0 {
		t.Fatal("historical job sent HTTP")
	}
}

func TestWebhookHistoricalSkipClaimFencing(t *testing.T) {
	f := newDeliveryFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected delivery") })
	f.enqueue(t, 1)
	f.exec(t, `UPDATE jobs SET payload=payload-'webhook_endpoint_uuid' WHERE type='webhook_delivery'`)
	first := f.lease(t)
	if first.WebhookEndpointUUID != nil {
		t.Fatal("historical target was synthesized")
	}
	f.exec(t, `UPDATE jobs SET locked_until=NOW()-interval '1 second' WHERE uuid=$1`, first.UUID)
	if applied, err := f.app.db.CompleteWebhookDeliveryJob(t.Context(), first, false); err != nil || applied {
		t.Fatalf("expired skip: %t %v", applied, err)
	}
	second := f.lease(t)
	if applied, err := f.app.db.CompleteWebhookDeliveryJob(t.Context(), first, false); err != nil || applied {
		t.Fatalf("stale skip: %t %v", applied, err)
	}
	if applied, err := f.app.db.CompleteWebhookDeliveryJob(t.Context(), second, false); err != nil || !applied {
		t.Fatalf("current skip: %t %v", applied, err)
	}
	if applied, err := f.app.db.CompleteWebhookDeliveryJob(t.Context(), second, false); err != nil || applied {
		t.Fatalf("duplicate skip: %t %v", applied, err)
	}
	f.assertState(t, second, "completed", 0, 0)
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
	webhooks.NewEnqueuer(f.app.db, nil).Enqueue(t.Context(), webhooks.EnqueueInput{
		OccurredAt: occurredAt, WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: scope.OrganizationUUID,
		WorkspaceExternalID: scope.WorkspaceExternalID, EventType: "session.status_idled", ResourceID: "sesn_fanout",
	})
	assertPayloadSQLCount(t, f.app, `SELECT count(DISTINCT payload->>'webhook_endpoint_uuid') FROM jobs WHERE type='webhook_delivery'`, 2)
	f.exec(t, `INSERT INTO jobs(external_id,workspace_uuid,type,status,payload) VALUES('job_legacy_mixed',$1,'webhook_delivery','pending','{"event_type":"session.status_idled","event":{"id":"wevt_legacy"}}')`, f.endpoint.WorkspaceUUID)
	if err := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil).RunOnce(t.Context(), "mixed"); err != nil {
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
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed' AND attempts=0`, 3)
}
