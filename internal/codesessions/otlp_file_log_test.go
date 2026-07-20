package codesessions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	collectlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestDecodeOTLPRequestRejectsMalformedPayloads(t *testing.T) {
	if _, err := decodeOTLPRequest("logs", otlpProtocolJSON, []byte(`{"resourceLogs":[`)); err == nil {
		t.Fatal("decode malformed json error = nil, want error")
	}
	if _, err := decodeOTLPRequest("metrics", otlpProtocolProtobuf, []byte{0xff, 0xff}); err == nil {
		t.Fatal("decode malformed protobuf error = nil, want error")
	}
}

func TestRecordCodeSessionWorkerOTLPFileLogRecordsDecodeErrors(t *testing.T) {
	root := t.TempDir()
	handler := NewHandler(config.Config{
		CodeSession: config.CodeSessionConfig{
			OTLPFileLogEnabled: true,
			OTLPLogRoot:        root,
		},
	}, newTestService(t, nil))
	req := httptest.NewRequest(http.MethodPost, "/v1/code/sessions/cse_bad/worker/otlp/logs", bytes.NewReader([]byte(`{"resourceLogs":[`)))
	req.Header.Set("Content-Type", "application/json")

	handler.recordCodeSessionWorkerOTLP(req, "cse_bad", []byte(`{"resourceLogs":[`), true, "query:worker_epoch", "7")

	requestLines := readJSONLObjects(t, filepath.Join(root, "cse_bad", "otlp", "requests.jsonl"))
	if len(requestLines) != 1 {
		t.Fatalf("request lines = %d, want 1: %#v", len(requestLines), requestLines)
	}
	decode := requestLines[0]["decode"].(map[string]any)
	if decode["ok"] != false || decode["error"] == "" {
		t.Fatalf("unexpected decode error line: %#v", requestLines[0])
	}
	if _, err := os.Stat(filepath.Join(root, "cse_bad", "otlp", "logs.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("logs.jsonl exists for decode failure, stat err=%v", err)
	}
}

func TestDecodeOTLPRequestSupportsJSONAndProtobuf(t *testing.T) {
	metricsRequest := testOTLPMetricsRequest()
	metricsJSON, err := protojson.Marshal(metricsRequest)
	if err != nil {
		t.Fatalf("marshal metrics json: %v", err)
	}
	metricsProto, err := proto.Marshal(metricsRequest)
	if err != nil {
		t.Fatalf("marshal metrics proto: %v", err)
	}
	logsRequest := testOTLPLogsRequest()
	logsJSON, err := protojson.Marshal(logsRequest)
	if err != nil {
		t.Fatalf("marshal logs json: %v", err)
	}
	logsProto, err := proto.Marshal(logsRequest)
	if err != nil {
		t.Fatalf("marshal logs proto: %v", err)
	}

	cases := []struct {
		name     string
		signal   string
		protocol string
		body     []byte
	}{
		{name: "metrics json", signal: "metrics", protocol: otlpProtocolJSON, body: metricsJSON},
		{name: "metrics protobuf", signal: "metrics", protocol: otlpProtocolProtobuf, body: metricsProto},
		{name: "logs json", signal: "logs", protocol: otlpProtocolJSON, body: logsJSON},
		{name: "logs protobuf", signal: "logs", protocol: otlpProtocolProtobuf, body: logsProto},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := decodeOTLPRequest(tc.signal, tc.protocol, tc.body)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !decoded.Summary.OK || decoded.Summary.ResourceCount != 1 || decoded.Summary.ScopeCount != 1 || decoded.Summary.RecordCount != 1 {
				t.Fatalf("unexpected summary: %+v", decoded.Summary)
			}
		})
	}
}

func TestDecodeOTLPRequestIgnoresUnknownJSONFields(t *testing.T) {
	decoded, err := decodeOTLPRequest("metrics", otlpProtocolJSON, []byte(`{"resourceMetrics":[],"unknownField":"ignored"}`))
	if err != nil {
		t.Fatalf("decode unknown json field: %v", err)
	}
	if !decoded.Summary.OK || decoded.Summary.ResourceCount != 0 {
		t.Fatalf("unexpected summary for unknown-field json: %+v", decoded.Summary)
	}
}

func TestSafeOTLPPathSegmentRejectsPathControlCharacters(t *testing.T) {
	got := safeOTLPPathSegment("../cse.test\\bad id")
	if got == "" || strings.ContainsAny(got, `/\.`) || strings.Contains(got, " ") {
		t.Fatalf("safeOTLPPathSegment returned unsafe segment %q", got)
	}
	if safeOTLPPathSegment(" \t") != "_" {
		t.Fatalf("empty safe path segment = %q, want _", safeOTLPPathSegment(" \t"))
	}
}

func TestOTLPBodyLooksTextUsesParsedMediaType(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{contentType: "application/not-json", want: false},
		{contentType: "application/vnd.otlp+json", want: true},
		{contentType: "text/plain; charset=utf-8", want: true},
		{contentType: "application/x-protobuf", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.contentType, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/otlp", nil)
			req.Header.Set("Content-Type", tc.contentType)
			if got := otlpBodyLooksText(req); got != tc.want {
				t.Fatalf("otlpBodyLooksText(%q) = %t, want %t", tc.contentType, got, tc.want)
			}
		})
	}
}

func TestRecordCodeSessionWorkerOTLPFileLogWritesRequestAndExpandedRecords(t *testing.T) {
	root := t.TempDir()
	handler := NewHandler(config.Config{
		CodeSession: config.CodeSessionConfig{
			OTLPFileLogEnabled:      true,
			OTLPLogRoot:             root,
			OTLPLogBodyPreviewBytes: 8,
		},
	}, newTestService(t, nil))

	metricsBody, err := proto.Marshal(testOTLPMetricsRequest())
	if err != nil {
		t.Fatalf("marshal metrics proto: %v", err)
	}
	metricsReq := httptest.NewRequest(http.MethodPost, "/v1/code/sessions/cse_test/worker/otlp/metrics", bytes.NewReader(metricsBody))
	metricsReq.Header.Set("Content-Type", "application/x-protobuf")
	handler.recordCodeSessionWorkerOTLP(metricsReq, "cse_test", metricsBody, true, "header:x-worker-epoch", "1")

	logsJSON, err := protojson.Marshal(testOTLPLogsRequest())
	if err != nil {
		t.Fatalf("marshal logs json: %v", err)
	}
	logsReq := httptest.NewRequest(http.MethodPost, "/v1/code/sessions/cse_test/worker/otlp/logs", bytes.NewReader(logsJSON))
	logsReq.Header.Set("Content-Type", "application/json")
	handler.recordCodeSessionWorkerOTLP(logsReq, "cse_test", logsJSON, false, "", "")

	requestLines := readJSONLObjects(t, filepath.Join(root, "cse_test", "otlp", "requests.jsonl"))
	if len(requestLines) != 2 {
		t.Fatalf("request lines = %d, want 2: %#v", len(requestLines), requestLines)
	}
	otlpDir := filepath.Join(root, "cse_test", "otlp")
	if info, err := os.Stat(otlpDir); err != nil {
		t.Fatalf("stat otlp dir: %v", err)
	} else if info.Mode().Perm() != 0o700 {
		t.Fatalf("otlp dir mode = %o, want 700", info.Mode().Perm())
	}
	if info, err := os.Stat(filepath.Join(otlpDir, "requests.jsonl")); err != nil {
		t.Fatalf("stat requests.jsonl: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("requests.jsonl mode = %o, want 600", info.Mode().Perm())
	}
	firstDecode := requestLines[0]["decode"].(map[string]any)
	if firstDecode["ok"] != true || firstDecode["record_count"].(float64) != 1 {
		t.Fatalf("unexpected first request decode: %#v", firstDecode)
	}
	firstPreview := requestLines[0]["body_preview"].(map[string]any)
	if firstPreview["encoding"] != "base64" || firstPreview["truncated"] != true {
		t.Fatalf("unexpected first body preview: %#v", firstPreview)
	}
	secondEpoch := requestLines[1]["worker_epoch"].(map[string]any)
	if secondEpoch["present"] != false {
		t.Fatalf("unexpected second worker_epoch: %#v", secondEpoch)
	}

	metricLines := readJSONLObjects(t, filepath.Join(otlpDir, "metrics.jsonl"))
	if len(metricLines) != 1 {
		t.Fatalf("metric lines = %d, want 1: %#v", len(metricLines), metricLines)
	}
	metric := metricLines[0]["metric"].(map[string]any)
	if metric["name"] != "claude_code.test.counter" || metric["type"] != "sum" {
		t.Fatalf("unexpected metric line: %#v", metricLines[0])
	}
	point := metricLines[0]["point"].(map[string]any)
	if point["value"].(float64) != 42 {
		t.Fatalf("unexpected metric point: %#v", point)
	}

	logLines := readJSONLObjects(t, filepath.Join(otlpDir, "logs.jsonl"))
	if len(logLines) != 1 {
		t.Fatalf("log lines = %d, want 1: %#v", len(logLines), logLines)
	}
	record := logLines[0]["log"].(map[string]any)
	if record["body"] != "claude_code.test_event" || record["severity_text"] != "INFO" {
		t.Fatalf("unexpected log record: %#v", record)
	}
}

func testOTLPMetricsRequest() *collectmetricspb.ExportMetricsServiceRequest {
	now := uint64(time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC).UnixNano())
	return &collectmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						stringKeyValue("service.name", "claude-code"),
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "com.anthropic.claude_code"},
						Metrics: []*metricspb.Metric{
							{
								Name:        "claude_code.test.counter",
								Description: "test counter",
								Unit:        "1",
								Data: &metricspb.Metric_Sum{
									Sum: &metricspb.Sum{
										AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
										IsMonotonic:            true,
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: now,
												Attributes: []*commonpb.KeyValue{
													stringKeyValue("phase", "unit-test"),
												},
												Value: &metricspb.NumberDataPoint_AsInt{AsInt: 42},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func testOTLPLogsRequest() *collectlogspb.ExportLogsServiceRequest {
	now := uint64(time.Date(2026, 7, 6, 12, 1, 0, 0, time.UTC).UnixNano())
	return &collectlogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						stringKeyValue("service.name", "claude-code"),
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						Scope: &commonpb.InstrumentationScope{Name: "com.anthropic.claude_code.events"},
						LogRecords: []*logspb.LogRecord{
							{
								TimeUnixNano:   now,
								SeverityText:   "INFO",
								SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO,
								Body: &commonpb.AnyValue{
									Value: &commonpb.AnyValue_StringValue{StringValue: "claude_code.test_event"},
								},
								Attributes: []*commonpb.KeyValue{
									stringKeyValue("event.name", "test_event"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func stringKeyValue(key string, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value},
		},
	}
}

func readJSONLObjects(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	result := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var object map[string]any
		if err := json.Unmarshal(line, &object); err != nil {
			t.Fatalf("decode jsonl line %q: %v", string(line), err)
		}
		result = append(result, object)
	}
	return result
}
