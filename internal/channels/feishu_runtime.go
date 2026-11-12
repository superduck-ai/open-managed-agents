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
	channelReplyPollInterval = time.Second
	channelReconcileInterval = 30 * time.Second
	channelReplyTimeout      = 30 * time.Minute
	channelPairingLimit      = 1000
	channelTitleLimit        = 30
	channelAskAttempts       = 5
	channelAskRetryBase      = time.Second
	channelListenerIdle      = 2 * time.Minute
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
