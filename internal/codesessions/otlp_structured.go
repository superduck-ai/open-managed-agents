package codesessions

import (
	"encoding/json"
	"math"
	"regexp"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

const modelRequestMaxAgeMillis = int64(60 * 60 * 1000)

var structuredValuePattern = regexp.MustCompile(`^[\x21-\x7e]{1,256}$`)

type nativeStructuredFacts struct {
	eventType          string
	turn               int64
	step               int64
	toolCallID         string
	toolName           string
	toolIsError        bool
	errorCode          string
	turnEndReason      string
	modelRequestStatus string
	startedAt          int64
	firstTokenAt       int64
	model              string
	usage              nativeTokenUsage
}

type nativeTokenUsage struct {
	input      int64
	output     int64
	cacheRead  int64
	cacheWrite int64
	reasoning  int64
}

type nativeUsagePayload struct {
	Usage *struct {
		InputTokens      json.RawMessage `json:"inputTokens"`
		OutputTokens     json.RawMessage `json:"outputTokens"`
		CacheReadTokens  json.RawMessage `json:"cacheReadTokens"`
		CacheWriteTokens json.RawMessage `json:"cacheWriteTokens"`
		ReasoningTokens  json.RawMessage `json:"reasoningTokens"`
	} `json:"usage"`
}

type nativePosition struct {
	Turn json.RawMessage `json:"turn"`
	Step json.RawMessage `json:"step"`
}

type nativeToolCall struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
}

type nativeToolResult struct {
	Message struct {
		ToolCallID string `json:"toolCallId"`
		IsError    bool   `json:"isError"`
	} `json:"message"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type nativeTurnEnd struct {
	Reason struct {
		Kind  string `json:"kind"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	} `json:"reason"`
}

type nativeAssistantMessage struct {
	Interrupted bool `json:"interrupted"`
}

type nativeAssistantAttempt struct {
	Stream []struct {
		Type  string `json:"type"`
		Chunk *struct {
			Type   string `json:"type"`
			Reason *struct {
				Kind    string `json:"kind"`
				Failure *struct {
					Code string `json:"code"`
				} `json:"failure"`
			} `json:"reason"`
		} `json:"chunk"`
	} `json:"stream"`
}

func structuredValue(value string) string {
	if structuredValuePattern.MatchString(value) {
		return value
	}
	return ""
}

func positiveInteger(raw json.RawMessage) int64 {
	var value int64
	if json.Unmarshal(raw, &value) != nil || value <= 0 {
		return 0
	}
	return value
}

func nonNegativeInteger(raw json.RawMessage) int64 {
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < 0 {
		return 0
	}
	return value
}

func tokenUsage(data json.RawMessage) nativeTokenUsage {
	var payload nativeUsagePayload
	if json.Unmarshal(data, &payload) != nil || payload.Usage == nil {
		return nativeTokenUsage{}
	}
	usage := payload.Usage
	return nativeTokenUsage{
		input:      nonNegativeInteger(usage.InputTokens),
		output:     nonNegativeInteger(usage.OutputTokens),
		cacheRead:  nonNegativeInteger(usage.CacheReadTokens),
		cacheWrite: nonNegativeInteger(usage.CacheWriteTokens),
		reasoning:  nonNegativeInteger(usage.ReasoningTokens),
	}
}

func extractNativeStructuredFacts(event nativeLogEvent, workerAttributes []*commonpb.KeyValue) nativeStructuredFacts {
	facts := nativeStructuredFacts{eventType: structuredValue(event.Type)}
	var position nativePosition
	if json.Unmarshal(event.Data, &position) == nil {
		facts.turn = positiveInteger(position.Turn)
		facts.step = positiveInteger(position.Step)
	}
	switch event.Type {
	case "tool/call":
		var call nativeToolCall
		if json.Unmarshal(event.Data, &call) == nil {
			facts.toolCallID = structuredValue(call.CallID)
			facts.toolName = structuredValue(call.Name)
		}
	case "tool/result":
		var result nativeToolResult
		if json.Unmarshal(event.Data, &result) == nil {
			facts.toolCallID = structuredValue(result.Message.ToolCallID)
			facts.toolIsError = result.Message.IsError
			if result.Error != nil && result.Message.IsError {
				facts.errorCode = structuredValue(result.Error.Code)
			}
		}
	case "turn/end":
		var end nativeTurnEnd
		if json.Unmarshal(event.Data, &end) == nil {
			facts.turnEndReason = structuredValue(end.Reason.Kind)
			if end.Reason.Error != nil {
				facts.errorCode = structuredValue(end.Reason.Error.Code)
			}
		}
	case "assistant/message":
		var message nativeAssistantMessage
		if json.Unmarshal(event.Data, &message) == nil {
			facts.modelRequestStatus = "ok"
			if message.Interrupted {
				facts.modelRequestStatus = "interrupted"
			}
		}
		facts.applyModelRequestTiming(event.Time, workerAttributes)
		facts.usage = tokenUsage(event.Data)
	case "assistant/attempt":
		var attempt nativeAssistantAttempt
		if json.Unmarshal(event.Data, &attempt) == nil {
			facts.modelRequestStatus, facts.errorCode = attemptOutcome(attempt)
		}
		facts.applyModelRequestTiming(event.Time, workerAttributes)
		facts.usage = tokenUsage(event.Data)
	}
	return facts
}

func attemptOutcome(attempt nativeAssistantAttempt) (string, string) {
	for index := len(attempt.Stream) - 1; index >= 0; index-- {
		chunk := attempt.Stream[index].Chunk
		if attempt.Stream[index].Type != "chunk" || chunk == nil || chunk.Type != "finish" || chunk.Reason == nil {
			continue
		}
		code := ""
		if chunk.Reason.Failure != nil {
			code = structuredValue(chunk.Reason.Failure.Code)
		}
		if chunk.Reason.Kind == "aborted" {
			return "cancelled", code
		}
		return "error", code
	}
	return "error", ""
}

func (facts *nativeStructuredFacts) applyModelRequestTiming(eventTime int64, attributes []*commonpb.KeyValue) {
	startedAt, startedOK := workerIntegerAttribute(attributes, "oma.model_request.started_at")
	if startedOK && startedAt <= eventTime && eventTime-startedAt <= modelRequestMaxAgeMillis {
		facts.startedAt = startedAt
	}
	lowerBound := eventTime - modelRequestMaxAgeMillis
	if facts.startedAt > 0 {
		lowerBound = facts.startedAt
	}
	if firstToken, ok := workerIntegerAttribute(attributes, "oma.model_request.first_token_at"); ok && firstToken >= lowerBound && firstToken <= eventTime {
		facts.firstTokenAt = firstToken
	}
	for _, attribute := range attributes {
		if attribute != nil && attribute.Key == "gen_ai.request.model" {
			facts.model = structuredValue(attribute.GetValue().GetStringValue())
		}
	}
}

func workerIntegerAttribute(attributes []*commonpb.KeyValue, key string) (int64, bool) {
	for _, attribute := range attributes {
		if attribute == nil || attribute.Key != key {
			continue
		}
		switch value := attribute.GetValue().GetValue().(type) {
		case *commonpb.AnyValue_IntValue:
			return value.IntValue, value.IntValue > 0
		case *commonpb.AnyValue_DoubleValue:
			if value.DoubleValue > 0 && value.DoubleValue < math.MaxInt64/1000000 && value.DoubleValue == math.Trunc(value.DoubleValue) {
				return int64(value.DoubleValue), true
			}
		}
		return 0, false
	}
	return 0, false
}

func (facts nativeStructuredFacts) attributes() []*commonpb.KeyValue {
	return []*commonpb.KeyValue{
		otlpStringAttribute("dsh.event.type", facts.eventType),
		otlpIntAttribute("dsh.turn", facts.turn),
		otlpIntAttribute("dsh.step", facts.step),
		otlpStringAttribute("dsh.tool.call_id", facts.toolCallID),
		otlpStringAttribute("dsh.tool.name", facts.toolName),
		{Key: "dsh.tool.is_error", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: facts.toolIsError}}},
		otlpStringAttribute("dsh.error.code", facts.errorCode),
		otlpStringAttribute("dsh.turn.end_reason", facts.turnEndReason),
		otlpStringAttribute("dsh.model_request.status", facts.modelRequestStatus),
		otlpIntAttribute("oma.model_request.started_at", facts.startedAt),
		otlpIntAttribute("oma.model_request.first_token_at", facts.firstTokenAt),
		otlpStringAttribute("gen_ai.request.model", facts.model),
		otlpIntAttribute("gen_ai.usage.input_tokens", facts.usage.input),
		otlpIntAttribute("gen_ai.usage.output_tokens", facts.usage.output),
		otlpIntAttribute("gen_ai.usage.cache_read.input_tokens", facts.usage.cacheRead),
		otlpIntAttribute("gen_ai.usage.cache_creation.input_tokens", facts.usage.cacheWrite),
		otlpIntAttribute("gen_ai.usage.reasoning.output_tokens", facts.usage.reasoning),
	}
}
