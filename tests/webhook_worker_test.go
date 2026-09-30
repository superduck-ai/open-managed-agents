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
			drainWebhookQueue(t, f.app, webhooks.NewWorker(f.app.db, f.app.webhookQueue, cfg, nil))
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

func TestWebhookWorkerAutomaticRefill(t *testing.T) {
	type blockedRequest struct {
		request capturedWebhookRequest
		release chan struct{}
	}
	entered := make(chan blockedRequest, 11)
	releaseAll := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(releaseAll) }) }
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
		release := make(chan struct{})
		entered <- blockedRequest{capturedWebhookRequest{Header: r.Header.Clone(), Body: body}, release}
		select {
		case <-release:
			w.WriteHeader(204)
		case <-releaseAll:
			w.WriteHeader(204)
		case <-r.Context().Done():
		}
	})
	t.Cleanup(unblock)
	f.app.cfg.Webhook.Timeout = 10 * time.Second
	f.enqueue(t, 11)
	stop := startWebhookWorker(t, webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil))
	defer stop()
	defer unblock()
	sdk := anthropic.NewClient(option.WithWebhookKey(f.endpoint.SigningSecret))
	receive := func() blockedRequest {
		t.Helper()
		select {
		case request := <-entered:
			if _, err := sdk.Beta.Webhooks.Unwrap(request.request.Body, request.request.Header); err != nil {
				t.Fatal(err)
			}
			return request
		case <-time.After(5 * time.Second):
			t.Fatal("request did not enter")
			return blockedRequest{}
		}
	}
	first := receive()
	for range 9 {
		receive()
	}
	if inUse := f.app.db.SQLDB().Stats().InUse; inUse != 0 {
		t.Fatalf("HTTP retained %d DB connections", inUse)
	}
	select {
	case <-entered:
		t.Fatal("eleventh request entered before a slot was free")
	case <-time.After(100 * time.Millisecond):
	}
	close(first.release)
	receive()
	if active.Load() != 10 || peak.Load() != 10 {
		t.Fatalf("active=%d peak=%d", active.Load(), peak.Load())
	}
	unblock()
	waitWebhookQueueEmpty(t, f.app)
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
			if len(counts) != 2 {
				t.Fatal("retry changed event ID")
			}
			retried := false
			for _, count := range counts {
				retried = retried || count == 2
			}
			if !retried {
				t.Fatal("retry was not delivered")
			}
		})
	}
}

func TestWebhookWorkerConcurrentFailuresDoNotUseCount(t *testing.T) {
	var calls atomic.Int32
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	f.enqueue(t, 21)
	worker := webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil)
	stop := startWebhookWorker(t, worker)
	defer stop()
	waitWebhookCondition(t, func() bool {
		endpoint, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
		if err != nil {
			t.Fatal(err)
		}
		return endpoint.ConsecutiveFailures == 21 && endpoint.FailureStartedAt != nil
	})
	stop()

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
	drainWebhookQueue(t, f.app, worker)
	assertWebhookQueueCount(t, f.app, 0)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=4`, 1, f.endpoint.UUID)
	remove()
	drainWebhookQueue(t, f.app, worker)
	if calls.Load() != 1 {
		t.Fatal("statistics failure duplicated successful HTTP")
	}
}

func TestWebhookWorkerMultipleInstances(t *testing.T) {
	received := make(chan string, 20)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	f := newDeliveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("webhook-id")
		select {
		case <-release:
			w.WriteHeader(204)
		case <-r.Context().Done():
		}
	})
	t.Cleanup(unblock)
	f.app.cfg.Webhook.Timeout = 10 * time.Second
	f.enqueue(t, 20)
	first := startWebhookWorker(t, webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil))
	defer first()
	defer unblock()
	waitWebhookCondition(t, func() bool { return len(received) == 10 })
	second := startWebhookWorker(t, webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil))
	defer second()
	defer unblock()
	waitWebhookCondition(t, func() bool { return len(received) == 20 })
	unblock()
	waitWebhookQueueEmpty(t, f.app)
	seen := map[string]bool{}
	for range 20 {
		id := <-received
		if seen[id] {
			t.Fatal("concurrent duplicate consumption")
		}
		seen[id] = true
	}
}

func TestWebhookWorkerStopCancelsActiveRequests(t *testing.T) {
	entered, exited := make(chan struct{}, 10), make(chan struct{}, 10)
	f := newDeliveryFixture(t, func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- struct{}{}
		<-r.Context().Done()
		exited <- struct{}{}
	})
	f.enqueue(t, 10)
	f.app.cfg.Webhook.Timeout = time.Minute
	stop := startWebhookWorker(t, webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil))
	defer stop()
	for range 10 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("no active request")
		}
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not wait/cancel")
	}
	for range 10 {
		select {
		case <-exited:
		case <-time.After(time.Second):
			t.Fatal("request leaked")
		}
	}
	assertWebhookQueueCount(t, f.app, 10)
	assertPayloadSQLCount(t, f.app, `SELECT count(*) FROM webhook_endpoints WHERE uuid=$1 AND consecutive_failures=0 AND failure_started_at IS NULL`, 1, f.endpoint.UUID)
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
	drainWebhookQueue(t, f.app, webhooks.NewWorker(f.app.db, f.app.webhookQueue, f.app.cfg.Webhook, nil))
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
