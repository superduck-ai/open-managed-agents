package channels

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type Service struct {
	agent      http.Handler
	workspace  http.Handler
	supervisor *runtimeSupervisor
}

func NewService(cfg config.Config, store *db.DB, logger *slog.Logger, cipher *secrets.Service) *Service {
	logger = logging.LoggerOrDefault(logger)
	if !localRuntimeEnabled(cfg) {
		return &Service{
			agent:     NewHandler(cfg.ChannelBridge, store, logger, cipher),
			workspace: NewWorkspaceHandler(cfg.ChannelBridge, store, logger, cipher),
		}
	}
	oma := newOMAClient(omaBaseURL(cfg))
	supervisor := newRuntimeSupervisor(store, cipher, oma, logger)
	service := newLocalService(store, cipher, supervisor, logger)
	handler := newLocalHandler(service, logger)
	return &Service{
		agent:      handler.agentRouter(),
		workspace:  handler.workspaceRouter(),
		supervisor: supervisor,
	}
}

func (s *Service) AgentHandler() http.Handler {
	return s.agent
}

func (s *Service) WorkspaceHandler() http.Handler {
	return s.workspace
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || s.supervisor == nil {
		return
	}
	s.supervisor.restore(ctx)
}

func (s *Service) Stop() {
	if s == nil || s.supervisor == nil {
		return
	}
	s.supervisor.stopAll()
}

func localRuntimeEnabled(cfg config.Config) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.Channels.Runtime)) {
	case "local":
		return true
	case "bridge":
		return false
	default:
		return cfg.ChannelBridge.URL == "" || cfg.ChannelBridge.Token == ""
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
