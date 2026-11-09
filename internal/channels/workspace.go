package channels

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

const workspaceAgentPageSize = 100

const workspaceChannelWorkers = 8

var maxWorkspaceAgents = 200

type workspaceChannelPage struct {
	Data       []map[string]any `json:"data"`
	Incomplete bool             `json:"incomplete"`
	Truncated  bool             `json:"truncated"`
}

type agentChannelResult struct {
	rows []map[string]any
	err  error
}

func (h *handler) workspaceRouter() http.Handler {
	router := chi.NewRouter()
	wrap := httpapi.NewErrorAdapter(h.logger).Wrap
	router.Get("/", wrap(h.listWorkspace))
	return router
}

func (h *handler) listWorkspace(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.WorkspaceUUID == "" || principal.OrganizationUUID == "" {
		return unauthenticated()
	}
	if _, err := h.baseEndpoint(); err != nil {
		return err
	}
	agents, truncated, err := h.listWorkspaceAgents(r.Context(), principal.WorkspaceUUID)
	if err != nil {
		return unavailable(err)
	}
	rows, incomplete := h.loadWorkspaceChannels(r.Context(), principal, agents)
	httpapi.WriteJSON(w, http.StatusOK, workspaceChannelPage{Data: rows, Incomplete: incomplete, Truncated: truncated})
	return nil
}

func (h *handler) listWorkspaceAgents(ctx context.Context, workspace string) ([]db.Agent, bool, error) {
	var agents []db.Agent
	var cursor *db.AgentPageCursor
	for {
		page, hasMore, err := h.store.ListAgentsPage(ctx, db.ListAgentsPageParams{
			WorkspaceUUID: workspace,
			Limit:         workspaceAgentPageSize,
			Cursor:        cursor,
		})
		if err != nil {
			return nil, false, err
		}
		agents = append(agents, page...)
		if len(agents) > maxWorkspaceAgents {
			return agents[:maxWorkspaceAgents], true, nil
		}
		if len(agents) == maxWorkspaceAgents && hasMore {
			return agents, true, nil
		}
		if !hasMore || len(page) == 0 {
			return agents, false, nil
		}
		last := page[len(page)-1]
		cursor = &db.AgentPageCursor{CreatedAt: last.CreatedAt, UUID: last.UUID}
	}
}

func (h *handler) loadWorkspaceChannels(ctx context.Context, principal auth.Principal, agents []db.Agent) ([]map[string]any, bool) {
	rows := make([]map[string]any, 0)
	if len(agents) == 0 {
		return rows, false
	}
	results := make([]agentChannelResult, len(agents))
	sem := make(chan struct{}, workspaceChannelWorkers)
	var group sync.WaitGroup
	for index, agent := range agents {
		group.Add(1)
		sem <- struct{}{}
		go func(index int, agent db.Agent) {
			defer group.Done()
			defer func() { <-sem }()
			results[index] = h.agentWorkspaceChannels(ctx, principal, agent)
		}(index, agent)
	}
	group.Wait()
	incomplete := false
	for _, result := range results {
		if result.err != nil {
			incomplete = true
			continue
		}
		rows = append(rows, result.rows...)
	}
	return rows, incomplete
}

func (h *handler) agentWorkspaceChannels(ctx context.Context, principal auth.Principal, agent db.Agent) agentChannelResult {
	body, status, err := h.bridgeGet(ctx, principal, agent.ExternalID, "")
	if err != nil || status != http.StatusOK {
		return agentChannelResult{err: bridgeStatusError(err, status)}
	}
	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return agentChannelResult{err: err}
	}
	settingsBody, settingsStatus, err := h.bridgeGet(ctx, principal, agent.ExternalID, "/settings")
	if err != nil || settingsStatus != http.StatusOK {
		return agentChannelResult{err: bridgeStatusError(err, settingsStatus)}
	}
	var settings environmentSettings
	if err := json.Unmarshal(settingsBody, &settings); err != nil {
		return agentChannelResult{err: err}
	}
	rows := make([]map[string]any, 0, len(page.Data))
	for _, channel := range page.Data {
		if channel == nil {
			continue
		}
		delete(channel, "app_secret")
		channel["agent_id"] = agent.ExternalID
		channel["agent_name"] = agent.Name
		channel["environment_id"] = settings.EnvironmentID
		rows = append(rows, channel)
	}
	return agentChannelResult{rows: rows}
}

func (h *handler) bridgeGet(ctx context.Context, principal auth.Principal, agentID, suffix string) ([]byte, int, error) {
	endpoint, err := h.baseEndpoint()
	if err != nil {
		return nil, 0, err
	}
	endpoint.Path += suffix
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-OMA-Organization", principal.OrganizationUUID)
	request.Header.Set("X-OMA-Workspace", principal.WorkspaceUUID)
	request.Header.Set("X-OMA-Agent", agentID)
	response, err := h.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, response.StatusCode, err
	}
	return body, response.StatusCode, nil
}

func bridgeStatusError(err error, status int) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("桥接请求返回 HTTP %d", status)
}
