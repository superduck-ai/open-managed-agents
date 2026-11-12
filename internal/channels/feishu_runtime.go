package channels

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const (
	channelReplyPollInterval = time.Second
	channelReplyTimeout      = 30 * time.Minute
	channelPairingLimit      = 1000
	channelTitleLimit        = 30
)

type runtimeStore interface {
	GetChannelRecord(context.Context, string, string, string, string) (db.ChannelRecord, error)
	GetChannelSessionBinding(context.Context, string, string) (string, error)
	SetChannelSessionBinding(context.Context, string, string, string) error
	GetChannelAgentEnvironment(context.Context, string, string, string) (string, error)
	ListChannelPairingRecords(context.Context, string) ([]db.ChannelPairingRecord, error)
	CountChannelPairingRecords(context.Context, string) (int, error)
	InsertChannelPairingRecord(context.Context, string, db.ChannelPairingInput) error
	EnsureChannelWorkspaceCredential(context.Context, platform.CreateConsoleAPIKeyInput, *secrets.Service) (string, error)
}

type runtimeSupervisor struct {
	store    runtimeStore
	cipher   *secrets.Service
	oma      *omaClient
	logger   *slog.Logger
	mu       sync.Mutex
	workers  map[string]*channelWorker
	apiKeys  map[string]string
	shutdown chan struct{}
}

func newRuntimeSupervisor(store runtimeStore, cipher *secrets.Service, oma *omaClient, logger *slog.Logger) *runtimeSupervisor {
	return &runtimeSupervisor{
		store:    store,
		cipher:   cipher,
		oma:      oma,
		logger:   logging.LoggerOrDefault(logger),
		workers:  make(map[string]*channelWorker),
		apiKeys:  make(map[string]string),
		shutdown: make(chan struct{}),
	}
}

func (s *runtimeSupervisor) restore(ctx context.Context) {
	records, err := s.listRunnable(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "restore channel runtimes", "error", err)
		return
	}
	for _, record := range records {
		if err := s.apply(ctx, record); err != nil {
			s.logger.WarnContext(ctx, "start channel runtime", "channel_id", record.ExternalID, "error", err)
		}
	}
}

func (s *runtimeSupervisor) listRunnable(ctx context.Context) ([]db.ChannelRecord, error) {
	lister, ok := s.store.(interface {
		ListRunnableChannelRecords(context.Context) ([]db.ChannelRecord, error)
	})
	if !ok {
		return nil, nil
	}
	return lister.ListRunnableChannelRecords(ctx)
}

func (s *runtimeSupervisor) apply(ctx context.Context, record db.ChannelRecord) error {
	if record.BindingStatus != "bound" || !record.Enabled || record.DeletedAt != nil {
		s.stop(record.ExternalID)
		return nil
	}
	if s.oma == nil || s.oma.baseURL == "" {
		s.stop(record.ExternalID)
		return errors.New("未配置 channels.oma_url，无法启动飞书渠道")
	}
	secret, err := openChannelSecret(ctx, s.cipher, record)
	if err != nil {
		s.stop(record.ExternalID)
		return err
	}
	s.stop(record.ExternalID)
	worker, err := newChannelWorker(s, record, secret)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.workers[record.ExternalID] = worker
	s.mu.Unlock()
	worker.start()
	return nil
}

func (s *runtimeSupervisor) stop(channelID string) {
	s.mu.Lock()
	worker, ok := s.workers[channelID]
	if ok {
		delete(s.workers, channelID)
	}
	s.mu.Unlock()
	if ok {
		worker.close()
	}
}

func (s *runtimeSupervisor) stopAll() {
	s.mu.Lock()
	workers := make([]*channelWorker, 0, len(s.workers))
	for _, worker := range s.workers {
		workers = append(workers, worker)
	}
	s.workers = make(map[string]*channelWorker)
	s.mu.Unlock()
	for _, worker := range workers {
		worker.close()
	}
}

func (s *runtimeSupervisor) online(channelID string) bool {
	s.mu.Lock()
	worker, ok := s.workers[channelID]
	s.mu.Unlock()
	return ok && worker.online.Load()
}

func (s *runtimeSupervisor) apiKey(ctx context.Context, record db.ChannelRecord) (string, error) {
	s.mu.Lock()
	key, ok := s.apiKeys[record.WorkspaceUUID]
	s.mu.Unlock()
	if ok && key != "" {
		return key, nil
	}
	key, err := s.store.EnsureChannelWorkspaceCredential(ctx, platform.CreateConsoleAPIKeyInput{
		OrgUUID:       record.OrganizationUUID,
		WorkspaceUUID: record.WorkspaceUUID,
	}, s.cipher)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.apiKeys[record.WorkspaceUUID] = key
	s.mu.Unlock()
	return key, nil
}

type channelWorker struct {
	supervisor *runtimeSupervisor
	record     db.ChannelRecord
	secret     string
	channel    channeltypes.Channel
	cancel     context.CancelFunc
	online     atomic.Bool
}

func newChannelWorker(supervisor *runtimeSupervisor, record db.ChannelRecord, secret string) (*channelWorker, error) {
	if record.AppID == "" || secret == "" {
		return nil, errors.New("飞书应用凭据不完整")
	}
	client := lark.NewClient(record.AppID, secret)
	wsClient := larkws.NewClient(record.AppID, secret)
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
		channel:    bot,
	}
	bot.OnReady(func() { worker.online.Store(true) })
	bot.OnReconnecting(func() { worker.online.Store(false) })
	bot.OnReconnected(func() { worker.online.Store(true) })
	bot.OnDisconnected(func() { worker.online.Store(false) })
	bot.OnMessage(worker.handleMessage)
	return worker, nil
}

func (w *channelWorker) start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go func() {
		defer w.online.Store(false)
		if err := w.channel.Start(ctx); err != nil && ctx.Err() == nil {
			w.supervisor.logger.WarnContext(ctx, "feishu channel disconnected", "channel_id", w.record.ExternalID, "error", err)
		}
	}()
}

func (w *channelWorker) close() {
	w.online.Store(false)
	if w.cancel != nil {
		w.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.channel.Stop(ctx); err != nil {
		w.supervisor.logger.DebugContext(ctx, "stop feishu channel", "channel_id", w.record.ExternalID, "error", err)
	}
}

func (w *channelWorker) handleMessage(ctx context.Context, msg *channeltypes.NormalizedMessage) error {
	if msg == nil || msg.UserID == "" || msg.ChatID == "" {
		return nil
	}
	text := msg.Content
	if text == "" {
		return w.replyText(ctx, msg, "当前渠道只支持文本消息，请直接发送文字。")
	}
	allowed, err := w.authorize(ctx, msg)
	if err != nil || !allowed {
		return err
	}
	sessionID, err := w.sessionFor(ctx, msg)
	if err != nil {
		w.supervisor.logger.WarnContext(ctx, "open channel session", "channel_id", w.record.ExternalID, "error", err)
		if errors.Is(err, db.ErrNotFound) {
			return w.replyText(ctx, msg, "当前渠道尚未配置执行环境，请在 OMA 渠道设置中完成配置。")
		}
		return w.replyText(ctx, msg, "OMA 会话创建失败，请稍后重试。")
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return err
	}
	events, err := w.supervisor.oma.listEvents(ctx, apiKey, sessionID)
	if err != nil {
		return err
	}
	if err := w.supervisor.oma.sendUserMessage(ctx, apiKey, sessionID, text); err != nil {
		return err
	}
	return w.streamReply(ctx, msg, apiKey, sessionID, len(events))
}

func (w *channelWorker) authorize(ctx context.Context, msg *channeltypes.NormalizedMessage) (bool, error) {
	if w.record.Mode != "pairing" {
		return true, nil
	}
	kind, target := "chat", msg.ChatID
	if msg.ChatType == "p2p" {
		kind, target = "user", msg.UserID
	}
	pairings, err := w.supervisor.store.ListChannelPairingRecords(ctx, w.record.ExternalID)
	if err != nil {
		return false, err
	}
	for _, pairing := range pairings {
		if pairing.Kind == kind && pairing.Target == target && pairing.Status == "approved" {
			return true, nil
		}
		if pairing.Kind == kind && pairing.Target == target {
			return false, w.replyText(ctx, msg, "配对申请正在等待管理员批准。")
		}
	}
	if err := w.recordPairing(ctx, kind, target, msg.UserID); err != nil {
		return false, err
	}
	return false, w.replyText(ctx, msg, "已提交配对申请，请等待管理员在 OMA 渠道页面批准。")
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

func (w *channelWorker) sessionFor(ctx context.Context, msg *channeltypes.NormalizedMessage) (string, error) {
	scopeKey := "chat:" + msg.ChatID
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
	created, err := w.supervisor.oma.createSession(ctx, apiKey, w.record.AgentExternalID, environmentID, channelSessionTitle(msg.Content), map[string]string{
		"source":     "feishu",
		"channel_id": w.record.ExternalID,
		"app_id":     w.record.AppID,
		"scope_id":   scopeKey,
		"chat_type":  channelChatType(msg.ChatType),
		"open_id":    msg.UserID,
		"actor_id":   msg.UserID,
	})
	if err != nil {
		return "", err
	}
	if err := w.supervisor.store.SetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey, created); err != nil {
		return "", err
	}
	return created, nil
}

func (w *channelWorker) streamReply(ctx context.Context, msg *channeltypes.NormalizedMessage, apiKey, sessionID string, baseline int) error {
	stream, err := w.channel.Stream(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Markdown: "正在处理…"})
	if err != nil {
		return err
	}
	defer stream.Close(context.WithoutCancel(ctx))
	seen := make(map[string]struct{})
	deadline := time.Now().Add(channelReplyTimeout)
	ticker := time.NewTicker(channelReplyPollInterval)
	defer ticker.Stop()
	for {
		events, err := w.supervisor.oma.listEvents(ctx, apiKey, sessionID)
		if err == nil {
			done := false
			for index, event := range events {
				if index < baseline {
					continue
				}
				if _, ok := seen[event.ID]; ok {
					continue
				}
				seen[event.ID] = struct{}{}
				if event.Type == "agent.message" {
					if text := eventText(event); text != "" {
						if err := stream.Append(ctx, text+"\n"); err != nil {
							return err
						}
					}
				}
				if terminalEvent(event) {
					done = true
				}
			}
			if done {
				return stream.Close(ctx)
			}
		}
		if time.Now().After(deadline) {
			return stream.Close(ctx)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.supervisor.shutdown:
			return nil
		case <-ticker.C:
		}
	}
}

func (w *channelWorker) replyText(ctx context.Context, msg *channeltypes.NormalizedMessage, text string) error {
	_, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Text: text})
	return err
}

func terminalEvent(event omaEvent) bool {
	switch event.Type {
	case "session.status_idle", "session.thread_status_idle":
		reason := stopReason(event)
		return reason == "end_turn" || reason == "interrupted" || reason == "retries_exhausted"
	case "session.status_terminated", "session.error":
		return true
	default:
		return false
	}
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
		return "", fmt.Errorf("渠道 %s 缺少飞书应用凭据", record.ExternalID)
	}
	return openSealedSecret(ctx, cipher, record, *record.AppSecretEnvelope)
}
