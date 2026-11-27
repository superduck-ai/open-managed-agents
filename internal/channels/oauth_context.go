package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type OAuthContext struct {
	Channel db.ChannelRecord
	Session db.Session
	ActorID string
	ChatID  string
	APIKey  string
	adapter channelAdapter
}

func (s *Service) OAuthContext(ctx context.Context, channelID, sessionID string) (OAuthContext, error) {
	s.supervisor.mu.Lock()
	worker := s.supervisor.workers[channelID]
	s.supervisor.mu.Unlock()
	if worker == nil || !worker.active(ctx) {
		return OAuthContext{}, db.ErrNotFound
	}
	session, found, err := s.store.GetSession(ctx, worker.record.WorkspaceUUID, sessionID)
	if err != nil {
		return OAuthContext{}, err
	}
	if !found || session.ArchivedAt != nil || session.Status == "terminated" {
		return OAuthContext{}, db.ErrNotFound
	}
	var metadata map[string]string
	if err := json.Unmarshal(session.Metadata, &metadata); err != nil {
		return OAuthContext{}, err
	}
	scopeKey := metadata["scope_id"]
	chatID := metadata["chat_id"]
	if legacyChatID, ok := strings.CutPrefix(scopeKey, "chat:"); chatID == "" && ok {
		chatID = legacyChatID
	}
	if metadata["channel_id"] != channelID || metadata["app_id"] != worker.record.AppID || metadata["chat_type"] != "p2p" || metadata["actor_id"] == "" || chatID == "" || !scopeMatchesChat(scopeKey, chatID) {
		return OAuthContext{}, errors.New("个人 OAuth 只能用于原操作者的私聊会话")
	}
	if worker.currentSession(ctx, scopeKey) != sessionID {
		return OAuthContext{}, db.ErrNotFound
	}
	allowed, err := worker.cardAuthorized(ctx, channelActionValue{ChatType: "p2p", OpenID: metadata["actor_id"]}, metadata["actor_id"], chatID)
	if err != nil || !allowed {
		return OAuthContext{}, errors.Join(db.ErrNotFound, err)
	}
	key, err := s.supervisor.apiKey(ctx, worker.record)
	if err != nil {
		return OAuthContext{}, err
	}
	return OAuthContext{Channel: worker.record, Session: session, ActorID: metadata["actor_id"], ChatID: chatID, APIKey: key, adapter: worker.adapter}, nil
}
