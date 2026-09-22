package tunnels

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
)

const (
	currentBeta       = "mcp-tunnels-2026-06-22"
	legacyBeta        = "mcp-tunnels-2026-05-19"
	maxManagementBody = 16 << 10
	defaultListLimit  = 20
	maxListLimit      = 1000
)

type Handler struct {
	service      *Service
	logger       *slog.Logger
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if service == nil {
		panic("tunnels: management service is required")
	}
	logger = logging.LoggerOrDefault(logger)
	handler := &Handler{
		service:      service,
		logger:       logger,
		errorAdapter: httpapi.NewErrorAdapter(logger),
	}
	router := chi.NewRouter()
	wrap := handler.errorAdapter.Wrap
	router.NotFound(wrap(handler.notFound))
	router.MethodNotAllowed(wrap(handler.notFound))
	router.Post("/", wrap(handler.createTunnel))
	router.Get("/", wrap(handler.listTunnels))
	router.Route("/{tunnel_id}", func(r chi.Router) {
		r.Get("/", wrap(handler.retrieveTunnel))
		r.Post("/archive", wrap(handler.archiveTunnel))
		r.Post("/reveal_token", wrap(handler.revealTunnelToken))
		r.Post("/rotate_token", wrap(handler.rotateTunnelToken))
		r.Route("/certificates", func(r chi.Router) {
			r.Get("/", wrap(handler.listCertificates))
			r.Post("/", wrap(handler.createCertificate))
			r.Get("/{certificate_id}", wrap(handler.retrieveCertificate))
			r.Post("/{certificate_id}/archive", wrap(handler.archiveCertificate))
		})
	})
	handler.router = router
	return handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !hasTunnelBeta(r.Header.Values("anthropic-beta")) {
		h.errorAdapter.Write(w, r, betaRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return routeNotFound()
}

func (h *Handler) createTunnel(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireAPIKey(r)
	if err != nil {
		return err
	}
	request, err := httpapi.DecodeObjectBodyAs[createTunnelRequest](w, r, maxManagementBody)
	if err != nil {
		return invalidRequest(err)
	}
	displayName, err := normalizeDisplayName(request.DisplayName)
	if err != nil {
		return invalidRequest(err)
	}
	tunnel, err := h.service.Create(r.Context(), createTunnelInput{
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		DisplayName:      displayName,
	})
	if err != nil {
		return err
	}
	h.logManagementSuccess(r, "mcp tunnel created", tunnel.ExternalID)
	httpapi.WriteJSON(w, http.StatusOK, responseFromTunnel(tunnel))
	return nil
}

func (h *Handler) retrieveTunnel(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	tunnel, err := h.service.Get(r.Context(), scope, chi.URLParam(r, "tunnel_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromTunnel(tunnel))
	return nil
}

func (h *Handler) listTunnels(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	limit, err := parseListLimit(r)
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseIncludeArchived(r)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := parseCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	tunnels, hasMore, err := h.service.List(r.Context(), scope, includeArchived, limit, offset)
	if err != nil {
		return err
	}
	var nextPage *string
	if hasMore {
		cursor, cursorErr := marshalCursor(offset + limit)
		if cursorErr != nil {
			return internalError("Could not encode tunnels page", fmt.Errorf("encode tunnels page: %w", cursorErr))
		}
		nextPage = &cursor
	}
	httpapi.WriteJSON(w, http.StatusOK, tunnelPageResponse{
		Data:     responsesFromTunnels(tunnels),
		NextPage: nextPage,
	})
	return nil
}

func (h *Handler) archiveTunnel(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	tunnel, err := h.service.Archive(r.Context(), scope, chi.URLParam(r, "tunnel_id"))
	if err != nil {
		return err
	}
	h.logManagementSuccess(r, "mcp tunnel archived", tunnel.ExternalID)
	httpapi.WriteJSON(w, http.StatusOK, responseFromTunnel(tunnel))
	return nil
}

func (h *Handler) revealTunnelToken(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	token, plaintext, err := h.service.RevealToken(r.Context(), scope, chi.URLParam(r, "tunnel_id"))
	if err != nil {
		return err
	}
	defer clear(plaintext)
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, tokenResponse(token, plaintext))
	return nil
}

func (h *Handler) rotateTunnelToken(w http.ResponseWriter, r *http.Request) error {
	scope, err := scopeFromRequest(r)
	if err != nil {
		return err
	}
	request, err := httpapi.DecodeObjectBodyAs[rotateTunnelTokenRequest](w, r, maxManagementBody)
	if err != nil {
		return invalidRequest(err)
	}
	// Claude accepts reason for audit purposes. OMA keeps the wire contract but does
	// not persist or log it until management audit events have a shared policy.
	_ = request.Reason
	token, plaintext, err := h.service.RotateToken(r.Context(), scope, chi.URLParam(r, "tunnel_id"))
	if err != nil {
		return err
	}
	defer clear(plaintext)
	h.logManagementSuccess(r, "mcp tunnel token rotated", chi.URLParam(r, "tunnel_id"))
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, tokenResponse(token, plaintext))
	return nil
}

func requireAPIKey(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.CredentialType != auth.CredentialTypeAPIKey ||
		principal.OrganizationUUID == "" || principal.WorkspaceUUID == "" {
		return auth.Principal{}, missingAPIKey()
	}
	return principal, nil
}

func scopeFromRequest(r *http.Request) (tunnelScope, error) {
	principal, err := requireAPIKey(r)
	if err != nil {
		return tunnelScope{}, err
	}
	return tunnelScope{
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
	}, nil
}

func normalizeDisplayName(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	length := utf8.RuneCountInString(normalized)
	if length < 1 || length > 255 {
		return nil, errors.New("display_name must be between 1 and 255 characters")
	}
	return &normalized, nil
}

func parseListLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultListLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxListLimit {
		return 0, errors.New("limit must be between 1 and 1000")
	}
	return limit, nil
}

func parseIncludeArchived(r *http.Request) (bool, error) {
	raw := r.URL.Query().Get("include_archived")
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("include_archived must be a boolean")
	}
	return value, nil
}

func parseCursor(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, errors.New("page must be a valid pagination cursor")
	}
	var cursor offsetCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Offset < 0 {
		return 0, errors.New("page must be a valid pagination cursor")
	}
	return cursor.Offset, nil
}

func hasTunnelBeta(values []string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			switch strings.TrimSpace(part) {
			case currentBeta, legacyBeta:
				return true
			}
		}
	}
	return false
}
