package codesessions

import (
	"encoding/json/v2"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/webtools"
)

func (h *Handler) WithWebTools(client *webtools.Client) *Handler {
	h.webTools = client
	return h
}

func (h *Handler) webToolPolicy(w http.ResponseWriter, r *http.Request, name string) (networkpolicy.Config, bool) {
	id := chi.URLParam(r, "code_session_id")
	claims, ok := h.authorizeSessionIngressClaims(w, r, id)
	if !ok {
		return networkpolicy.Config{}, false
	}
	if err := h.db.ValidateCodeSessionIngressWorkerEpoch(r.Context(), claims.OrganizationUUID, claims.WorkspaceUUID, id, claims.WorkerEpoch); err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return networkpolicy.Config{}, false
	}
	record, err := h.db.GetCodeSessionNetworkPolicyContext(r.Context(), id, claims.OrganizationUUID, claims.WorkspaceUUID)
	if err != nil {
		h.errorAdapter.Write(w, r, webToolContextFailure(err))
		return networkpolicy.Config{}, false
	}
	permission, _ := resolveToolPermissionFromAgentSnapshot(record.AgentSnapshot, name)
	if permission == resolvedToolPermissionDeny {
		h.errorAdapter.Write(w, r, webToolDenied())
		return networkpolicy.Config{}, false
	}
	policy, err := networkpolicy.ParseConfig(record.EnvironmentConfig)
	if err != nil {
		h.errorAdapter.Write(w, r, webToolContextFailure(err))
		return policy, false
	}
	return policy, true
}

func (h *Handler) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	policy, ok := h.webToolPolicy(w, r, "WebSearch")
	if !ok {
		return
	}
	var input webtools.SearchRequest
	if !decodeWebInput(w, r, &input) {
		h.errorAdapter.Write(w, r, invalidWebToolRequest())
		return
	}
	output, err := h.webTools.Search(r.Context(), input, policy)
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, output)
}

func (h *Handler) handleWebFetch(w http.ResponseWriter, r *http.Request) {
	policy, ok := h.webToolPolicy(w, r, "WebFetch")
	if !ok {
		return
	}
	var input webtools.FetchRequest
	if !decodeWebInput(w, r, &input) {
		h.errorAdapter.Write(w, r, invalidWebToolRequest())
		return
	}
	output, err := h.webTools.Fetch(r.Context(), input, policy)
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, output)
}

func decodeWebInput(w http.ResponseWriter, r *http.Request, input any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	return err == nil && json.Unmarshal(raw, input) == nil
}
