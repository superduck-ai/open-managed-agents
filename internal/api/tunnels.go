package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/mcpcatalogs"
	"github.com/superduck-ai/open-managed-agents/internal/platformapi"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

func (s *Server) configureTunnels(catalog *mcpcatalogs.Handler, logger *slog.Logger) {
	// Lightweight routing tests omit DB and Broker; production supplies both.
	if s.db == nil {
		return
	}
	service := tunnels.NewService(s.cfg.Tunnel, s.db, s.vaultSecrets, s.tunnelBroker)
	prober := tunnelCatalogProber{service: service}
	catalog.WithTunnelProber(prober.recognize, prober.probe)
	s.tunnels = tunnels.NewHandler(service, logger.With("component", "tunnels"))
	s.consoleTunnels = tunnels.NewConsoleHandler(
		service, s.tunnelBroker, s.resolveTunnelConsoleScope,
		logger.With("component", "console_mcp_tunnels"),
	)
	if s.tunnelBroker == nil {
		return
	}
	s.connector = tunnels.NewConnectorHandler(s.cfg.Tunnel, s.db, s.tunnelBroker, logger.With("component", "tunnel_connector"))
	s.tunnelIngress = tunnels.NewIngressHandler(s.cfg.Tunnel, s.db, s.tunnelBroker, logger.With("component", "tunnel_ingress"))
	s.codeSessions.WithTunnelInvoker(s.tunnelIngress)
}

func (s *Server) resolveTunnelConsoleScope(w http.ResponseWriter, r *http.Request) (tunnels.ConsoleScope, bool) {
	scope, ok := platformapi.ResolveConsoleWorkspaceRequest(w, r, s.db)
	if !ok {
		return tunnels.ConsoleScope{}, false
	}
	principal, _ := auth.PrincipalFromContext(r.Context()) // The Console resolver already requires a principal.
	principal, accessErr := s.resolvePlatformWorkspace(r, principal, scope.WorkspaceUUID)
	if accessErr != nil {
		httpapi.WriteError(w, r, accessErr)
		return tunnels.ConsoleScope{}, false
	}
	return tunnels.ConsoleScope{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID}, true
}

// tunnelCatalogProber adapts the Tunnel service to the catalog boundary.
type tunnelCatalogProber struct {
	service *tunnels.Service
}

func (p tunnelCatalogProber) recognize(ctx context.Context, organizationUUID, workspaceUUID, endpoint string) (bool, error) {
	_, recognized, err := p.service.ResolveProbeTarget(ctx, tunnels.ConsoleScope{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
	}, endpoint)
	return recognized, err
}

func (p tunnelCatalogProber) probe(ctx context.Context, organizationUUID, workspaceUUID, endpoint string) ([]mcpcatalogs.CatalogTool, bool, error) {
	result, recognized, err := p.service.ProbeTarget(ctx, tunnels.ConsoleScope{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
	}, endpoint)
	tools := make([]mcpcatalogs.CatalogTool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, mcpcatalogs.CatalogTool{
			Name: tool.Name, Title: tool.Title, Description: tool.Description,
		})
	}
	return tools, recognized, err
}
