package mcpcatalogs

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	cfg      config.Config
	database *db.DB
}

type catalogResponse struct {
	ServerName          string          `json:"server_name"`
	EndpointFingerprint string          `json:"endpoint_fingerprint,omitempty"`
	Status              string          `json:"status"`
	Tools               json.RawMessage `json:"tools"`
	Source              *string         `json:"source,omitempty"`
	ProtocolVersion     *string         `json:"protocol_version,omitempty"`
	DiscoveredAt        *string         `json:"discovered_at,omitempty"`
	ExpiresAt           *string         `json:"expires_at,omitempty"`
	LastError           *catalogError   `json:"last_error,omitempty"`
	Generation          int64           `json:"generation"`
}

type catalogError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type catalogListResponse struct {
	Data    []catalogResponse `json:"data"`
	Version int               `json:"version"`
}

type refreshRequest struct {
	ServerNames []string `json:"server_names"`
}

type refreshResponse struct {
	Data []refreshResult `json:"data"`
}

type refreshResult struct {
	ServerName string `json:"server_name"`
	Generation int64  `json:"generation"`
	Queued     bool   `json:"queued"`
}

func NewHandler(cfg config.Config, database *db.DB) *Handler {
	return &Handler{cfg: cfg, database: database}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspaceId}/agents/{agentId}/mcp_tool_catalogs", h.list)
	r.Post("/workspaces/{workspaceId}/agents/{agentId}/mcp_tool_catalogs/refresh", h.refresh)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, agent, version, ok := h.authorizedAgent(w, r)
	if !ok {
		return
	}
	servers, err := ParseAgentServers(agent.MCPServers)
	if err != nil {
		writeCatalogError(w, r, http.StatusInternalServerError, "Could not read the Agent MCP configuration")
		return
	}
	now := time.Now().UTC()
	responses := make([]catalogResponse, 0, len(servers))
	for _, server := range servers {
		response, buildErr := h.ensureAndMap(r, principal, server, false, "detail_read", now)
		if buildErr != nil {
			log.Printf("list mcp catalog workspace_id=%d agent_id=%s server=%s: %v", principal.WorkspaceID, agent.ExternalID, server.Name, buildErr)
			writeCatalogError(w, r, http.StatusInternalServerError, "Could not load MCP tool catalogs")
			return
		}
		responses = append(responses, response)
	}
	httpapi.WriteJSON(w, http.StatusOK, catalogListResponse{Data: responses, Version: version})
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.MCPDiscoveryEnabled {
		writeCatalogError(w, r, http.StatusServiceUnavailable, "MCP tool discovery is disabled")
		return
	}
	principal, agent, _, ok := h.authorizedAgent(w, r)
	if !ok {
		return
	}
	var input refreshRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if r.Body != nil && r.Body != http.NoBody {
		if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			writeCatalogError(w, r, http.StatusBadRequest, "Invalid refresh request")
			return
		}
	}
	servers, err := ParseAgentServers(agent.MCPServers)
	if err != nil {
		writeCatalogError(w, r, http.StatusInternalServerError, "Could not read the Agent MCP configuration")
		return
	}
	selected := map[string]struct{}{}
	for _, name := range input.ServerNames {
		name = strings.TrimSpace(name)
		if name == "" {
			writeCatalogError(w, r, http.StatusBadRequest, "server_names must not contain empty names")
			return
		}
		selected[name] = struct{}{}
	}
	if len(selected) > 0 {
		known := map[string]struct{}{}
		for _, server := range servers {
			known[server.Name] = struct{}{}
		}
		for name := range selected {
			if _, exists := known[name]; !exists {
				writeCatalogError(w, r, http.StatusBadRequest, "Unknown MCP server name: "+name)
				return
			}
		}
	}
	results := make([]refreshResult, 0, len(servers))
	for _, server := range servers {
		if len(selected) > 0 {
			if _, include := selected[server.Name]; !include {
				continue
			}
		}
		normalized, normalizeErr := NormalizeEndpoint(server.URL)
		if normalizeErr != nil {
			writeCatalogError(w, r, http.StatusBadRequest, "MCP server "+server.Name+" has an invalid endpoint")
			return
		}
		ensure, ensureErr := h.database.EnsureMCPToolCatalog(r.Context(), db.EnsureMCPToolCatalogInput{
			OrganizationID: principal.OrganizationID,
			WorkspaceID:    principal.WorkspaceID,
			TransportType:  "url",
			EndpointURL:    normalized,
			EndpointKey:    EndpointKey(h.cfg.MCPDiscoveryHMACKey, normalized),
			AuthScopeKey:   "anonymous",
			Trigger:        "manual_refresh",
			Force:          true,
			Now:            time.Now().UTC(),
		})
		if ensureErr != nil {
			log.Printf("refresh mcp catalog workspace_id=%d agent_id=%s server=%s: %v", principal.WorkspaceID, agent.ExternalID, server.Name, ensureErr)
			writeCatalogError(w, r, http.StatusInternalServerError, "Could not refresh MCP tool catalogs")
			return
		}
		results = append(results, refreshResult{ServerName: server.Name, Generation: ensure.Catalog.RequestedGeneration, Queued: ensure.Queued})
	}
	httpapi.WriteJSON(w, http.StatusAccepted, refreshResponse{Data: results})
}

func (h *Handler) authorizedAgent(w http.ResponseWriter, r *http.Request) (auth.Principal, db.Agent, int, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principalCanSeeOrganization(principal, chi.URLParam(r, "orgUuid")) || !principalCanSeeWorkspace(principal, chi.URLParam(r, "workspaceId")) {
		writeCatalogError(w, r, http.StatusNotFound, "Agent not found")
		return auth.Principal{}, db.Agent{}, 0, false
	}
	version := 0
	if rawVersion := strings.TrimSpace(r.URL.Query().Get("version")); rawVersion != "" {
		parsed, err := strconv.Atoi(rawVersion)
		if err != nil || parsed < 1 {
			writeCatalogError(w, r, http.StatusBadRequest, "version must be at least 1")
			return auth.Principal{}, db.Agent{}, 0, false
		}
		version = parsed
	}
	var agent db.Agent
	var err error
	if version == 0 {
		agent, err = h.database.GetAgent(r.Context(), principal.WorkspaceID, chi.URLParam(r, "agentId"))
		version = agent.CurrentVersion
	} else {
		agent, err = h.database.GetAgentVersion(r.Context(), principal.WorkspaceID, chi.URLParam(r, "agentId"), version)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeCatalogError(w, r, http.StatusNotFound, "Agent not found")
		} else {
			log.Printf("get agent for mcp catalog: %v", err)
			writeCatalogError(w, r, http.StatusInternalServerError, "Could not load Agent")
		}
		return auth.Principal{}, db.Agent{}, 0, false
	}
	return principal, agent, version, true
}

func (h *Handler) ensureAndMap(r *http.Request, principal auth.Principal, server AgentServer, force bool, trigger string, now time.Time) (catalogResponse, error) {
	normalized, err := NormalizeEndpoint(server.URL)
	if err != nil {
		return catalogResponse{
			ServerName: server.Name,
			Status:     "error",
			Tools:      json.RawMessage("null"),
			LastError:  &catalogError{Code: "invalid_endpoint", Message: "The MCP endpoint is invalid."},
		}, nil
	}
	endpointKey := EndpointKey(h.cfg.MCPDiscoveryHMACKey, normalized)
	if !h.cfg.MCPDiscoveryEnabled {
		catalog, getErr := h.database.GetMCPToolCatalog(r.Context(), principal.OrganizationID, principal.WorkspaceID, endpointKey, "anonymous")
		if errors.Is(getErr, db.ErrNotFound) {
			return catalogResponse{ServerName: server.Name, EndpointFingerprint: endpointKey, Status: "unknown", Tools: json.RawMessage("null")}, nil
		}
		if getErr != nil {
			return catalogResponse{}, getErr
		}
		return mapCatalog(server.Name, endpointKey, catalog, now), nil
	}
	ensured, err := h.database.EnsureMCPToolCatalog(r.Context(), db.EnsureMCPToolCatalogInput{
		OrganizationID: principal.OrganizationID,
		WorkspaceID:    principal.WorkspaceID,
		TransportType:  "url",
		EndpointURL:    normalized,
		EndpointKey:    endpointKey,
		AuthScopeKey:   "anonymous",
		Trigger:        trigger,
		Force:          force,
		Now:            now,
	})
	if err != nil {
		return catalogResponse{}, err
	}
	return mapCatalog(server.Name, endpointKey, ensured.Catalog, now), nil
}

func mapCatalog(serverName, endpointKey string, catalog db.MCPToolCatalog, now time.Time) catalogResponse {
	status := "unknown"
	active := catalog.RequestedGeneration > catalog.SettledGeneration
	if catalog.Tools != nil {
		if active {
			status = "refreshing"
		} else if catalog.ExpiresAt != nil && catalog.ExpiresAt.After(now) && stringValue(catalog.LastResultStatus) == "success" {
			status = "ready"
		} else {
			status = "stale"
		}
	} else if active {
		status = "loading"
	} else if stringValue(catalog.LastResultStatus) == "auth_required" {
		status = "auth_required"
	} else if stringValue(catalog.LastResultStatus) == "error" {
		status = "error"
	}
	tools := catalog.Tools
	if tools == nil {
		tools = json.RawMessage("null")
	}
	response := catalogResponse{
		ServerName:          serverName,
		EndpointFingerprint: endpointKey,
		Status:              status,
		Tools:               tools,
		Source:              catalog.Source,
		ProtocolVersion:     catalog.ProtocolVersion,
		DiscoveredAt:        formatTime(catalog.DiscoveredAt),
		ExpiresAt:           formatTime(catalog.ExpiresAt),
		Generation:          catalog.RequestedGeneration,
	}
	if catalog.LastErrorCode != nil {
		message := "MCP tool discovery failed."
		if catalog.LastErrorMessage != nil && strings.TrimSpace(*catalog.LastErrorMessage) != "" {
			message = *catalog.LastErrorMessage
		}
		response.LastError = &catalogError{Code: *catalog.LastErrorCode, Message: message}
	}
	return response
}

func principalCanSeeOrganization(principal auth.Principal, value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && (value == strings.TrimSpace(principal.OrganizationUUID) || value == strings.TrimSpace(principal.OrganizationExternalID))
}

func principalCanSeeWorkspace(principal auth.Principal, value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && (value == "default" || value == strings.TrimSpace(principal.WorkspaceUUID) || value == strings.TrimSpace(principal.WorkspaceExternalID))
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func formatTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func writeCatalogError(w http.ResponseWriter, r *http.Request, status int, message string) {
	httpapi.WriteError(w, r, httpapi.NewError(status, "invalid_request_error", message))
}
