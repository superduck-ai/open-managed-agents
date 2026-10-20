package codesessions

import (
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
	threadID := r.Header.Get(runtimeThreadHeader)
	if err == nil && threadID != "" {
		codeSession, found, loadErr := h.db.GetCodeSession(r.Context(), id)
		if loadErr != nil || !found {
			err = db.ErrNotFound
		} else {
			seen := map[string]bool{}
			var scopes []webtools.ToolConfig
			for threadID != "" && err == nil {
				if len(seen) >= 32 || seen[threadID] {
					err = db.ErrNotFound
					break
				}
				seen[threadID] = true
				thread, loadErr := h.db.GetSessionThread(r.Context(), codeSession.WorkspaceUUID, codeSession.SessionExternalID, threadID)
				if loadErr != nil {
					err = loadErr
					break
				}
				if thread.ParentThreadExternalID == nil {
					if thread.OrganizationUUID != codeSession.OrganizationUUID || thread.WorkspaceUUID != codeSession.WorkspaceUUID || thread.SessionExternalID != codeSession.SessionExternalID || thread.Status == "terminated" || thread.ArchivedAt != nil || thread.DeletedAt != nil {
						err = db.ErrNotFound
					}
					break
				}
				snapshot, loadErr := validatedMCPThreadSnapshot(codeSession, thread)
				if loadErr != nil {
					err = loadErr
					break
				}
				var scope webtools.Policy
				err = h.addWebToolScope(&scope, snapshot, name)
				scopes = append(scopes, scope.Tools...)
				threadID = *thread.ParentThreadExternalID
			}
			for index := len(scopes) - 1; index >= 0; index-- {
				policy.Tools = append(policy.Tools, scopes[index])
			}
		}
	}
	if err != nil {
		h.errorAdapter.Write(w, r, webToolContextFailure(err))
		return policy, false
	}
	return policy, true
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
