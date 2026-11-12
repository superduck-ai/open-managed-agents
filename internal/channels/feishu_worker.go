package channels

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type channelWorker struct {
	supervisor *runtimeSupervisor
	record     db.ChannelRecord
	secret     string
	client     *lark.Client
	channel    channeltypes.Channel
	cancel     context.CancelFunc
	online     atomic.Bool

	mu        sync.Mutex
	scopes    map[string]*scopeQueue
	replies   map[string][]*channelReplyTask
	asked     map[string]*askedCard
	artifacts map[string]struct{}
	tails     map[string]string
	ctx       context.Context
}

type channelReplyTask struct {
	ctx          context.Context
	scopeKey     string
	sessionID    string
	inputEventID string
	inbound      map[string]struct{}
	listener     bool
	idle         time.Duration
	cancel       context.CancelFunc
	done         chan struct{}
}

func newFeishuWSClient(appID, secret string) *larkws.Client {
	return larkws.NewClient(appID, secret, larkws.WithEventHandler(larkevent.NewEventDispatcher("", "")))
}

func newChannelWorker(supervisor *runtimeSupervisor, record db.ChannelRecord, secret string) (*channelWorker, error) {
	if record.AppID == "" || secret == "" {
		return nil, errors.New("飞书应用凭据不完整")
	}
	client := lark.NewClient(record.AppID, secret)
	wsClient := newFeishuWSClient(record.AppID, secret)
	requireMention := true
	respondToMentionAll := false
	bot := channel.NewChannel(client, wsClient, channeltypes.WithPolicyConfig(channeltypes.PolicyConfig{
		RequireMention:      &requireMention,
		RespondToMentionAll: &respondToMentionAll,
		DMMode:              "open",
	}))
	worker := &channelWorker{
		supervisor: supervisor,
		record:     record,
		secret:     secret,
		client:     client,
		channel:    bot,
		scopes:     make(map[string]*scopeQueue),
		replies:    make(map[string][]*channelReplyTask),
		asked:      make(map[string]*askedCard),
		artifacts:  make(map[string]struct{}),
		tails:      make(map[string]string),
	}
	bot.OnReady(func() { worker.online.Store(true) })
	bot.OnReconnecting(func() { worker.online.Store(false) })
	bot.OnReconnected(func() { worker.online.Store(true) })
	bot.OnDisconnected(func() { worker.online.Store(false) })
	bot.OnMessage(worker.handleMessage)
	bot.OnCardAction(worker.handleCardAction)
	return worker, nil
}

func (w *channelWorker) outdated(record db.ChannelRecord) bool {
	return w.record.AppID != record.AppID || w.record.UpdatedAt.Before(record.UpdatedAt)
}

func (w *channelWorker) start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.mu.Lock()
	w.ctx = ctx
	w.mu.Unlock()
	go func() {
		defer w.online.Store(false)
		if err := w.channel.Start(ctx); err != nil && ctx.Err() == nil {
			w.supervisor.logger.WarnContext(ctx, "feishu channel disconnected", "channel_id", w.record.ExternalID, "error", err)
		}
		w.supervisor.forget(w.record.ExternalID, w)
	}()
}

func (w *channelWorker) close() {
	w.online.Store(false)
	if w.cancel != nil {
		w.cancel()
	}
	w.stopAllReplies()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.channel.Stop(ctx); err != nil {
		w.supervisor.logger.DebugContext(ctx, "stop feishu channel", "channel_id", w.record.ExternalID, "error", err)
	}
}

func (w *channelWorker) active(ctx context.Context) bool {
	record, err := w.supervisor.store.GetChannelRecord(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, w.record.AgentExternalID, w.record.ExternalID)
	if err != nil {
		return false
	}
	return record.Enabled && record.DeletedAt == nil && record.BindingStatus == "bound"
}

func (w *channelWorker) sessionFor(ctx context.Context, msg *channeltypes.NormalizedMessage, scopeKey string) (string, error) {
	sessionID, err := w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey)
	if err == nil && sessionID != "" {
		apiKey, keyErr := w.supervisor.apiKey(ctx, w.record)
		if keyErr == nil {
			if session, sessionErr := w.supervisor.oma.getSession(ctx, apiKey, sessionID); sessionErr == nil && session.ArchivedAt == nil {
				return sessionID, nil
			}
		}
	}
	environmentID, err := w.supervisor.store.GetChannelAgentEnvironment(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, w.record.AgentExternalID)
	if err != nil {
		return "", err
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return "", err
	}
	resources, vaultIDs := channelSessionResources(w.record, w.supervisor.openGitToken(ctx, w.record))
	created, err := w.supervisor.oma.createSession(ctx, apiKey, w.record.AgentExternalID, environmentID, channelSessionTitle(msg.Content), map[string]string{
		"source":     "feishu",
		"channel_id": w.record.ExternalID,
		"app_id":     w.record.AppID,
		"scope_id":   scopeKey,
		"chat_type":  channelChatType(msg.ChatType),
		"open_id":    msg.UserID,
		"actor_id":   msg.UserID,
	}, resources, vaultIDs)
	if err != nil {
		return "", err
	}
	if err := w.supervisor.store.SetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey, created); err != nil {
		return "", err
	}
	return created, nil
}

func (w *channelWorker) currentSession(ctx context.Context, scopeKey string) string {
	sessionID, err := w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey)
	if err != nil {
		return ""
	}
	return sessionID
}

func (w *channelWorker) sessionBound(ctx context.Context, task *channelReplyTask) bool {
	if task.ctx.Err() != nil {
		return false
	}
	return w.currentSession(ctx, task.scopeKey) == task.sessionID
}

type pairingDecision int

const (
	pairingApproved pairingDecision = iota
	pairingAwaiting
	pairingMissing
)

func channelPairTarget(chatType, chatID, openID string) (string, string) {
	if chatType == "p2p" {
		return "user", openID
	}
	return "chat", chatID
}

func (w *channelWorker) pairingFor(ctx context.Context, kind, target string) (pairingDecision, error) {
	pairings, err := w.supervisor.store.ListChannelPairingRecords(ctx, w.record.ExternalID)
	if err != nil {
		return pairingMissing, err
	}
	for _, pairing := range pairings {
		if pairing.Kind != kind || pairing.Target != target {
			continue
		}
		if pairing.Status == "approved" {
			return pairingApproved, nil
		}
		return pairingAwaiting, nil
	}
	return pairingMissing, nil
}

func (w *channelWorker) authorize(ctx context.Context, msg *channeltypes.NormalizedMessage) (bool, error) {
	if w.record.Mode != "pairing" {
		return true, nil
	}
	kind, target := channelPairTarget(msg.ChatType, msg.ChatID, msg.UserID)
	decision, err := w.pairingFor(ctx, kind, target)
	if err != nil {
		return false, err
	}
	switch decision {
	case pairingApproved:
		return true, nil
	case pairingAwaiting:
		return false, w.replyText(ctx, msg, "配对申请正在等待管理员批准。")
	}
	if err := w.recordPairing(ctx, kind, target, msg.UserID); err != nil {
		return false, err
	}
	return false, w.replyText(ctx, msg, "已提交配对申请，请等待管理员在 OMA 渠道页面批准。")
}

func (w *channelWorker) cardAuthorized(ctx context.Context, value channelActionValue, operator, chatID string) (bool, error) {
	if w.record.Mode != "pairing" {
		return true, nil
	}
	kind, target := channelPairTarget(value.ChatType, chatID, operator)
	decision, err := w.pairingFor(ctx, kind, target)
	if err != nil {
		return false, err
	}
	return decision == pairingApproved, nil
}

func (w *channelWorker) recordPairing(ctx context.Context, kind, target, displayName string) error {
	count, err := w.supervisor.store.CountChannelPairingRecords(ctx, w.record.ExternalID)
	if err != nil || count >= channelPairingLimit {
		return err
	}
	externalID, err := ids.New("chpr_")
	if err != nil {
		return err
	}
	return w.supervisor.store.InsertChannelPairingRecord(ctx, w.record.ExternalID, db.ChannelPairingInput{
		ExternalID:  externalID,
		Kind:        kind,
		Target:      target,
		DisplayName: displayName,
	})
}

func channelScopeKey(msg *channeltypes.NormalizedMessage) string {
	if threadID := channelThreadID(msg); threadID != "" {
		return "thread:" + msg.ChatID + ":" + threadID
	}
	return "chat:" + msg.ChatID
}

func channelThreadID(msg *channeltypes.NormalizedMessage) string {
	if raw, ok := msg.RawEvent.(map[string]any); ok {
		return threadIDFromPayload(raw)
	}
	if msg.RawEvent == nil {
		return ""
	}
	encoded, err := json.Marshal(msg.RawEvent)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return ""
	}
	return threadIDFromPayload(payload)
}

func threadIDFromPayload(payload map[string]any) string {
	if value := threadIDFromMessage(payload); value != "" {
		return value
	}
	for _, key := range []string{"message", "event"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if value := threadIDFromMessage(nested); value != "" {
			return value
		}
		if message, ok := nested["message"].(map[string]any); ok {
			if value := threadIDFromMessage(message); value != "" {
				return value
			}
		}
	}
	return ""
}

func threadIDFromMessage(payload map[string]any) string {
	for _, key := range []string{"thread_id", "root_id"} {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func channelSessionResources(record db.ChannelRecord, gitToken string) ([]map[string]any, []string) {
	mounts := parseChannelMounts(record.Mounts)
	resources := make([]map[string]any, 0, 2)
	vaultIDs := make([]string, 0, 1)
	if mounts.MemoryStoreID != "" {
		resources = append(resources, map[string]any{"type": "memory_store", "memory_store_id": mounts.MemoryStoreID})
	}
	if mounts.GitRepository != nil && mounts.GitRepository.URL != "" {
		git := map[string]any{"type": "github_repository", "url": mounts.GitRepository.URL}
		if mounts.GitRepository.MountPath != "" {
			git["mount_path"] = mounts.GitRepository.MountPath
		}
		if len(mounts.GitRepository.Checkout) > 0 {
			var checkout any
			if err := json.Unmarshal(mounts.GitRepository.Checkout, &checkout); err == nil {
				git["checkout"] = checkout
			}
		}
		if gitToken != "" {
			git["authorization_token"] = gitToken
		}
		resources = append(resources, git)
	}
	if mounts.VaultID != "" {
		vaultIDs = append(vaultIDs, mounts.VaultID)
	}
	return resources, vaultIDs
}

func channelChatType(chatType string) string {
	if chatType == "p2p" {
		return "p2p"
	}
	return "group"
}

func channelSessionTitle(text string) string {
	trimmed := []rune(text)
	if len(trimmed) == 0 {
		return "飞书会话"
	}
	if len(trimmed) > channelTitleLimit {
		trimmed = trimmed[:channelTitleLimit]
	}
	return string(trimmed)
}

func openChannelSecret(ctx context.Context, cipher *secrets.Service, record db.ChannelRecord) (string, error) {
	if cipher == nil {
		return "", errors.New("secrets service is not configured")
	}
	if record.AppSecretEnvelope == nil || *record.AppSecretEnvelope == "" {
		return "", errors.New("渠道缺少飞书应用凭据")
	}
	return openSealedSecret(ctx, cipher, record, *record.AppSecretEnvelope)
}
