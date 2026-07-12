package mcpcatalogs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
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

const defaultProbeTimeout = 10 * time.Second

type Handler struct {
	cfg      config.Config
	database *db.DB
	prober   Prober
}

// catalogResponse 是面向 Agent Detail 的 server 视图，不暴露全局 catalog 的 endpoint 和内部 ID。
// nil tools 会编码为 null，表示尚未成功刷新；成功发现零工具则编码为 []。
type catalogResponse struct {
	ServerName string                  `json:"server_name"`
	Status     string                  `json:"status"`
	Tools      []db.MCPToolCatalogItem `json:"tools"`
}

type catalogListResponse struct {
	Data    []catalogResponse `json:"data"`
	Version int               `json:"version"`
}

type refreshRequest struct {
	ServerName string `json:"server_name"`
}

type refreshResponse struct {
	Data    catalogResponse `json:"data"`
	Version int             `json:"version"`
}

func NewHandler(cfg config.Config, database *db.DB) *Handler {
	return &Handler{cfg: cfg, database: database, prober: Prober{}}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspaceId}/agents/{agentId}/mcp_tool_catalogs", h.list)
	r.Post("/workspaces/{workspaceId}/agents/{agentId}/mcp_tool_catalogs/refresh", h.refresh)
}

// list 只读取已经成功保存的工具快照。详情页读取不会连接外部 MCP，也不会隐式创建任务。
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	_, agent, version, ok := h.authorizedAgent(w, r)
	if !ok {
		return
	}
	servers, err := ParseAgentServers(agent.MCPServers)
	if err != nil {
		writeCatalogError(w, r, http.StatusInternalServerError, "Could not read the Agent MCP configuration")
		return
	}
	responses := make([]catalogResponse, 0, len(servers))
	for _, server := range servers {
		response, getErr := h.readCatalog(r.Context(), server)
		if getErr != nil {
			log.Printf("list mcp catalog agent_id=%s server=%s: %v", agent.ExternalID, server.Name, getErr)
			writeCatalogError(w, r, http.StatusInternalServerError, "Could not load MCP tool catalogs")
			return
		}
		responses = append(responses, response)
	}
	httpapi.WriteJSON(w, http.StatusOK, catalogListResponse{Data: responses, Version: version})
}

// refresh 在一次 HTTP 请求内完成单个 MCP 的匿名工具发现，并且只在成功后替换数据库快照。
// 因而探测失败或请求取消时，详情页仍可继续展示上一次成功保存的工具列表。
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.MCPDiscoveryEnabled {
		writeCatalogError(w, r, http.StatusServiceUnavailable, "MCP tool discovery is disabled")
		return
	}
	principal, agent, version, ok := h.authorizedAgent(w, r)
	if !ok {
		return
	}

	var input refreshRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if r.Body == nil || r.Body == http.NoBody || decoder.Decode(&input) != nil {
		writeCatalogError(w, r, http.StatusBadRequest, "Invalid refresh request")
		return
	}
	// 一个请求只能包含一个 JSON object；拒绝拼接的第二个值，避免边界层出现含糊解析。
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeCatalogError(w, r, http.StatusBadRequest, "Invalid refresh request")
		return
	}
	input.ServerName = strings.TrimSpace(input.ServerName)
	if input.ServerName == "" {
		writeCatalogError(w, r, http.StatusBadRequest, "server_name must not be empty")
		return
	}
	// 请求只选择 Agent 已保存的 server；endpoint 始终来自持久化的指定版本。
	// 这样 Agent 版本仍是配置事实来源，刷新接口也不需要接受或保存客户端 URL。
	servers, err := ParseAgentServers(agent.MCPServers)
	if err != nil {
		writeCatalogError(w, r, http.StatusInternalServerError, "Could not read the Agent MCP configuration")
		return
	}
	server, found := findAgentServer(servers, input.ServerName)
	if !found {
		writeCatalogError(w, r, http.StatusBadRequest, "Unknown MCP server name: "+input.ServerName)
		return
	}
	normalized, err := NormalizeEndpoint(server.URL)
	if err != nil {
		writeCatalogError(w, r, http.StatusBadRequest, "MCP server "+server.Name+" has an invalid endpoint")
		return
	}

	// 手动刷新应有明确的等待上限，且沿用请求 context：浏览器断开时会取消探测并且不会写库。
	probeCtx, cancel := context.WithTimeout(r.Context(), configuredProbeTimeout(h.cfg.MCPDiscoveryProbeTimeout))
	defer cancel()
	result, err := h.prober.Probe(probeCtx, normalized)
	if err != nil {
		status, message := probeHTTPError(err)
		log.Printf("refresh mcp catalog workspace_external_id=%s agent_id=%s server=%s: %v", principal.WorkspaceExternalID, agent.ExternalID, server.Name, err)
		writeCatalogError(w, r, status, message)
		return
	}
	// Prober 对成功的零工具结果返回非 nil 空切片；DB 层也会拒绝 nil，避免把 unknown 写成成功快照。
	catalog, err := h.database.UpsertMCPToolCatalog(r.Context(), "url", normalized, result.Tools)
	if err != nil {
		log.Printf("save mcp catalog workspace_external_id=%s agent_id=%s server=%s: %v", principal.WorkspaceExternalID, agent.ExternalID, server.Name, err)
		writeCatalogError(w, r, http.StatusInternalServerError, "Could not save MCP tool catalog")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, refreshResponse{
		Data:    mapCatalog(server.Name, catalog),
		Version: version,
	})
}

func (h *Handler) authorizedAgent(w http.ResponseWriter, r *http.Request) (auth.Principal, db.Agent, int, bool) {
	// 跨组织、跨 workspace 与资源不存在统一返回 404，避免通过响应差异枚举 Agent。
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principalCanSeeOrganization(r, principal, chi.URLParam(r, "orgUuid")) || !principalCanSeeWorkspace(principal, chi.URLParam(r, "workspaceId")) {
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

func (h *Handler) readCatalog(ctx context.Context, server AgentServer) (catalogResponse, error) {
	normalized, err := NormalizeEndpoint(server.URL)
	if err != nil {
		return catalogResponse{ServerName: server.Name, Status: "error", Tools: nil}, nil
	}
	catalog, err := h.database.GetMCPToolCatalog(ctx, "url", normalized)
	if errors.Is(err, db.ErrNotFound) {
		return catalogResponse{ServerName: server.Name, Status: "unknown", Tools: nil}, nil
	}
	if err != nil {
		return catalogResponse{}, err
	}
	return mapCatalog(server.Name, catalog), nil
}

func mapCatalog(serverName string, catalog db.MCPToolCatalog) catalogResponse {
	return catalogResponse{ServerName: serverName, Status: "ready", Tools: catalog.Tools}
}

func findAgentServer(servers []AgentServer, name string) (AgentServer, bool) {
	for _, server := range servers {
		if server.Name == name {
			return server, true
		}
	}
	return AgentServer{}, false
}

func configuredProbeTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return defaultProbeTimeout
	}
	return value
}

func probeHTTPError(err error) (int, string) {
	var probeErr *ProbeError
	if errors.As(err, &probeErr) {
		if probeErr.Code == "timeout" {
			return http.StatusGatewayTimeout, probeErr.Message
		}
		return http.StatusBadGateway, probeErr.Message
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout, "The MCP server did not respond in time."
	}
	return http.StatusBadGateway, "Could not refresh MCP tool catalog"
}

func principalCanSeeOrganization(r *http.Request, principal auth.Principal, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if value == strings.TrimSpace(principal.OrganizationUUID) || value == strings.TrimSpace(principal.OrganizationExternalID) {
		return true
	}
	// platform.claude.com 的镜像 session 恢复后，path 仍携带官方 organization UUID，
	// principal 则指向本地镜像 organization；只在受信 Console host 上接受中间件写入的别名。
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

func principalCanSeeWorkspace(principal auth.Principal, value string) bool {
	// "default" 只是控制台路由中“当前已认证 workspace”的别名，不代表全局默认 workspace；
	// 后续 Agent 查询仍使用 principal.WorkspaceID 作为真实租户边界。
	value = strings.TrimSpace(value)
	return value != "" && (value == "default" || value == strings.TrimSpace(principal.WorkspaceUUID) || value == strings.TrimSpace(principal.WorkspaceExternalID))
}

func writeCatalogError(w http.ResponseWriter, r *http.Request, status int, message string) {
	httpapi.WriteError(w, r, httpapi.NewError(status, "invalid_request_error", message))
}
