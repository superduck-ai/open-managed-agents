package channels

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type Service struct {
	oauthProviders vaults.EnvOAuthProviderResolver
	startMCPOAuth  func(context.Context, OAuthTarget) (string, error)
	agent          http.Handler
	workspace      http.Handler
	supervisor     *runtimeSupervisor
	local          *localService
	store          *db.DB
	logger         *slog.Logger
}

func NewService(cfg config.Config, store *db.DB, logger *slog.Logger, cipher *secrets.Service, deps Dependencies) *Service {
	logger = logging.LoggerOrDefault(logger)
	oma := newOMAClient(omaBaseURL(cfg))
	supervisor := newRuntimeSupervisor(store, cipher, oma, logger)
	supervisor.adapters = deps.Adapters
	supervisor.oauthOrigin = cfg.Auth.ConsoleURL
	supervisor.fileMaxBytes = cfg.Storage.MaxFileBytes
	service := newLocalService(store, cipher, supervisor, logger, deps)
	handler := newLocalHandler(service, logger)
	result := &Service{
		agent:      handler.agentRouter(),
		workspace:  handler.workspaceRouter(),
		supervisor: supervisor,
		local:      service,
		store:      store,
		logger:     logger,
	}
	supervisor.personalVault = result.ensurePersonalVault
	supervisor.personalVaultLookup = result.lookupPersonalVault
	return result
}

func (s *Service) WithIdentitySessionCreator(create func(context.Context, string, []byte, db.InsertSessionIdentityParams) (string, error)) {
	s.supervisor.createIdentitySession = create
}

func (s *Service) AgentHandler() http.Handler {
	return s.agent
}

func (s *Service) WorkspaceHandler() http.Handler {
	return s.workspace
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || s.supervisor == nil || s.store == nil {
		return
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(ctx, "restore channel runtimes", "panic", recovered)
			}
		}()
		s.supervisor.restore(ctx)
	}()
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	if s.local != nil {
		s.local.Stop()
	}
	if s.supervisor != nil {
		s.supervisor.stopAll()
	}
}

func omaBaseURL(cfg config.Config) string {
	if value := strings.TrimSpace(cfg.Channels.OMAURL); value != "" {
		return value
	}
	addr := strings.TrimSpace(cfg.Server.Addr)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr
}

type SessionEventSubscription interface {
	ReceiveUntil(context.Context, <-chan time.Time) (sessions.StreamEvent, error)
	Close()
}

type SessionEventSubscriber func(context.Context, string, string, sessions.StreamOptions) (SessionEventSubscription, error)

func (s *Service) WithSessionEvents(subscribe SessionEventSubscriber) *Service {
	s.supervisor.subscribeEvents = subscribe
	return s
}

type SessionAttachmentWriter func(context.Context, string, string, string, string, io.Reader) (db.FileRecord, error)

func (s *Service) WithSessionAttachments(put SessionAttachmentWriter) *Service {
	s.supervisor.putAttachment = put
	return s
}
