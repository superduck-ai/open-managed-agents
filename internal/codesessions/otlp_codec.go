package codesessions

import (
	"fmt"
	"mime"
	"net/http"
	"strings"
	"unicode"

	collectlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collecttracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type otlpProtocol string

const (
	otlpProtocolJSON     otlpProtocol = "json"
	otlpProtocolProtobuf otlpProtocol = "protobuf"
)

const otlpKeyValueMessageName protoreflect.FullName = "opentelemetry.proto.common.v1.KeyValue"

type trustedOTLPResourceAttributes struct {
	organizationUUID string
	workspaceUUID    string
	publicSessionID  string
	codeSessionID    string
	agentID          string
	agentVersion     int64
	workerEpoch      int64
}

func parseOTLPProtocol(contentType string) (otlpProtocol, error) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return "", fmt.Errorf("invalid OTLP Content-Type: %w", err)
	}
	switch strings.ToLower(mediaType) {
	case "application/json":
		return otlpProtocolJSON, nil
	case "application/x-protobuf", "application/protobuf":
		return otlpProtocolProtobuf, nil
	default:
		return "", fmt.Errorf("unsupported OTLP Content-Type %q", mediaType)
	}
}

func canonicalizeOTLPPayload(signal string, protocol otlpProtocol, body []byte, attrs trustedOTLPResourceAttributes) ([]byte, error) {
	message, err := newOTLPRequest(signal)
	if err != nil {
		return nil, err
	}
	switch protocol {
	case otlpProtocolJSON:
		err = (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, message)
	case otlpProtocolProtobuf:
		err = proto.Unmarshal(body, message)
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol %q", protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("decode OTLP %s: %w", signal, err)
	}

	stripReservedOTLPAttributes(message.ProtoReflect())
	injectTrustedOTLPResources(message, attrs)
	if protocol == otlpProtocolJSON {
		// OpenObserve's OTLP/HTTP JSON ingestion expects protobuf enum values as
		// their numeric representation (for example Span.kind), not enum names.
		return (protojson.MarshalOptions{UseEnumNumbers: true}).Marshal(message)
	}
	return proto.Marshal(message)
}

func newOTLPRequest(signal string) (proto.Message, error) {
	switch signal {
	case "metrics":
		return &collectmetricspb.ExportMetricsServiceRequest{}, nil
	case "logs":
		return &collectlogspb.ExportLogsServiceRequest{}, nil
	case "traces":
		return &collecttracepb.ExportTraceServiceRequest{}, nil
	default:
		return nil, fmt.Errorf("unsupported OTLP signal %q", signal)
	}
}

func stripReservedOTLPAttributes(message protoreflect.Message) {
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsList() && field.Kind() == protoreflect.MessageKind {
			list := value.List()
			if field.Message().FullName() == otlpKeyValueMessageName {
				stripReservedOTLPKeyValues(list)
				return true
			}
			for index := 0; index < list.Len(); index++ {
				stripReservedOTLPAttributes(list.Get(index).Message())
			}
			return true
		}
		if field.Kind() == protoreflect.MessageKind && message.Has(field) {
			stripReservedOTLPAttributes(value.Message())
		}
		return true
	})
}

func stripReservedOTLPKeyValues(list protoreflect.List) {
	writeIndex := 0
	for readIndex := 0; readIndex < list.Len(); readIndex++ {
		value := list.Get(readIndex)
		keyValue := value.Message()
		stripReservedOTLPAttributes(keyValue)
		keyField := keyValue.Descriptor().Fields().ByName("key")
		if keyField != nil && isReservedOTLPAttributeKey(keyValue.Get(keyField).String()) {
			continue
		}
		if writeIndex != readIndex {
			list.Set(writeIndex, value)
		}
		writeIndex++
	}
	list.Truncate(writeIndex)
}

// OpenObserve normalizes OTLP attribute keys to lowercase alphanumeric or
// underscore columns, and stores trace *resource* attributes with an extra
// service_ prefix, so the trusted tenant columns are oma_* on metrics/logs and
// service_oma_* on traces. Protect both server-owned namespaces after that
// same normalization: a client alias such as oma_organization_uuid, or a span
// attribute like service.oma.organization.uuid that flattens straight into the
// traces tenant column, must never override trusted values at the sink.
func isReservedOTLPAttributeKey(key string) bool {
	var normalized strings.Builder
	normalized.Grow(len(key))
	for _, character := range key {
		switch {
		case character == '_' || unicode.IsLower(character) || unicode.IsNumber(character):
			normalized.WriteRune(character)
		case unicode.IsUpper(character):
			normalized.WriteRune(unicode.ToLower(character))
		default:
			normalized.WriteByte('_')
		}
	}
	canonical := strings.TrimPrefix(normalized.String(), "service_")
	return canonical == "oma" || strings.HasPrefix(canonical, "oma_")
}

func injectTrustedOTLPResources(message proto.Message, attrs trustedOTLPResourceAttributes) {
	trusted := trustedOTLPAttributes(attrs)
	switch request := message.(type) {
	case *collectmetricspb.ExportMetricsServiceRequest:
		for _, item := range request.ResourceMetrics {
			item.Resource = appendTrustedOTLPAttributes(item.Resource, trusted)
		}
	case *collectlogspb.ExportLogsServiceRequest:
		for _, item := range request.ResourceLogs {
			item.Resource = appendTrustedOTLPAttributes(item.Resource, trusted)
		}
	case *collecttracepb.ExportTraceServiceRequest:
		for _, item := range request.ResourceSpans {
			item.Resource = appendTrustedOTLPAttributes(item.Resource, trusted)
		}
	}
}

func appendTrustedOTLPAttributes(resource *resourcepb.Resource, attrs []*commonpb.KeyValue) *resourcepb.Resource {
	if resource == nil {
		resource = &resourcepb.Resource{}
	}
	resource.Attributes = append(resource.Attributes, attrs...)
	return resource
}

func trustedOTLPAttributes(attrs trustedOTLPResourceAttributes) []*commonpb.KeyValue {
	attributes := []*commonpb.KeyValue{
		otlpStringAttribute("oma.organization.uuid", attrs.organizationUUID),
		otlpStringAttribute("oma.workspace.uuid", attrs.workspaceUUID),
		otlpStringAttribute("oma.session.id", attrs.publicSessionID),
		otlpStringAttribute("oma.code_session.id", attrs.codeSessionID),
		otlpStringAttribute("oma.agent.id", attrs.agentID),
		otlpIntAttribute("oma.agent.version", attrs.agentVersion),
	}
	if attrs.workerEpoch > 0 {
		attributes = append(attributes, otlpIntAttribute("oma.worker.epoch", attrs.workerEpoch))
	}
	return attributes
}

func otlpStringAttribute(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func otlpIntAttribute(key string, value int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: value}}}
}

func writeOTLPStatus(w http.ResponseWriter, protocol otlpProtocol, statusCode int, message, retryAfter string) {
	if retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	payload, err := marshalOTLPMessage(protocol, &statuspb.Status{Code: int32(otlpCodeForHTTPStatus(statusCode)), Message: message})
	if err != nil {
		payload = nil
	}
	w.Header().Set("Content-Type", otlpContentType(protocol))
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
}

func writeOTLPSuccess(w http.ResponseWriter, protocol otlpProtocol, body []byte) {
	w.Header().Set("Content-Type", otlpContentType(protocol))
	w.WriteHeader(http.StatusOK)
	if len(body) > 0 {
		_, _ = w.Write(body)
		return
	}
	if protocol == otlpProtocolJSON {
		_, _ = w.Write([]byte("{}"))
	}
}

func marshalOTLPMessage(protocol otlpProtocol, message proto.Message) ([]byte, error) {
	if protocol == otlpProtocolJSON {
		return protojson.Marshal(message)
	}
	return proto.Marshal(message)
}

func otlpContentType(protocol otlpProtocol) string {
	if protocol == otlpProtocolJSON {
		return "application/json"
	}
	return "application/x-protobuf"
}

func otlpCodeForHTTPStatus(statusCode int) codes.Code {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType:
		return codes.InvalidArgument
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.NotFound
	case http.StatusConflict:
		return codes.Aborted
	case http.StatusGone:
		return codes.FailedPrecondition
	case http.StatusRequestEntityTooLarge, http.StatusTooManyRequests:
		return codes.ResourceExhausted
	case http.StatusGatewayTimeout:
		return codes.DeadlineExceeded
	default:
		return codes.Unavailable
	}
}
