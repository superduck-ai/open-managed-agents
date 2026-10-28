//go:build integration

package liveworker

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
	"github.com/superduck-ai/open-managed-agents/internal/telemetrydestinations"
)

const (
	callerTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	callerSpanID  = "00f067aa0ba902b7"
)

type receivedSpans struct {
	resource      map[string]string
	spans         []*tracepb.Span
	authorization string
	encoding      string
	contentType   string
}

type otlpReceiver struct {
	*httptest.Server
	mutex    sync.Mutex
	requests map[string][]receivedSpans
}

func newOTLPReceiver(t *testing.T) *otlpReceiver {
	t.Helper()
	receiver := &otlpReceiver{requests: map[string][]receivedSpans{}}
	receiver.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			unzipped, err := gzip.NewReader(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			reader = unzipped
		}
		body, _ := io.ReadAll(reader)
		request := &coltracepb.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(body, request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received := receivedSpans{resource: map[string]string{}, authorization: r.Header.Get("Authorization"), encoding: r.Header.Get("Content-Encoding"), contentType: r.Header.Get("Content-Type")}
		for _, resource := range request.ResourceSpans {
			for _, attribute := range resource.GetResource().GetAttributes() {
				received.resource[attribute.Key] = attribute.Value.GetStringValue()
			}
			for _, scope := range resource.ScopeSpans {
				received.spans = append(received.spans, scope.Spans...)
			}
		}
		receiver.mutex.Lock()
		receiver.requests[r.URL.Path] = append(receiver.requests[r.URL.Path], received)
		receiver.mutex.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)
	return receiver
}

func (r *otlpReceiver) received(path string) ([]receivedSpans, []*tracepb.Span) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	requests := append([]receivedSpans(nil), r.requests[path]...)
	var spans []*tracepb.Span
	for _, request := range requests {
		spans = append(spans, request.spans...)
	}
	return requests, spans
}

func spanAttribute(span *tracepb.Span, key string) string {
	for _, attribute := range span.Attributes {
		if attribute.Key == key {
			if value := attribute.Value.GetStringValue(); value != "" {
				return value
			}
			if attribute.Value.GetBoolValue() {
				return "true"
			}
		}
	}
	return ""
}

func createTraceDestination(t *testing.T, e *liveEnv, cfg config.Config, receiver *otlpReceiver, name, mode string) {
	t.Helper()
	insertTraceDestination(t, e, cfg, name, receiver.URL+"/"+name, mode, map[string]string{"Authorization": "Bearer verify-" + name})
}

func insertTraceDestination(t *testing.T, e *liveEnv, cfg config.Config, name, endpoint, mode string, headers map[string]string) {
	t.Helper()
	secrets, err := secretservice.New(cfg.Vault.MasterKey)
	requireOK(t, err)
	id := "tdst_verify_" + name
	names := make([]string, 0, len(headers))
	for header := range headers {
		names = append(names, header)
	}
	destination := db.TelemetryDestination{
		UUID: uuid.NewString(), ExternalID: id, OrganizationUUID: e.key.OrganizationUUID.String(),
		EndpointURL: endpoint, TracesEnabled: true, TracesEndpointURL: endpoint + "/v1/traces",
		TracesContentMode: mode, TracesMaxContentBytes: telemetrydestinations.DefaultMaxContentBytes, WorkspaceScope: "all", State: "enabled",
		HeaderNames: names,
	}
	sealed, err := json.Marshal(headers)
	requireOK(t, err)
	envelope, err := secrets.Seal(t.Context(), telemetrydestinations.HeaderBinding(destination.OrganizationUUID, id), sealed)
	requireOK(t, err)
	destination.Headers = &envelope
	_, err = e.database.CreateTelemetryDestination(t.Context(), destination, telemetrydestinations.MaxDestinations)
	requireOK(t, err)
	t.Cleanup(func() {
		_ = e.database.DeleteTelemetryDestination(context.Background(), destination.OrganizationUUID, id)
	})
}

func sendTracedMessage(t *testing.T, f *liveSession, text string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": text}}}}})
	requireOK(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.env.url+"/v1/sessions/"+f.session.ExternalID+"/events?beta=true", bytes.NewReader(body))
	requireOK(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	request.Header.Set("Authorization", "Bearer "+f.env.apiKey)
	request.Header.Set("traceparent", "00-"+callerTraceID+"-"+callerSpanID+"-01")
	request.Header.Set("tracestate", "verify=be")
	response, err := http.DefaultClient.Do(request)
	requireOK(t, err)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("send traced message: %d %s", response.StatusCode, payload)
	}
}

func verifyTraceExport(t *testing.T, e *liveEnv, cfg config.Config, started time.Time) {
	t.Helper()
	receiver := newOTLPReceiver(t)
	createTraceDestination(t, e, cfg, receiver, "metadata", "metadata_only")
	createTraceDestination(t, e, cfg, receiver, "content", "content")
	createOpenObserveTraceDestinations(t, e, cfg)
	allowAgentTools(t, e)
	configureChatModel(t, e, brainSubagentModel(t, true).URL)
	traced, _ := createRemovableChat(t, e)
	sendTracedMessage(t, traced, "请委派子 agent 并返回可追踪的结果。")
	childThread := ""
	waitRealWorker(t, "spans delivered to both customer OTLP destinations", func() bool {
		if childThread == "" {
			childThread = subagentThreadID(t, traced)
		}
		for _, path := range []string{"/metadata/v1/traces", "/content/v1/traces"} {
			_, spans := receiver.received(path)
			turns, tools := 0, 0
			for _, span := range spans {
				switch span.Name {
				case "oma.session.turn":
					turns++
				case "oma.tool_use Write":
					tools++
				}
			}
			if turns < 2 || tools == 0 {
				return false
			}
		}
		return childThread != ""
	}, 60*time.Second)
	sendForgedTenantSpan(t, e, traced)
	waitRealWorker(t, "forged tenant span delivered to both customer OTLP destinations", func() bool {
		for _, path := range []string{"/metadata/v1/traces", "/content/v1/traces"} {
			_, spans := receiver.received(path)
			if !slices.ContainsFunc(spans, func(span *tracepb.Span) bool { return hex.EncodeToString(span.SpanId) == forgedSpanID }) {
				return false
			}
		}
		return true
	}, 30*time.Second)
	primary, _, err := e.database.GetPrimarySessionThreadExternalID(t.Context(), e.key.WorkspaceUUID.String(), traced.session.ExternalID)
	requireOK(t, err)
	workspace, err := e.database.GetWorkspaceIdentifiers(t.Context(), e.key.WorkspaceUUID.String())
	requireOK(t, err)
	for _, test := range []struct {
		path    string
		content bool
	}{{"/metadata/v1/traces", false}, {"/content/v1/traces", true}} {
		requests, spans := receiver.received(test.path)
		name := strings.Split(test.path, "/")[1]
		for _, request := range requests {
			if request.authorization != "Bearer verify-"+name || request.encoding != "gzip" || request.contentType != "application/x-protobuf" {
				t.Fatalf("%s delivery headers = %+v", name, request)
			}
			if request.resource["service.name"] != "oma-platform" || request.resource["oma.session.id"] != traced.session.ExternalID || request.resource["oma.workspace.id"] != workspace.WorkspaceExternalID || request.resource["oma.organization.id"] != e.key.OrganizationUUID.String() {
				t.Fatalf("%s resource = %+v", name, request.resource)
			}
		}
		verifyTraceShape(t, spans, primary, childThread, e.agent.ExternalID, test.content)
		verifyToolCallID(t, traced, spans)
		verifyContentIsolation(t, name, spans, test.content)
		verifyForgedSpan(t, name, spans, primary, e.agent.ExternalID)
	}
	verifyOpenObserveTraces(t, cfg, started, openObserveExpectation{
		organizationID: e.key.OrganizationUUID.String(), workspaceID: workspace.WorkspaceExternalID, sessionID: traced.session.ExternalID,
		primary: primary, childThread: childThread, agentID: e.agent.ExternalID,
	})
	for _, id := range []string{"tdst_verify_metadata", "tdst_verify_content"} {
		destination, err := e.database.GetTelemetryDestination(t.Context(), e.key.OrganizationUUID.String(), id)
		requireOK(t, err)
		if destination.LastSuccessAt == nil || destination.State != "enabled" {
			t.Fatalf("%s delivery health = %+v", id, destination)
		}
	}
	waitRealWorker(t, "traced session idle", func() bool {
		var state struct {
			Status string `json:"status"`
		}
		requireOK(t, json.Unmarshal(e.request(t, "GET", "/v1/sessions/"+traced.session.ExternalID, e.apiKey, nil, 200), &state))
		return state.Status == "idle"
	})
	chatProof(t, started, "brain_observability_trace_export")
}

func verifyTraceShape(t *testing.T, spans []*tracepb.Span, primary, childThread, agentID string, content bool) {
	t.Helper()
	var root, child *tracepb.Span
	for _, span := range spans {
		if hex.EncodeToString(span.TraceId) != callerTraceID {
			t.Fatalf("span %s left the caller's trace", span.Name)
		}
		if span.Name != "oma.session.turn" {
			continue
		}
		switch spanAttribute(span, "gen_ai.conversation.id") {
		case primary:
			if hex.EncodeToString(span.ParentSpanId) == callerSpanID {
				root = span
			}
		case childThread:
			child = span
		}
	}
	if root == nil || child == nil {
		t.Fatalf("missing root or subagent turn (primary=%s child=%s)", primary, childThread)
	}
	if spanAttribute(root, "gen_ai.agent.id") != agentID || root.TraceState != "verify=be" || spanAttribute(root, "oma.session.turn.stop_reason") != "end_turn" {
		t.Fatalf("root turn = %v", root)
	}
	if !bytes.Equal(child.ParentSpanId, root.SpanId) {
		t.Fatal("subagent turn is not nested under the caller-parented turn")
	}
	failed, succeeded := 0, 0
	for _, span := range spans {
		if span.Name == "oma.model_request" && bytes.Equal(span.ParentSpanId, root.SpanId) {
			if span.GetStatus().GetCode() == tracepb.Status_STATUS_CODE_ERROR && spanAttribute(span, "error.type") != "" {
				failed++
			} else {
				succeeded++
			}
		}
		if strings.HasPrefix(span.Name, "oma.tool_use send_to_agent") {
			t.Fatal("multi-agent delegation produced a tool span")
		}
	}
	if failed != 1 || succeeded < 2 {
		t.Fatalf("model request spans: failed=%d succeeded=%d", failed, succeeded)
	}
	inputs := spanAttribute(root, "gen_ai.input.messages")
	if content != strings.Contains(inputs, "可追踪") {
		t.Fatalf("content mode %v exported input messages %q", content, inputs)
	}
	if content && spanAttribute(child, "gen_ai.agent.name") != "observer" {
		t.Fatalf("content destination lost the inline subagent name: %v", child)
	}
	if !content && spanAttribute(child, "gen_ai.agent.name") != "" {
		t.Fatal("metadata destination received a model-generated subagent name")
	}
}

const traceWriteCallID = "call_obs_trace_write"

func allowAgentTools(t *testing.T, e *liveEnv) {
	t.Helper()
	var agent struct {
		Version int `json:"version"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "GET", "/v1/agents/"+e.agent.ExternalID, e.apiKey, nil, 200), &agent))
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": agent.Version,
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, 200)
}

func verifyToolCallID(t *testing.T, f *liveSession, spans []*tracepb.Span) {
	t.Helper()
	exported := ""
	for _, span := range spans {
		if span.Name == "oma.tool_use Write" {
			exported = spanAttribute(span, "gen_ai.tool.call.id")
		}
	}
	var history struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?types[]=agent.tool_use", f.env.apiKey, nil, 200), &history))
	public := []string{}
	for _, event := range history.Data {
		if strings.EqualFold(event.Name, "write") {
			public = append(public, event.ID)
		}
	}
	if exported == "" || len(public) != 1 || public[0] != exported {
		t.Fatalf("exported tool call id %q does not match public agent.tool_use events %v", exported, public)
	}
}
