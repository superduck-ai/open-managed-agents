package codesessions

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type OAuthCredentialWaiter func(context.Context, db.CodeSession, OAuthRequest, func(context.Context, db.VaultCredential) error) (json.RawMessage, error)

func (h *Handler) WithOAuthCredentials(wait OAuthCredentialWaiter) *Handler {
	h.oauthCredentials = wait
	return h
}

func (h *Handler) handleSandboxOAuth(w http.ResponseWriter, r *http.Request) {
	claims, _, ok := h.authorizeRuntimeSession(w, r)
	if !ok {
		return
	}
	if claims.Role != "sandbox" || h.oauthCredentials == nil || h.prepareEnvOAuth == nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "OAuth requires a sandbox tool call"))
		return
	}
	input, err := httpapi.DecodeObjectBodyAs[OAuthRequest](w, r, 4096)
	if err == nil {
		err = input.Validate()
	}
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Invalid OAuth request"))
		return
	}
	parent, err := h.db.GetSandboxToolCall(r.Context(), db.SandboxToolCallScope{
		OrganizationUUID: claims.OrganizationUUID, WorkspaceUUID: claims.WorkspaceUUID,
		CodeSessionID: claims.SessionID, SandboxUUID: claims.SandboxUUID, ThreadID: "root", CallID: input.ParentCallID,
	})
	if err != nil || parent.SandboxUUID == nil || *parent.SandboxUUID != claims.SandboxUUID || parent.State != "running" || parent.ToolName != "bash" || parent.UUID != r.Header.Get("X-OMA-Tool-Call") || input.InvocationID == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "OAuth request requires its running Bash call"))
		return
	}
	static := false
	if input.Provider == "" {
		scope, err := h.db.GetCodeSessionCredentialScope(r.Context(), claims.SessionID, claims.OrganizationUUID, claims.WorkspaceUUID)
		if err != nil {
			h.errorAdapter.Write(w, r, toolCallOperationError(err))
			return
		}
		credentials, err := h.db.ListActiveVaultCredentialsForVaultIDs(r.Context(), claims.WorkspaceUUID, scope.VaultIDs)
		if err != nil {
			h.errorAdapter.Write(w, r, toolCallOperationError(err))
			return
		}
		for _, credential := range vaults.SelectCredentialBindings(scope, credentials) {
			if credential.CredentialKey != input.SecretName {
				continue
			}
			if credential.AuthType == "environment_variable" {
				static = true
				break
			}
			if credential.AuthType == "env_oauth" {
				input.Provider, err = vaults.EnvOAuthCredentialProvider(credential.Auth)
				if err != nil {
					h.errorAdapter.Write(w, r, toolCallOperationError(err))
					return
				}
				break
			}
		}
	}
	var provider vaults.EnvOAuthProviderSnapshot
	if !static {
		provider, err = h.resolveSandboxOAuthProvider(r.Context(), claims.WorkspaceUUID, *input)
	}
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	input.Provider = provider.ProviderKey
	input.ProviderName = provider.Name
	code, found, err := h.db.GetScopedCodeSession(r.Context(), claims.OrganizationUUID, claims.WorkspaceUUID, claims.SessionID)
	if err != nil || !found {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Session unavailable"))
		return
	}
	report := db.SandboxToolCallReport{Scope: db.SandboxToolCallScope{
		OrganizationUUID: claims.OrganizationUUID, WorkspaceUUID: claims.WorkspaceUUID,
		CodeSessionID: claims.SessionID, SandboxUUID: claims.SandboxUUID,
	}, CallUUID: parent.UUID, Attempt: parent.Attempt, Event: "oauth_wait"}
	if _, err := h.db.ReportSandboxToolCall(r.Context(), report); err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	result, err := h.oauthCredentials(r.Context(), code, *input, h.prepareEnvOAuth)
	resumeCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	report.Event = "oauth_resume"
	if _, resumeErr := h.db.ReportSandboxToolCall(resumeCtx, report); err == nil {
		err = resumeErr
	}
	if err != nil {
		message := "OAuth 未完成，当前操作未继续"
		if errors.Is(err, vaults.ErrCredentialAuthorizationManaged) {
			message = vaults.ErrCredentialAuthorizationManaged.Error()
		}
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "invalid_request_error", message))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) resolveSandboxOAuthProvider(ctx context.Context, workspaceUUID string, input OAuthRequest) (vaults.EnvOAuthProviderSnapshot, error) {
	var candidates []vaults.EnvOAuthProviderSnapshot
	if input.Provider != "" {
		provider, err := h.envOAuthProviders.ResolveEnvOAuthProvider(ctx, input.Provider)
		if err != nil {
			return vaults.EnvOAuthProviderSnapshot{}, fmt.Errorf("OAuth 客户端不可用")
		}
		candidates = []vaults.EnvOAuthProviderSnapshot{provider}
	} else {
		var err error
		candidates, err = h.envOAuthProviders.ListProviders(ctx)
		if err != nil {
			return vaults.EnvOAuthProviderSnapshot{}, fmt.Errorf("OAuth 客户端目录不可用")
		}
	}
	var selected *vaults.EnvOAuthProviderSnapshot
	for _, provider := range candidates {
		if !provider.Enabled || !provider.Configured || provider.DefaultSecretName != input.SecretName {
			continue
		}
		if input.TargetHost != "" {
			allowed, err := networkpolicy.AllowsHost(provider.DefaultAllowedHosts, input.TargetHost, "443")
			if err != nil || !allowed {
				continue
			}
		}
		if selected != nil {
			return vaults.EnvOAuthProviderSnapshot{}, fmt.Errorf("存在多个匹配的 OAuth 客户端，请明确指定连接")
		}
		selected = new(provider)
	}
	if selected == nil {
		return vaults.EnvOAuthProviderSnapshot{}, fmt.Errorf("没有匹配当前变量和目标服务的可用 OAuth 客户端")
	}
	return *selected, nil
}

func (s *Service) RequestSandboxOAuth(ctx context.Context, code db.CodeSession, input OAuthRequest) (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	data, err := jsonv2.Marshal(input)
	if err != nil {
		return "", err
	}
	var fields map[string]any
	if err := jsonv2.Unmarshal(data, &fields); err != nil {
		return "", err
	}
	invocation := fmt.Sprintf("%x", sha256.Sum256([]byte(input.InvocationID)))[:16]
	requestID := fmt.Sprintf("sandbox-oauth:%x", sha256.Sum256([]byte(code.ExternalID+"\x00"+input.ParentCallID+"\x00"+input.Provider+"\x00"+input.InvocationID)))
	payload := workerControlRequestPayload{RequestID: requestID, Request: workerPermissionRequest{
		Subtype: "request_oauth", ToolName: "RequestOAuth", ToolUseID: input.ParentCallID + ":oauth:" + input.Provider + ":" + invocation, Input: fields,
	}}
	if err := s.handleOAuthRequest(ctx, code.ExternalID, code.CurrentWorkerEpoch, &payload, EventMetadata{RequestID: &requestID}); err != nil {
		return "", err
	}
	request := toolPermissionRequestFromWorkerEvent(&payload, EventMetadata{RequestID: &requestID})
	request, _, err = toolCallPublicPayload(code.ExternalID, request, toolIdentity{Kind: "oauth"}, resolvedToolPermissionAllow, code.CreatedAt)
	return request.PublicEventID, err
}

func sandboxOAuthRequest(request toolPermissionRequest) bool {
	return request.ToolName == "RequestOAuth" && strings.HasPrefix(request.RequestID, "sandbox-oauth:")
}
