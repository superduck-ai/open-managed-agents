package codesessions

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/webtools"
)

func (h *Handler) WithWebTools(client *webtools.Client) *Handler { h.webTools = client; return h }

func (h *Handler) webToolPolicy(w http.ResponseWriter, r *http.Request, name string) (webtools.Policy, bool) {
	var policy webtools.Policy
	id := chi.URLParam(r, "code_session_id")
	claims, ok := h.authorizeSessionIngressClaims(w, r, id)
	if !ok {
		return policy, false
	}
	if err := h.db.ValidateCodeSessionIngressWorkerEpoch(r.Context(), claims.OrganizationUUID, claims.WorkspaceUUID, id, claims.WorkerEpoch); err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return policy, false
	}
	record, err := h.db.GetCodeSessionNetworkPolicyContext(r.Context(), id, claims.OrganizationUUID, claims.WorkspaceUUID)
	if err != nil {
		h.errorAdapter.Write(w, r, webToolContextFailure(err))
		return policy, false
	}
	policy.Environment, err = networkpolicy.ParseConfig(record.EnvironmentConfig)
	if err == nil {
		err = h.addWebToolScope(&policy, record.AgentSnapshot, name)
	}
	if err == nil {
		var scopes []webtools.ToolConfig
		scopes, err = h.ancestorWebScopes(r.Context(), id, r.Header.Get(runtimeThreadHeader), name)
		policy.Tools = append(policy.Tools, scopes...)
	}
	if err != nil {
		h.errorAdapter.Write(w, r, webToolContextFailure(err))
		return policy, false
	}
	return policy, true
}

func (h *Handler) ancestorWebScopes(ctx context.Context, id, threadID, name string) ([]webtools.ToolConfig, error) {
	if threadID == "" {
		return nil, nil
	}
	codeSession, found, err := h.db.GetCodeSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	seen := map[string]bool{}
	var scopes webtools.Policy
	for threadID != "" {
		if len(seen) >= 32 || seen[threadID] {
			return nil, db.ErrNotFound
		}
		seen[threadID] = true
		thread, err := h.db.GetSessionThread(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, threadID)
		if err != nil {
			return nil, err
		}
		if thread.ParentThreadExternalID == nil {
			if thread.OrganizationUUID != codeSession.OrganizationUUID || thread.WorkspaceUUID != codeSession.WorkspaceUUID || thread.SessionExternalID != codeSession.SessionExternalID || thread.Status == "terminated" || thread.ArchivedAt != nil || thread.DeletedAt != nil {
				return nil, db.ErrNotFound
			}
			break
		}
		snapshot, err := validatedMCPThreadSnapshot(codeSession, thread)
		if err != nil {
			return nil, err
		}
		if err := h.addWebToolScope(&scopes, snapshot, name); err != nil {
			return nil, err
		}
		threadID = *thread.ParentThreadExternalID
	}
	slices.Reverse(scopes.Tools)
	return scopes.Tools, nil
}

func (h *Handler) addWebToolScope(policy *webtools.Policy, snapshot []byte, name string) error {
	tools, err := runtimeRootToolsets(snapshot)
	if err != nil {
		return err
	}
	permission, _ := resolveToolPermissionFromAgentSnapshot(snapshot, name)
	if permission == resolvedToolPermissionDeny || !slices.Contains(runtimeBuiltinTools(tools), name) {
		return webToolDenied()
	}
	config, err := webtools.ConfigFromSnapshot(snapshot, name)
	if err != nil {
		return err
	}
	policy.Tools = append(policy.Tools, config)
	return nil
}

func (h *Handler) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	policy, ok := h.webToolPolicy(w, r, "web_search")
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
	policy, ok := h.webToolPolicy(w, r, "web_fetch")
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
	return err == nil && json.Unmarshal(raw, input, json.RejectUnknownMembers(true)) == nil
}
