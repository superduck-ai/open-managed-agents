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
	GetMCPToolCatalog(context.Context, int64, int64, string, string) (db.MCPToolCatalog, error)
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

func (e *Enqueuer) EnsureAgent(ctx context.Context, organizationID, workspaceID int64, raw json.RawMessage, trigger string) error {
	if e == nil || !e.cfg.MCPDiscoveryEnabled {
		return nil
	}
	servers, err := ParseAgentServers(raw)
	if err != nil {
		return err
	}
	var joined error
	for _, server := range servers {
		normalizedURL, normalizeErr := NormalizeEndpoint(server.URL)
		if normalizeErr != nil {
			joined = errors.Join(joined, normalizeErr)
			continue
		}
		_, ensureErr := e.store.EnsureMCPToolCatalog(ctx, db.EnsureMCPToolCatalogInput{
			OrganizationID: organizationID,
			WorkspaceID:    workspaceID,
			TransportType:  "url",
			EndpointURL:    normalizedURL,
			EndpointKey:    EndpointKey(e.cfg.MCPDiscoveryHMACKey, normalizedURL),
			AuthScopeKey:   "anonymous",
			Trigger:        trigger,
			Now:            time.Now().UTC(),
		})
		if ensureErr != nil {
			joined = errors.Join(joined, ensureErr)
		}
	}
	return joined
}
