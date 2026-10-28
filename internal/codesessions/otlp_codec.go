package codesessions

import (
	"fmt"
	"mime"
	"net/http"
	"strings"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type otlpProtocol string

const (
	otlpProtocolJSON     otlpProtocol = "json"
	otlpProtocolProtobuf otlpProtocol = "protobuf"
)

type trustedOTLPResourceAttributes struct {
	organizationUUID string
	workspaceUUID    string
	publicSessionID  string
	codeSessionID    string
	agentID          string
	agentVersion     int64
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

func trustedOTLPAttributes(attrs trustedOTLPResourceAttributes) []*commonpb.KeyValue {
	attributes := []*commonpb.KeyValue{
		otlpStringAttribute("oma.organization.uuid", attrs.organizationUUID),
		otlpStringAttribute("oma.workspace.uuid", attrs.workspaceUUID),
		otlpStringAttribute("oma.session.id", attrs.publicSessionID),
		otlpStringAttribute("oma.code_session.id", attrs.codeSessionID),
		otlpStringAttribute("oma.agent.id", attrs.agentID),
		otlpIntAttribute("oma.agent.version", attrs.agentVersion),
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
		return (protojson.MarshalOptions{UseEnumNumbers: true}).Marshal(message)
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
