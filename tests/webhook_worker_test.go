package tests

import (
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
	enqueuer := webhooks.NewEnqueuer(f.app.db, f.app.webhookQueue, nil)
	for range count {
		enqueuer.Enqueue(t.Context(), webhooks.EnqueueInput{
			OccurredAt:    time.Now().UTC(),
			WorkspaceUUID: f.endpoint.WorkspaceUUID, OrganizationUUID: f.endpoint.OrganizationUUID,
			EventType: "session.status_idled", ResourceID: "sesn_" + uuid.NewV4().String(),
		})
	}
	assertWebhookQueueCount(t, f.app, count)
}

func (f deliveryFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.app.pool.Exec(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookWorkerTargetIsolation(t *testing.T) {
	for _, mode := range []string{"deleted", "missing", "disabled", "other-workspace"} {
		t.Run(mode, func(t *testing.T) {
			var endpointCalls atomic.Int32
			f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { endpointCalls.Add(1); w.WriteHeader(204) })
			cfg := f.app.cfg.Webhook
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
				f.exec(t, `UPDATE webhook_endpoints SET workspace_uuid=$1 WHERE uuid=$2`, uuid.NewV4().String(), f.endpoint.UUID)
			}
			if err := webhooks.NewWorker(f.app.db, f.app.webhookQueue, cfg, nil).RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if endpointCalls.Load() != 0 {
				t.Fatalf("unexpected calls: endpoint=%d", endpointCalls.Load())
			}
			assertWebhookQueueCount(t, f.app, 0)
			if mode == "disabled" {
				assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=4`, 1, f.endpoint.UUID)
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
	worker := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
	done := make(chan error, 1)
	go func() { done <- worker.RunOnce(t.Context()) }()
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
	assertWebhookQueueCount(t, f.app, 11)
	if inUse := f.app.db.SQLDB().Stats().InUse; inUse != 0 {
		t.Fatalf("HTTP requests retained %d database connections", inUse)
	}
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
	assertWebhookQueueCount(t, f.app, 1)
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(entered) != 1 || peak.Load() != 10 {
		t.Fatal("eleventh task or concurrency limit incorrect")
	}
	assertWebhookQueueCount(t, f.app, 0)
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
			worker := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
			if err := worker.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertWebhookQueueCount(t, f.app, 1)
			retryID := queuedWebhookEvents(t, f.app)[0].ID
			drainWebhookQueue(t, f.app, worker)

			sdk := anthropic.NewClient(option.WithWebhookKey(f.endpoint.SigningSecret))
			counts := map[string]int{}
			bodies := map[string]string{}
			timestamps := map[string]string{}
			for range 3 {
				req := <-captured
				if _, err := sdk.Beta.Webhooks.Unwrap(req.Body, req.Header); err != nil {
					t.Fatal(err)
				}
				id := req.Header.Get("webhook-id")
				if previous, found := bodies[id]; found && previous != string(req.Body) {
					t.Fatal("retry changed payload or occurrence time")
				}
				if previous, found := timestamps[id]; found && previous == req.Header.Get("webhook-timestamp") {
					t.Fatal("retry did not regenerate signing time")
				}
				timestamps[id] = req.Header.Get("webhook-timestamp")
				bodies[id] = string(req.Body)
				counts[id]++
			}
			if counts[retryID] != 2 {
				t.Fatal("retry changed event ID")
			}
		})
	}
}

func TestWebhookWorkerConcurrentFailuresDoNotUseCount(t *testing.T) {
	var calls atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	f.enqueue(t, 21)
	worker := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
	var firstWindow *time.Time
	for range 3 {
		if err := worker.RunOnce(t.Context()); err != nil {
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
	assertWebhookQueueCount(t, f.app, 21)
}

func TestWebhookStatisticsFailureDoesNotRetrySuccessfulHTTP(t *testing.T) {
	var calls atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) })
	f.exec(t, `UPDATE webhook_endpoints SET consecutive_failures=4 WHERE uuid=$1`, f.endpoint.UUID)
	f.enqueue(t, 1)
	remove := installWebhookMutationFailure(t, f.app, "webhook_endpoints", "UPDATE", "NEW.uuid = '"+f.endpoint.UUID+"'")
	worker := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertWebhookQueueCount(t, f.app, 0)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=4`, 1, f.endpoint.UUID)
	remove()
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("statistics failure duplicated successful HTTP")
	}
}

func TestWebhookWorkerMultipleInstances(t *testing.T) {
	received := make(chan string, 20)
	f := newDeliveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("webhook-id")
		w.WriteHeader(204)
	})
	f.enqueue(t, 20)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			if err := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil).RunOnce(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	assertWebhookQueueCount(t, f.app, 0)
	if len(received) != 20 {
		t.Fatalf("deliveries=%d", len(received))
	}
	seen := map[string]bool{}
	for range 20 {
		id := <-received
		if seen[id] {
			t.Fatal("concurrent duplicate consumption")
		}
		seen[id] = true
	}
}

func TestWebhookWorkerStopCancelsActiveBatch(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	f := newDeliveryFixture(t, func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(exited)
	})
	f.enqueue(t, 1)
	f.app.cfg.Webhook.Timeout = time.Minute
	stop := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil).Start(t.Context())
	defer stop()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no active request")
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not wait/cancel")
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("request leaked")
	}
	assertWebhookQueueCount(t, f.app, 1)
}

func TestWebhookWorkerUsesCurrentTarget(t *testing.T) {
	f := newDeliveryFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("old endpoint received request") })
	f.enqueue(t, 1)
	received := make(chan capturedWebhookRequest, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- capturedWebhookRequest{Header: r.Header.Clone(), Body: body}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	const secret = "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	f.exec(t, `UPDATE webhook_endpoints SET url=$2,signing_secret=$3,enabled_events='[]'::jsonb WHERE uuid=$1`, f.endpoint.UUID, receiver.URL, secret)
	if err := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 {
		t.Fatal("queued event lost when event selection changed")
	}
	request := <-received
	sdk := anthropic.NewClient(option.WithWebhookKey(secret))
	if _, err := sdk.Beta.Webhooks.Unwrap(request.Body, request.Header); err != nil {
		t.Fatal(err)
	}
	assertWebhookQueueCount(t, f.app, 0)
}
