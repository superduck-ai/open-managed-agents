package channels

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type pairingOAuthReceipt struct {
	PairingID string `json:"pairing_id"`
	Provider  string `json:"oauth_provider"`
	Card      string `json:"oauth_card"`
}

func (s *Service) promptPairingOAuth(ctx context.Context, w *channelWorker, msg *channelMessage) (bool, error) {
	if !w.active(ctx) {
		return true, db.ErrInvalidState
	}
	allowed, err := w.authorize(ctx, msg)
	if err != nil || !allowed {
		return true, err
	}
	id, err := s.supervisor.ingress.PrepareSelfPairing(ctx, channelIngress(w.record, msg, msg.Input.ScopeKey))
	if err != nil || id == "" {
		return false, err
	}
	provider := parseChannelMounts(w.record.Mounts).PairingProvider
	name, err := s.supervisor.pairingProvider(ctx, provider)
	if err != nil {
		return true, err
	}
	data, err := json.Marshal(map[string]string{"provider_name": name})
	if err != nil {
		return true, err
	}
	action := contracts.Interaction{Kind: "oauth", Name: "RequestOAuth", Input: string(data), Value: channelActionValue{Kind: "pairing_oauth", ChatType: "p2p", ChannelID: w.record.ExternalID, SourceID: msg.Input.UUID}}
	stream, err := w.adapter.OpenReply(ctx, msg, contracts.Reply{DeliveryKey: msg.Input.UUID, Title: "连接 " + name, Status: "等待授权", OAuthOrigin: s.supervisor.oauthOrigin, Interactions: []contracts.Interaction{action}})
	if err != nil {
		return true, err
	}
	patch, err := json.Marshal(pairingOAuthReceipt{id, provider, stream.Receipt()})
	if err != nil {
		return true, err
	}
	if err = s.store.HoldPairingOAuth(ctx, w.inputScope(), msg.Input.UUID, w.deliveryOwner, string(patch)); err != nil {
		return true, err
	}
	return true, stream.Close(ctx)
}

func (s *Service) pairingOAuthTarget(ctx context.Context, c *db.OAuthContinuation, completed bool) (OAuthTarget, *channelWorker, db.ChannelInput, error) {
	s.supervisor.mu.Lock()
	w := s.supervisor.workers[c.ChannelID]
	s.supervisor.mu.Unlock()
	var target OAuthTarget
	var row db.ChannelInput
	if w == nil || !w.active(ctx) || w.record.UUID != c.ChannelUUID {
		return target, w, row, db.ErrNotFound
	}
	row, err := s.store.PairingOAuthInput(ctx, w.inputScope(), c.InputUUID)
	if err != nil {
		return target, w, row, err
	}
	if row.State != "pairing_wait" && !completed {
		return target, w, row, db.ErrInvalidState
	}
	var msg channelMessage
	var receipt pairingOAuthReceipt
	if json.Unmarshal([]byte(row.Payload), &msg) != nil || json.Unmarshal([]byte(row.Payload), &receipt) != nil || msg.ChatType != "p2p" || msg.UserID == "" || receipt.PairingID == "" || receipt.Provider == "" {
		return target, w, row, db.ErrInvalidState
	}
	current, err := s.store.GetChannelRecordByID(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, c.ChannelID)
	if err != nil {
		return target, w, row, err
	}
	if !current.Enabled || current.EntryClosed || current.AppID != w.record.AppID || current.ExecutionMode == nil || *current.ExecutionMode != "pairing" || current.Mode != "pairing" || parseChannelMounts(current.Mounts).PairingProvider != receipt.Provider {
		return target, w, row, db.ErrInvalidState
	}
	if err := s.validateOAuthPairing(ctx, current, receipt.PairingID, msg.UserID, completed); err != nil {
		return target, w, row, err
	}
	allowed, err := w.authorize(ctx, &msg)
	if err != nil {
		return target, w, row, err
	}
	if !allowed {
		return target, w, row, db.ErrInvalidState
	}
	v, err := s.ensurePersonalVault(ctx, current, msg.UserID)
	if err != nil {
		return target, w, row, err
	}
	vault, err := s.store.GetVault(ctx, current.WorkspaceUUID, v)
	if err != nil {
		return target, w, row, err
	}
	p, err := s.oauthProviders.ResolveEnvOAuthProvider(ctx, receipt.Provider)
	if err != nil {
		return target, w, row, err
	}
	if !envOAuthProviderReady(p, p.DefaultSecretName) {
		return target, w, row, db.ErrInvalidState
	}
	target = OAuthTarget{Scope: OAuthContext{Channel: current, ActorID: msg.UserID, ChatID: msg.ChatID, adapter: w.adapter}, Vault: vault, Request: codesessions.OAuthRequest{Kind: "env_oauth", Provider: receipt.Provider, ProviderName: p.Name, SecretName: p.DefaultSecretName}, Continuation: c}
	return target, w, row, nil
}

func (s *Service) validateOAuthPairing(ctx context.Context, current db.ChannelRecord, id, actor string, completed bool) error {
	pairing, err := s.store.FindScopePairing(ctx, current.WorkspaceUUID, id)
	if err != nil {
		return err
	}
	if pairing.ChannelUUID != current.UUID || pairing.PlatformAccount != current.AppID || pairing.ScopeKind != "direct" || pairing.RemoteID != actor || (pairing.Status != "pending" && !(completed && pairing.Status == "active")) {
		return db.ErrInvalidState
	}
	return nil
}

func (s *Service) BeginPairingOAuth(ctx context.Context, channelID, inputID, browser string) (string, error) {
	s.supervisor.mu.Lock()
	w := s.supervisor.workers[channelID]
	s.supervisor.mu.Unlock()
	if w == nil {
		return "", db.ErrNotFound
	}
	c := &db.OAuthContinuation{ChannelID: channelID, ChannelUUID: w.record.UUID, InputUUID: inputID, BrowserHash: browser, Identity: true}
	target, _, _, err := s.pairingOAuthTarget(ctx, c, false)
	if err != nil {
		return "", err
	}
	return s.beginActorCheck(ctx, target)
}

func (s *Service) CompletePairingOAuth(ctx context.Context, workspace string, c *db.OAuthContinuation, credentialID string) error {
	t, w, row, err := s.pairingOAuthTarget(ctx, c, true)
	if err != nil {
		return err
	}
	if workspace != t.Scope.Channel.WorkspaceUUID {
		return db.ErrNotFound
	}
	cred, err := s.store.GetVaultCredential(ctx, workspace, t.Vault.ExternalID, credentialID)
	if err != nil {
		return err
	}
	provider, err := vaults.EnvOAuthCredentialProvider(cred.Auth)
	if err != nil || provider != t.Request.Provider || cred.CredentialKey != t.Request.SecretName || cred.ArchivedAt != nil {
		return db.ErrInvalidState
	}
	var receipt pairingOAuthReceipt
	if err = json.Unmarshal([]byte(row.Payload), &receipt); err != nil {
		return err
	}
	if row.State != "pairing_wait" {
		return nil
	}
	msg := &channelMessage{UserID: t.Scope.ActorID, ChatID: t.Scope.ChatID, ChatType: "p2p"}
	var version *int
	if v := parseChannelMounts(t.Scope.Channel.Mounts).AgentVersion; v > 0 {
		version = &v
	}
	if err = s.supervisor.ingress.ConfirmSelfPairing(ctx, channelIngress(t.Scope.Channel, msg, row.ScopeKey), receipt.PairingID, version); err != nil {
		return err
	}
	waiters, err := s.store.ResumeOAuthWaiters(ctx, w.inputScope(), db.ChannelPairingResume{ScopeKey: row.ScopeKey, ActorID: t.Scope.ActorID, PairingID: receipt.PairingID}, t.Request.Provider)
	if err != nil {
		return err
	}
	var dispatchErr error
	for _, waiter := range waiters {
		var msg channelMessage
		if err := json.Unmarshal([]byte(waiter.Payload), &msg); err == nil && msg.PairingCard != "" {
			if err := w.adapter.ResumeReply(msg.PairingCard).Update(ctx, contracts.Reply{Status: "已连接", Blocks: []contracts.ReplyBlock{{Text: "已连接"}}, Ended: true}); err != nil {
				s.logger.WarnContext(ctx, "更新配对授权卡失败", "input_uuid", waiter.UUID, "error", err)
			}
		}
		dispatchErr = errors.Join(dispatchErr, w.claimAndDispatchInput(ctx, waiter.UUID))
	}
	return dispatchErr
}
