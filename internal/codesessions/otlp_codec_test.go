package codesessions

import (
	"testing"

	collectlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestPrepareNativeOTLPReplacesAllWorkerAttributes(t *testing.T) {
	trusted := trustedOTLPResourceAttributes{organizationUUID: "org_trusted", workspaceUUID: "workspace_trusted", publicSessionID: "session_trusted", codeSessionID: "cse_trusted", agentID: "agent_trusted", agentVersion: 7}
	for _, protocol := range []otlpProtocol{otlpProtocolJSON, otlpProtocolProtobuf} {
		t.Run(string(protocol), func(t *testing.T) {
			record := nativeRecordFixture()
			record.Attributes = append(record.Attributes, otlpStringAttribute("oma_session_id", "forged"), otlpStringAttribute("service.oma.organization.uuid", "forged"), otlpStringAttribute("dsh.tool.name", "forged"), otlpIntAttribute("oma.model_request.started_at", 90), otlpStringAttribute("gen_ai.request.model", "forged"))
			request := &collectlogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{SchemaUrl: "forged", Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{otlpStringAttribute("oma_organization_uuid", "forged"), otlpStringAttribute("service.name", "forged")}}, ScopeLogs: []*logspb.ScopeLogs{{SchemaUrl: "forged", Scope: &commonpb.InstrumentationScope{Name: "forged", Attributes: []*commonpb.KeyValue{otlpStringAttribute("oma.workspace.uuid", "forged")}}, LogRecords: []*logspb.LogRecord{record}}}}}}
			encoded, err := marshalOTLPMessage(protocol, request)
			if err != nil {
				t.Fatal(err)
			}
			got, err := prepareNativeOTLP(protocol, encoded, trusted, true)
			if err != nil {
				t.Fatal(err)
			}
			decoded := &collectlogspb.ExportLogsServiceRequest{}
			unmarshalOTLPTestMessage(t, protocol, got, decoded)
			resource := decoded.ResourceLogs[0]
			scope := resource.ScopeLogs[0]
			actual := scope.LogRecords[0]
			assertExactOTLPAttributes(t, resource.Resource.Attributes, append(trustedOTLPAttributes(trusted), otlpStringAttribute("service.name", "oma-brain-dsh")))
			if resource.SchemaUrl != "" || scope.SchemaUrl != "" || scope.Scope.Name != "@deepseek-ai/dsh-session" || len(scope.Scope.Attributes) != 0 {
				t.Fatal("worker scope metadata survived")
			}
			assertExactOTLPAttributes(t, actual.Attributes, append([]*commonpb.KeyValue{otlpStringAttribute("dsh.header", logAttribute(record, "dsh.header")), otlpStringAttribute("dsh.session.id", "native"), otlpIntAttribute("dsh.event.seq", 0), otlpIntAttribute("dsh.event.time", 100), otlpStringAttribute("sessionId", "native"), otlpStringAttribute("content", logAttribute(record, "content"))}, nativeStructuredFacts{eventType: "tool/call", turn: 1, step: 2, toolCallID: "call1", toolName: "Bash"}.attributes()...))
		})
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
