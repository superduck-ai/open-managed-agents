package codesessions

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	sandboxNotForwardedHeader  = "X-Oma-Sandbox-Not-Forwarded"
	sandboxToolCallWriteWindow = 15 * time.Minute
)

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

func (h *Handler) serveSandboxMCP(w http.ResponseWriter, r *http.Request, session db.CodeSession) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSandboxMCPRequestBytes))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxRequestReadError(err))
		return
	}
	var call sandboxRPCRequest
	if json.Unmarshal(body, &call) != nil || call.JSONRPC != "2.0" || call.Method == "" {
		h.errorAdapter.Write(w, r, sandboxInvalidRequest(nil))
		return
	}
	if call.Method == "tools/call" {
		policies, err := h.service.currentRuntimeToolPolicies(r.Context(), session, strings.TrimSpace(r.Header.Get(runtimeThreadHeader)))
		if err != nil {
			h.errorAdapter.Write(w, r, sandboxOperationError(err))
			return
		}
		policy, known := policies[call.Params.Name]
		if policies != nil && (!known || policy == string(resolvedToolPermissionDeny)) {
			h.errorAdapter.Write(w, r, sandboxToolDenied())
			return
		}
	}
	endpoint, err := h.ensureSessionSandbox(r.Context(), session, !strings.HasPrefix(call.Method, "notifications/"))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	if err := h.db.ValidateCodeSessionIngressWorkerEpoch(r.Context(), session.OrganizationUUID, session.WorkspaceUUID, session.ExternalID, session.CurrentWorkerEpoch); err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	if h.rejectMemoryToolMutation(w, r, session, call) {
		return
	}
	commit, err := h.prepareMemoryToolCommit(r.Context(), session, call)
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, bytes.NewReader(body))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	copySandboxMCPRequestHeaders(request.Header, r.Header)
	if call.Method == "tools/call" {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(sandboxToolCallWriteWindow))
	}
	w.Header().Del(sandboxNotForwardedHeader)
	response, sent, err := sendSandboxMCPRequest(request)
	if err != nil {
		if !sent {
			w.Header().Set(sandboxNotForwardedHeader, "true")
		}
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	defer response.Body.Close()
	if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		copySandboxMCPResponseHeaders(w.Header(), response.Header)
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(response.StatusCode)
		if commit == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			streamSandboxMCPResponse(w, response.Body)
		} else if err := streamMemoryToolResponse(r.Context(), w, response.Body, call, commit); err != nil {
			h.logger.WarnContext(r.Context(), "Memory tool version confirmation failed", "session_id", session.SessionExternalID, "error", err)
		}
		return
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if len(responseBody) > 8<<20 {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		responseBody, err = commit.confirm(r.Context(), call, responseBody)
		if err != nil {
			h.logger.WarnContext(r.Context(), "Memory tool version confirmation failed", "session_id", session.SessionExternalID, "error", err)
		}
	}
	copySandboxMCPResponseHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
}

func sendSandboxMCPRequest(request *http.Request) (*http.Response, bool, error) {
	var sent atomic.Bool
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			sent.Store(true)
		}
	}}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request.WithContext(httptrace.WithClientTrace(request.Context(), trace)))
	return response, sent.Load(), err
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

func streamSandboxMCPResponse(w http.ResponseWriter, body io.Reader) {
	buffer := make([]byte, 32<<10)
	controller := http.NewResponseController(w)
	for {
		count, err := body.Read(buffer)
		if count > 0 {
			if _, writeErr := w.Write(buffer[:count]); writeErr != nil {
				return
			}
			if flushErr := controller.Flush(); flushErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
