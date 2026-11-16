package channels

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/mcpnames"
	"github.com/superduck-ai/open-managed-agents/internal/oauthflows"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type OAuthTarget struct {
	Scope        OAuthContext
	Request      codesessions.OAuthRequest
	Event        db.SessionEvent
	Vault        db.Vault
	Continuation *db.OAuthContinuation
}

func (s *Service) WithOAuthProviders(providers vaults.EnvOAuthProviderResolver, startMCP func(context.Context, OAuthTarget) (string, error)) *Service {
	s.oauthProviders, s.startMCPOAuth = providers, startMCP
	return s
}

func (s *Service) oauthTarget(ctx context.Context, scope OAuthContext, event db.SessionEvent, continuation *db.OAuthContinuation) (OAuthTarget, error) {
	var payload struct {
		Name  string                    `json:"name"`
		Input codesessions.OAuthRequest `json:"input"`
	}
	if event.EventType != "agent.custom_tool_use" || jsonv2.Unmarshal(event.Payload, &payload) != nil || payload.Name != "RequestOAuth" {
		return OAuthTarget{}, errors.New("OAuth 请求不存在")
	}
	if err := payload.Input.Validate(); err != nil {
		return OAuthTarget{}, err
	}
	if payload.Input.Kind == "mcp_oauth" {
		type configuredServer struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		var snapshot struct {
			Servers []configuredServer `json:"mcp_servers"`
		}
		if err := jsonv2.Unmarshal(scope.Session.AgentSnapshot, &snapshot); err != nil {
			return OAuthTarget{}, err
		}
		if !slices.ContainsFunc(snapshot.Servers, func(server configuredServer) bool {
			return mcpnames.ServerKey(server.Name) == payload.Input.MCPServerName && server.URL == payload.Input.MCPServerURL
		}) {
			return OAuthTarget{}, errors.New("目标 MCP 未挂载到原会话")
		}
	}
	id := personalVaultID(scope.Channel, scope.ActorID)
	if !slices.Contains(scope.Session.VaultIDs, id) {
		return OAuthTarget{}, errors.New("当前会话未挂载个人连接保险库，请新建私聊会话")
	}
	vault, err := s.store.GetVault(ctx, scope.Session.WorkspaceUUID, id)
	if err != nil {
		return OAuthTarget{}, err
	}
	if vault.ArchivedAt != nil || vault.DeletedAt != nil {
		return OAuthTarget{}, db.ErrNotFound
	}
	return OAuthTarget{Scope: scope, Request: payload.Input, Event: event, Vault: vault, Continuation: continuation}, nil
}

func (s *Service) BeginOAuth(ctx context.Context, channelID, sessionID, eventID, browserHash string) (string, error) {
	scope, err := s.OAuthContext(ctx, channelID, sessionID)
	if err != nil {
		return "", err
	}
	event, err := s.store.GetSessionEvent(ctx, scope.Session.WorkspaceUUID, sessionID, eventID)
	if err != nil {
		return "", err
	}
	continuation := &db.OAuthContinuation{SessionUUID: scope.Session.UUID, EventUUID: event.UUID, ChannelUUID: scope.Channel.UUID, BrowserHash: browserHash, Identity: true}
	target, err := s.oauthTarget(ctx, scope, event, continuation)
	if err != nil {
		return "", err
	}
	if err := s.requirePendingOAuth(ctx, target); err != nil {
		return "", err
	}
	provider := channelIdentityProvider(scope)
	_, link, err := oauthflows.CreateEnv(ctx, s.store, s.supervisor.cipher, provider, db.EnvOAuthFlow{
		OrganizationUUID: scope.Channel.OrganizationUUID, WorkspaceUUID: scope.Channel.WorkspaceUUID,
		VaultUUID: target.Vault.UUID, VaultExternalID: target.Vault.ExternalID,
		SecretName: "OMA_CHANNEL_IDENTITY", RedirectURL: s.supervisor.oauthOrigin + "/oauth/vault/env-success",
		NetworkingType: "limited", AllowedHosts: []string{"open.feishu.cn"}, Continuation: continuation,
		CreatedByAPIKeyUUID: scope.Session.CreatedByAPIKeyUUID,
	})
	return link, err
}

func channelIdentityProvider(scope OAuthContext) vaults.EnvOAuthProviderSnapshot {
	return vaults.EnvOAuthProviderSnapshot{
		Builtin: true, Enabled: true, Configured: true, ProviderKey: "builtin:feishu", Name: "渠道操作者核验",
		Version: 1, ClientID: scope.Channel.AppID, ClientSecret: scope.Secret,
		AuthorizeEndpoint:       "https://accounts.feishu.cn/open-apis/authen/v1/authorize",
		TokenEndpoint:           "https://open.feishu.cn/open-apis/authen/v2/oauth/token",
		TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "json", Scope: "contact:user.employee_id:readonly",
	}
}

func (s *Service) OAuthContinuationTarget(ctx context.Context, workspaceUUID string, continuation *db.OAuthContinuation) (OAuthTarget, error) {
	if continuation == nil {
		return OAuthTarget{}, db.ErrNotFound
	}
	session, found, err := s.store.GetSessionByUUID(ctx, workspaceUUID, continuation.SessionUUID)
	if err != nil || !found {
		return OAuthTarget{}, errors.Join(db.ErrNotFound, err)
	}
	var metadata map[string]string
	if err := jsonv2.Unmarshal(session.Metadata, &metadata); err != nil {
		return OAuthTarget{}, err
	}
	scope, err := s.OAuthContext(ctx, metadata["channel_id"], session.ExternalID)
	if err != nil {
		return OAuthTarget{}, err
	}
	if scope.Channel.UUID != continuation.ChannelUUID {
		return OAuthTarget{}, db.ErrNotFound
	}
	event, err := s.store.GetSessionEventByUUID(ctx, workspaceUUID, session.UUID, continuation.EventUUID)
	if err != nil {
		return OAuthTarget{}, err
	}
	return s.oauthTarget(ctx, scope, event, continuation)
}

func (s *Service) OAuthIdentityProvider(ctx context.Context, flow db.EnvOAuthFlow) (vaults.EnvOAuthProviderSnapshot, error) {
	target, err := s.OAuthContinuationTarget(ctx, flow.WorkspaceUUID, flow.Continuation)
	if err != nil {
		return vaults.EnvOAuthProviderSnapshot{}, err
	}
	return channelIdentityProvider(target.Scope), nil
}

func (s *Service) CompleteOAuthIdentity(ctx context.Context, flow db.EnvOAuthFlow, token string) (string, error) {
	target, err := s.OAuthContinuationTarget(ctx, flow.WorkspaceUUID, flow.Continuation)
	if err != nil {
		return "", err
	}
	if err := s.requirePendingOAuth(ctx, target); err != nil {
		return "", err
	}
	client := lark.NewClient(target.Scope.Channel.AppID, target.Scope.Secret, lark.WithLogLevel(larkcore.LogLevelError))
	info, err := client.Authen.V1.UserInfo.Get(ctx, larkcore.WithUserAccessToken(token))
	if err != nil || !info.Success() || info.Data == nil || info.Data.OpenId == nil || *info.Data.OpenId != target.Scope.ActorID {
		return "", errors.New("授权用户与原消息发送者不一致")
	}
	next := *flow.Continuation
	next.Identity = false
	target.Continuation = &next
	if target.Request.Kind == "mcp_oauth" {
		if s.startMCPOAuth == nil {
			return "", errors.New("MCP OAuth 不可用")
		}
		return s.startMCPOAuth(ctx, target)
	}
	provider, err := s.oauthProviders.ResolveEnvOAuthProvider(ctx, target.Request.Provider)
	if err != nil {
		return "", err
	}
	if !provider.Enabled || !provider.Configured || provider.WorkspaceUUID != "" && provider.WorkspaceUUID != flow.WorkspaceUUID || provider.DefaultSecretName != target.Request.SecretName || len(provider.DefaultAllowedHosts) == 0 {
		return "", errors.New("目标服务的个人 OAuth 配置不可用")
	}
	credentials, _, err := s.store.ListVaultCredentialsPage(ctx, db.ListVaultCredentialsPageParams{WorkspaceUUID: flow.WorkspaceUUID, VaultExternalID: target.Vault.ExternalID, Limit: 50})
	if err != nil {
		return "", err
	}
	credentialID := ""
	for _, credential := range credentials {
		if credential.AuthType != "env_oauth" || credential.CredentialKey != target.Request.SecretName {
			continue
		}
		key, err := vaults.EnvOAuthCredentialProvider(credential.Auth)
		if err != nil || key != target.Request.Provider {
			return "", errors.New("目标变量已有其他服务的连接")
		}
		credentialID = credential.ExternalID
	}
	_, link, err := oauthflows.CreateEnv(ctx, s.store, s.supervisor.cipher, provider, db.EnvOAuthFlow{
		OrganizationUUID: flow.OrganizationUUID, WorkspaceUUID: flow.WorkspaceUUID,
		VaultUUID: target.Vault.UUID, VaultExternalID: target.Vault.ExternalID, CredentialExternalID: credentialID,
		SecretName: target.Request.SecretName, RedirectURL: s.supervisor.oauthOrigin + "/oauth/vault/env-success",
		NetworkingType: "limited", AllowedHosts: provider.DefaultAllowedHosts, Continuation: target.Continuation,
		CreatedByAPIKeyUUID: target.Scope.Session.CreatedByAPIKeyUUID,
	})
	return link, err
}

func (s *Service) OAuthCompletion(ctx context.Context, workspaceUUID string, continuation *db.OAuthContinuation, credentialID string) (OAuthTarget, json.RawMessage, error) {
	target, err := s.OAuthContinuationTarget(ctx, workspaceUUID, continuation)
	if err != nil {
		return OAuthTarget{}, nil, err
	}
	credential, err := s.store.GetVaultCredential(ctx, workspaceUUID, target.Vault.ExternalID, credentialID)
	if err != nil {
		return OAuthTarget{}, nil, err
	}
	if credential.AuthType != target.Request.Kind || credential.ArchivedAt != nil {
		return OAuthTarget{}, nil, db.ErrInvalidState
	}
	if target.Request.Kind == "env_oauth" {
		provider, err := vaults.EnvOAuthCredentialProvider(credential.Auth)
		if err != nil || provider != target.Request.Provider || credential.CredentialKey != target.Request.SecretName {
			return OAuthTarget{}, nil, db.ErrInvalidState
		}
	} else if credential.CredentialKey != target.Request.MCPServerURL {
		return OAuthTarget{}, nil, db.ErrInvalidState
	}
	result, err := jsonv2.Marshal(struct {
		Authorized bool `json:"authorized"`
	}{true})
	return target, result, err
}

func (s *Service) requirePendingOAuth(ctx context.Context, target OAuthTarget) error {
	code, err := s.store.GetCodeSessionBySessionExternalID(ctx, target.Scope.Session.WorkspaceUUID, target.Scope.Session.ExternalID)
	if err != nil {
		return err
	}
	pending, err := managedagentsevents.PendingToolEventIDs(code.WorkerExternalMetadata, "", "")
	if err != nil || !slices.Contains(pending, target.Event.ExternalID) {
		return errors.Join(db.ErrNotFound, err)
	}
	accepted, err := s.store.HasAcceptedSessionToolControl(ctx, target.Scope.Session.WorkspaceUUID, target.Scope.Session.ExternalID, target.Event.ExternalID)
	if err != nil {
		return err
	}
	if accepted {
		return db.ErrInvalidState
	}
	return nil
}
