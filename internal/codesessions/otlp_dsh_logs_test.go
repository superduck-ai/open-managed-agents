package codesessions

import (
	collectlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"strings"
	"testing"
)

func TestNativeOTLPRejectsNonNativePayload(t *testing.T) {
	for _, record := range []*logspb.LogRecord{
		{},
		{Attributes: append([]*commonpb.KeyValue{}, otlpStringAttribute("dsh.header", `{"id":"native","version":3}`), otlpStringAttribute("content", `{"seq":0,"time":100,"type":"turn/start","data":{}}`))},
	} {
		if err := validateNativeRecord(record, true); err == nil {
			t.Fatal("accepted invalid native record")
		}
	}
}

func nativeRecordFixture() *logspb.LogRecord {
	return &logspb.LogRecord{SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO, Attributes: []*commonpb.KeyValue{
		otlpStringAttribute("dsh.header", `{"id":"native","version":4,"cwd":"/private/path"}`),
		otlpStringAttribute("content", `{"seq":0,"time":100,"type":"tool/call","data":{"turn":1,"step":2,"callId":"call1","name":"Bash","arguments":"{\"secret\":\"private-key\"}"}}`),
		otlpStringAttribute("dsh.session.id", "forged"),
	}}
}

func TestNativeOTLPPreservesEnvelopeAndEnforcesContentPolicy(t *testing.T) {
	for _, capture := range []bool{false, true} {
		record := nativeRecordFixture()
		original := logAttribute(record, "content")
		request := &collectlogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{record}}}}}}
		encoded, err := protojson.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := prepareNativeOTLP(otlpProtocolJSON, encoded, trustedOTLPResourceAttributes{}, capture)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"severityNumber":9`) && !strings.Contains(string(body), `"severityNumber": 9`) {
			t.Fatal("OTLP severity must remain numeric for OpenObserve")
		}
		decoded := &collectlogspb.ExportLogsServiceRequest{}
		if err := protojson.Unmarshal(body, decoded); err != nil {
			t.Fatal(err)
		}
		actual := decoded.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
		content := logAttribute(actual, "content")
		if logAttribute(actual, "dsh.session.id") != "native" {
			t.Fatal("worker-selected identity was trusted")
		}
		if capture && content != original {
			t.Fatal("native event was rewritten")
		}
		if !capture && (strings.Contains(content, "private-key") || strings.Contains(logAttribute(actual, "dsh.header"), "/private/path")) {
			t.Fatal("content policy did not strip private content")
		}
		if actual.TimeUnixNano != 100000000 {
			t.Fatal("native event timestamp lost")
		}
	}
}

func TestRedactNativeJSONRejectsMalformedContent(t *testing.T) {
	for _, raw := range []string{`{`, `{} {}`, `{"value":NaN}`} {
		redacted, err := redactNativeJSON(raw)
		if err == nil || redacted != "" {
			t.Fatalf("accepted malformed JSON: %q, %v", redacted, err)
		}
	}
}

func TestRedactNativeJSONPreservesNumbers(t *testing.T) {
	raw := `{"seq":9007199254740993,"time":100,"type":"tool/call","data":{"usage":9007199254740995,"large":1e400,"text":"secret"}}`
	redacted, err := redactNativeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"9007199254740993", "9007199254740995", "1e400"} {
		if !strings.Contains(redacted, number) {
			t.Fatalf("number %s changed: %s", number, redacted)
		}
	}
	if strings.Contains(redacted, "secret") {
		t.Fatal("private content survived")
	}
}
