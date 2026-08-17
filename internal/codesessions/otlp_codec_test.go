package codesessions

import (
	"testing"

	collectlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collecttracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestCanonicalizeOTLPPayloadReplacesUntrustedOMAAttributes(t *testing.T) {
	trusted := trustedOTLPResourceAttributes{
		organizationUUID: "org_trusted",
		workspaceUUID:    "workspace_trusted",
		publicSessionID:  "session_trusted",
		codeSessionID:    "cse_trusted",
		agentID:          "agent_trusted",
		agentVersion:     7,
	}
	wantResourceAttributes := []*commonpb.KeyValue{
		otlpStringAttribute("service.name", "claude-code"),
		otlpStringAttribute("oma.organization.uuid", "org_trusted"),
		otlpStringAttribute("oma.workspace.uuid", "workspace_trusted"),
		otlpStringAttribute("oma.session.id", "session_trusted"),
		otlpStringAttribute("oma.code_session.id", "cse_trusted"),
		otlpStringAttribute("oma.agent.id", "agent_trusted"),
		otlpIntAttribute("oma.agent.version", 7),
	}
	wantNestedAttributes := []*commonpb.KeyValue{otlpStringAttribute("event.name", "tool")}

	for _, signal := range []string{"metrics", "logs", "traces"} {
		for _, protocol := range []otlpProtocol{otlpProtocolJSON, otlpProtocolProtobuf} {
			t.Run(signal+"/"+string(protocol), func(t *testing.T) {
				request := spoofedOTLPRequest(t, signal)
				encoded, err := marshalOTLPMessage(protocol, request)
				if err != nil {
					t.Fatalf("marshal fixture: %v", err)
				}

				got, err := canonicalizeOTLPPayload(signal, protocol, encoded, trusted)
				if err != nil {
					t.Fatalf("canonicalizeOTLPPayload() error = %v", err)
				}
				decoded, err := newOTLPRequest(signal)
				if err != nil {
					t.Fatalf("newOTLPRequest() error = %v", err)
				}
				unmarshalOTLPTestMessage(t, protocol, got, decoded)

				resourceAttributes, nestedAttributes := firstOTLPAttributeLists(t, decoded)
				assertExactOTLPAttributes(t, resourceAttributes, wantResourceAttributes)
				assertExactOTLPAttributes(t, nestedAttributes, wantNestedAttributes)
			})
		}
	}
}

func TestReservedOTLPAttributeKeyMatchesOpenObserveAliases(t *testing.T) {
	tests := []struct {
		key      string
		reserved bool
	}{
		{key: "oma", reserved: true},
		{key: "oma.organization.uuid", reserved: true},
		{key: "oma_organization_uuid", reserved: true},
		{key: "OMA.ORGANIZATION.UUID", reserved: true},
		{key: "oma-organization-uuid", reserved: true},
		{key: "oma/organization uuid", reserved: true},
		{key: "service_oma", reserved: true},
		{key: "service.oma.organization.uuid", reserved: true},
		{key: "service_oma_organization_uuid", reserved: true},
		{key: "SERVICE-OMA-SESSION-ID", reserved: true},
		{key: "service.name", reserved: false},
		{key: "myservice_oma_organization_uuid", reserved: false},
		{key: "custom.oma_organization_uuid", reserved: false},
		{key: "omaOrganizationUuid", reserved: false},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			if got := isReservedOTLPAttributeKey(test.key); got != test.reserved {
				t.Fatalf("isReservedOTLPAttributeKey(%q) = %t, want %t", test.key, got, test.reserved)
			}
		})
	}
}

func spoofedOTLPRequest(t *testing.T, signal string) proto.Message {
	t.Helper()
	resource := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
		otlpStringAttribute("service.name", "claude-code"),
		otlpStringAttribute("oma.agent.id", "spoofed-agent"),
		otlpStringAttribute("oma_organization_uuid", "spoofed-organization"),
		otlpStringAttribute("OMA-WORKSPACE-UUID", "spoofed-workspace"),
	}}
	nested := []*commonpb.KeyValue{
		otlpStringAttribute("event.name", "tool"),
		otlpStringAttribute("oma.workspace.uuid", "spoofed-workspace"),
		otlpStringAttribute("oma_session_id", "spoofed-session"),
		otlpStringAttribute("OMA-AGENT-ID", "spoofed-agent"),
		// On traces, OpenObserve stores resource attributes as service_oma_*;
		// a bare span attribute with that name must not reach the same column.
		otlpStringAttribute("service.oma.organization.uuid", "spoofed-organization"),
		otlpStringAttribute("service_oma_session_id", "spoofed-session"),
	}

	switch signal {
	case "metrics":
		return &collectmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: resource,
			ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{{
				Name: "claude_code_token_usage",
				Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{{
					Attributes: nested,
					Value:      &metricspb.NumberDataPoint_AsInt{AsInt: 1},
				}}}},
			}}}},
		}}}
	case "logs":
		return &collectlogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
			Resource:  resource,
			ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{Attributes: nested}}}},
		}}}
	case "traces":
		return &collecttracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
			Resource:   resource,
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "test", Attributes: nested}}}},
		}}}
	default:
		t.Fatalf("unsupported test signal %q", signal)
		return nil
	}
}

func unmarshalOTLPTestMessage(t *testing.T, protocol otlpProtocol, payload []byte, message proto.Message) {
	t.Helper()
	var err error
	if protocol == otlpProtocolJSON {
		err = protojson.Unmarshal(payload, message)
	} else {
		err = proto.Unmarshal(payload, message)
	}
	if err != nil {
		t.Fatalf("decode canonical payload: %v", err)
	}
}

func firstOTLPAttributeLists(t *testing.T, message proto.Message) ([]*commonpb.KeyValue, []*commonpb.KeyValue) {
	t.Helper()
	switch request := message.(type) {
	case *collectmetricspb.ExportMetricsServiceRequest:
		metric := request.ResourceMetrics[0].ScopeMetrics[0].Metrics[0]
		return request.ResourceMetrics[0].Resource.Attributes, metric.GetGauge().DataPoints[0].Attributes
	case *collectlogspb.ExportLogsServiceRequest:
		return request.ResourceLogs[0].Resource.Attributes, request.ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes
	case *collecttracepb.ExportTraceServiceRequest:
		return request.ResourceSpans[0].Resource.Attributes, request.ResourceSpans[0].ScopeSpans[0].Spans[0].Attributes
	default:
		t.Fatalf("unsupported OTLP message %T", message)
		return nil, nil
	}
}

func assertExactOTLPAttributes(t *testing.T, got, want []*commonpb.KeyValue) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("attributes = %#v, want %#v", got, want)
	}
	matched := make([]bool, len(want))
	for _, attribute := range got {
		match := -1
		for index, expected := range want {
			if !matched[index] && proto.Equal(attribute, expected) {
				match = index
				break
			}
		}
		if match == -1 {
			t.Fatalf("unexpected or duplicate attribute %#v; want %#v", attribute, want)
		}
		matched[match] = true
	}
}
