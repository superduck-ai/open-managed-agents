package codesessions

import (
	"encoding/json"
	collectlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	"math"
	"strings"
)

type nativeLogHeader struct {
	ID        string `json:"id"`
	Version   int    `json:"version"`
	CreatedAt int64  `json:"createdAt"`
}

type nativeLogEvent struct {
	Seq  *int64          `json:"seq"`
	Time int64           `json:"time"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func prepareNativeOTLP(protocol otlpProtocol, body []byte, attrs trustedOTLPResourceAttributes, capture bool) ([]byte, error) {
	request := &collectlogspb.ExportLogsServiceRequest{}
	var err error
	if protocol == otlpProtocolJSON {
		err = (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, request)
	} else {
		err = proto.Unmarshal(body, request)
	}
	if err != nil {
		return nil, err
	}
	for _, resource := range request.ResourceLogs {
		if resource == nil {
			return nil, errNativeTrajectoryPayload
		}
		resource.Resource = &resourcepb.Resource{Attributes: append(trustedOTLPAttributes(attrs), otlpStringAttribute("service.name", "oma-brain-dsh"))}
		resource.SchemaUrl = ""
		for _, scope := range resource.ScopeLogs {
			if scope == nil {
				return nil, errNativeTrajectoryPayload
			}
			scope.Scope = &commonpb.InstrumentationScope{Name: "@deepseek-ai/dsh-session"}
			scope.SchemaUrl = ""
			for _, record := range scope.LogRecords {
				if err := validateNativeRecord(record, capture); err != nil {
					return nil, err
				}
			}
		}
	}
	return marshalOTLPMessage(protocol, request)
}

func logAttribute(record *logspb.LogRecord, key string) string {
	for _, attr := range record.Attributes {
		if attr != nil && attr.Key == key {
			return attr.Value.GetStringValue()
		}
	}
	return ""
}

func validateNativeRecord(record *logspb.LogRecord, capture bool) error {
	if record == nil {
		return errNativeTrajectoryPayload
	}
	headerJSON := logAttribute(record, "dsh.header")
	eventJSON := logAttribute(record, "content")
	var header nativeLogHeader
	var event nativeLogEvent
	if json.Unmarshal([]byte(headerJSON), &header) != nil || header.ID == "" || header.Version != 4 || json.Unmarshal([]byte(eventJSON), &event) != nil || event.Seq == nil || *event.Seq < 0 || event.Time <= 0 || event.Time > math.MaxInt64/1000000 || event.Type == "" || !json.Valid(event.Data) {
		return errNativeTrajectoryPayload
	}
	if !capture {
		var err error
		eventJSON, err = redactNativeJSON(eventJSON)
		if err != nil {
			return err
		}
		headerJSON, err = redactNativeJSON(headerJSON)
		if err != nil {
			return err
		}
	}
	record.Attributes = []*commonpb.KeyValue{
		otlpStringAttribute("dsh.header", headerJSON),
		otlpStringAttribute("dsh.session.id", header.ID),
		otlpIntAttribute("dsh.event.seq", *event.Seq),
		otlpIntAttribute("dsh.event.time", event.Time),
		otlpStringAttribute("sessionId", header.ID),
		otlpStringAttribute("content", eventJSON),
	}
	record.Body = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "session-log"}}
	record.TimeUnixNano = uint64(event.Time) * 1000000
	record.ObservedTimeUnixNano = record.TimeUnixNano
	return nil
}

func redactNativeJSON(raw string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", errNativeTrajectoryPayload
	}
	result, err := json.Marshal(redactNativeValue(value, ""))
	return string(result), err
}

func redactNativeValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for name, child := range typed {
			result[name] = redactNativeValue(child, name)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = redactNativeValue(child, key)
		}
		return result
	case string:
		if nativeStructuralKey(key) {
			return typed
		}
		if key == "arguments" || key == "argumentsDelta" {
			return "{}"
		}
		return "[REDACTED]"
	default:
		return value
	}
}

func nativeStructuralKey(key string) bool {
	switch key {
	case "id", "type", "kind", "name", "provider", "model", "callId", "toolCallId", "parentSession", "origin", "mode", "role", "format", "blockType", "surfaceOp", "op":
		return true
	default:
		return false
	}
}
