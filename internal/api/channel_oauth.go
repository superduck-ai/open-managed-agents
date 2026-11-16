package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/channels"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func (s *Server) startChannelMCPOAuth(ctx context.Context, target channels.OAuthTarget) (string, error) {
	ctx, err := s.channelContext(ctx, target.Scope.APIKey)
	if err != nil {
		return "", err
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("渠道运行身份不可用")
	}
	result, _, err := s.createMCPVaultOAuth(ctx, principal, target.Vault, platformMCPVaultAuthStartRequest{
		MCPServerURL: target.Request.MCPServerURL, DisplayName: target.Request.MCPServerName, Source: "channel",
	}, target.Request.MCPServerURL, s.cfg.Auth.ConsoleURL+"/oauth/vault/success", target.Continuation)
	return result.RedirectURL, err
}

func channelOAuthBrowserHash(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func validChannelOAuthBrowser(r *http.Request, continuation *db.OAuthContinuation) bool {
	if continuation == nil {
		return true
	}
	cookie, err := r.Cookie("oma_channel_oauth")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(channelOAuthBrowserHash(cookie.Value)), []byte(continuation.BrowserHash)) == 1
}

func (s *Server) handleChannelOAuthStart(w http.ResponseWriter, r *http.Request) {
	browser := rand.Text() + rand.Text()
	if cookie, err := r.Cookie("oma_channel_oauth"); err == nil && len(cookie.Value) >= 32 {
		browser = cookie.Value
	}
	link, err := s.channelService.BeginOAuth(r.Context(), chi.URLParam(r, "channel_id"), chi.URLParam(r, "session_id"), chi.URLParam(r, "request_id"), channelOAuthBrowserHash(browser))
	if err != nil {
		http.Error(w, "授权请求不可用，请回到原对话重新发起。", http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.SetCookie(w, &http.Cookie{Name: "oma_channel_oauth", Value: browser, Path: "/oauth", Secure: r.TLS != nil || s.cfg.Env != "dev", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 900})
	http.Redirect(w, r, link, http.StatusSeeOther)
}

func (s *Server) resolveEnvOAuthFlowProvider(ctx context.Context, flow db.EnvOAuthFlow) (vaults.EnvOAuthProviderSnapshot, error) {
	if flow.Continuation != nil && flow.Continuation.Identity {
		return s.channelService.OAuthIdentityProvider(ctx, flow)
	}
	return s.envOAuthProviders.ResolveEnvOAuthProvider(ctx, flow.ProviderKey)
}

func (s *Server) completeChannelOAuthIdentity(w http.ResponseWriter, r *http.Request, flow db.EnvOAuthFlow, token string) {
	link, err := s.channelService.CompleteOAuthIdentity(r.Context(), flow, token)
	if err == nil {
		err = s.db.CompleteEnvOAuthIdentity(r.Context(), flow.ExternalID, time.Now().UTC())
	}
	if err != nil {
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorInvalidRequest, time.Now().UTC())
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{Type: "vault_oauth_complete", Status: "error", ErrorCode: envOAuthErrorInvalidRequest})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, link, http.StatusSeeOther)
}

func (s *Server) resumeChannelOAuth(ctx context.Context, workspaceUUID string, continuation *db.OAuthContinuation, credentialID string) error {
	if continuation == nil {
		return nil
	}
	target, result, err := s.channelService.OAuthCompletion(ctx, workspaceUUID, continuation, credentialID)
	if err != nil {
		return err
	}
	ctx, err = s.channelContext(ctx, target.Scope.APIKey)
	if err != nil {
		return err
	}
	return s.sessions.ResolveOAuthResult(ctx, target.Scope.Session.ExternalID, target.Event.ExternalID, result)
}

func (s *Server) completeChannelEnvOAuth(w http.ResponseWriter, r *http.Request, flow db.EnvOAuthFlow, credentialID string) {
	payload := envOAuthCallbackPayload{
		Type: "vault_oauth_complete", Status: "ok", FlowID: flow.ExternalID,
		VaultID: flow.VaultExternalID, CredentialID: credentialID,
	}
	if err := s.resumeChannelOAuth(r.Context(), flow.WorkspaceUUID, flow.Continuation, credentialID); err != nil {
		s.logger.WarnContext(r.Context(), "resume channel oauth", "flow_uuid", flow.UUID, "error", err)
		payload.Status, payload.ErrorCode = "error", envOAuthErrorInvalidRequest
	}
	writeEnvOAuthCallback(w, payload)
}

func (s *Server) completeChannelMCPOAuth(w http.ResponseWriter, r *http.Request, flow db.MCPOAuthFlow, credentialID string) {
	payload := platformMCPVaultAuthCallbackPayload{
		Type: "vault_oauth_complete", FlowID: flow.ExternalID,
		VaultID: flow.VaultExternalID, CredentialID: credentialID,
	}
	if err := s.resumeChannelOAuth(r.Context(), flow.WorkspaceUUID, flow.Continuation, credentialID); err != nil {
		s.logger.WarnContext(r.Context(), "resume channel mcp oauth", "flow_uuid", flow.UUID, "error", err)
		payload.ErrorCode = platformMCPVaultAuthVerificationRequestFailed
	}
	writePlatformMCPVaultAuthCallback(w, payload)
}
