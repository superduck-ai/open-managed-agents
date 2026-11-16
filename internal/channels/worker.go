package channels

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type channelWorker struct {
	deliveryOwner string
	supervisor    *runtimeSupervisor
	record        db.ChannelRecord
	adapter       channelAdapter
	cancel        context.CancelFunc
	online        atomic.Bool

	mu            sync.Mutex
	checkedInputs map[string]string
	scopes        map[string]*scopeQueue
	replies       map[string][]*channelReplyTask
	ctx           context.Context
}

type channelReplyTask struct {
	artifactBaseline  artifactBaseline
	ctx               context.Context
	scopeKey          string
	sessionID         string
	inputEventID      string
	inbound           map[string]struct{}
	listener          bool
	cancel            context.CancelFunc
	done              chan struct{}
	usedTools         bool
	normalEnd         bool
	runner            *replyRunner
	delivery          *db.ChannelReplyDelivery
	terminal          bool
	deliveryRenewedAt time.Time
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
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var replyRecovery time.Time
		renewed := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if w.online.Load() {
					if !time.Now().Before(replyRecovery) {
						w.restoreReplies(ctx)
						replyRecovery = time.Now().Add(channelReconcileInterval)
						next, found, err := w.supervisor.store.NextChannelReplyRecovery(ctx, w.deliveryScope(), w.deliveryOwner)
						if err != nil {
							w.supervisor.logger.WarnContext(ctx, "schedule channel reply recovery", "channel_id", w.record.ExternalID, "error", err)
						} else if found && next.Before(replyRecovery) {
							replyRecovery = next
						}
					}
					if time.Since(renewed) >= 10*time.Second {
						if err := w.supervisor.store.RenewChannelInputs(ctx, w.inputScope(), w.deliveryOwner); err != nil {
							w.supervisor.logger.WarnContext(ctx, "renew channel inputs", "channel_id", w.record.ExternalID, "error", err)
						}
						renewed = time.Now()
					}
					if w.active(ctx) {
						w.restoreInputs(ctx)
					}
				}
			}
		}
	}()
	go func() {
		defer w.online.Store(false)
		if err := w.adapter.start(ctx); err != nil && ctx.Err() == nil {
			w.supervisor.logger.WarnContext(ctx, "channel disconnected", "channel_id", w.record.ExternalID, "error", err)
		}
		w.supervisor.forget(w.record.ExternalID, w)
	}()
}

func (w *channelWorker) close() {
	w.online.Store(false)
	if w.cancel != nil {
		w.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, task := range w.stopAllReplies() {
		select {
		case <-task.done:
		case <-ctx.Done():
			return
		}
	}
	if err := w.adapter.close(ctx); err != nil {
		w.supervisor.logger.DebugContext(ctx, "stop channel", "channel_id", w.record.ExternalID, "error", err)
	}
}

func (w *channelWorker) active(ctx context.Context) bool {
	record, err := w.supervisor.store.GetChannelRecord(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, w.record.AgentExternalID, w.record.ExternalID)
	if err != nil {
		return false
	}
	return record.Enabled && record.DeletedAt == nil && record.BindingStatus == "bound"
}

func (w *channelWorker) sessionFor(ctx context.Context, msg *channelMessage, scopeKey string) (string, error) {
	environmentID, err := w.supervisor.store.GetChannelAgentEnvironment(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, w.record.AgentExternalID)
	if err != nil {
		return "", err
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return "", err
	}
	personalID := ""
	if msg.ChatType == "p2p" && w.supervisor.personalVault != nil {
		personalID, err = w.supervisor.personalVault(ctx, w.record, msg.UserID)
		if err != nil {
			return "", err
		}
	}
	sessionID, err := w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return "", errors.Join(errChannelSessionUnconfirmed, err)
	}
	if err == nil && sessionID != "" {
		session, sessionErr := w.supervisor.oma.getSession(ctx, apiKey, sessionID)
		if sessionErr != nil {
			return "", errors.Join(errChannelSessionUnconfirmed, sessionErr)
		}
		if session.ArchivedAt == nil && w.compatibleSessionConfiguration(session, environmentID) {
			return sessionID, nil
		}
	}
	resources, err := channelSessionResources(ctx, w.supervisor.cipher, w.record)
	if err != nil {
		return "", err
	}
	metadata := channelSessionMetadata(w.record, msg, scopeKey)
	metadata["channel_resources"] = resources.Fingerprint
	if personalID != "" {
		resources.VaultIDs = append(resources.VaultIDs, personalID)
	}
	created, err := w.supervisor.oma.createSession(ctx, apiKey, w.record.AgentExternalID, parseChannelMounts(w.record.Mounts).AgentVersion, environmentID, channelSessionTitle(msg.Content), metadata, resources.Resources, resources.VaultIDs)
	if err != nil {
		return "", err
	}
	if err := w.supervisor.store.SetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey, created); err != nil {
		return "", err
	}
	return created, nil
}

func channelSessionMetadata(record db.ChannelRecord, msg *channelMessage, scopeKey string) map[string]string {
	metadata := map[string]string{"source": record.ChannelType, "channel_id": record.ExternalID, "app_id": record.AppID, "scope_id": scopeKey, "chat_type": channelChatType(msg.ChatType), "actor_id": msg.UserID}
	if version := parseChannelMounts(record.Mounts).AgentVersion; version > 0 {
		metadata["agent_version"] = strconv.Itoa(version)
	}
	if record.ChannelType == "feishu" {
		metadata["open_id"] = msg.UserID
	}
	return metadata
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
	pairingRejected
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
		if pairing.Status == "rejected" {
			return pairingRejected, nil
		}
		return pairingAwaiting, nil
	}
	return pairingMissing, nil
}

func (w *channelWorker) authorize(ctx context.Context, msg *channelMessage) (bool, error) {
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
	case pairingRejected:
		return false, w.replyText(ctx, msg, "当前聊天未获授权，请联系管理员重新批准配对。")
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

func newChannelWorker(supervisor *runtimeSupervisor, record db.ChannelRecord, secret string) (*channelWorker, error) {
	if record.ChannelType == "" {
		record.ChannelType = "feishu"
	}
	factory, ok := supervisor.adapters[record.ChannelType]
	if !ok {
		return nil, unsupportedChannelType(record.ChannelType)
	}
	worker := &channelWorker{checkedInputs: make(map[string]string), deliveryOwner: uuid.NewV4().String(), supervisor: supervisor, record: record, scopes: make(map[string]*scopeQueue), replies: make(map[string][]*channelReplyTask)}
	adapter, err := factory(worker, secret)
	if err != nil {
		return nil, err
	}
	worker.adapter = adapter
	return worker, nil
}

func (w *channelWorker) replyText(ctx context.Context, msg *channelMessage, text string) error {
	return w.adapter.replyText(ctx, msg, text)
}
