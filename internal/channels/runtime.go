package channels

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const (
	channelReplyMaintenanceInterval = time.Second
	channelReconcileInterval        = 30 * time.Second
	channelPairingLimit             = 1000
	channelTitleLimit               = 30
	channelAskAttempts              = 5
	channelAskRetryBase             = time.Second
)

type runtimeStore interface {
	ReceiveChannelInput(context.Context, db.ChannelInputScope, db.ChannelInput) (db.ChannelInput, bool, error)
	ClaimChannelInput(context.Context, db.ChannelInputScope, string, string) (db.ChannelInput, bool, error)
	ResumeChannelInputAfterPairing(context.Context, db.ChannelInputScope, db.ChannelPairingResume) (string, bool, error)
	ListRecoverableChannelInputs(context.Context, db.ChannelInputScope) ([]db.ChannelInput, error)
	PrepareChannelInputBatch(context.Context, db.ChannelInputScope, string, db.ChannelInputBatch) (db.ChannelInput, error)
	AdvanceChannelInput(context.Context, db.ChannelInputScope, string, string, db.ChannelInputAdvance) (db.ChannelInput, error)
	CompleteChannelInput(context.Context, db.ChannelInputScope, string, string) error
	ReleaseChannelInput(context.Context, db.ChannelInputScope, string, string) error
	RenewChannelInputs(context.Context, db.ChannelInputScope, string) error
	GetChannelReplyDeliveryByReceipt(context.Context, db.ChannelDeliveryScope, string) (db.ChannelReplyDelivery, error)
	PrepareChannelReplyDelivery(context.Context, db.ChannelDeliveryScope, db.ChannelReplyDelivery) (db.ChannelReplyDelivery, error)
	ClaimChannelReplyDelivery(context.Context, db.ChannelDeliveryScope, string, string) (db.ChannelReplyDelivery, bool, error)
	SaveChannelReplyReceipt(context.Context, db.ChannelDeliveryScope, string, string, string) error
	SaveChannelArtifactDelivery(context.Context, db.ChannelDeliveryScope, string, string, string) error
	RenewChannelReplyDelivery(context.Context, db.ChannelDeliveryScope, string, string) error
	ReleaseChannelReplyDelivery(context.Context, db.ChannelDeliveryScope, string, string) error
	CompleteChannelReplyDelivery(context.Context, db.ChannelDeliveryScope, string, string) error
	RetireChannelReplyDelivery(context.Context, db.ChannelDeliveryScope, string, string) error
	NextChannelReplyRecovery(context.Context, db.ChannelDeliveryScope, string) (time.Time, bool, error)
	ListChannelReplyDeliveries(context.Context, db.ChannelDeliveryScope) ([]db.ChannelReplyDelivery, error)
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
	personalVault       func(context.Context, db.ChannelRecord, string) (string, error)
	personalVaultLookup func(context.Context, db.ChannelRecord, string) (string, bool, error)
	agentEnvironment    func(context.Context, string, string, string) (string, error)
	oauthOrigin         string
	fileMaxBytes        int64
	putAttachment       SessionAttachmentWriter
	subscribeEvents     SessionEventSubscriber
	adapters            map[string]channelAdapterFactory
	store               runtimeStore
	cipher              *secrets.Service
	oma                 *omaClient
	logger              *slog.Logger
	mu                  sync.Mutex
	workers             map[string]*channelWorker
	shutdown            chan struct{}
	ingress             identityIngress
}

func newRuntimeSupervisor(store runtimeStore, cipher *secrets.Service, oma *omaClient, logger *slog.Logger) *runtimeSupervisor {
	supervisor := &runtimeSupervisor{
		adapters: make(map[string]channelAdapterFactory),
		store:    store,
		cipher:   cipher,
		oma:      oma,
		logger:   logging.LoggerOrDefault(logger),
		workers:  make(map[string]*channelWorker),
		shutdown: make(chan struct{}),
		agentEnvironment: func(ctx context.Context, organizationUUID, workspaceUUID, agentID string) (string, error) {
			return store.GetChannelAgentEnvironment(ctx, organizationUUID, workspaceUUID, agentID)
		},
	}
	return supervisor
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
			s.mu.Lock()
			worker := s.workers[record.ExternalID]
			s.mu.Unlock()
			if worker != nil {
				worker.restoreReplies(ctx)
			}
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
		return errors.New("未配置 channels.oma_url，无法启动渠道")
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

func (s *runtimeSupervisor) forget(channelID string, worker *channelWorker) {
	s.mu.Lock()
	current, ok := s.workers[channelID]
	if ok && current == worker {
		delete(s.workers, channelID)
	}
	s.mu.Unlock()
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

func (s *runtimeSupervisor) apiKey(ctx context.Context, record db.ChannelRecord) (string, error) {
	return s.store.EnsureChannelWorkspaceCredential(ctx, platform.CreateConsoleAPIKeyInput{
		OrgUUID:       record.OrganizationUUID,
		WorkspaceUUID: record.WorkspaceUUID,
	}, s.cipher)
}
