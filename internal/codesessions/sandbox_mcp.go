package codesessions

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (h *Handler) handleSandboxMCP(w http.ResponseWriter, r *http.Request) {
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxInvalidRequest(err))
		return
	}
	var call struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &call) != nil || call.JSONRPC != "2.0" || call.Method == "" {
		h.errorAdapter.Write(w, r, sandboxInvalidRequest(nil))
		return
	}
	if call.Method == "tools/call" {
		permitted, err := sandboxToolPermitted(session.Metadata, call.Params.Name)
		if err != nil {
			h.errorAdapter.Write(w, r, sandboxOperationError(err))
			return
		}
		if !permitted {
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
	request, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, bytes.NewReader(body))
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	copySandboxMCPRequestHeaders(request.Header, r.Header)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	defer response.Body.Close()
	if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		copySandboxMCPResponseHeaders(w.Header(), response.Header)
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(response.StatusCode)
		streamSandboxMCPResponse(w, response.Body)
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
	copySandboxMCPResponseHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
}

func sandboxToolPermitted(metadata json.RawMessage, toolName string) (bool, error) {
	var record struct {
		Config struct {
			RuntimeToolPolicies map[string]string `json:"runtime_tool_permissions"`
		} `json:"config"`
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &record); err != nil {
			return false, err
		}
	}
	policies := record.Config.RuntimeToolPolicies
	if policies == nil {
		return true, nil
	}
	policy, ok := policies[toolName]
	return ok && policy != string(resolvedToolPermissionDeny), nil
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
