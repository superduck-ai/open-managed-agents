package telemetryexport

import (
	"bytes"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

const (
	ServiceName          = "oma-platform"
	ScopeName            = "oma.platform"
	maxSpanDuration      = 24 * time.Hour
	maxMetadataValue     = 1024
	maxTraceState        = 512
	maxDeliveryBytes     = 900 << 10
	inlineAgentAttribute = "oma.agent.inline"
	truncatedAttribute   = "oma.content.truncated"
	conversationKey      = "gen_ai.conversation.id"
)

var threadIDPattern = regexp.MustCompile(`^sthr_[A-Za-z0-9_-]{1,120}$`)

var fixedSpanNames = []string{"oma.session.turn", "oma.model_request", "oma.compaction"}

var toolSpanPrefixes = []string{"oma.tool_use ", "oma.mcp_tool_use "}

var metadataAttributes = map[string]bool{
	"gen_ai.operation.name": true, "gen_ai.agent.id": true, "gen_ai.agent.name": true, "gen_ai.agent.description": true,
	"gen_ai.request.model": true, "gen_ai.provider.name": true, "gen_ai.tool.name": true,
	"oma.session.turn.stop_reason": true, "oma.step": true, "oma.model_request.ttft_ms": true, "oma.mcp.server.name": true,
	"oma.tool.evaluated_permission": true, "error.type": true,
}

var ContentAttributes = []string{"gen_ai.system_instructions", "gen_ai.input.messages", "gen_ai.output.messages", "gen_ai.tool.call.arguments", "oma.tool_result.content"}

type Source struct {
	OrganizationUUID string
	WorkspaceUUID    string
	WorkspaceID      string
	SessionID        string
	CodeSessionID    string
	PrimaryThreadID  string
	AgentID          string
}

type Delivery struct {
	DestinationUUID string
	ContentPolicy   string
	Payload         []byte
}

func validSpanName(name string) bool {
	if slices.Contains(fixedSpanNames, name) {
		return true
	}
	for _, prefix := range toolSpanPrefixes {
		if tool, found := strings.CutPrefix(name, prefix); found && tool != "" && len(tool) <= 256 && utf8.ValidString(tool) {
			return true
		}
	}
	return false
}

func validID(id []byte, size int) bool {
	return len(id) == size && !bytes.Equal(id, make([]byte, size))
}

func validSpan(span *tracepb.Span) bool {
	if span == nil || !validSpanName(span.Name) || !validID(span.TraceId, 16) || !validID(span.SpanId, 8) {
		return false
	}
	if len(span.ParentSpanId) != 0 && !validID(span.ParentSpanId, 8) {
		return false
	}
	start, end := span.StartTimeUnixNano, span.EndTimeUnixNano
	return start > 0 && end >= start && end-start <= uint64(maxSpanDuration)
}

func sanitizeSpans(request *coltracepb.ExportTraceServiceRequest, source Source) ([]*tracepb.Span, int) {
	var spans []*tracepb.Span
	rejected := 0
	for _, resource := range request.GetResourceSpans() {
		for _, scope := range resource.GetScopeSpans() {
			for _, span := range scope.GetSpans() {
				if !validSpan(span) {
					rejected++
					continue
				}
				spans = append(spans, sanitizeSpan(span, source))
			}
		}
	}
	return spans, rejected
}

func sanitizeSpan(span *tracepb.Span, source Source) *tracepb.Span {
	clean := &tracepb.Span{
		TraceId: span.TraceId, SpanId: span.SpanId, ParentSpanId: span.ParentSpanId, Name: span.Name,
		Kind: span.Kind, StartTimeUnixNano: span.StartTimeUnixNano, EndTimeUnixNano: span.EndTimeUnixNano,
	}
	if len(span.TraceState) <= maxTraceState {
		clean.TraceState = span.TraceState
	}
	if status := span.GetStatus(); status != nil && status.Code != tracepb.Status_STATUS_CODE_UNSET {
		clean.Status = &tracepb.Status{Code: status.Code}
	}
	for _, link := range span.GetLinks() {
		if validID(link.GetTraceId(), 16) && validID(link.GetSpanId(), 8) && len(clean.Links) < 32 {
			clean.Links = append(clean.Links, &tracepb.Span_Link{TraceId: link.TraceId, SpanId: link.SpanId})
		}
	}
	conversation := source.PrimaryThreadID
	callID := ""
	for _, attribute := range span.GetAttributes() {
		key, value := attribute.GetKey(), attribute.GetValue()
		switch {
		case key == conversationKey:
			if thread := value.GetStringValue(); threadIDPattern.MatchString(thread) {
				conversation = thread
			}
		case key == "gen_ai.tool.call.id":
			if value := value.GetStringValue(); value != "" && len(value) <= maxMetadataValue {
				callID = value
			}
		case key == inlineAgentAttribute:
			if value.GetBoolValue() {
				clean.Attributes = append(clean.Attributes, attribute)
			}
		case slices.Contains(ContentAttributes, key):
			if _, ok := value.GetValue().(*commonpb.AnyValue_StringValue); ok {
				clean.Attributes = append(clean.Attributes, attribute)
			}
		case metadataAttributes[key] && validMetadataValue(value):
			clean.Attributes = append(clean.Attributes, attribute)
		}
	}
	if conversation != "" {
		clean.Attributes = append(clean.Attributes, stringAttribute(conversationKey, conversation))
	}
	if span.Name == "oma.session.turn" && conversation == source.PrimaryThreadID && source.AgentID != "" {
		clean.Attributes = slices.DeleteFunc(clean.Attributes, func(attribute *commonpb.KeyValue) bool { return attribute.Key == "gen_ai.agent.id" })
		clean.Attributes = append(clean.Attributes, stringAttribute("gen_ai.agent.id", source.AgentID))
	}
	if callID != "" {
		thread := ""
		if conversation != source.PrimaryThreadID {
			thread = conversation
		}
		clean.Attributes = append(clean.Attributes, stringAttribute("gen_ai.tool.call.id", managedagentsevents.ToolUseEventID(source.CodeSessionID, callID, thread)))
	}
	return clean
}

func validMetadataValue(value *commonpb.AnyValue) bool {
	switch typed := value.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return len(typed.StringValue) <= maxMetadataValue && utf8.ValidString(typed.StringValue)
	case *commonpb.AnyValue_IntValue, *commonpb.AnyValue_DoubleValue, *commonpb.AnyValue_BoolValue:
		return true
	default:
		return false
	}
}

func stringAttribute(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func resourceFor(source Source) *resourcepb.Resource {
	return &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
		stringAttribute("service.name", ServiceName),
		stringAttribute("oma.organization.id", source.OrganizationUUID),
		stringAttribute("oma.workspace.id", source.WorkspaceID),
		stringAttribute("oma.session.id", source.SessionID),
	}}
}

func forDestination(spans []*tracepb.Span, destination db.TelemetryDestination) []*tracepb.Span {
	result := make([]*tracepb.Span, 0, len(spans))
	for _, span := range spans {
		copied := proto.Clone(span).(*tracepb.Span)
		copied.Attributes = applyContentPolicy(copied.Attributes, destination.TracesContentMode == "content", destination.TracesMaxContentBytes)
		result = append(result, copied)
	}
	return result
}

func applyContentPolicy(attributes []*commonpb.KeyValue, content bool, budget int) []*commonpb.KeyValue {
	inline := slices.ContainsFunc(attributes, func(attribute *commonpb.KeyValue) bool { return attribute.Key == inlineAgentAttribute })
	result := make([]*commonpb.KeyValue, 0, len(attributes)+1)
	truncated := false
	for _, attribute := range attributes {
		switch {
		case attribute.Key == inlineAgentAttribute:
			continue
		case attribute.Key == "gen_ai.agent.name" && inline && !content:
			continue
		case slices.Contains(ContentAttributes, attribute.Key):
			if !content {
				continue
			}
			text := attribute.GetValue().GetStringValue()
			if len(text) > budget {
				text, truncated = truncateUTF8(text, budget), true
			}
			budget -= len(text)
			if text != "" {
				result = append(result, stringAttribute(attribute.Key, text))
			}
		default:
			result = append(result, attribute)
		}
	}
	if truncated {
		result = append(result, &commonpb.KeyValue{Key: truncatedAttribute, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}})
	}
	return result
}

func truncateUTF8(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

func encodeDeliveries(source Source, spans []*tracepb.Span) ([][]byte, error) {
	var payloads [][]byte
	var batch []*tracepb.Span
	size := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		payload, err := proto.Marshal(&coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
			Resource:   resourceFor(source),
			ScopeSpans: []*tracepb.ScopeSpans{{Scope: &commonpb.InstrumentationScope{Name: ScopeName}, Spans: batch}},
		}}})
		if err != nil {
			return err
		}
		payloads = append(payloads, payload)
		batch, size = nil, 0
		return nil
	}
	for _, span := range spans {
		spanSize := proto.Size(span)
		if size+spanSize > maxDeliveryBytes {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		batch = append(batch, span)
		size += spanSize
	}
	return payloads, flush()
}

func PrepareDeliveries(request *coltracepb.ExportTraceServiceRequest, source Source, destinations []db.TelemetryDestination) ([]Delivery, int, error) {
	spans, rejected := sanitizeSpans(request, source)
	if len(spans) == 0 {
		return nil, rejected, nil
	}
	var deliveries []Delivery
	for _, destination := range destinations {
		payloads, err := encodeDeliveries(source, forDestination(spans, destination))
		if err != nil {
			return nil, rejected, err
		}
		for _, payload := range payloads {
			deliveries = append(deliveries, Delivery{DestinationUUID: destination.UUID, ContentPolicy: contentPolicy(destination), Payload: payload})
		}
	}
	return deliveries, rejected, nil
}

func contentPolicy(destination db.TelemetryDestination) string {
	if destination.TracesContentMode == "content" {
		return "content:" + strconv.Itoa(destination.TracesMaxContentBytes)
	}
	return destination.TracesContentMode
}
