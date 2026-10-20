package codesessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

var sandboxMCPMethods = telemetry.NewEnum("server/discover", "initialize", "notifications/initialized", "ping", "tools/list", "tools/call", "notifications/cancelled")

const sandboxNotForwardedHeader = "X-Oma-Sandbox-Not-Forwarded"

func (h *Handler) handleSandboxMCP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(sandboxNotForwardedHeader, "true")
	id := chi.URLParam(r, "code_session_id")
	claims, ok := h.authorizeSessionIngressClaims(w, r, id)
	if !ok {
		return
	}
	session, found, err := h.db.GetCodeSession(r.Context(), id)
	if err == nil && (!found || session.OrganizationUUID != claims.OrganizationUUID || session.WorkspaceUUID != claims.WorkspaceUUID || session.CurrentWorkerEpoch != claims.WorkerEpoch) {
		err = db.ErrWorkerEpochMismatch
	}
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	h.serveSandboxMCP(w, r, session)
}

type sandboxMCPCall struct {
	method  string
	tool    string
	outcome string
}

func (c sandboxMCPCall) with(outcome string) sandboxMCPCall {
	c.outcome = outcome
	return c
}

func (h *Handler) serveSandboxMCP(w http.ResponseWriter, r *http.Request, session db.CodeSession) {
	ctx, span := telemetry.Start(r.Context(), "oma.sandbox_mcp.call",
		telemetry.AttrCodeSessionID.String(session.ExternalID), telemetry.AttrSessionID.String(session.SessionExternalID))
	startedAt := time.Now()
	call := h.proxySandboxMCP(w, r.WithContext(ctx), session, span)
	span.SetAttributes(telemetry.AttrMCPMethod.String(call.method), telemetry.AttrOutcome.String(call.outcome))
	if sandboxMCPSpanFailed(call.outcome) {
		span.SetStatus(codes.Error, call.outcome)
	}
	attrs := []attribute.KeyValue{telemetry.AttrMCPMethod.String(call.method), telemetry.AttrOutcome.String(call.outcome)}
	if call.tool != "" {
		attrs = append(attrs, telemetry.AttrToolName.String(call.tool))
	}
	codeSessionMetrics.Get().sandboxMCP.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
	span.End()
}

func (h *Handler) proxySandboxMCP(w http.ResponseWriter, r *http.Request, session db.CodeSession, span trace.Span) sandboxMCPCall {
	call := sandboxMCPCall{method: telemetry.OtherValue, outcome: "invalid_request"}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSandboxMCPRequestBytes))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxRequestReadError(err))
		return call
	}
	var request sandboxMCPRequest
	if json.Unmarshal(body, &request) != nil || request.JSONRPC != "2.0" || request.Method == "" {
		h.errorAdapter.Write(w, r, sandboxInvalidRequest(nil))
		return call
	}
	call.method, call.tool = sandboxMCPMethods.Normalize(request.Method), sandboxMCPToolName(request)
	if call.tool != "" {
		span.SetAttributes(telemetry.AttrToolName.String(call.tool), telemetry.AttrMCPRequestID.String(sandboxMCPRequestID(request)))
	}
	if request.Method == "tools/call" {
		policies, err := h.service.currentRuntimeToolPolicies(r.Context(), session)
		if err != nil {
			h.errorAdapter.Write(w, r, sandboxOperationError(err))
			return call.with("policy_error")
		}
		policy, known := policies[request.Params.Name]
		if policies != nil && (!known || policy == string(resolvedToolPermissionDeny)) {
			h.errorAdapter.Write(w, r, sandboxToolDenied())
			return call.with("tool_denied")
		}
	}
	endpoint, err := h.ensureSessionSandbox(r.Context(), session, !strings.HasPrefix(request.Method, "notifications/"))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return call.with("sandbox_unavailable")
	}
	if err := h.db.ValidateCodeSessionIngressWorkerEpoch(r.Context(), session.OrganizationUUID, session.WorkspaceUUID, session.ExternalID, session.CurrentWorkerEpoch); err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return call.with("epoch_mismatch")
	}
	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, bytes.NewReader(body))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return call.with("upstream_error")
	}
	copySandboxMCPRequestHeaders(upstream.Header, r.Header)
	telemetry.InjectTraceParent(r.Context(), upstream.Header.Set)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	w.Header().Del(sandboxNotForwardedHeader)
	response, err := client.Do(upstream)
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return call.with(sandboxMCPTransportOutcome(r))
	}
	defer response.Body.Close()
	span.SetAttributes(attribute.Int("oma.sandbox_mcp.upstream_status_code", response.StatusCode))
	call.outcome = sandboxMCPStatusOutcome(response.StatusCode)
	if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return call.with(h.relaySandboxMCPStream(w, r, response, call))
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, sandboxMCPResponseLimit+1))
	if len(responseBody) > sandboxMCPResponseLimit {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return call.with(sandboxMCPTransportOutcome(r))
	}
	if replyOutcome, ok := sandboxMCPReplyOutcome(responseBody); ok && call.outcome == telemetry.OutcomeOK {
		call.outcome = replyOutcome
	}
	copySandboxMCPResponseHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
	return call
}

func (h *Handler) relaySandboxMCPStream(w http.ResponseWriter, r *http.Request, response *http.Response, call sandboxMCPCall) string {
	copySandboxMCPResponseHeaders(w.Header(), response.Header)
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(response.StatusCode)
	observe := func([]byte) {}
	inspector := &sandboxMCPStreamInspector{}
	if call.tool != "" {
		observe = inspector.observe
	}
	if !streamSandboxMCPResponse(w, response.Body, observe) {
		return sandboxMCPTransportOutcome(r)
	}
	if call.outcome == telemetry.OutcomeOK && inspector.outcome != "" {
		return inspector.outcome
	}
	return call.outcome
}

func copySandboxMCPRequestHeaders(target, source http.Header) {
	for _, key := range []string{"Content-Type", "Accept", "Mcp-Protocol-Version", "Mcp-Method", "Mcp-Name"} {
		if value := source.Get(key); value != "" {
			target.Set(key, value)
		}
	}
	for key, values := range source {
		if strings.HasPrefix(http.CanonicalHeaderKey(key), "Mcp-Param-") {
			target[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
		}
	}
	if target.Get("Accept") == "" {
		target.Set("Accept", "application/json, text/event-stream")
	}
}

func copySandboxMCPResponseHeaders(target, source http.Header) {
	for _, key := range []string{"Content-Type", "Cache-Control", "X-Accel-Buffering"} {
		if value := source.Get(key); value != "" {
			target.Set(key, value)
		}
	}
}

func streamSandboxMCPResponse(w http.ResponseWriter, body io.Reader, observe func([]byte)) bool {
	buffer := make([]byte, 32<<10)
	controller := http.NewResponseController(w)
	for {
		count, err := body.Read(buffer)
		if count > 0 {
			observe(buffer[:count])
			if _, writeErr := w.Write(buffer[:count]); writeErr != nil {
				return false
			}
			if flushErr := controller.Flush(); flushErr != nil {
				return false
			}
		}
		if err != nil {
			observe([]byte("\n\n"))
			return errors.Is(err, io.EOF)
		}
	}
}
