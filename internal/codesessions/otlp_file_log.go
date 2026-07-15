package codesessions

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	collectlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	otlpProtocolJSON     = "json"
	otlpProtocolProtobuf = "protobuf"
)

type otlpWorkerEpochLogInfo struct {
	Present bool   `json:"present"`
	Value   string `json:"value,omitempty"`
	Source  string `json:"source,omitempty"`
}

type otlpFileLogMetadata struct {
	ReceivedAt    time.Time
	RequestID     string
	CodeSessionID string
	Signal        string
	Protocol      string
	Method        string
	Path          string
	Query         string
	ContentType   string
	Accept        string
	UserAgent     string
	ContentLength int64
	BodyBytes     int
	WorkerEpoch   otlpWorkerEpochLogInfo
	BodyPreview   otlpBodyPreview
}

type otlpBodyPreview struct {
	Encoding  string `json:"encoding"`
	Value     string `json:"value"`
	Truncated bool   `json:"truncated"`
}

type otlpDecodeSummary struct {
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	ResourceCount int    `json:"resource_count"`
	ScopeCount    int    `json:"scope_count"`
	RecordCount   int    `json:"record_count"`
}

type otlpDecodedRequest struct {
	Metrics *collectmetricspb.ExportMetricsServiceRequest
	Logs    *collectlogspb.ExportLogsServiceRequest
	Summary otlpDecodeSummary
}

type otlpFileLogBatch struct {
	Requests []any
	Metrics  []any
	Logs     []any
}

func (h *Handler) recordCodeSessionWorkerOTLP(r *http.Request, codeSessionID string, body []byte, epochFound bool, epochSource string, epochValue string) {
	if h == nil || !h.cfg.CodeSessionOTLPFileLogEnabled {
		return
	}
	signal := otlpSignalFromPath("")
	path := ""
	query := ""
	if r != nil && r.URL != nil {
		path = r.URL.Path
		query = r.URL.RawQuery
		signal = otlpSignalFromPath(path)
	}
	previewBytes := h.cfg.CodeSessionOTLPLogBodyPreviewBytes
	if previewBytes <= 0 {
		previewBytes = maxLoggedWorkerRequestBytes
	}
	meta := otlpFileLogMetadata{
		ReceivedAt:    time.Now().UTC(),
		RequestID:     requestIDFromRequest(r),
		CodeSessionID: codeSessionID,
		Signal:        signal,
		Protocol:      otlpProtocolFromContentType(headerValue(r, "Content-Type")),
		Method:        requestMethod(r),
		Path:          path,
		Query:         query,
		ContentType:   headerValue(r, "Content-Type"),
		Accept:        headerValue(r, "Accept"),
		UserAgent:     headerValue(r, "User-Agent"),
		ContentLength: requestContentLength(r),
		BodyBytes:     len(body),
		WorkerEpoch: otlpWorkerEpochLogInfo{
			Present: epochFound,
			Value:   strings.TrimSpace(epochValue),
			Source:  strings.TrimSpace(epochSource),
		},
		BodyPreview: otlpBodyPreviewForFileLog(r, body, previewBytes),
	}
	decoded, err := decodeOTLPRequest(signal, meta.Protocol, body)
	if err != nil {
		decoded.Summary.OK = false
		decoded.Summary.Error = err.Error()
	}
	batch := buildOTLPFileLogBatch(meta, decoded)
	if err := h.appendOTLPFileLogBatch(codeSessionID, batch); err != nil {
		log.Printf("write code session worker otlp file log request_id=%s code_session_id=%s signal=%s: %v", meta.RequestID, codeSessionID, signal, err)
	}
}

func (h *Handler) appendOTLPFileLogBatch(codeSessionID string, batch otlpFileLogBatch) error {
	if len(batch.Requests) == 0 && len(batch.Metrics) == 0 && len(batch.Logs) == 0 {
		return nil
	}
	root := strings.TrimSpace(h.cfg.CodeSessionOTLPLogRoot)
	if root == "" {
		root = "logs"
	}
	dir := filepath.Join(root, safeOTLPPathSegment(codeSessionID), "otlp")
	h.otlpLogMu.Lock()
	defer h.otlpLogMu.Unlock()
	if len(batch.Requests) > 0 {
		if err := appendJSONLLines(filepath.Join(dir, "requests.jsonl"), batch.Requests); err != nil {
			return err
		}
	}
	if len(batch.Metrics) > 0 {
		if err := appendJSONLLines(filepath.Join(dir, "metrics.jsonl"), batch.Metrics); err != nil {
			return err
		}
	}
	if len(batch.Logs) > 0 {
		if err := appendJSONLLines(filepath.Join(dir, "logs.jsonl"), batch.Logs); err != nil {
			return err
		}
	}
	return nil
}

func appendJSONLLines(path string, lines []any) error {
	if len(lines) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, line := range lines {
		if err := encoder.Encode(line); err != nil {
			return err
		}
	}
	return nil
}

func decodeOTLPRequest(signal string, protocol string, body []byte) (otlpDecodedRequest, error) {
	switch signal {
	case "metrics":
		req := &collectmetricspb.ExportMetricsServiceRequest{}
		if err := unmarshalOTLP(protocol, body, req); err != nil {
			return otlpDecodedRequest{}, err
		}
		return otlpDecodedRequest{Metrics: req, Summary: metricsDecodeSummary(req)}, nil
	case "logs":
		req := &collectlogspb.ExportLogsServiceRequest{}
		if err := unmarshalOTLP(protocol, body, req); err != nil {
			return otlpDecodedRequest{}, err
		}
		return otlpDecodedRequest{Logs: req, Summary: logsDecodeSummary(req)}, nil
	default:
		return otlpDecodedRequest{}, fmt.Errorf("unknown otlp signal %q", signal)
	}
}

func unmarshalOTLP(protocol string, body []byte, target proto.Message) error {
	switch protocol {
	case otlpProtocolJSON:
		if len(strings.TrimSpace(string(body))) == 0 {
			return errors.New("empty OTLP JSON body")
		}
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(body, target)
	default:
		return proto.Unmarshal(body, target)
	}
}

func metricsDecodeSummary(req *collectmetricspb.ExportMetricsServiceRequest) otlpDecodeSummary {
	summary := otlpDecodeSummary{OK: true}
	if req == nil {
		return summary
	}
	summary.ResourceCount = len(req.ResourceMetrics)
	for _, resourceMetrics := range req.ResourceMetrics {
		summary.ScopeCount += len(resourceMetrics.ScopeMetrics)
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
			for _, metric := range scopeMetrics.Metrics {
				summary.RecordCount += metricDataPointCount(metric)
			}
		}
	}
	return summary
}

func logsDecodeSummary(req *collectlogspb.ExportLogsServiceRequest) otlpDecodeSummary {
	summary := otlpDecodeSummary{OK: true}
	if req == nil {
		return summary
	}
	summary.ResourceCount = len(req.ResourceLogs)
	for _, resourceLogs := range req.ResourceLogs {
		summary.ScopeCount += len(resourceLogs.ScopeLogs)
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			summary.RecordCount += len(scopeLogs.LogRecords)
		}
	}
	return summary
}

func metricDataPointCount(metric *metricspb.Metric) int {
	if metric == nil {
		return 0
	}
	switch data := metric.Data.(type) {
	case *metricspb.Metric_Gauge:
		return len(data.Gauge.GetDataPoints())
	case *metricspb.Metric_Sum:
		return len(data.Sum.GetDataPoints())
	case *metricspb.Metric_Histogram:
		return len(data.Histogram.GetDataPoints())
	case *metricspb.Metric_ExponentialHistogram:
		return len(data.ExponentialHistogram.GetDataPoints())
	case *metricspb.Metric_Summary:
		return len(data.Summary.GetDataPoints())
	default:
		return 0
	}
}

func buildOTLPFileLogBatch(meta otlpFileLogMetadata, decoded otlpDecodedRequest) otlpFileLogBatch {
	requestLine := baseOTLPFileLogLine(meta, "otlp_request")
	requestLine["decode"] = decoded.Summary
	batch := otlpFileLogBatch{Requests: []any{requestLine}}
	if decoded.Summary.OK {
		if decoded.Metrics != nil {
			batch.Metrics = expandedMetricLines(meta, decoded.Metrics)
		}
		if decoded.Logs != nil {
			batch.Logs = expandedLogLines(meta, decoded.Logs)
		}
	}
	return batch
}

func expandedMetricLines(meta otlpFileLogMetadata, req *collectmetricspb.ExportMetricsServiceRequest) []any {
	var lines []any
	if req == nil {
		return lines
	}
	for _, resourceMetrics := range req.ResourceMetrics {
		resource := resourceLineValue(resourceMetrics.GetResource(), resourceMetrics.GetSchemaUrl())
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
			scope := scopeLineValue(scopeMetrics.GetScope(), scopeMetrics.GetSchemaUrl())
			for _, metric := range scopeMetrics.Metrics {
				metricInfo := metricLineValue(metric)
				for _, point := range metricPointLineValues(metric) {
					line := baseOTLPFileLogLine(meta, "otlp_metric")
					line["resource"] = resource
					line["scope"] = scope
					line["metric"] = metricInfo
					line["point"] = point
					lines = append(lines, line)
				}
			}
		}
	}
	return lines
}

func expandedLogLines(meta otlpFileLogMetadata, req *collectlogspb.ExportLogsServiceRequest) []any {
	var lines []any
	if req == nil {
		return lines
	}
	for _, resourceLogs := range req.ResourceLogs {
		resource := resourceLineValue(resourceLogs.GetResource(), resourceLogs.GetSchemaUrl())
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			scope := scopeLineValue(scopeLogs.GetScope(), scopeLogs.GetSchemaUrl())
			for _, record := range scopeLogs.LogRecords {
				line := baseOTLPFileLogLine(meta, "otlp_log")
				line["resource"] = resource
				line["scope"] = scope
				line["log"] = logRecordLineValue(record)
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func baseOTLPFileLogLine(meta otlpFileLogMetadata, lineType string) map[string]any {
	return map[string]any{
		"type":            lineType,
		"received_at":     meta.ReceivedAt.Format(time.RFC3339Nano),
		"request_id":      meta.RequestID,
		"code_session_id": meta.CodeSessionID,
		"signal":          meta.Signal,
		"protocol":        meta.Protocol,
		"method":          meta.Method,
		"path":            meta.Path,
		"query":           meta.Query,
		"content_type":    meta.ContentType,
		"accept":          meta.Accept,
		"user_agent":      meta.UserAgent,
		"content_length":  meta.ContentLength,
		"body_bytes":      meta.BodyBytes,
		"worker_epoch":    meta.WorkerEpoch,
		"body_preview":    meta.BodyPreview,
	}
}

func metricLineValue(metric *metricspb.Metric) map[string]any {
	value := map[string]any{
		"name":        metric.GetName(),
		"description": metric.GetDescription(),
		"unit":        metric.GetUnit(),
		"type":        metricDataType(metric),
	}
	switch data := metric.GetData().(type) {
	case *metricspb.Metric_Sum:
		value["aggregation_temporality"] = data.Sum.GetAggregationTemporality().String()
		value["is_monotonic"] = data.Sum.GetIsMonotonic()
	case *metricspb.Metric_Histogram:
		value["aggregation_temporality"] = data.Histogram.GetAggregationTemporality().String()
	case *metricspb.Metric_ExponentialHistogram:
		value["aggregation_temporality"] = data.ExponentialHistogram.GetAggregationTemporality().String()
	}
	return value
}

func metricDataType(metric *metricspb.Metric) string {
	if metric == nil {
		return ""
	}
	switch metric.GetData().(type) {
	case *metricspb.Metric_Gauge:
		return "gauge"
	case *metricspb.Metric_Sum:
		return "sum"
	case *metricspb.Metric_Histogram:
		return "histogram"
	case *metricspb.Metric_ExponentialHistogram:
		return "exponential_histogram"
	case *metricspb.Metric_Summary:
		return "summary"
	default:
		return ""
	}
}

func metricPointLineValues(metric *metricspb.Metric) []map[string]any {
	if metric == nil {
		return nil
	}
	switch data := metric.GetData().(type) {
	case *metricspb.Metric_Gauge:
		points := make([]map[string]any, 0, len(data.Gauge.GetDataPoints()))
		for _, point := range data.Gauge.GetDataPoints() {
			points = append(points, numberPointLineValue(point))
		}
		return points
	case *metricspb.Metric_Sum:
		points := make([]map[string]any, 0, len(data.Sum.GetDataPoints()))
		for _, point := range data.Sum.GetDataPoints() {
			points = append(points, numberPointLineValue(point))
		}
		return points
	case *metricspb.Metric_Histogram:
		points := make([]map[string]any, 0, len(data.Histogram.GetDataPoints()))
		for _, point := range data.Histogram.GetDataPoints() {
			points = append(points, histogramPointLineValue(point))
		}
		return points
	case *metricspb.Metric_ExponentialHistogram:
		points := make([]map[string]any, 0, len(data.ExponentialHistogram.GetDataPoints()))
		for _, point := range data.ExponentialHistogram.GetDataPoints() {
			points = append(points, exponentialHistogramPointLineValue(point))
		}
		return points
	case *metricspb.Metric_Summary:
		points := make([]map[string]any, 0, len(data.Summary.GetDataPoints()))
		for _, point := range data.Summary.GetDataPoints() {
			points = append(points, summaryPointLineValue(point))
		}
		return points
	default:
		return nil
	}
}

func baseMetricPointLineValue(startTimeUnixNano uint64, timeUnixNano uint64, attributes []*commonpb.KeyValue, flags uint32, raw proto.Message) map[string]any {
	return map[string]any{
		"start_time_unix_nano": strconv.FormatUint(startTimeUnixNano, 10),
		"time_unix_nano":       strconv.FormatUint(timeUnixNano, 10),
		"start_time":           unixNanoRFC3339(startTimeUnixNano),
		"time":                 unixNanoRFC3339(timeUnixNano),
		"attributes":           keyValuesLineValue(attributes),
		"flags":                flags,
		"raw_data_point":       protoMessageLineValue(raw),
	}
}

func numberPointLineValue(point *metricspb.NumberDataPoint) map[string]any {
	value := baseMetricPointLineValue(point.GetStartTimeUnixNano(), point.GetTimeUnixNano(), point.GetAttributes(), point.GetFlags(), point)
	switch number := point.GetValue().(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		value["as_double"] = number.AsDouble
		value["value"] = number.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		value["as_int"] = number.AsInt
		value["value"] = number.AsInt
	}
	return value
}

func histogramPointLineValue(point *metricspb.HistogramDataPoint) map[string]any {
	value := baseMetricPointLineValue(point.GetStartTimeUnixNano(), point.GetTimeUnixNano(), point.GetAttributes(), point.GetFlags(), point)
	value["count"] = point.GetCount()
	value["sum"] = point.GetSum()
	value["bucket_counts"] = point.GetBucketCounts()
	value["explicit_bounds"] = point.GetExplicitBounds()
	value["min"] = point.GetMin()
	value["max"] = point.GetMax()
	return value
}

func exponentialHistogramPointLineValue(point *metricspb.ExponentialHistogramDataPoint) map[string]any {
	value := baseMetricPointLineValue(point.GetStartTimeUnixNano(), point.GetTimeUnixNano(), point.GetAttributes(), point.GetFlags(), point)
	value["count"] = point.GetCount()
	value["sum"] = point.GetSum()
	value["scale"] = point.GetScale()
	value["zero_count"] = point.GetZeroCount()
	value["min"] = point.GetMin()
	value["max"] = point.GetMax()
	return value
}

func summaryPointLineValue(point *metricspb.SummaryDataPoint) map[string]any {
	value := baseMetricPointLineValue(point.GetStartTimeUnixNano(), point.GetTimeUnixNano(), point.GetAttributes(), point.GetFlags(), point)
	value["count"] = point.GetCount()
	value["sum"] = point.GetSum()
	quantiles := make([]map[string]any, 0, len(point.GetQuantileValues()))
	for _, quantile := range point.GetQuantileValues() {
		quantiles = append(quantiles, map[string]any{
			"quantile": quantile.GetQuantile(),
			"value":    quantile.GetValue(),
		})
	}
	value["quantile_values"] = quantiles
	return value
}

func logRecordLineValue(record *logspb.LogRecord) map[string]any {
	if record == nil {
		return map[string]any{}
	}
	return map[string]any{
		"time_unix_nano":           strconv.FormatUint(record.GetTimeUnixNano(), 10),
		"time":                     unixNanoRFC3339(record.GetTimeUnixNano()),
		"observed_time_unix_nano":  strconv.FormatUint(record.GetObservedTimeUnixNano(), 10),
		"observed_time":            unixNanoRFC3339(record.GetObservedTimeUnixNano()),
		"severity_number":          record.GetSeverityNumber().String(),
		"severity_text":            record.GetSeverityText(),
		"body":                     anyValueLineValue(record.GetBody()),
		"attributes":               keyValuesLineValue(record.GetAttributes()),
		"dropped_attributes_count": record.GetDroppedAttributesCount(),
		"flags":                    record.GetFlags(),
		"trace_id":                 hex.EncodeToString(record.GetTraceId()),
		"span_id":                  hex.EncodeToString(record.GetSpanId()),
		"raw_log_record":           protoMessageLineValue(record),
	}
}

func resourceLineValue(resource *resourcepb.Resource, schemaURL string) map[string]any {
	return map[string]any{
		"schema_url":               schemaURL,
		"attributes":               keyValuesLineValue(resource.GetAttributes()),
		"dropped_attributes_count": resource.GetDroppedAttributesCount(),
	}
}

func scopeLineValue(scope *commonpb.InstrumentationScope, schemaURL string) map[string]any {
	return map[string]any{
		"schema_url":               schemaURL,
		"name":                     scope.GetName(),
		"version":                  scope.GetVersion(),
		"attributes":               keyValuesLineValue(scope.GetAttributes()),
		"dropped_attributes_count": scope.GetDroppedAttributesCount(),
	}
}

func keyValuesLineValue(values []*commonpb.KeyValue) map[string]any {
	result := map[string]any{}
	for _, value := range values {
		if value == nil || value.Key == "" {
			continue
		}
		result[value.Key] = anyValueLineValue(value.Value)
	}
	return result
}

func anyValueLineValue(value *commonpb.AnyValue) any {
	if value == nil {
		return nil
	}
	switch typed := value.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return typed.StringValue
	case *commonpb.AnyValue_BoolValue:
		return typed.BoolValue
	case *commonpb.AnyValue_IntValue:
		return typed.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return typed.DoubleValue
	case *commonpb.AnyValue_BytesValue:
		return base64.StdEncoding.EncodeToString(typed.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		values := make([]any, 0, len(typed.ArrayValue.GetValues()))
		for _, child := range typed.ArrayValue.GetValues() {
			values = append(values, anyValueLineValue(child))
		}
		return values
	case *commonpb.AnyValue_KvlistValue:
		return keyValuesLineValue(typed.KvlistValue.GetValues())
	default:
		return nil
	}
}

func protoMessageLineValue(message proto.Message) any {
	if message == nil {
		return nil
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: false}.Marshal(message)
	if err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}

func otlpBodyPreviewForFileLog(r *http.Request, body []byte, limit int) otlpBodyPreview {
	if limit <= 0 {
		limit = maxLoggedWorkerRequestBytes
	}
	truncated := len(body) > limit
	if truncated {
		body = body[:limit]
	}
	if otlpBodyLooksText(r) {
		return otlpBodyPreview{Encoding: "utf8", Value: strings.ToValidUTF8(string(body), ""), Truncated: truncated}
	}
	return otlpBodyPreview{Encoding: "base64", Value: base64.StdEncoding.EncodeToString(body), Truncated: truncated}
}

func otlpProtocolFromContentType(contentType string) string {
	if strings.Contains(strings.ToLower(contentType), "json") {
		return otlpProtocolJSON
	}
	return otlpProtocolProtobuf
}

func unixNanoRFC3339(value uint64) string {
	if value == 0 || value > uint64(1<<63-1) {
		return ""
	}
	return time.Unix(0, int64(value)).UTC().Format(time.RFC3339Nano)
}

func safeOTLPPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '_' || char == '-':
			builder.WriteRune(char)
		default:
			builder.WriteByte('_')
		}
	}
	safe := builder.String()
	if strings.Trim(safe, "_") == "" {
		return "_"
	}
	return safe
}

func requestIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return httpapi.RequestID(r.Context())
}

func requestMethod(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Method
}

func requestContentLength(r *http.Request) int64 {
	if r == nil {
		return 0
	}
	return r.ContentLength
}

func headerValue(r *http.Request, key string) string {
	if r == nil {
		return ""
	}
	return r.Header.Get(key)
}
