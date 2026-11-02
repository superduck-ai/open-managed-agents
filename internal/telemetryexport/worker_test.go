package telemetryexport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func testQueue(t *testing.T, cfg config.TelemetryExportConfig) *Queue {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS startup failed")
	}
	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	queue, err := NewQueue(t.Context(), conn, config.WebhookStreamConfig{MaxBytes: 64 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return queue
}

type deliveryStore struct {
	mutex        sync.Mutex
	destinations map[string]db.TelemetryDestination
	successes    int
	failures     []string
	disableAfter time.Duration
}

func (s *deliveryStore) GetTelemetryDestinationByUUID(_ context.Context, uuid string) (db.TelemetryDestination, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	destination, found := s.destinations[uuid]
	if !found {
		return db.TelemetryDestination{}, db.ErrNotFound
	}
	return destination, nil
}

func (s *deliveryStore) RecordTelemetryDeliverySuccess(context.Context, string, time.Time) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.successes++
	return nil
}

func (s *deliveryStore) RecordTelemetryDeliveryFailure(_ context.Context, uuid, failure, immediate string, disableAfter time.Duration, _ time.Time) (bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.failures = append(s.failures, failure)
	s.disableAfter = disableAfter
	if immediate != "" {
		destination := s.destinations[uuid]
		destination.State = "disabled"
		s.destinations[uuid] = destination
		return true, nil
	}
	return false, nil
}

func (s *deliveryStore) counts() (int, int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.successes, len(s.failures)
}

type plainOpener struct{}

func (plainOpener) Open(_ context.Context, binding secrets.Binding, envelope secrets.Envelope) ([]byte, error) {
	if binding.VaultExternalID != "telemetry_destination" {
		return nil, errors.New("unexpected binding")
	}
	return envelope.Ciphertext, nil
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestWorkerRetriesThenDeliversAndDisablesRedirects(t *testing.T) {
	cfg := config.TelemetryExportConfig{WorkerEnabled: true, Concurrency: 2, Timeout: 2 * time.Second, MaxAttempts: 3, FailureDisableAfter: time.Hour, AllowInsecure: true}
	queue := testQueue(t, cfg)
	var calls atomic.Int32
	var authorization atomic.Value
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization.Store(r.Header.Get("Authorization"))
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(flaky.Close)
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example.com", http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)
	headers, _ := json.Marshal(map[string]string{"Authorization": "Bearer customer"})
	store := &deliveryStore{destinations: map[string]db.TelemetryDestination{
		"flaky":      {UUID: "flaky", ExternalID: "tdst_flaky", OrganizationUUID: "org", State: "enabled", TracesEnabled: true, TracesEndpointURL: flaky.URL, Headers: &secrets.Envelope{Ciphertext: headers}},
		"redirect":   {UUID: "redirect", ExternalID: "tdst_redirect", OrganizationUUID: "org", State: "enabled", TracesEnabled: true, TracesEndpointURL: redirecting.URL},
		"disabled":   {UUID: "disabled", State: "disabled", TracesEnabled: true, TracesEndpointURL: flaky.URL},
		"no_traces":  {UUID: "no_traces", State: "enabled", TracesEnabled: false, TracesEndpointURL: flaky.URL},
		"downgraded": {UUID: "downgraded", State: "enabled", TracesEnabled: true, TracesContentMode: "metadata_only", TracesEndpointURL: flaky.URL},
		"shrunk":     {UUID: "shrunk", State: "enabled", TracesEnabled: true, TracesContentMode: "content", TracesMaxContentBytes: 100, TracesEndpointURL: flaky.URL},
		"matched":    {UUID: "matched", State: "enabled", TracesEnabled: true, TracesContentMode: "content", TracesMaxContentBytes: 100, TracesEndpointURL: flaky.URL},
	}}
	stop, err := NewWorker(store, plainOpener{}, queue, cfg, nil).Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	for _, destination := range []string{"flaky", "redirect", "disabled", "no_traces", "deleted"} {
		if err := queue.Publish(t.Context(), Delivery{DestinationUUID: destination, Payload: []byte("spans-" + destination)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.Publish(t.Context(), Delivery{DestinationUUID: "flaky", Payload: []byte("spans-flaky")}); err != nil {
		t.Fatal(err)
	}
	for _, delivery := range []Delivery{
		{DestinationUUID: "downgraded", ContentPolicy: "content:786432", Payload: []byte("content-spans")},
		{DestinationUUID: "shrunk", ContentPolicy: "content:786432", Payload: []byte("content-spans")},
		{DestinationUUID: "matched", ContentPolicy: "content:100", Payload: []byte("content-spans")},
	} {
		if err := queue.Publish(t.Context(), delivery); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool {
		successes, failures := store.counts()
		return successes == 2 && failures == 2
	})
	waitFor(t, func() bool {
		info, err := queue.consumer.Info(t.Context())
		return err == nil && info.NumPending == 0 && info.NumAckPending == 0
	})
	if calls.Load() != 3 || authorization.Load() != "Bearer customer" || store.destinations["redirect"].State != "disabled" || store.disableAfter != time.Hour {
		t.Fatalf("calls=%d authorization=%v store=%+v", calls.Load(), authorization.Load(), store.destinations["redirect"])
	}
	if err := queue.Publish(t.Context(), Delivery{DestinationUUID: "flaky", Payload: make([]byte, maxMessageBytes)}); err == nil {
		t.Fatal("accepted an oversized delivery")
	}
}

type staticStore struct {
	destinations []db.TelemetryDestination
	err          error
}

func (s staticStore) ListTelemetryDestinationsForWorkspace(context.Context, string, string) ([]db.TelemetryDestination, error) {
	return s.destinations, s.err
}

type capturePublisher struct {
	deliveries []Delivery
	err        error
}

func (p *capturePublisher) Publish(_ context.Context, delivery Delivery) error {
	p.deliveries = append(p.deliveries, delivery)
	return p.err
}

func TestExporterCoverageAndPublishing(t *testing.T) {
	failing := NewExporter(staticStore{err: errors.New("down")}, &capturePublisher{})
	if _, err := failing.Coverage(context.Background(), "org", "ws"); err == nil {
		t.Fatal("store failure hidden")
	}
	publisher := &capturePublisher{err: errors.New("queue full")}
	exporter := NewExporter(staticStore{destinations: []db.TelemetryDestination{destination("a", "metadata_only", 10)}}, publisher)
	if _, err := exporter.Export(context.Background(), testSource, request(span("oma.session.turn"))); err == nil {
		t.Fatal("publish failure hidden")
	}
	exporter = NewExporter(staticStore{}, &capturePublisher{})
	if coverage, err := exporter.Coverage(context.Background(), "org", "ws"); err != nil || coverage.Enabled {
		t.Fatalf("coverage without destinations = %+v %v", coverage, err)
	}
	publisher = &capturePublisher{}
	exporter = NewExporter(staticStore{destinations: []db.TelemetryDestination{destination("a", "metadata_only", 10), destination("b", "content", 10)}}, publisher)
	coverage, err := exporter.Coverage(context.Background(), "org", "ws")
	if err != nil || !coverage.Enabled || !coverage.Content {
		t.Fatalf("coverage = %+v %v", coverage, err)
	}
	rejected, err := exporter.Export(context.Background(), testSource, request(span("oma.session.turn"), span("bogus")))
	if err != nil || rejected != 1 || len(publisher.deliveries) != 2 {
		t.Fatalf("rejected=%d deliveries=%d err=%v", rejected, len(publisher.deliveries), err)
	}
	empty := &coltracepb.ExportTraceServiceRequest{}
	if rejected, err := exporter.Export(context.Background(), testSource, empty); err != nil || rejected != 0 {
		t.Fatal("empty export failed")
	}
}

func TestQueueDeduplicatesPerContentPolicy(t *testing.T) {
	queue := testQueue(t, config.TelemetryExportConfig{MaxAttempts: 1, Timeout: time.Second})
	for _, delivery := range []Delivery{
		{DestinationUUID: "d", ContentPolicy: "metadata_only", Payload: []byte("spans")},
		{DestinationUUID: "d", ContentPolicy: "content:100", Payload: []byte("spans")},
		{DestinationUUID: "d", ContentPolicy: "content:100", Payload: []byte("spans")},
	} {
		if err := queue.Publish(t.Context(), delivery); err != nil {
			t.Fatal(err)
		}
	}
	stream, err := queue.js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := stream.Info(t.Context()); err != nil || info.State.Msgs != 2 {
		t.Fatalf("stream messages = %+v %v", info, err)
	}
}
