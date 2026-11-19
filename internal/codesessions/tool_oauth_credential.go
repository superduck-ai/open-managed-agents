package codesessions

import (
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/clitools"
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
	resume := r.URL.Query().Get("resume") == "1"
	if input.Kind == "env_oauth" {
		if scope.ThreadID != "root" {
			h.errorAdapter.Write(w, r, sandboxToolDenied())
			return
		}
		policies, err := h.service.currentRuntimeToolPolicies(ctx, code, "")
		if err != nil {
			h.errorAdapter.Write(w, r, toolCallOperationError(err))
			return
		}
		if p, ok := policies["bash"]; policies != nil && (!ok || p == string(resolvedToolPermissionDeny)) {
			h.errorAdapter.Write(w, r, sandboxToolDenied())
			return
		}
		digest, err := hex.DecodeString(input.InvocationID)
		if err != nil || len(digest) != 32 {
			httpapi.WriteError(w, r, httpapi.NewError(400, "invalid_request_error", "调用参数摘要无效"))
			return
		}
		var snapshot struct {
			Config clitools.OAuthConfig `json:"config"`
		}
		if err := json.Unmarshal(code.Metadata, &snapshot); err != nil {
			h.errorAdapter.Write(w, r, toolCallOperationError(err))
			return
		}
		providerKey := snapshot.Config.ProviderFor(h.service.cliTools, input.SecretName, input.TargetHost)
		if providerKey == "" || (input.Provider != "" && input.Provider != providerKey) {
			h.errorAdapter.Write(w, r, sandboxToolDenied())
			return
		}
		input.Provider = providerKey
		provider, err := h.resolveSandboxOAuthProvider(ctx, *input, nil)
		if err != nil {
			httpapi.WriteError(w, r, httpapi.NewError(400, "invalid_request_error", err.Error()))
			return
		}
		input.ProviderName = provider.Name
	} else if !resume {
		h.errorAdapter.Write(w, r, sandboxToolDenied())
		return
	}
	if resume {
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
	} else {
		input.CheckOnly = true
	}

	result, err := h.oauthCredentials(ctx, code, *input, h.prepareEnvOAuth)
	if err != nil {
		h.errorAdapter.Write(w, r, toolOAuthCredentialError(err))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) toolOAuthResumeEvent(ctx context.Context, code db.CodeSession, input OAuthRequest, threadID string) (string, error) {
	eventID := maevents.ToolUseEventID(code.ExternalID, input.ParentCallID+":oauth:"+cmp.Or(input.Provider, input.MCPServerName), threadID)
	event, err := h.db.GetSessionEvent(ctx, code.WorkspaceUUID, code.SessionExternalID, eventID)
	if errors.Is(err, db.ErrNotFound) && input.Kind == "env_oauth" {
		eventID = maevents.ToolUseEventID(code.ExternalID, input.toolUseID(), "")
		event, err = h.db.GetSessionEvent(ctx, code.WorkspaceUUID, code.SessionExternalID, eventID)
	}
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
	if input.Kind == "env_oauth" {
		input.Reason = original.Input.Reason
	}
	input.ProviderName = original.Input.ProviderName
	if input != original.Input {
		return "", sandboxToolDenied()
	}
	return eventID, nil
}
