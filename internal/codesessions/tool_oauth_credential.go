package codesessions

import (
	"context"
	"encoding/json/v2"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (h *Handler) handleToolOAuthCredential(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, false)
	if !ok {
		return
	}
	if scope.ThreadID == "" || h.oauthCredentials == nil || h.prepareEnvOAuth == nil {
		h.errorAdapter.Write(w, r, sandboxToolDenied())
		return
	}
	ctx := r.Context()
	code, found, err := h.db.GetScopedCodeSession(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionID)
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	if !found {
		h.errorAdapter.Write(w, r, sandboxToolDenied())
		return
	}
	input, err := httpapi.DecodeObjectBodyAs[OAuthRequest](w, r, 4096)
	if err != nil || input.ParentCallID != scope.CallID || input.Validate() != nil {
		httpapi.WriteError(w, r, httpapi.NewError(400, "invalid_request_error", "工具授权引用无效"))
		return
	}
	if input.Kind != "mcp_oauth" || r.URL.Query().Get("resume") != "1" {
		h.errorAdapter.Write(w, r, sandboxToolDenied())
		return
	}
	threadID := ""
	if scope.ThreadID != "root" {
		threadID = scope.ThreadID
		if _, err := h.loadMCPThreadSnapshot(ctx, code, threadID); err != nil {
			h.errorAdapter.Write(w, r, toolCallOperationError(err))
			return
		}
	}
	input.ResumeEventID, err = h.toolOAuthResumeEvent(ctx, code, *input, threadID)
	input.ResumeThreadID = threadID
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}

	result, err := h.oauthCredentials(ctx, code, *input, h.prepareEnvOAuth)
	if err != nil {
		h.errorAdapter.Write(w, r, toolOAuthCredentialError(err))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) toolOAuthResumeEvent(ctx context.Context, code db.CodeSession, input OAuthRequest, threadID string) (string, error) {
	eventID := maevents.ToolUseEventID(code.ExternalID, input.ParentCallID+":oauth:"+input.MCPServerName, threadID)
	event, err := h.db.GetSessionEvent(ctx, code.WorkspaceUUID, code.SessionExternalID, eventID)
	if err != nil {
		return "", toolCallOperationError(err)
	}
	var original struct {
		Name  string       `json:"name"`
		Input OAuthRequest `json:"input"`
	}
	if json.Unmarshal(event.Payload, &original) != nil || event.EventType != "agent.custom_tool_use" || original.Name != "RequestOAuth" {
		return "", sandboxToolDenied()
	}
	input.ProviderName = original.Input.ProviderName
	if input != original.Input {
		return "", sandboxToolDenied()
	}
	return eventID, nil
}
