package codesessions

import (
	"encoding/json"
	"strings"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

func structuredEvent(eventType string, time int64, data string) nativeLogEvent {
	seq := int64(0)
	return nativeLogEvent{Seq: &seq, Time: time, Type: eventType, Data: json.RawMessage(data)}
}

func timingAttributes(startedAt, firstToken *commonpb.AnyValue, model string) []*commonpb.KeyValue {
	attributes := []*commonpb.KeyValue{otlpStringAttribute("gen_ai.request.model", model)}
	if startedAt != nil {
		attributes = append(attributes, &commonpb.KeyValue{Key: "oma.model_request.started_at", Value: startedAt})
	}
	if firstToken != nil {
		attributes = append(attributes, &commonpb.KeyValue{Key: "oma.model_request.first_token_at", Value: firstToken})
	}
	return attributes
}

func intValue(value int64) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: value}}
}

func doubleValue(value float64) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value}}
}

func TestStructuredFactsDropInvalidValues(t *testing.T) {
	const now = int64(10_000_000)
	for _, test := range []struct {
		name       string
		event      nativeLogEvent
		attributes []*commonpb.KeyValue
		want       nativeStructuredFacts
	}{
		{"malformed tool call keeps only the event type", structuredEvent("tool/call", now, `{"turn":"1","callId":7}`), nil, nativeStructuredFacts{eventType: "tool/call"}},
		{"non-positive positions", structuredEvent("step/start", now, `{"turn":0,"step":-2}`), nil, nativeStructuredFacts{eventType: "step/start"}},
		{"fractional positions", structuredEvent("step/start", now, `{"turn":1.5,"step":2}`), nil, nativeStructuredFacts{eventType: "step/start", step: 2}},
		{"whitespace and oversized values", structuredEvent("tool/call", now, `{"callId":"bad id","name":"`+strings.Repeat("x", 257)+`"}`), nil, nativeStructuredFacts{eventType: "tool/call"}},
		{"negative and fractional token counts", structuredEvent("assistant/message", now, `{"usage":{"inputTokens":-4449,"outputTokens":1.5,"cacheReadTokens":4736,"reasoningTokens":"7"}}`), nil, nativeStructuredFacts{eventType: "assistant/message", modelRequestStatus: "ok", usage: nativeTokenUsage{cacheRead: 4736}}},
		{"token usage is ignored outside model requests", structuredEvent("tool/result", now, `{"usage":{"inputTokens":10}}`), nil, nativeStructuredFacts{eventType: "tool/result"}},
		{"error code requires an error result", structuredEvent("tool/result", now, `{"message":{"toolCallId":"c1","isError":false},"error":{"code":"DENIED"}}`), nil, nativeStructuredFacts{eventType: "tool/result", toolCallID: "c1"}},
		{"worker timing ignored outside model requests", structuredEvent("turn/start", now, `{"turn":1}`), timingAttributes(intValue(now-10), intValue(now-5), "model"), nativeStructuredFacts{eventType: "turn/start", turn: 1}},
		{"future start and early first token", structuredEvent("assistant/message", now, `{"turn":1,"step":1}`), timingAttributes(intValue(now+1), intValue(now-modelRequestMaxAgeMillis-1), "bad model"), nativeStructuredFacts{eventType: "assistant/message", turn: 1, step: 1, modelRequestStatus: "ok"}},
		{"stale start and token before start", structuredEvent("assistant/message", now, `{}`), timingAttributes(intValue(now-modelRequestMaxAgeMillis-1), intValue(now-modelRequestMaxAgeMillis+5), ""), nativeStructuredFacts{eventType: "assistant/message", modelRequestStatus: "ok", firstTokenAt: now - modelRequestMaxAgeMillis + 5}},
		{"first token before start", structuredEvent("assistant/message", now, `{}`), timingAttributes(intValue(now-50), intValue(now-60), ""), nativeStructuredFacts{eventType: "assistant/message", modelRequestStatus: "ok", startedAt: now - 50}},
		{"non-integral and string timing", structuredEvent("assistant/attempt", now, `{"stream":[]}`), timingAttributes(doubleValue(float64(now)-0.5), &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "1"}}, ""), nativeStructuredFacts{eventType: "assistant/attempt", modelRequestStatus: "error"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := extractNativeStructuredFacts(test.event, test.attributes); got != test.want {
				t.Fatalf("facts = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestStructuredFactsExtractNativeEvents(t *testing.T) {
	const now = int64(10_000_000)
	for _, test := range []struct {
		name       string
		event      nativeLogEvent
		attributes []*commonpb.KeyValue
		want       nativeStructuredFacts
	}{
		{"tool call", structuredEvent("tool/call", now, `{"turn":2,"step":3,"callId":"fc_call_1","name":"mcp__docs__search","arguments":"{}"}`), nil, nativeStructuredFacts{eventType: "tool/call", turn: 2, step: 3, toolCallID: "fc_call_1", toolName: "mcp__docs__search"}},
		{"failed tool result", structuredEvent("tool/result", now, `{"turn":2,"step":3,"message":{"toolCallId":"fc_call_1","isError":true},"error":{"name":"McpError","code":"MCP_AUTH"}}`), nil, nativeStructuredFacts{eventType: "tool/result", turn: 2, step: 3, toolCallID: "fc_call_1", toolIsError: true, errorCode: "MCP_AUTH"}},
		{"failed turn", structuredEvent("turn/end", now, `{"turn":4,"reason":{"kind":"error","error":{"message":"boom","code":"SERVER"}}}`), nil, nativeStructuredFacts{eventType: "turn/end", turn: 4, turnEndReason: "error", errorCode: "SERVER"}},
		{"cancelled turn", structuredEvent("turn/end", now, `{"turn":4,"reason":{"kind":"aborted","reason":{"kind":"user"}}}`), nil, nativeStructuredFacts{eventType: "turn/end", turn: 4, turnEndReason: "aborted"}},
		{"interrupted message with timing", structuredEvent("assistant/message", now, `{"turn":1,"step":1,"interrupted":true}`), timingAttributes(intValue(now-900), doubleValue(float64(now-400)), "magpie/sub2api/deepseek-flash"), nativeStructuredFacts{eventType: "assistant/message", turn: 1, step: 1, modelRequestStatus: "interrupted", startedAt: now - 900, firstTokenAt: now - 400, model: "magpie/sub2api/deepseek-flash"}},
		{"message usage", structuredEvent("assistant/message", now, `{"usage":{"inputTokens":287,"outputTokens":178,"cacheReadTokens":4736,"cacheWriteTokens":12,"reasoningTokens":40}}`), nil, nativeStructuredFacts{eventType: "assistant/message", modelRequestStatus: "ok", usage: nativeTokenUsage{input: 287, output: 178, cacheRead: 4736, cacheWrite: 12, reasoning: 40}}},
		{"failed attempt", structuredEvent("assistant/attempt", now, `{"turn":1,"step":1,"stream":[{"type":"chunk","time":1,"chunk":{"type":"finish","reason":{"kind":"error","failure":{"message":"overloaded","code":"RATE_LIMIT"}}}}]}`), timingAttributes(intValue(now-200), nil, "model-a"), nativeStructuredFacts{eventType: "assistant/attempt", turn: 1, step: 1, modelRequestStatus: "error", errorCode: "RATE_LIMIT", startedAt: now - 200, model: "model-a"}},
		{"cancelled attempt", structuredEvent("assistant/attempt", now, `{"stream":[{"type":"chunk","time":1,"chunk":{"type":"finish","reason":{"kind":"aborted","failure":{"message":"x","code":"ABORTED"}}}},{"type":"text-chunks"}]}`), nil, nativeStructuredFacts{eventType: "assistant/attempt", modelRequestStatus: "cancelled", errorCode: "ABORTED"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := extractNativeStructuredFacts(test.event, test.attributes); got != test.want {
				t.Fatalf("facts = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestStructuredAttributesSurviveRedactionAndStayDense(t *testing.T) {
	for _, content := range []string{
		`{"seq":0,"time":100,"type":"tool/result","data":{"turn":1,"step":1,"message":{"toolCallId":"c1","isError":true,"content":[{"type":"text","text":"secret"}]},"error":{"name":"E","code":"DENIED","reason":"secret"}}}`,
		`{"seq":1,"time":100,"type":"request/context","data":{"model":"m"}}`,
	} {
		record := &logspb.LogRecord{Attributes: []*commonpb.KeyValue{otlpStringAttribute("dsh.header", `{"id":"native","version":4}`), otlpStringAttribute("content", content)}}
		if err := validateNativeRecord(record, false); err != nil {
			t.Fatal(err)
		}
		keys := map[string]*commonpb.AnyValue{}
		for _, attribute := range record.Attributes {
			keys[attribute.Key] = attribute.Value
		}
		for _, key := range []string{"dsh.event.type", "dsh.turn", "dsh.step", "dsh.tool.call_id", "dsh.tool.name", "dsh.tool.is_error", "dsh.error.code", "dsh.turn.end_reason", "dsh.model_request.status", "oma.model_request.started_at", "oma.model_request.first_token_at", "gen_ai.request.model", "gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens", "gen_ai.usage.cache_read.input_tokens", "gen_ai.usage.cache_creation.input_tokens", "gen_ai.usage.reasoning.output_tokens"} {
			if keys[key] == nil {
				t.Fatalf("dense attribute %s missing", key)
			}
		}
		if strings.Contains(logAttribute(record, "content"), "secret") {
			t.Fatal("content policy skipped")
		}
		if strings.Contains(content, "DENIED") && (keys["dsh.error.code"].GetStringValue() != "DENIED" || !keys["dsh.tool.is_error"].GetBoolValue()) {
			t.Fatal("structural facts were taken after redaction")
		}
	}
}
