package sessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func (h *Handler) ResolveOAuthResult(ctx context.Context, sessionID, requestID string, result json.RawMessage) error {
	session, err := h.authorizeSessionContext(ctx, sessionID, sessionAccessEventsSend)
	if err != nil {
		return err
	}
	request, err := h.db.GetSessionEvent(ctx, session.WorkspaceUUID, sessionID, requestID)
	if err != nil {
		return err
	}
	var source struct {
		Name     string `json:"name"`
		ThreadID string `json:"session_thread_id"`
	}
	if jsonv2.Unmarshal(request.Payload, &source) != nil || request.EventType != "agent.custom_tool_use" || source.Name != "RequestOAuth" {
		return db.ErrNotFound
	}
	if source.ThreadID == "" {
		thread, err := h.ensurePrimarySessionThread(ctx, session)
		if err != nil {
			return err
		}
		source.ThreadID = thread.ExternalID
	}
	input, err := jsonv2.Marshal(map[string]any{
		"type": "user.custom_tool_result", "custom_tool_use_id": requestID, "session_thread_id": source.ThreadID,
		"content": []map[string]string{{"type": "text", "text": string(result)}},
	})
	if err != nil {
		return err
	}
	event, _, _, err := normalizeInputEvent(session, input, time.Now().UTC())
	if err != nil {
		return err
	}
	batch, err := jsonv2.Marshal([]json.RawMessage{input})
	if err != nil {
		return err
	}
	idempotency, err := sessionInputRequest("oauth-result:"+request.UUID, batch)
	if err != nil {
		return err
	}
	changes, err := h.eventPayloads.AppendSessionInputRequest(ctx, session.WorkspaceUUID, sessionID, []db.SessionEvent{event}, nil, *idempotency)
	if err != nil {
		return err
	}
	if !changes.Replayed {
		h.publishSessionEvents(ctx, changes.Events)
		h.enqueueWebhooksForSessionEvents(ctx, session.WorkspaceUUID, sessionID, changes.Events)
	}
	if err := queueSessionInputChanges(ctx, session, changes, h.codeSessions.QueuePublicSessionEvents); err != nil {
		return err
	}
	return h.db.CompleteSessionInputRequestHandoff(ctx, session, *idempotency)
}

func (h *Handler) WaitForOAuthCredential(ctx context.Context, code db.CodeSession, input codesessions.OAuthRequest, prepare func(context.Context, db.VaultCredential) error) (json.RawMessage, error) {
	if input.Kind != "env_oauth" && input.ResumeEventID == "" {
		return nil, errors.New("CLI 入口只支持 ENV OAuth")
	}
	if input.ResumeEventID == "" && input.Reason == "missing" {
		if result, err := h.oauthEnvironmentResult(ctx, code, input, prepare, false); errors.Is(err, vaults.ErrEnvOAuthReauthorizationRequired) {
			input.Reason = "reauthorization_required"
		} else if err != nil || result != nil {
			return result, err
		}
	}
	if input.CheckOnly {
		return jsonv2.Marshal(map[string]any{"requirement": input})
	}
	session, found, err := h.db.GetSession(ctx, code.WorkspaceUUID, code.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	thread, err := h.ensurePrimarySessionThread(ctx, session)
	if err != nil {
		return nil, err
	}
	subscription, err := h.subscribeSession(ctx, session, newStreamConnection(thread.ExternalID, true, nil), StreamOptions{AfterEventID: input.ResumeEventID})
	if err != nil {
		return nil, err
	}
	defer subscription.Close()
	requestID := input.ResumeEventID
	if requestID == "" {
		requestID, err = h.codeSessions.RequestSandboxOAuth(ctx, code, input)
		if err != nil {
			return nil, err
		}
	}
	requestSeen := input.ResumeEventID != ""
	for {
		event, err := subscription.Receive(ctx)
		if err != nil {
			return nil, err
		}
		if event.ExternalID == requestID {
			requestSeen = true
		}
		if !requestSeen {
			continue
		}
		if event.EventType == "user.interrupt" || event.EventType == "session.status_terminated" {
			return nil, apperr.New(apperr.Conflict, "OAuth 等待已取消", nil)
		}
		if event.EventType != "user.custom_tool_result" {
			continue
		}
		var result struct {
			RequestID string `json:"custom_tool_use_id"`
			IsError   bool   `json:"is_error"`
		}
		if err := jsonv2.Unmarshal(event.Payload, &result); err != nil {
			return nil, err
		}
		if result.RequestID != requestID {
			continue
		}
		if result.IsError {
			return nil, apperr.New(apperr.Conflict, "OAuth 授权未完成", nil)
		}
		var grant struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		var decision struct {
			Authorized bool `json:"authorized"`
		}
		if jsonv2.Unmarshal(event.Payload, &grant) != nil || len(grant.Content) != 1 || jsonv2.Unmarshal([]byte(grant.Content[0].Text), &decision) != nil || !decision.Authorized {
			return nil, apperr.New(apperr.Conflict, "OAuth 授权未确认", nil)
		}
		if input.Kind == "mcp_oauth" {
			return jsonv2.Marshal(decision)
		}
		resultJSON, err := h.oauthEnvironmentResult(ctx, code, input, prepare, true)
		if err == nil && resultJSON == nil {
			err = apperr.New(apperr.Conflict, "OAuth 凭据未就绪", nil)
		}
		return resultJSON, err
	}
}

func (h *Handler) oauthEnvironmentResult(ctx context.Context, code db.CodeSession, input codesessions.OAuthRequest, prepare func(context.Context, db.VaultCredential) error, authorizationCompleted bool) (json.RawMessage, error) {
	scope, err := h.db.GetCodeSessionCredentialScope(ctx, code.ExternalID, code.OrganizationUUID, code.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	credentials, err := h.db.ListActiveVaultCredentialsForVaultIDs(ctx, code.WorkspaceUUID, scope.VaultIDs)
	if err != nil {
		return nil, err
	}
	for _, credential := range vaults.SelectCredentialBindings(scope, credentials) {
		if credential.AuthType != "env_oauth" && credential.AuthType != "environment_variable" || credential.CredentialKey != input.SecretName {
			continue
		}
		if credential.AuthType == "environment_variable" && input.Provider != "" {
			if scope.PinBindings {
				return nil, apperr.New(apperr.Conflict, "当前变量已绑定普通环境凭据", nil)
			}
			continue
		}
		if credential.AuthType == "env_oauth" {
			provider, err := vaults.EnvOAuthCredentialProvider(credential.Auth)
			if err != nil {
				return nil, err
			}
			if provider != input.Provider {
				if scope.PinBindings {
					return nil, apperr.New(apperr.Conflict, "当前变量已绑定其他 OAuth 连接", nil)
				}
				continue
			}
			if err := prepare(ctx, credential); err != nil {
				if scope.PinBindings && credential.VaultExternalID != scope.OAuthVaultID && errors.Is(err, vaults.ErrEnvOAuthReauthorizationRequired) {
					return nil, vaults.ErrCredentialAuthorizationManaged
				}
				return nil, err
			}
		}
		placeholder, err := vaults.EnvironmentCredentialPlaceholder(credential)
		if err != nil {
			return nil, err
		}
		return jsonv2.Marshal(struct {
			Authorized             bool   `json:"authorized"`
			AuthorizationCompleted bool   `json:"authorization_completed"`
			SecretName             string `json:"secret_name"`
			Placeholder            string `json:"placeholder"`
		}{true, authorizationCompleted, input.SecretName, placeholder})
	}
	return nil, nil
}
