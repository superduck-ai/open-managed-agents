package codesessions

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
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
	provider, err := h.envOAuthProviders.ResolveEnvOAuthProvider(r.Context(), input.Provider)
	if err != nil || !provider.Enabled || !provider.Configured || provider.WorkspaceUUID != "" && provider.WorkspaceUUID != claims.WorkspaceUUID || provider.DefaultSecretName != input.SecretName {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "OAuth 服务未配置到当前工作区"))
		return
	}
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
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "invalid_request_error", "OAuth 未完成，当前操作未继续"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
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
