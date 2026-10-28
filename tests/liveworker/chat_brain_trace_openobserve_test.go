//go:build integration

package liveworker

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/telemetryexport"
)

const (
	forgedSpanID        = "f0f0f0f0f0f0f0f0"
	forgedParentSpanID  = "0badc0de0badc0de"
	openObserveMetadata = "oma_trace_metadata"
	openObserveContent  = "oma_trace_content"
)

var requiredContentAttributes = []string{"gen_ai.input.messages", "gen_ai.output.messages", "gen_ai.tool.call.arguments", "oma.tool_result.content"}

type openObserveExpectation struct {
	organizationID string
	workspaceID    string
	sessionID      string
	primary        string
	childThread    string
	agentID        string
}

func createOpenObserveTraceDestinations(t *testing.T, e *liveEnv, cfg config.Config) {
	t.Helper()
	backend := cfg.Observability.OpenObserve
	authorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(backend.Ingestion.Username+":"+backend.Ingestion.Password))
	endpoint := strings.TrimRight(backend.BaseURL, "/") + "/api/" + backend.Organization
	insertTraceDestination(t, e, cfg, "openobserve_metadata", endpoint, "metadata_only", map[string]string{"Authorization": authorization, "stream-name": openObserveMetadata})
	insertTraceDestination(t, e, cfg, "openobserve_content", endpoint, "content", map[string]string{"Authorization": authorization, "stream-name": openObserveContent})
}

func stringKeyValue(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func sendForgedTenantSpan(t *testing.T, e *liveEnv, traced *liveSession) {
	t.Helper()
	traceID, err := hex.DecodeString(callerTraceID)
	requireOK(t, err)
	spanID, err := hex.DecodeString(forgedSpanID)
	requireOK(t, err)
	parentID, err := hex.DecodeString(forgedParentSpanID)
	requireOK(t, err)
	now := uint64(time.Now().UnixNano())
	payload, err := proto.Marshal(&coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			stringKeyValue("service.name", "attacker"), stringKeyValue("oma.organization.id", "org_forged"),
			stringKeyValue("oma.workspace.id", "wrkspc_forged"), stringKeyValue("oma.session.id", "sesn_forged"),
		}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			TraceId: traceID, SpanId: spanID, ParentSpanId: parentID, Name: "oma.session.turn",
			StartTimeUnixNano: now - uint64(time.Second), EndTimeUnixNano: now,
			Attributes: []*commonpb.KeyValue{
				stringKeyValue("gen_ai.conversation.id", "cse_forged"), stringKeyValue("gen_ai.agent.id", "agent_forged"),
				stringKeyValue("attacker.injected", "forged"),
			},
		}}}},
	}}})
	requireOK(t, err)
	waitRealWorker(t, "forged tenant span accepted by the worker traces ingress", func() bool {
		code, err := e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), traced.session.ExternalID)
		requireOK(t, err)
		probe := &liveSession{env: e, session: traced.session, code: code}
		probe.issueToken(t)
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, e.url+probe.path("/worker/otlp/v1/traces"), bytes.NewReader(payload))
		requireOK(t, err)
		request.Header.Set("Content-Type", "application/x-protobuf")
		request.Header.Set("Authorization", "Bearer "+probe.token)
		response, err := http.DefaultClient.Do(request)
		requireOK(t, err)
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusOK {
			t.Logf("forged span ingress status %d %s", response.StatusCode, body)
			return false
		}
		result := &coltracepb.ExportTraceServiceResponse{}
		requireOK(t, proto.Unmarshal(body, result))
		if rejected := result.GetPartialSuccess().GetRejectedSpans(); rejected != 0 {
			t.Fatalf("forged tenant span rejected: %d %s", rejected, result.GetPartialSuccess().GetErrorMessage())
		}
		t.Logf("OTEL_EVIDENCE %s", mustJSON(t, map[string]any{"forged_probe": map[string]string{"code_session_id": code.ExternalID, "span_id": forgedSpanID, "forged_service_name": "attacker", "forged_session_id": "sesn_forged"}}))
		return true
	}, 20*time.Second)
}

func verifyForgedSpan(t *testing.T, name string, spans []*tracepb.Span, primary, agentID string) {
	t.Helper()
	for _, span := range spans {
		if hex.EncodeToString(span.SpanId) != forgedSpanID {
			continue
		}
		if spanAttribute(span, "gen_ai.conversation.id") != primary || spanAttribute(span, "gen_ai.agent.id") != agentID || spanAttribute(span, "attacker.injected") != "" {
			t.Fatalf("%s forged span attributes were not rewritten: %v", name, span.Attributes)
		}
		return
	}
	t.Fatalf("%s did not receive the forged tenant span", name)
}

func verifyContentIsolation(t *testing.T, name string, spans []*tracepb.Span, content bool) {
	t.Helper()
	present := map[string]bool{}
	for _, span := range spans {
		for _, attribute := range span.Attributes {
			if slices.Contains(telemetryexport.ContentAttributes, attribute.Key) {
				present[attribute.Key] = true
			}
		}
	}
	if !content && len(present) > 0 {
		t.Fatalf("%s metadata destination received content attributes %v", name, present)
	}
	if content {
		for _, key := range requiredContentAttributes {
			if !present[key] {
				t.Fatalf("%s content destination missing %s (present=%v)", name, key, present)
			}
		}
	}
	t.Logf("OTEL_EVIDENCE %s", mustJSON(t, map[string]any{"receiver": name, "spans": len(spans), "content_attributes": present}))
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	requireOK(t, err)
	return string(data)
}

type openObserveRow map[string]any

func (r openObserveRow) text(key string) string {
	value, ok := r[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func (r openObserveRow) has(key string) bool {
	return r.text(key) != ""
}

func openObserveField(attribute string) string {
	return strings.ReplaceAll(attribute, ".", "_")
}

func searchOpenObserveTraces(t *testing.T, cfg config.Config, started time.Time, stream string) []openObserveRow {
	t.Helper()
	backend := cfg.Observability.OpenObserve
	body, err := json.Marshal(map[string]any{"query": map[string]any{
		"sql":        fmt.Sprintf(`SELECT * FROM "%s" WHERE trace_id = '%s'`, stream, callerTraceID),
		"start_time": started.Add(-10 * time.Minute).UnixMicro(), "end_time": time.Now().Add(time.Minute).UnixMicro(), "from": 0, "size": 500,
	}})
	requireOK(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, strings.TrimRight(backend.BaseURL, "/")+"/api/"+backend.Organization+"/_search?type=traces", bytes.NewReader(body))
	requireOK(t, err)
	request.SetBasicAuth(backend.Query.Username, backend.Query.Password)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	requireOK(t, err)
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Logf("OpenObserve %s search status %d %s", stream, response.StatusCode, payload)
		return nil
	}
	var result struct {
		Hits []openObserveRow `json:"hits"`
	}
	requireOK(t, json.Unmarshal(payload, &result))
	return result.Hits
}

func openObserveReady(rows []openObserveRow) bool {
	turns, writes, forged := 0, 0, false
	for _, row := range rows {
		switch row.text("operation_name") {
		case "oma.session.turn":
			turns++
		case "oma.tool_use Write":
			writes++
		}
		forged = forged || row.text("span_id") == forgedSpanID
	}
	return turns >= 3 && writes > 0 && forged
}

func verifyOpenObserveTraces(t *testing.T, cfg config.Config, started time.Time, want openObserveExpectation) {
	t.Helper()
	streams := map[string][]openObserveRow{}
	waitRealWorker(t, "spans queryable from real OpenObserve traces streams", func() bool {
		for _, stream := range []string{openObserveMetadata, openObserveContent} {
			streams[stream] = searchOpenObserveTraces(t, cfg, started, stream)
			if !openObserveReady(streams[stream]) {
				return false
			}
		}
		return true
	}, 60*time.Second)
	writeCallIDs := map[string]string{}
	for _, stream := range []string{openObserveMetadata, openObserveContent} {
		writeCallIDs[stream] = verifyOpenObserveStream(t, stream, streams[stream], want, stream == openObserveContent)
	}
	if writeCallIDs[openObserveMetadata] == "" || writeCallIDs[openObserveMetadata] != writeCallIDs[openObserveContent] {
		t.Fatalf("OpenObserve Write tool call ids differ across destinations: %v", writeCallIDs)
	}
}

func verifyOpenObserveStream(t *testing.T, stream string, rows []openObserveRow, want openObserveExpectation, content bool) string {
	t.Helper()
	var root, child, forged openObserveRow
	for _, row := range rows {
		if row.text("service_name") != "oma-platform" || row.text("service_oma_organization_id") != want.organizationID || row.text("service_oma_workspace_id") != want.workspaceID || row.text("service_oma_session_id") != want.sessionID {
			t.Fatalf("OpenObserve %s span %s has untrusted resource %v", stream, row.text("span_id"), row)
		}
		if row.text("span_id") == forgedSpanID {
			forged = row
			continue
		}
		if row.text("operation_name") != "oma.session.turn" {
			continue
		}
		switch row.text("gen_ai_conversation_id") {
		case want.primary:
			if row.text("reference_parent_span_id") == callerSpanID {
				root = row
			}
		case want.childThread:
			child = row
		}
	}
	if root == nil || child == nil || forged == nil {
		t.Fatalf("OpenObserve %s missing root/child/forged span (rows=%d)", stream, len(rows))
	}
	if root.text("gen_ai_agent_id") != want.agentID || child.text("reference_parent_span_id") != root.text("span_id") {
		t.Fatalf("OpenObserve %s turn nesting: root=%v child=%v", stream, root, child)
	}
	if forged.text("gen_ai_conversation_id") != want.primary || forged.text("gen_ai_agent_id") != want.agentID || forged.has("attacker_injected") {
		t.Fatalf("OpenObserve %s forged span not rewritten: %v", stream, forged)
	}
	failed, succeeded, writeCallID := []string{}, 0, ""
	contentFields := map[string]bool{}
	for _, row := range rows {
		operation := row.text("operation_name")
		if operation == "oma.model_request" && row.text("reference_parent_span_id") == root.text("span_id") {
			if row.text("span_status") == "ERROR" && row.has("error_type") {
				failed = append(failed, row.text("span_id")+":"+row.text("error_type"))
			} else {
				succeeded++
			}
		}
		if strings.HasPrefix(operation, "oma.tool_use send_to_agent") {
			t.Fatalf("OpenObserve %s has a multi-agent delegation tool span", stream)
		}
		if operation == "oma.tool_use Write" {
			writeCallID = row.text("gen_ai_tool_call_id")
		}
		for _, attribute := range telemetryexport.ContentAttributes {
			if row.has(openObserveField(attribute)) {
				contentFields[attribute] = true
			}
		}
	}
	if len(failed) != 1 || succeeded < 2 {
		t.Fatalf("OpenObserve %s model request spans: failed=%v succeeded=%d", stream, failed, succeeded)
	}
	if !content && (len(contentFields) > 0 || child.has("gen_ai_agent_name")) {
		t.Fatalf("OpenObserve %s metadata stream holds content %v agent_name=%q", stream, contentFields, child.text("gen_ai_agent_name"))
	}
	if content {
		for _, attribute := range requiredContentAttributes {
			if !contentFields[attribute] {
				t.Fatalf("OpenObserve %s content stream missing %s (present=%v)", stream, attribute, contentFields)
			}
		}
		if child.text("gen_ai_agent_name") != "observer" {
			t.Fatalf("OpenObserve %s content stream lost the inline subagent name: %v", stream, child)
		}
	}
	t.Logf("OTEL_EVIDENCE %s", mustJSON(t, map[string]any{
		"openobserve_stream": stream, "rows": len(rows), "trace_id": callerTraceID,
		"root": summarizeRow(root), "child": summarizeRow(child), "forged": summarizeRow(forged),
		"failed_model_requests": failed, "succeeded_model_requests": succeeded, "write_tool_call_id": writeCallID,
		"content_attributes": contentFields, "child_agent_name": child.text("gen_ai_agent_name"),
	}))
	return writeCallID
}

func summarizeRow(row openObserveRow) map[string]string {
	summary := map[string]string{}
	for _, key := range []string{"operation_name", "trace_id", "span_id", "reference_parent_span_id", "span_status", "gen_ai_conversation_id", "gen_ai_agent_id", "service_name", "service_oma_organization_id", "service_oma_workspace_id", "service_oma_session_id"} {
		summary[key] = row.text(key)
	}
	return summary
}
