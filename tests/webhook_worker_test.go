package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

type deliveryFixture struct {
	app      *testApp
	endpoint db.WebhookEndpoint
}

func newDeliveryFixture(t *testing.T, handler http.HandlerFunc) deliveryFixture {
	t.Helper()
	receiver := httptest.NewServer(handler)
	t.Cleanup(receiver.Close)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Webhook = config.WebhookConfig{WorkerEnabled: true, AllowInsecure: true, Timeout: time.Second, MaxAttempts: 3}
	app := newTestAppWithStore(t, &cfg, newFakeStore("delivery-worker"))
	t.Cleanup(app.close)
	clearWebhookState(t, app)
	t.Cleanup(func() { clearWebhookState(t, app) })
	endpoint := createWebhook(t, app, `{"url":`+quoteJSON(receiver.URL)+`,"enabled_events":["session.status_idled"]}`)
	key, err := app.db.GetAPIKey(t.Context(), auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := app.db.GetWebhookEndpoint(t.Context(), key.WorkspaceUUID.String(), endpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	return deliveryFixture{app: app, endpoint: stored}
}

func (f deliveryFixture) enqueue(t *testing.T, count int) {
	t.Helper()
	enqueuer := webhooks.NewEnqueuer(f.app.db, f.app.cfg.Webhook, nil)
	for range count {
		enqueuer.Enqueue(t.Context(), webhooks.EnqueueInput{
			OccurredAt:    time.Now().UTC(),
			WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: f.endpoint.OrganizationUUID,
			EventType: "session.status_idled", ResourceID: "sesn_" + uuid.NewV4().String(),
		})
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery'`, count)
}

func (f deliveryFixture) lease(t *testing.T) db.WebhookDeliveryJob {
	t.Helper()
	jobs, err := f.app.db.LeaseWebhookDeliveryJobs(t.Context(), "same-worker", 1, time.Minute)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("lease count=%d error=%v", len(jobs), err)
	}
	return jobs[0]
}

func (f deliveryFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.app.pool.Exec(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f deliveryFixture) assertState(t *testing.T, job db.WebhookDeliveryJob, status string, attempts, failures int) {
	t.Helper()
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE uuid=$1 AND status=$2 AND attempts=$3`, 1, job.UUID, status, attempts)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=$2`, 1, f.endpoint.UUID, failures)
}

func TestWebhookWorkerTargetIsolation(t *testing.T) {
	for _, mode := range []string{"deleted", "missing", "disabled", "other-workspace"} {
		t.Run(mode, func(t *testing.T) {
			var endpointCalls, globalCalls atomic.Int32
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { endpointCalls.Add(1); w.WriteHeader(204) })
			global := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { globalCalls.Add(1); w.WriteHeader(204) }))
			defer global.Close()
			cfg := f.app.cfg.Webhook
			cfg.EndpointURL, cfg.SigningKey = global.URL, f.endpoint.SigningSecret
			f.enqueue(t, 1)
			switch mode {
			case "deleted":
				if err := f.app.db.DeleteWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID); err != nil {
					t.Fatal(err)
				}
			case "missing":
				f.exec(t, `DELETE FROM webhook_endpoints WHERE uuid=$1`, f.endpoint.UUID)
			case "disabled":
				f.exec(t, `UPDATE webhook_endpoints SET status='disabled', consecutive_failures=4 WHERE uuid=$1`, f.endpoint.UUID)
			case "other-workspace":
				f.exec(t, `UPDATE jobs SET workspace_uuid=$1 WHERE type='webhook_delivery'`, uuid.NewV4().String())
			}
			if err := webhooks.NewWorker(f.app.db, cfg, nil).RunOnce(t.Context(), "isolation"); err != nil {
				t.Fatal(err)
			}
			if endpointCalls.Load() != 0 || globalCalls.Load() != 0 {
				t.Fatalf("unexpected calls: endpoint=%d global=%d", endpointCalls.Load(), globalCalls.Load())
			}
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed' AND attempts=0`, 1)
			if mode == "disabled" {
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=4`, 1, f.endpoint.UUID)
			}

			// Only an originally global job may take the configured global route.
			event := json.RawMessage(`{"id":"wevt_legacy","type":"event","data":{"type":"session.status_idled"}}`)
			if err := f.app.db.EnqueueWebhookDeliveryJob(t.Context(), f.endpoint.WorkspaceUUID, "session.status_idled", event); err != nil {
				t.Fatal(err)
			}
			if err := webhooks.NewWorker(f.app.db, cfg, nil).RunOnce(t.Context(), "legacy"); err != nil {
				t.Fatal(err)
			}
			if globalCalls.Load() != 1 || endpointCalls.Load() != 0 {
				t.Fatal("global-only job did not preserve its target")
			}
		})
	}
}

func TestWebhookWorkerClaimFencing(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	f.enqueue(t, 1)
	first := f.lease(t)
	f.exec(t, `UPDATE webhook_endpoints SET consecutive_failures=4 WHERE uuid=$1`, f.endpoint.UUID)
	assertRejected := func(job db.WebhookDeliveryJob) {
		t.Helper()
		if applied, err := f.app.db.ExhaustWebhookDeliveryJob(t.Context(), job); err != nil || applied {
			t.Fatalf("stale exhaustion=%t %v", applied, err)
		}
		if applied, err := f.app.db.CompleteWebhookDeliveryJob(t.Context(), job, true); err != nil || applied {
			t.Fatalf("stale completion=%t %v", applied, err)
		}
		if applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), job, db.WebhookDeliveryFailure{Reason: "stale", RetryDelay: time.Minute, MaxAttempts: 3, Terminal: true, DisableAfter: 24 * time.Hour}); err != nil || applied {
			t.Fatalf("stale failure=%t %v", applied, err)
		}
	}
	wrong := first
	wrong.WorkspaceUUID = uuid.NewV4().String()
	assertRejected(wrong)
	wrong = first
	wrong.ClaimToken = "wrong-claim"
	assertRejected(wrong)
	f.exec(t, `UPDATE jobs SET locked_until=clock_timestamp()-interval '1 second' WHERE uuid=$1`, first.UUID)
	assertRejected(first) // Expired, even before another worker claims it.
	f.assertState(t, first, "running", 0, 4)
	second := f.lease(t)
	if second.ClaimToken == first.ClaimToken || second.ClaimToken == "" {
		t.Fatal("claim token reused")
	}
	assertRejected(first)
	f.assertState(t, second, "running", 0, 4)
	applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), second, db.WebhookDeliveryFailure{Reason: "temporary", RetryDelay: time.Minute, MaxAttempts: 3, DisableAfter: 24 * time.Hour})
	if err != nil || !applied {
		t.Fatalf("current failure=%t %v", applied, err)
	}
	f.assertState(t, second, "retry", 1, 5)
	assertRejected(second)
	f.assertState(t, second, "retry", 1, 5)
	f.exec(t, `UPDATE jobs SET run_after=NOW()-interval '1 second' WHERE uuid=$1`, second.UUID)
	third := f.lease(t)
	applied, err = f.app.db.CompleteWebhookDeliveryJob(t.Context(), third, true)
	if err != nil || !applied {
		t.Fatalf("current completion=%t %v", applied, err)
	}
	assertRejected(third)
	f.assertState(t, third, "completed", 1, 0)
}

func TestWebhookWorkerResultRollback(t *testing.T) {
	for _, failure := range []bool{true, false} {
		t.Run(fmt.Sprintf("failure=%t", failure), func(t *testing.T) {
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			f.enqueue(t, 1)
			job := f.lease(t)
			f.exec(t, `UPDATE webhook_endpoints SET consecutive_failures=4 WHERE uuid=$1`, f.endpoint.UUID)
			remove := installWebhookMutationFailure(t, f.app, "webhook_endpoints", "UPDATE", "NEW.uuid = '"+f.endpoint.UUID+"'")
			finish := func() (bool, error) {
				if failure {
					return f.app.db.FailWebhookDeliveryJob(t.Context(), job, db.WebhookDeliveryFailure{Reason: "temporary", RetryDelay: time.Minute, MaxAttempts: 3, DisableAfter: 24 * time.Hour})
				}
				return f.app.db.CompleteWebhookDeliveryJob(t.Context(), job, true)
			}
			if applied, err := finish(); err == nil || applied {
				t.Fatalf("rollback=%t %v", applied, err)
			}
			f.assertState(t, job, "running", 0, 4)
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE uuid=$1 AND locked_by=$2`, 1, job.UUID, job.ClaimToken)
			remove()
			if applied, err := finish(); err != nil || !applied {
				t.Fatalf("retry result=%t %v", applied, err)
			}
			if failure {
				f.assertState(t, job, "retry", 1, 5)
			} else {
				f.assertState(t, job, "completed", 0, 0)
			}
		})
	}
}

func TestWebhookWorkerConcurrentBatch(t *testing.T) {
	entered := make(chan capturedWebhookRequest, 11)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var active, peak atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); n > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, n) {
				break
			}
		}
		entered <- capturedWebhookRequest{Header: r.Header.Clone(), Body: body}
		select {
		case <-release:
			w.WriteHeader(204)
		case <-r.Context().Done():
		}
	})
	// Ensure a failed assertion releases blocked receiver handlers before cleanup.
	t.Cleanup(unblock)
	f.app.cfg.Webhook.Timeout = 10 * time.Second
	f.enqueue(t, 11)
	worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
	done := make(chan error, 1)
	go func() { done <- worker.RunOnce(t.Context(), "parallel") }()
	sdk := anthropic.NewClient(option.WithWebhookKey(f.endpoint.SigningSecret))
	for range 10 {
		select {
		case req := <-entered:
			if _, err := sdk.Beta.Webhooks.Unwrap(req.Body, req.Header); err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("10 requests did not enter concurrently")
		}
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='pending'`, 1)
	if peak.Load() != 10 {
		t.Fatalf("peak=%d", peak.Load())
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not finish")
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed'`, 10)
	if err := worker.RunOnce(t.Context(), "parallel"); err != nil {
		t.Fatal(err)
	}
	if len(entered) != 1 || peak.Load() != 10 {
		t.Fatal("eleventh task or concurrency limit incorrect")
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed'`, 11)
}

func TestWebhookWorkerIndependentFailuresAndRetrySignature(t *testing.T) {
	for _, status := range []int{500, 0} {
		t.Run(fmt.Sprintf("status=%d", status), func(t *testing.T) {
			var calls atomic.Int32
			captured := make(chan capturedWebhookRequest, 3)
			f := newDeliveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				captured <- capturedWebhookRequest{Header: r.Header.Clone(), Body: body}
				if calls.Add(1) == 1 {
					if status == 0 {
						<-r.Context().Done()
						return
					}
					w.WriteHeader(status)
					return
				}
				w.WriteHeader(204)
			})
			f.app.cfg.Webhook.Timeout = 100 * time.Millisecond
			f.enqueue(t, 2)
			worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
			if err := worker.RunOnce(t.Context(), "independent"); err != nil {
				t.Fatal(err)
			}
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed'`, 1)
			assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='retry' AND attempts=1`, 1)
			var retryID string
			if err := f.app.pool.QueryRow(t.Context(), `SELECT payload->'event'->>'id' FROM jobs WHERE type='webhook_delivery' AND status='retry'`).Scan(&retryID); err != nil {
				t.Fatal(err)
			}
			f.exec(t, `UPDATE jobs SET run_after=NOW()-interval '1 second' WHERE type='webhook_delivery' AND status='retry'`)
			if err := worker.RunOnce(t.Context(), "retry"); err != nil {
				t.Fatal(err)
			}
			sdk := anthropic.NewClient(option.WithWebhookKey(f.endpoint.SigningSecret))
			counts := map[string]int{}
			bodies := map[string]string{}
			for range 3 {
				req := <-captured
				if _, err := sdk.Beta.Webhooks.Unwrap(req.Body, req.Header); err != nil {
					t.Fatal(err)
				}
				id := req.Header.Get("webhook-id")
				if previous, found := bodies[id]; found && previous != string(req.Body) {
					t.Fatal("retry changed payload or occurrence time")
				}
				bodies[id] = string(req.Body)
				counts[id]++
			}
			if counts[retryID] != 2 {
				t.Fatal("retry changed event ID")
			}
		})
	}
}

func TestWebhookWorkerCancellationAndLeaseBudget(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	f := newDeliveryFixture(t, func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(exited)
	})
	f.app.cfg.Webhook.Timeout = 70 * time.Second
	f.enqueue(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil).RunOnce(ctx, "cancel") }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery")
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND locked_until-updated_at=interval '100 seconds'`, 1)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled write reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit")
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request did not exit")
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='running' AND attempts=0`, 1)
	f.exec(t, `UPDATE jobs SET locked_until=NOW()-interval '1 second' WHERE type='webhook_delivery'`)
	recovered := f.lease(t)
	if applied, err := f.app.db.CompleteWebhookDeliveryJob(t.Context(), recovered, false); err != nil || !applied {
		t.Fatalf("recovery=%t %v", applied, err)
	}
}

func TestWebhookWorkerConcurrentClaims(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	f.enqueue(t, 20)
	type result struct {
		jobs []db.WebhookDeliveryJob
		err  error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			jobs, err := f.app.db.LeaseWebhookDeliveryJobs(t.Context(), "same-name", 10, time.Minute)
			results <- result{jobs, err}
		}()
	}
	close(start)
	seen := map[string]bool{}
	tokens := map[string]bool{}
	for range 2 {
		r := <-results
		if r.err != nil || len(r.jobs) != 10 {
			t.Fatalf("jobs=%d error=%v", len(r.jobs), r.err)
		}
		tokens[r.jobs[0].ClaimToken] = true
		for _, job := range r.jobs {
			if seen[job.UUID] {
				t.Fatal("duplicate lease")
			}
			seen[job.UUID] = true
		}
	}
	if len(seen) != 20 || len(tokens) != 2 {
		t.Fatal("claims were not independent")
	}
	jobs, err := f.app.db.LeaseWebhookDeliveryJobs(t.Context(), "third", 10, time.Minute)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("unexpired jobs reclaimed: %d %v", len(jobs), err)
	}
}

func TestWebhookWorkerCommitFailure(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	f.enqueue(t, 1)
	job := f.lease(t)
	// A deferred constraint trigger fails COMMIT, after both updates succeeded.
	f.exec(t, `CREATE FUNCTION test_webhook_result_commit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.type='webhook_delivery' AND NEW.status='retry' THEN RAISE EXCEPTION 'test commit rejected'; END IF; RETURN NEW; END $$;
 CREATE CONSTRAINT TRIGGER test_webhook_result_commit AFTER UPDATE ON jobs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION test_webhook_result_commit()`)
	t.Cleanup(func() {
		if _, err := f.app.pool.Exec(context.Background(), `DROP TRIGGER test_webhook_result_commit ON jobs; DROP FUNCTION test_webhook_result_commit()`); err != nil {
			t.Error(err)
		}
	})
	applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), job, db.WebhookDeliveryFailure{Reason: "temporary", RetryDelay: time.Minute, MaxAttempts: 3, DisableAfter: 24 * time.Hour})
	if err == nil || applied {
		t.Fatalf("commit failure=%t %v", applied, err)
	}
	f.assertState(t, job, "running", 0, 0)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE uuid=$1 AND locked_by=$2`, 1, job.UUID, job.ClaimToken)
}

func TestWebhookWorkerConcurrentFailuresDoNotUseCount(t *testing.T) {
	var calls atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	f.enqueue(t, 21)
	worker := webhooks.NewWorker(f.app.db, f.app.cfg.Webhook, nil)
	var firstWindow *time.Time
	for range 3 {
		if err := worker.RunOnce(t.Context(), "threshold"); err != nil {
			t.Fatal(err)
		}
		endpoint, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
		if err != nil || endpoint.FailureStartedAt == nil {
			t.Fatalf("missing window: %+v %v", endpoint, err)
		}
		if firstWindow == nil {
			firstWindow = endpoint.FailureStartedAt
		} else if !endpoint.FailureStartedAt.Equal(*firstWindow) {
			t.Fatal("concurrent failures moved the first failure time")
		}
	}
	if calls.Load() != 21 {
		t.Fatalf("requests=%d, want 21 without disabling", calls.Load())
	}
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=21 AND status='enabled' AND failure_started_at IS NOT NULL`, 1, f.endpoint.UUID)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='retry' AND attempts=1`, 21)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND status='completed' AND attempts=0`, 0)
}
