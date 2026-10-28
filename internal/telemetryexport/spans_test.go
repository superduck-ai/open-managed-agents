package telemetryexport

import (
	"math"
	"strings"
	"testing"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

var testSource = Source{OrganizationUUID: "org-uuid", WorkspaceUUID: "ws-uuid", WorkspaceID: "wrkspc_1", SessionID: "sesn_1", CodeSessionID: "cse_1", PrimaryThreadID: "sthr_primary", AgentID: "agent_trusted"}

func id(size int, fill byte) []byte {
	value := make([]byte, size)
	for index := range value {
		value[index] = fill
	}
	return value
}

func span(name string, attributes ...*commonpb.KeyValue) *tracepb.Span {
	return &tracepb.Span{TraceId: id(16, 1), SpanId: id(8, 2), Name: name, StartTimeUnixNano: 1_000, EndTimeUnixNano: 2_000, Attributes: attributes}
}

func request(spans ...*tracepb.Span) *coltracepb.ExportTraceServiceRequest {
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource:   &resourcepb.Resource{Attributes: []*commonpb.KeyValue{stringAttribute("oma.organization.id", "forged"), stringAttribute("service.name", "forged")}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}},
	}}}
}

func attributes(span *tracepb.Span) map[string]*commonpb.AnyValue {
	values := map[string]*commonpb.AnyValue{}
	for _, attribute := range span.Attributes {
		values[attribute.Key] = attribute.Value
	}
	return values
}

func decode(t *testing.T, payload []byte) *coltracepb.ExportTraceServiceRequest {
	t.Helper()
	decoded := &coltracepb.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(payload, decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func destination(uuid, mode string, budget int) db.TelemetryDestination {
	return db.TelemetryDestination{UUID: uuid, TracesContentMode: mode, TracesMaxContentBytes: budget}
}

func TestSanitizeRejectsInvalidSpans(t *testing.T) {
	zeroTrace := span("oma.session.turn")
	zeroTrace.TraceId = id(16, 0)
	shortSpan := span("oma.session.turn")
	shortSpan.SpanId = id(4, 2)
	badParent := span("oma.session.turn")
	badParent.ParentSpanId = id(8, 0)
	backwards := span("oma.session.turn")
	backwards.EndTimeUnixNano = 10
	tooLong := span("oma.session.turn")
	tooLong.EndTimeUnixNano = tooLong.StartTimeUnixNano + uint64(maxSpanDuration) + 1
	overflowing := span("oma.session.turn")
	overflowing.StartTimeUnixNano, overflowing.EndTimeUnixNano = 1, math.MaxUint64
	spans, rejected := sanitizeSpans(request(span("http.request"), span("oma.tool_use "), span("oma.tool_use"), zeroTrace, shortSpan, badParent, backwards, tooLong, overflowing, nil), testSource)
	if len(spans) != 0 || rejected != 10 {
		t.Fatalf("accepted %d spans, rejected %d", len(spans), rejected)
	}
}

func TestSanitizeKeepsOnlyAllowedAttributesAndMapsConversations(t *testing.T) {
	turn := span("oma.session.turn",
		stringAttribute("gen_ai.operation.name", "invoke_agent"),
		stringAttribute("gen_ai.conversation.id", "cse_1"),
		stringAttribute("oma.workspace.id", "forged"),
		stringAttribute("http.url", "https://internal"),
		stringAttribute("error.type", strings.Repeat("x", maxMetadataValue+1)),
		&commonpb.KeyValue{Key: "oma.step", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 2}}},
		&commonpb.KeyValue{Key: "gen_ai.input.messages", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 1}}},
	)
	turn.Events = []*tracepb.Span_Event{{Name: "secret"}}
	turn.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "private detail"}
	turn.Links = []*tracepb.Span_Link{{TraceId: id(16, 5), SpanId: id(8, 6), Attributes: []*commonpb.KeyValue{stringAttribute("x", "y")}}, {TraceId: id(16, 0), SpanId: id(8, 6)}}
	child := span("oma.mcp_tool_use docs.search", stringAttribute("gen_ai.conversation.id", "sthr_child-1"), stringAttribute("gen_ai.tool.call.id", "toolu_1"))
	forged := span("oma.compaction", stringAttribute("gen_ai.conversation.id", "cse_other_session"))
	spans, rejected := sanitizeSpans(request(turn, child, forged), testSource)
	if rejected != 0 || len(spans) != 3 {
		t.Fatalf("spans=%d rejected=%d", len(spans), rejected)
	}
	values := attributes(spans[0])
	if len(values) != 4 || values["gen_ai.agent.id"].GetStringValue() != "agent_trusted" || values["gen_ai.operation.name"].GetStringValue() != "invoke_agent" || values["oma.step"].GetIntValue() != 2 || values[conversationKey].GetStringValue() != "sthr_primary" {
		t.Fatalf("turn attributes = %v", values)
	}
	if spans[0].Status.Message != "" || len(spans[0].Events) != 0 || len(spans[0].Links) != 1 || len(spans[0].Links[0].Attributes) != 0 {
		t.Fatalf("turn shape = %+v", spans[0])
	}
	if attributes(spans[1])["gen_ai.tool.call.id"].GetStringValue() != managedagentsevents.ToolUseEventID("cse_1", "toolu_1", "sthr_child-1") {
		t.Fatal("tool call ID was not mapped to the public tool_use event")
	}
	if attributes(spans[1])[conversationKey].GetStringValue() != "sthr_child-1" || attributes(spans[2])[conversationKey].GetStringValue() != "sthr_primary" {
		t.Fatal("conversation IDs were not mapped to session threads")
	}
}

func TestDeliveriesApplyEachDestinationContentPolicy(t *testing.T) {
	turn := span("oma.session.turn",
		stringAttribute("gen_ai.agent.name", "model-named-helper"),
		&commonpb.KeyValue{Key: inlineAgentAttribute, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}},
		stringAttribute("gen_ai.input.messages", "你好世界"),
		stringAttribute("gen_ai.output.messages", "answer"),
	)
	deliveries, rejected, err := PrepareDeliveries(request(turn), testSource, []db.TelemetryDestination{
		destination("metadata", "metadata_only", 1000),
		destination("content", "content", 1000),
		destination("truncated", "content", 7),
	})
	if err != nil || rejected != 0 || len(deliveries) != 3 {
		t.Fatalf("deliveries=%d rejected=%d err=%v", len(deliveries), rejected, err)
	}
	if deliveries[0].ContentPolicy != "metadata_only" || deliveries[1].ContentPolicy != "content:1000" || deliveries[2].ContentPolicy != "content:7" {
		t.Fatalf("delivery content policies = %q %q %q", deliveries[0].ContentPolicy, deliveries[1].ContentPolicy, deliveries[2].ContentPolicy)
	}
	for _, delivery := range deliveries {
		decoded := decode(t, delivery.Payload)
		resource := decoded.ResourceSpans[0]
		resourceValues := map[string]string{}
		for _, attribute := range resource.Resource.Attributes {
			resourceValues[attribute.Key] = attribute.Value.GetStringValue()
		}
		if resourceValues["service.name"] != ServiceName || resourceValues["oma.organization.id"] != "org-uuid" || resourceValues["oma.workspace.id"] != "wrkspc_1" || resourceValues["oma.session.id"] != "sesn_1" || resource.ScopeSpans[0].Scope.Name != ScopeName {
			t.Fatalf("resource = %v", resourceValues)
		}
		values := attributes(resource.ScopeSpans[0].Spans[0])
		if values[inlineAgentAttribute] != nil {
			t.Fatal("internal inline marker leaked")
		}
		switch delivery.DestinationUUID {
		case "metadata":
			if values["gen_ai.input.messages"] != nil || values["gen_ai.agent.name"] != nil || values[truncatedAttribute] != nil {
				t.Fatalf("metadata destination received content: %v", values)
			}
		case "content":
			if values["gen_ai.input.messages"].GetStringValue() != "你好世界" || values["gen_ai.agent.name"].GetStringValue() != "model-named-helper" || values[truncatedAttribute] != nil {
				t.Fatalf("content destination = %v", values)
			}
		case "truncated":
			if values["gen_ai.input.messages"].GetStringValue() != "你好" || values["gen_ai.output.messages"].GetStringValue() != "a" || !values[truncatedAttribute].GetBoolValue() {
				t.Fatalf("truncated destination = %v", values)
			}
		}
	}
}

func TestDeliveriesSplitLargeBatches(t *testing.T) {
	var spans []*tracepb.Span
	for index := range 4 {
		item := span("oma.model_request", stringAttribute("gen_ai.output.messages", strings.Repeat("y", 400<<10)))
		item.SpanId = id(8, byte(index+1))
		spans = append(spans, item)
	}
	deliveries, _, err := PrepareDeliveries(request(spans...), testSource, []db.TelemetryDestination{destination("content", "content", DefaultBudget)})
	if err != nil || len(deliveries) != 2 {
		t.Fatalf("deliveries = %d, err = %v", len(deliveries), err)
	}
	for _, delivery := range deliveries {
		if len(delivery.Payload) > maxDeliveryBytes+4096 || len(decode(t, delivery.Payload).ResourceSpans[0].ScopeSpans[0].Spans) != 2 {
			t.Fatalf("delivery size = %d", len(delivery.Payload))
		}
	}
	if none, _, err := PrepareDeliveries(request(spans...), testSource, nil); err != nil || len(none) != 0 {
		t.Fatal("spans without destinations must be dropped")
	}
}

const DefaultBudget = 786432
