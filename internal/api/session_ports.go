package api

import (
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/portgateway"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
)

func (s *Server) configureSessionPorts(deps ServerDeps, codeSessionService *codesessions.Service, tunnelLogger, gatewayLogger *slog.Logger) *portgateway.Gateway {
	s.sessions.WithSessionPorts(deps.SessionPorts)
	s.codeSessions.WithSessionPorts(deps.SessionPorts)
	if !deps.SessionPorts.Enabled() || deps.SessionPortRegistry == nil {
		return nil
	}
	ports := deps.Config.SessionPorts
	s.sessionPortTunnel = porttunnel.NewHandler(codeSessionService, deps.SessionPortRegistry, porttunnel.Limits{
		KeepaliveInterval:     ports.KeepaliveInterval,
		MaxConcurrentRequests: ports.MaxConcurrentRequests,
		MaxWebSockets:         ports.MaxWebSockets,
	}, tunnelLogger)
	return portgateway.New(portgateway.Deps{
		Settings:       deps.SessionPorts,
		Config:         ports,
		SandboxTimeout: deps.Config.E2B.SandboxTimeout,
		Store:          deps.DB,
		Router:         sessionPortRouter(deps),
		Sandboxes:      deps.SandboxTimeoutExtender,
		Logger:         gatewayLogger,
	})
}

func sessionPortRouter(deps ServerDeps) porttunnel.Router {
	if deps.SessionPortRouter != nil {
		return deps.SessionPortRouter
	}
	return deps.SessionPortRegistry
}
