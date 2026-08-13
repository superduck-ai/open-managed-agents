package mcpservers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/mcpcatalogs"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxBodySize = 64 << 10

var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type Store interface {
	CreateWorkspaceMCPServer(context.Context, db.WorkspaceMCPServer) (db.WorkspaceMCPServer, error)
	ListWorkspaceMCPServersPage(context.Context, db.ListWorkspaceMCPServersPageParams) ([]db.WorkspaceMCPServer, bool, error)
	GetWorkspaceMCPServer(context.Context, string, string) (db.WorkspaceMCPServer, error)
	UpdateWorkspaceMCPServer(context.Context, string, string, db.WorkspaceMCPServer) (db.WorkspaceMCPServer, error)
	ArchiveWorkspaceMCPServer(context.Context, string, string) (db.WorkspaceMCPServer, error)
	DeleteWorkspaceMCPServer(context.Context, string, string) error
}

type Handler struct {
	store  Store
	logger *slog.Logger
}

type mutationRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type serverResponse struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Name       string  `json:"name"`
	Transport  string  `json:"transport_type"`
	URL        string  `json:"url"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	ArchivedAt *string `json:"archived_at"`
}

type pageResponse struct {
	Data     []serverResponse `json:"data"`
	NextPage *string          `json:"next_page"`
}

func NewHandler(store Store, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	return &Handler{store: store, logger: logger}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/workspaces/{workspaceId}/mcp_servers", h.wrap(h.create))
	r.Get("/workspaces/{workspaceId}/mcp_servers", h.wrap(h.list))
	r.Get("/workspaces/{workspaceId}/mcp_servers/{mcpServerId}", h.wrap(h.retrieve))
	r.Post("/workspaces/{workspaceId}/mcp_servers/{mcpServerId}", h.wrap(h.update))
	r.Post("/workspaces/{workspaceId}/mcp_servers/{mcpServerId}/archive", h.wrap(h.archive))
	r.Delete("/workspaces/{workspaceId}/mcp_servers/{mcpServerId}", h.wrap(h.delete))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	input, err := httpapi.DecodeObjectBodyAs[mutationRequest](w, r, maxBodySize)
	if err != nil {
		return invalidJSONRequest(err)
	}
	name, endpointURL, err := normalizeInput(*input)
	if err != nil {
		return err
	}
	externalID, err := ids.New("mcpsrv_")
	if err != nil {
		return mapStoreError(err, "create")
	}
	now := time.Now().UTC()
	created, err := h.store.CreateWorkspaceMCPServer(r.Context(), db.WorkspaceMCPServer{
		UUID:             uuid.NewString(),
		ExternalID:       externalID,
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		Name:             name,
		TransportType:    "url",
		EndpointURL:      endpointURL,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return mapStoreError(err, "create")
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromServer(created))
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return invalidLimit(err)
	}
	cursor, err := decodeCursor(r.URL.Query().Get("page"))
	if err != nil {
		return err
	}
	includeArchived, err := optionalBool(r.URL.Query().Get("include_archived"))
	if err != nil {
		return err
	}
	servers, hasMore, err := h.store.ListWorkspaceMCPServersPage(r.Context(), db.ListWorkspaceMCPServersPageParams{
		WorkspaceUUID: principal.WorkspaceUUID, Search: strings.TrimSpace(r.URL.Query().Get("search")),
		Limit: limit, Cursor: cursor, IncludeArchived: includeArchived,
	})
	if err != nil {
		return mapStoreError(err, "list")
	}
	data := make([]serverResponse, 0, len(servers))
	for _, server := range servers {
		data = append(data, responseFromServer(server))
	}
	var nextPage *string
	if hasMore && len(servers) > 0 {
		value := encodeCursor(servers[len(servers)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	server, err := h.store.GetWorkspaceMCPServer(r.Context(), principal.WorkspaceUUID, chi.URLParam(r, "mcpServerId"))
	if err != nil {
		return mapStoreError(err, "load")
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromServer(server))
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	input, err := httpapi.DecodeObjectBodyAs[mutationRequest](w, r, maxBodySize)
	if err != nil {
		return invalidJSONRequest(err)
	}
	name, endpointURL, err := normalizeInput(*input)
	if err != nil {
		return err
	}
	updated, err := h.store.UpdateWorkspaceMCPServer(r.Context(), principal.WorkspaceUUID, chi.URLParam(r, "mcpServerId"), db.WorkspaceMCPServer{
		OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
		Name: name, TransportType: "url", EndpointURL: endpointURL, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return mapStoreError(err, "update")
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromServer(updated))
	return nil
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	server, err := h.store.ArchiveWorkspaceMCPServer(r.Context(), principal.WorkspaceUUID, chi.URLParam(r, "mcpServerId"))
	if err != nil {
		return mapStoreError(err, "archive")
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromServer(server))
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	externalID := chi.URLParam(r, "mcpServerId")
	if err := h.store.DeleteWorkspaceMCPServer(r.Context(), principal.WorkspaceUUID, externalID); err != nil {
		return mapStoreError(err, "delete")
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"id": externalID, "type": "mcp_server_deleted", "deleted": true})
	return nil
}

func scopedPrincipal(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.OrganizationUUID) == "" || strings.TrimSpace(principal.WorkspaceUUID) == "" {
		return auth.Principal{}, resourceNotFound()
	}
	if !organizationPathMatches(r, principal, chi.URLParam(r, "orgUuid")) || !workspacePathMatches(principal, chi.URLParam(r, "workspaceId")) {
		return auth.Principal{}, resourceNotFound()
	}
	return principal, nil
}

func organizationPathMatches(r *http.Request, principal auth.Principal, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if value == strings.TrimSpace(principal.OrganizationUUID) {
		return true
	}
	alias := strings.TrimSpace(auth.PlatformMirrorOrganizationAliasFromContext(r.Context()))
	return alias != "" && alias == value && isPlatformClaudeHost(r.Host)
}

func isPlatformClaudeHost(requestHost string) bool {
	host := strings.TrimSpace(strings.ToLower(requestHost))
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	return host == "platform.claude.com" || strings.HasSuffix(host, ".platform.claude.com")
}

func workspacePathMatches(principal auth.Principal, value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && (value == "default" || value == principal.WorkspaceUUID || value == principal.WorkspaceExternalID)
}

func normalizeInput(input mutationRequest) (string, string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return "", "", nameRequired()
	}
	if len(name) > 255 {
		return "", "", nameTooLong()
	}
	if !serverNamePattern.MatchString(name) || strings.Contains(name, "__") {
		return "", "", invalidNameFormat()
	}
	rawEndpointURL := strings.TrimSpace(input.URL)
	if len(rawEndpointURL) > 2048 {
		return "", "", endpointTooLong()
	}
	endpointURL, err := mcpcatalogs.NormalizeEndpoint(rawEndpointURL)
	if err != nil {
		return "", "", invalidEndpoint(err)
	}
	return name, endpointURL, nil
}

func responseFromServer(server db.WorkspaceMCPServer) serverResponse {
	status := "active"
	if server.ArchivedAt != nil {
		status = "archived"
	}
	return serverResponse{
		ID:         server.ExternalID,
		Type:       "mcp_server",
		Name:       server.Name,
		Transport:  server.TransportType,
		URL:        server.EndpointURL,
		Status:     status,
		CreatedAt:  server.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:  server.UpdatedAt.Format(time.RFC3339Nano),
		ArchivedAt: optionalTime(server.ArchivedAt),
	}
}

func optionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339Nano)
	return &formatted
}

func optionalBool(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, invalidArchivedFilter(err)
	}
	return value, nil
}

func encodeCursor(server db.WorkspaceMCPServer) string {
	payload, _ := json.Marshal(struct {
		CreatedAt time.Time `json:"created_at"`
		UUID      string    `json:"uuid"`
	}{CreatedAt: server.CreatedAt, UUID: server.UUID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(raw string) (*db.WorkspaceMCPServerPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalidPage(err)
	}
	var wire struct {
		CreatedAt time.Time `json:"created_at"`
		UUID      string    `json:"uuid"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, invalidPage(err)
	}
	if wire.CreatedAt.IsZero() {
		return nil, invalidPage(errInvalidPageCursor)
	}
	parsedUUID, err := uuid.Parse(strings.TrimSpace(wire.UUID))
	if err != nil {
		return nil, invalidPage(err)
	}
	return &db.WorkspaceMCPServerPageCursor{CreatedAt: wire.CreatedAt, UUID: parsedUUID.String()}, nil
}
