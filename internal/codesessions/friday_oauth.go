package codesessions

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (h *Handler) handleFridayOAuth(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, false)
	if !ok {
		return
	}
	if scope.ThreadID != "root" || h.oauthCredentials == nil || h.prepareEnvOAuth == nil {
		h.errorAdapter.Write(w, r, sandboxToolDenied())
		return
	}
	ctx := r.Context()
	code, found, err := h.db.GetScopedCodeSession(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionID)
	if err != nil || !found {
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
	input := OAuthRequest{Kind: "env_oauth", Provider: stringField(codeSessionConfig(code.Metadata), "friday_oauth_provider"), SecretName: "FRIDAY_4A_TOKEN", TargetHost: "friday-tx.seewo.com", ParentCallID: scope.CallID, InvocationID: "friday", Reason: "missing"}
	if input.Provider == "" || input.Validate() != nil {
		httpapi.WriteError(w, r, httpapi.NewError(400, "invalid_request_error", "环境缺少有效的 Friday OAuth 客户端引用"))
		return
	}
	provider, err := h.resolveSandboxOAuthProvider(ctx, input, nil)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(400, "invalid_request_error", err.Error()))
		return
	}
	input.ProviderName = provider.Name
	result, err := h.oauthCredentials(ctx, code, input, h.prepareEnvOAuth)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(409, "invalid_request_error", "Friday 授权未就绪"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}
