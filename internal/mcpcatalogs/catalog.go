package mcpcatalogs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type AgentServer struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type Store interface {
	EnsureMCPToolCatalog(context.Context, db.EnsureMCPToolCatalogInput) (db.EnsureMCPToolCatalogResult, error)
}

type Enqueuer struct {
	cfg   config.Config
	store Store
}

func NewEnqueuer(cfg config.Config, store Store) *Enqueuer {
	return &Enqueuer{cfg: cfg, store: store}
}

func ParseAgentServers(raw json.RawMessage) ([]AgentServer, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []AgentServer{}, nil
	}
	var servers []AgentServer
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, err
	}
	for index := range servers {
		servers[index].Name = strings.TrimSpace(servers[index].Name)
		servers[index].Type = strings.TrimSpace(servers[index].Type)
		servers[index].URL = strings.TrimSpace(servers[index].URL)
	}
	return servers, nil
}

// EnsureAgent 解析 Agent 配置中的 MCP servers，并为每个合法 endpoint 幂等确保全局匿名工具目录及异步发现任务。
// workspaceExternalID 只记录通用 job 的来源，不参与 catalog identity；单个 server 失败时仍会继续处理其余 server。
func (e *Enqueuer) EnsureAgent(ctx context.Context, workspaceExternalID string, raw json.RawMessage, trigger string) error {
	if e == nil || !e.cfg.MCPDiscoveryEnabled {
		return nil
	}
	servers, err := ParseAgentServers(raw)
	if err != nil {
		return err
	}
	// 每个 MCP 独立预热：单个 endpoint 无效或入队失败时继续处理其余 server，
	// 最后合并错误交给调用方记录，避免一个坏配置阻断整个 Agent 的 catalog 建立。
	var joined error
	for _, server := range servers {
		normalizedURL, normalizeErr := NormalizeEndpoint(server.URL)
		if normalizeErr != nil {
			joined = errors.Join(joined, normalizeErr)
			continue
		}
		_, ensureErr := e.store.EnsureMCPToolCatalog(ctx, db.EnsureMCPToolCatalogInput{
			JobWorkspaceExternalID: workspaceExternalID,
			TransportType:          "url",
			EndpointURL:            normalizedURL,
			Trigger:                trigger,
			Now:                    time.Now().UTC(),
		})
		if ensureErr != nil {
			joined = errors.Join(joined, ensureErr)
		}
	}
	return joined
}
