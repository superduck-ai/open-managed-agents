package telemetryexport

import (
	"context"
	"crypto/rand"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetrydestinations"
)

type DestinationStore interface {
	ListTelemetryDestinationsForWorkspace(context.Context, string, string) ([]db.TelemetryDestination, error)
}

type Publisher interface {
	Publish(context.Context, Delivery) error
}

type Exporter struct {
	store     DestinationStore
	publisher Publisher
}

func NewExporter(store DestinationStore, publisher Publisher) *Exporter {
	return &Exporter{store: store, publisher: publisher}
}

type Coverage struct {
	Enabled bool
	Content bool
}

func (e *Exporter) Coverage(ctx context.Context, organizationUUID, workspaceUUID string) (Coverage, error) {
	destinations, err := e.store.ListTelemetryDestinationsForWorkspace(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return Coverage{}, err
	}
	coverage := Coverage{Enabled: len(destinations) > 0}
	for _, destination := range destinations {
		coverage.Content = coverage.Content || destination.TracesContentMode == "content"
	}
	return coverage, nil
}

func (e *Exporter) Export(ctx context.Context, source Source, request *coltracepb.ExportTraceServiceRequest) (int, error) {
	destinations, err := e.store.ListTelemetryDestinationsForWorkspace(ctx, source.OrganizationUUID, source.WorkspaceUUID)
	if err != nil {
		return 0, err
	}
	deliveries, rejected, err := PrepareDeliveries(request, source, destinations)
	if err != nil {
		return rejected, err
	}
	for _, delivery := range deliveries {
		if err := e.publisher.Publish(ctx, delivery); err != nil {
			return rejected, err
		}
	}
	return rejected, nil
}

type Tester struct {
	opener        Opener
	allowInsecure bool
	timeout       time.Duration
}

func NewTester(opener Opener, cfg Config) *Tester {
	return &Tester{opener: opener, allowInsecure: cfg.AllowInsecure, timeout: cfg.Timeout}
}

type Config struct {
	AllowInsecure bool
	Timeout       time.Duration
}

func testPayload(destination db.TelemetryDestination, now time.Time) ([]byte, error) {
	traceID, spanID := make([]byte, 16), make([]byte, 8)
	_, _ = rand.Read(traceID)
	_, _ = rand.Read(spanID)
	return proto.Marshal(&coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: resourceFor(Source{OrganizationUUID: destination.OrganizationUUID}),
		ScopeSpans: []*tracepb.ScopeSpans{{Scope: &commonpb.InstrumentationScope{Name: ScopeName}, Spans: []*tracepb.Span{{
			TraceId: traceID, SpanId: spanID, Name: "oma.test", Kind: tracepb.Span_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: uint64(now.UnixNano()), EndTimeUnixNano: uint64(now.UnixNano()),
			Attributes: []*commonpb.KeyValue{stringAttribute("oma.telemetry_destination.id", destination.ExternalID)},
		}}}},
	}}})
}

func (t *Tester) Test(ctx context.Context, destination db.TelemetryDestination) telemetrydestinations.TestResult {
	failed := func(message string, status int) telemetrydestinations.TestResult {
		result := telemetrydestinations.TestResult{Error: &message}
		if status > 0 {
			result.StatusCode = &status
		}
		return result
	}
	headers, err := destinationHeaders(ctx, t.opener, destination)
	if err != nil {
		return failed("destination headers could not be decrypted", 0)
	}
	payload, err := testPayload(destination, time.Now())
	if err != nil {
		return failed("test span could not be encoded", 0)
	}
	client, transport := newDeliveryClient(ctx, t.allowInsecure, t.timeout, 1)
	defer transport.CloseIdleConnections()
	result := deliver(ctx, client, destination.TracesEndpointURL, headers, payload)
	if result.failed() {
		return failed(result.err.Error(), result.status)
	}
	status := result.status
	return telemetrydestinations.TestResult{Success: true, StatusCode: &status}
}
