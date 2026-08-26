package tunnels

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
)

type ConsoleScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
}

type ConsoleScopeResolver func(http.ResponseWriter, *http.Request) (ConsoleScope, bool)

type ConsoleHandler struct {
	service      *Service
	broker       *Broker
	logger       *slog.Logger
	resolveScope ConsoleScopeResolver
	router       chi.Router
}

type consoleTunnelResponse struct {
	tunnelResponse
	MCPURL     string            `json:"mcp_url"`
	Connection ConnectorSnapshot `json:"connection"`
}

type consoleEndpoint func(http.ResponseWriter, *http.Request) error

func NewConsoleHandler(service *Service, broker *Broker, resolveScope ConsoleScopeResolver, logger *slog.Logger) *ConsoleHandler {
	if service == nil || resolveScope == nil {
		panic("tunnels: console service and scope resolver are required")
	}
	handler := &ConsoleHandler{
		service: service, broker: broker, resolveScope: resolveScope,
		logger: logging.LoggerOrDefault(logger),
	}
	router := chi.NewRouter()
	router.Get("/", handler.wrap(handler.list))
	router.Post("/", handler.wrap(handler.create))
	router.Route("/{tunnel_id}", func(r chi.Router) {
		r.Get("/", handler.wrap(handler.get))
		r.Post("/archive", handler.wrap(handler.archive))
		r.Post("/probe", handler.wrap(handler.probe))
		r.Post("/reveal_token", handler.wrap(handler.revealToken))
		r.Post("/rotate_token", handler.wrap(handler.rotateToken))
	})
	router.NotFound(handler.wrap(func(http.ResponseWriter, *http.Request) error { return routeNotFound() }))
	router.MethodNotAllowed(handler.wrap(func(http.ResponseWriter, *http.Request) error { return routeNotFound() }))
	handler.router = router
	return handler
}

type probeTunnelRequest struct {
	Channel string `json:"channel"`
}

func (h *ConsoleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *ConsoleHandler) wrap(next consoleEndpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := next(w, r); err != nil {
			h.writeError(w, r, err)
		}
	}
}

func (h *ConsoleHandler) list(w http.ResponseWriter, r *http.Request) error {
	scope, ok := h.resolveScope(w, r)
	if !ok {
		return nil
	}
	includeArchived, err := parseIncludeArchived(r)
	if err != nil {
		return invalidRequest(err)
	}
	tunnelScope := tunnelScope{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID}
	records, _, err := h.service.List(r.Context(), tunnelScope, includeArchived, maxListLimit, 0)
	if err != nil {
		return err
	}
	connections := h.connectorSnapshots(r, records)
	responses := make([]consoleTunnelResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, consoleResponseFromTunnel(r, h.service.cfg, record, connections[record.UUID]))
	}
	httpapi.WriteJSON(w, http.StatusOK, responses)
	return nil
}

func (h *ConsoleHandler) get(w http.ResponseWriter, r *http.Request) error {
	scope, ok := h.resolveScope(w, r)
	if !ok {
		return nil
	}
	record, err := h.service.Get(r.Context(), tunnelScope{
		OrganizationUUID: scope.OrganizationUUID,
		WorkspaceUUID:    scope.WorkspaceUUID,
	}, chi.URLParam(r, "tunnel_id"))
	if err != nil {
		return err
	}
	connections := h.connectorSnapshots(r, []db.MCPTunnel{record})
	httpapi.WriteJSON(w, http.StatusOK, consoleResponseFromTunnel(r, h.service.cfg, record, connections[record.UUID]))
	return nil
}

func (h *ConsoleHandler) create(w http.ResponseWriter, r *http.Request) error {
	scope, ok := h.resolveScope(w, r)
	if !ok {
		return nil
	}
	request, err := httpapi.DecodeObjectBodyAs[createTunnelRequest](w, r, maxManagementBody)
	if err != nil {
		return invalidRequest(err)
	}
	displayName, err := normalizeDisplayName(request.DisplayName)
	if err != nil {
		return invalidRequest(err)
	}
	record, err := h.service.Create(r.Context(), createTunnelInput{
		OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, DisplayName: displayName,
	})
	if err != nil {
		return err
	}
	logScopedManagementSuccess(h.logger, r, "mcp tunnel created", record.ExternalID, scope.OrganizationUUID, scope.WorkspaceUUID)
	httpapi.WriteJSON(w, http.StatusOK, consoleResponseFromTunnel(r, h.service.cfg, record, disconnectedSnapshot()))
	return nil
}

func (h *ConsoleHandler) archive(w http.ResponseWriter, r *http.Request) error {
	scope, ok := h.resolveScope(w, r)
	if !ok {
		return nil
	}
	record, err := h.service.Archive(r.Context(), tunnelScope{
		OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID,
	}, chi.URLParam(r, "tunnel_id"))
	if err != nil {
		return err
	}
	logScopedManagementSuccess(h.logger, r, "mcp tunnel archived", record.ExternalID, scope.OrganizationUUID, scope.WorkspaceUUID)
	httpapi.WriteJSON(w, http.StatusOK, consoleResponseFromTunnel(r, h.service.cfg, record, disconnectedSnapshot()))
	return nil
}

func (h *ConsoleHandler) probe(w http.ResponseWriter, r *http.Request) error {
	scope, ok := h.resolveScope(w, r)
	if !ok {
		return nil
	}
	request, err := httpapi.DecodeObjectBodyAs[probeTunnelRequest](w, r, maxManagementBody)
	if err != nil {
		return invalidRequest(err)
	}
	result, err := h.service.Probe(r.Context(), tunnelScope{
		OrganizationUUID: scope.OrganizationUUID,
		WorkspaceUUID:    scope.WorkspaceUUID,
	}, chi.URLParam(r, "tunnel_id"), request.Channel)
	if err != nil {
		return err
	}
	logScopedManagementSuccess(h.logger, r, "mcp tunnel probe succeeded", chi.URLParam(r, "tunnel_id"), scope.OrganizationUUID, scope.WorkspaceUUID)
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *ConsoleHandler) revealToken(w http.ResponseWriter, r *http.Request) error {
	scope, ok := h.resolveScope(w, r)
	if !ok {
		return nil
	}
	token, plaintext, err := h.service.RevealToken(r.Context(), tunnelScope{
		OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID,
	}, chi.URLParam(r, "tunnel_id"))
	if err != nil {
		return err
	}
	defer clear(plaintext)
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, tokenResponse(token, plaintext))
	return nil
}

func (h *ConsoleHandler) rotateToken(w http.ResponseWriter, r *http.Request) error {
	scope, ok := h.resolveScope(w, r)
	if !ok {
		return nil
	}
	request, err := httpapi.DecodeObjectBodyAs[rotateTunnelTokenRequest](w, r, maxManagementBody)
	if err != nil {
		return invalidRequest(err)
	}
	_ = request.Reason
	tunnelID := chi.URLParam(r, "tunnel_id")
	token, plaintext, err := h.service.RotateToken(r.Context(), tunnelScope{
		OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID,
	}, tunnelID)
	if err != nil {
		return err
	}
	defer clear(plaintext)
	logScopedManagementSuccess(h.logger, r, "mcp tunnel token rotated", tunnelID, scope.OrganizationUUID, scope.WorkspaceUUID)
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, tokenResponse(token, plaintext))
	return nil
}

func (h *ConsoleHandler) connectorSnapshots(r *http.Request, records []db.MCPTunnel) map[string]ConnectorSnapshot {
	snapshots := make(map[string]ConnectorSnapshot, len(records))
	ids := make([]string, 0, len(records))
	for _, record := range records {
		snapshots[record.UUID] = unknownSnapshot()
		ids = append(ids, record.UUID)
	}
	if h.broker == nil || len(ids) == 0 {
		return snapshots
	}
	resolved, err := h.broker.ConnectorSnapshots(r.Context(), ids)
	if err != nil {
		h.logger.WarnContext(r.Context(), "read mcp tunnel connector status failed",
			"request_id", httpapi.RequestID(r.Context()), "error", err)
		return snapshots
	}
	return resolved
}

func consoleResponseFromTunnel(r *http.Request, cfg config.TunnelConfig, record db.MCPTunnel, connection ConnectorSnapshot) consoleTunnelResponse {
	return consoleTunnelResponse{
		tunnelResponse: responseFromTunnel(record),
		MCPURL:         canonicalTunnelMCPURL(r, cfg, record.ExternalID, "main"),
		Connection:     connection,
	}
}

func disconnectedSnapshot() ConnectorSnapshot {
	return ConnectorSnapshot{State: "disconnected", Channels: []ConnectorChannelSnapshot{}}
}

func unknownSnapshot() ConnectorSnapshot {
	return ConnectorSnapshot{State: "unknown", Channels: []ConnectorChannelSnapshot{}}
}

func (h *ConsoleHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := consoleErrorResponse(err)
	if status >= http.StatusInternalServerError {
		h.logger.ErrorContext(r.Context(), "console mcp tunnel request failed",
			"request_id", httpapi.RequestID(r.Context()), "method", r.Method, "path", r.URL.Path, "error", err)
	}
	httpapi.WriteJSON(w, status, map[string]any{"error": code, "message": message})
}

func consoleErrorResponse(err error) (int, string, string) {
	appError, ok := errors.AsType[*apperr.Error](err)
	if !ok || appError.PublicMessage == "" {
		return http.StatusInternalServerError, "internal_error", "Internal server error"
	}
	switch appError.Kind {
	case apperr.InvalidArgument:
		return http.StatusBadRequest, "invalid_request", appError.PublicMessage
	case apperr.Unauthenticated:
		return http.StatusUnauthorized, "authentication_error", appError.PublicMessage
	case apperr.PermissionDenied:
		return http.StatusForbidden, "permission_denied", appError.PublicMessage
	case apperr.NotFound:
		return http.StatusNotFound, "not_found", appError.PublicMessage
	case apperr.Conflict, apperr.InvalidState, apperr.PreconditionFailed:
		return http.StatusConflict, "conflict", appError.PublicMessage
	case apperr.RateLimited:
		return http.StatusTooManyRequests, "rate_limited", appError.PublicMessage
	case apperr.Timeout:
		return http.StatusGatewayTimeout, "timeout", appError.PublicMessage
	case apperr.Unavailable, apperr.Overloaded:
		return http.StatusServiceUnavailable, "unavailable", appError.PublicMessage
	default:
		return http.StatusInternalServerError, "internal_error", appError.PublicMessage
	}
}
