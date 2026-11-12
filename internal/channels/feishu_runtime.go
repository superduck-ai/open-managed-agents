package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const (
	channelReplyPollInterval = time.Second
	channelReconcileInterval = 30 * time.Second
	channelReplyTimeout      = 30 * time.Minute
	channelPairingLimit      = 1000
	channelTitleLimit        = 30
)

type runtimeStore interface {
	GetChannelRecord(context.Context, string, string, string, string) (db.ChannelRecord, error)
	GetChannelSessionBinding(context.Context, string, string) (string, error)
	SetChannelSessionBinding(context.Context, string, string, string) error
	DeleteChannelSessionBinding(context.Context, string, string) error
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
	s.reconcile(ctx)
	go s.watch(ctx)
}

func (s *runtimeSupervisor) watch(ctx context.Context) {
	ticker := time.NewTicker(channelReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *runtimeSupervisor) reconcile(ctx context.Context) {
	records, err := s.listRunnable(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "reconcile channel runtimes", "error", err)
		return
	}
	wanted := make(map[string]db.ChannelRecord, len(records))
	for _, record := range records {
		wanted[record.ExternalID] = record
	}
	s.mu.Lock()
	stale := make([]string, 0)
	for channelID, worker := range s.workers {
		if record, ok := wanted[channelID]; !ok || worker.outdated(record) {
			stale = append(stale, channelID)
		}
	}
	s.mu.Unlock()
	for _, channelID := range stale {
		s.stop(channelID)
	}
	for _, record := range records {
		if s.running(record.ExternalID) {
			continue
		}
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

func (s *runtimeSupervisor) running(channelID string) bool {
	s.mu.Lock()
	_, ok := s.workers[channelID]
	s.mu.Unlock()
	return ok
}

func (s *runtimeSupervisor) online(channelID string) bool {
	s.mu.Lock()
	worker, ok := s.workers[channelID]
	s.mu.Unlock()
	return ok && worker.online.Load()
}

func (s *runtimeSupervisor) openGitToken(ctx context.Context, record db.ChannelRecord) string {
	if record.GitTokenEnvelope == nil || s.cipher == nil {
		return ""
	}
	token, err := openSealedSecret(ctx, s.cipher, record, *record.GitTokenEnvelope)
	if err != nil {
		s.logger.WarnContext(ctx, "open channel git token", "channel_id", record.ExternalID, "error", err)
		return ""
	}
	return token
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
	client     *lark.Client
	channel    channeltypes.Channel
	cancel     context.CancelFunc
	online     atomic.Bool
	askedMu    sync.Mutex
	asked      map[string]struct{}
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
		client:     client,
		channel:    bot,
		asked:      make(map[string]struct{}),
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
	if !w.active(ctx) {
		return nil
	}
	text := msg.Content
	if text == "" && len(msg.Resources) == 0 {
		return w.replyText(ctx, msg, "当前渠道只支持文本、图片和 PDF 附件，请重新发送。")
	}
	switch strings.TrimSpace(text) {
	case "/new", "/stop":
		return w.handleCommand(ctx, msg, strings.TrimSpace(text))
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
	content, skipped := w.messageContent(ctx, msg, text)
	if len(content) == 0 {
		return w.replyText(ctx, msg, "附件格式暂不支持，目前仅支持图片、PDF 与纯文本文件。")
	}
	if len(skipped) > 0 {
		_, _ = w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Markdown: "以下附件格式暂不支持，已跳过：" + strings.Join(skipped, "、")})
	}
	if err := w.supervisor.oma.sendContent(ctx, apiKey, sessionID, content); err != nil {
		return err
	}
	return w.streamReply(ctx, msg, apiKey, sessionID, len(events))
}

func (w *channelWorker) messageContent(ctx context.Context, msg *channeltypes.NormalizedMessage, text string) ([]map[string]any, []string) {
	content := make([]map[string]any, 0, 3)
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	skipped := make([]string, 0)
	for _, resource := range msg.Resources {
		block, ok := w.attachmentBlock(ctx, resource)
		if !ok {
			name := resource.FileName
			if name == "" {
				name = resource.Type
			}
			skipped = append(skipped, name)
			continue
		}
		content = append(content, block)
	}
	return content, skipped
}

func (w *channelWorker) attachmentBlock(ctx context.Context, resource channeltypes.Resource) (map[string]any, bool) {
	if resource.FileKey == "" {
		return nil, false
	}
	mediaType := resource.Type
	if mediaType != "image" {
		mediaType = "file"
	}
	data, err := w.channel.DownloadFile(ctx, resource.FileKey, mediaType)
	if err != nil || len(data) == 0 {
		w.supervisor.logger.WarnContext(ctx, "download channel attachment", "channel_id", w.record.ExternalID, "error", err)
		return nil, false
	}
	detected := http.DetectContentType(data)
	if resource.Type == "image" {
		switch detected {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			return map[string]any{
				"type":   "image",
				"source": map[string]string{"type": "base64", "media_type": detected, "data": base64.StdEncoding.EncodeToString(data)},
			}, true
		default:
			return nil, false
		}
	}
	switch {
	case detected == "application/pdf":
		return map[string]any{
			"type":   "document",
			"source": map[string]string{"type": "base64", "media_type": "application/pdf", "data": base64.StdEncoding.EncodeToString(data)},
		}, true
	case strings.HasPrefix(detected, "text/"):
		return map[string]any{
			"type":   "document",
			"source": map[string]string{"type": "text", "media_type": "text/plain", "data": string(data)},
		}, true
	default:
		return nil, false
	}
}

func (w *channelWorker) active(ctx context.Context) bool {
	record, err := w.supervisor.store.GetChannelRecord(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, w.record.AgentExternalID, w.record.ExternalID)
	if err != nil {
		return false
	}
	return record.Enabled && record.DeletedAt == nil && record.BindingStatus == "bound"
}

func (w *channelWorker) handleCommand(ctx context.Context, msg *channeltypes.NormalizedMessage, command string) error {
	scopeKey := channelScopeKey(msg)
	if command == "/new" {
		if err := w.supervisor.store.DeleteChannelSessionBinding(ctx, w.record.ExternalID, scopeKey); err != nil {
			return err
		}
		return w.replyText(ctx, msg, "已开启新会话，下一条消息会创建新的会话。")
	}
	sessionID, err := w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey)
	if err != nil || sessionID == "" {
		return w.replyText(ctx, msg, "当前没有进行中的会话。")
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return err
	}
	if err := w.supervisor.oma.interrupt(ctx, apiKey, sessionID); err != nil {
		return err
	}
	return w.replyText(ctx, msg, "已请求停止当前任务。")
}

func channelScopeKey(msg *channeltypes.NormalizedMessage) string {
	if threadID := channelThreadID(msg); threadID != "" {
		return "thread:" + threadID
	}
	return "chat:" + msg.ChatID
}

func channelThreadID(msg *channeltypes.NormalizedMessage) string {
	raw, ok := msg.RawEvent.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"thread_id", "thread_key"} {
		if value, ok := raw[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	event, ok := raw["event"].(map[string]any)
	if !ok {
		return ""
	}
	if message, ok := event["message"].(map[string]any); ok {
		if value, ok := message["thread_id"].(string); ok {
			return strings.TrimSpace(value)
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
	scopeKey := channelScopeKey(msg)
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

func (w *channelWorker) streamReply(ctx context.Context, msg *channeltypes.NormalizedMessage, apiKey, sessionID string, baseline int) error {
	stream, err := w.openStream(ctx, msg)
	if err != nil {
		return err
	}
	defer stream.Close(context.WithoutCancel(ctx))
	reply := channelReply{card: w.record.IncludeThinking, thinking: w.record.IncludeThinking}
	if reply.card {
		if err := stream.UpdateCard(ctx, reply.render()); err != nil {
			return err
		}
	}
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
				text, changed := reply.consume(w.record, event)
				if changed {
					if reply.card {
						if err := stream.UpdateCard(ctx, reply.render()); err != nil {
							return err
						}
					} else if text != "" {
						if err := stream.Append(ctx, text); err != nil {
							return err
						}
					}
				}
				if err := w.askIfNeeded(ctx, msg, sessionID, event); err != nil {
					w.supervisor.logger.WarnContext(ctx, "send channel action card", "channel_id", w.record.ExternalID, "error", err)
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

func (w *channelWorker) openStream(ctx context.Context, msg *channeltypes.NormalizedMessage) (channeltypes.StreamController, error) {
	if !w.record.ReplyInThread || msg.MessageID == "" {
		if w.record.IncludeThinking {
			return w.channel.Stream(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Card: channelCardStart()})
		}
		return w.channel.Stream(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Markdown: "正在处理…"})
	}
	if w.record.IncludeThinking {
		messageID, err := w.replyMessage(ctx, msg.MessageID, "interactive", channelCardStart())
		if err != nil {
			return nil, err
		}
		return channel.NewCardStreamController(w.client, channeltypes.DefaultChannelConfig(), messageID), nil
	}
	content, err := normalize.SimpleMarkdownToPost("", "正在处理…", nil)
	if err != nil {
		return nil, err
	}
	messageID, err := w.replyMessage(ctx, msg.MessageID, "post", content)
	if err != nil {
		return nil, err
	}
	return channel.NewMarkdownStreamController(w.client, channeltypes.DefaultChannelConfig(), messageID, "正在处理…", ""), nil
}

func (w *channelWorker) replyMessage(ctx context.Context, messageID, msgType, content string) (string, error) {
	request := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType(msgType).Content(content).ReplyInThread(true).Build()).
		Build()
	response, err := w.client.Im.V1.Message.Reply(ctx, request)
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", fmt.Errorf("飞书回复消息失败: %d %s", response.Code, response.Msg)
	}
	if response.Data == nil || response.Data.MessageId == nil {
		return "", errors.New("飞书回复消息缺少 message_id")
	}
	return *response.Data.MessageId, nil
}

func (w *channelWorker) replyText(ctx context.Context, msg *channeltypes.NormalizedMessage, text string) error {
	if w.record.ReplyInThread && msg.MessageID != "" {
		content, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return err
		}
		_, err = w.replyMessage(ctx, msg.MessageID, "text", string(content))
		return err
	}
	_, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Text: text})
	return err
}

type channelReply struct {
	card     bool
	thinking bool
	text     strings.Builder
}

func (r *channelReply) consume(record db.ChannelRecord, event omaEvent) (string, bool) {
	switch event.Type {
	case "agent.message":
		text := eventText(event)
		if text == "" {
			return "", false
		}
		r.text.WriteString(text + "\n")
		r.thinking = false
		return text + "\n", true
	case "agent.thinking":
		if record.IncludeThinking && !r.thinking {
			r.thinking = true
			return "", true
		}
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		if !record.IncludeToolCalls {
			return "", false
		}
		name := eventToolName(event)
		if name == "" {
			return "", false
		}
		line := "🔧 " + name + "\n"
		r.text.WriteString(line)
		r.thinking = false
		return line, true
	}
	return "", false
}

func (r *channelReply) render() string {
	return channelCard(r.text.String(), r.thinking)
}

func channelCardStart() string {
	return channelCard("", true)
}

func channelCard(text string, thinking bool) string {
	elements := make([]map[string]any, 0, 2)
	if text != "" {
		elements = append(elements, map[string]any{"tag": "markdown", "content": text})
	}
	if thinking {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_🧠 正在思考…_", "text_size": "notation"})
	}
	if len(elements) == 0 {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_正在处理…_"})
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"update_multi": true},
		"body":   map[string]any{"elements": elements},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "{}"
	}
	return string(encoded)
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
