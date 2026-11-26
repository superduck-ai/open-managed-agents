package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

type qoderModel struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"display_name"`
	DefaultContextWindow int64  `json:"default_context_window"`
	IsVL                 bool   `json:"is_vl"`
	IsEnabled            bool   `json:"is_enabled"`
}

type qoderModelsEnvelope struct {
	Data   []qoderModel `json:"data"`
	Models []qoderModel `json:"models"`
}

type qoderModelRef string

func (m *qoderModelRef) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*m = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		*m = qoderModelRef(s)
		return nil
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return err
	}
	*m = qoderModelRef(obj.ID)
	return nil
}

type qoderAgent struct {
	ID          string            `json:"id"`
	Version     int64             `json:"version"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Model       qoderModelRef     `json:"model"`
	System      string            `json:"system"`
	Tools       json.RawMessage   `json:"tools"`
	MCPServers  json.RawMessage   `json:"mcp_servers"`
	Skills      json.RawMessage   `json:"skills"`
	Metadata    map[string]string `json:"metadata"`
	ArchivedAt  *time.Time        `json:"archived_at"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type qoderCreateAgentRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Model       string            `json:"model"`
	System      string            `json:"system,omitempty"`
	Tools       json.RawMessage   `json:"tools,omitempty"`
	MCPServers  json.RawMessage   `json:"mcp_servers,omitempty"`
	Skills      json.RawMessage   `json:"skills,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type qoderUpdateAgentRequest struct {
	Version     int64             `json:"version"`
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Model       *string           `json:"model,omitempty"`
	System      *string           `json:"system,omitempty"`
	Tools       json.RawMessage   `json:"tools,omitempty"`
	MCPServers  json.RawMessage   `json:"mcp_servers,omitempty"`
	Skills      json.RawMessage   `json:"skills,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type agentBindingSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Model       string `json:"model,omitempty"`
	Version     int64  `json:"version,omitempty"`
}

type qoderEnvironment struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Config      json.RawMessage   `json:"config"`
	Metadata    map[string]string `json:"metadata"`
	ArchivedAt  *time.Time        `json:"archived_at"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type qoderCreateEnvironmentRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Config      json.RawMessage   `json:"config,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type qoderUpdateEnvironmentRequest struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Config      json.RawMessage   `json:"config,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type environmentBindingSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (r *Runtime) ListModels(ctx context.Context, _ agentruntime.Scope) ([]agentruntime.Model, error) {
	return r.fetchModels(ctx)
}

func (r *Runtime) fetchModels(ctx context.Context) ([]agentruntime.Model, error) {
	var raw json.RawMessage
	if err := r.doJSON(ctx, http.MethodGet, "/models", nil, nil, nil, &raw); err != nil {
		return nil, err
	}
	parsed, err := decodeModelsPayload(raw)
	if err != nil {
		return nil, upstreamTransportError(err)
	}
	models := make([]agentruntime.Model, 0, len(parsed))
	for _, m := range parsed {
		if !m.IsEnabled || strings.TrimSpace(m.ID) == "" {
			continue
		}
		displayName := m.DisplayName
		if displayName == "" {
			displayName = m.ID
		}
		models = append(models, agentruntime.Model{
			ID:                   m.ID,
			DisplayName:          displayName,
			DefaultContextWindow: m.DefaultContextWindow,
			IsVL:                 m.IsVL,
			Enabled:              true,
			ProviderKind:         agentruntime.ProviderQoderCloud,
		})
	}
	return models, nil
}

func decodeModelsPayload(raw json.RawMessage) ([]qoderModel, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []qoderModel
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var env qoderModelsEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, err
	}
	if len(env.Data) > 0 {
		return env.Data, nil
	}
	return env.Models, nil
}

func (r *Runtime) CreateAgent(ctx context.Context, scope agentruntime.Scope, input agentruntime.CreateAgentInput) (agentruntime.Agent, error) {
	translatedTools, err := TranslateToolsToQoder(input.Tools)
	if err != nil {
		return agentruntime.Agent{}, err
	}
	reqBody := qoderCreateAgentRequest{
		Name:        input.Name,
		Description: input.Description,
		Model:       input.Model,
		System:      input.System,
		Tools:       translatedTools,
		MCPServers:  input.MCPServers,
		Skills:      input.Skills,
		Metadata:    injectWatermark(input.Metadata, scope),
	}
	var remote qoderAgent
	if err := r.doJSON(ctx, http.MethodPost, "/agents", nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Agent{}, err
	}
	omaID, err := ids.New("agent_")
	if err != nil {
		return agentruntime.Agent{}, agentruntime.UnavailableError("failed to generate agent identifier", err)
	}
	summary, _ := json.Marshal(agentBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
		Model:       string(remote.Model),
		Version:     remote.Version,
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeAgent,
		OMAExternalID: omaID,
		RemoteID:      remote.ID,
	}, summary); err != nil {
		return agentruntime.Agent{}, err
	}
	return mapQoderAgent(omaID, remote)
}

func (r *Runtime) GetAgent(ctx context.Context, scope agentruntime.Scope, agentID string) (agentruntime.Agent, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, agentID)
	if err != nil {
		return agentruntime.Agent{}, err
	}
	var remote qoderAgent
	if err := r.doJSON(ctx, http.MethodGet, "/agents/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.Agent{}, err
	}
	return mapQoderAgent(binding.OMAExternalID, remote)
}

func (r *Runtime) GetAgentVersion(ctx context.Context, scope agentruntime.Scope, agentID string, version int64) (agentruntime.Agent, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, agentID)
	if err != nil {
		return agentruntime.Agent{}, err
	}
	params := url.Values{}
	params.Set("version", strconv.FormatInt(version, 10))
	var remote qoderAgent
	if err := r.doJSON(ctx, http.MethodGet, "/agents/"+url.PathEscape(binding.RemoteID), params, nil, nil, &remote); err != nil {
		return agentruntime.Agent{}, err
	}
	return mapQoderAgent(binding.OMAExternalID, remote)
}

type qoderAgentVersionsPage struct {
	Data     []qoderAgent `json:"data"`
	NextPage *string      `json:"next_page"`
	HasMore  bool         `json:"has_more"`
}

func (r *Runtime) ListAgentVersions(ctx context.Context, scope agentruntime.Scope, agentID string, query agentruntime.ListAgentVersionsQuery) (agentruntime.Page[agentruntime.Agent], error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, agentID)
	if err != nil {
		return agentruntime.Page[agentruntime.Agent]{}, err
	}
	params := url.Values{}
	if query.Limit > 0 {
		params.Set("limit", strconv.Itoa(query.Limit))
	}
	if strings.TrimSpace(query.Page) != "" {
		params.Set("page", strings.TrimSpace(query.Page))
	}
	var remotePage qoderAgentVersionsPage
	if err := r.doJSON(ctx, http.MethodGet, "/agents/"+url.PathEscape(binding.RemoteID)+"/versions", params, nil, nil, &remotePage); err != nil {
		return agentruntime.Page[agentruntime.Agent]{}, err
	}
	items := make([]agentruntime.Agent, 0, len(remotePage.Data))
	for _, entry := range remotePage.Data {
		mapped, err := mapQoderAgent(binding.OMAExternalID, entry)
		if err != nil {
			return agentruntime.Page[agentruntime.Agent]{}, err
		}
		items = append(items, mapped)
	}
	nextCursor := ""
	if remotePage.NextPage != nil {
		nextCursor = strings.TrimSpace(*remotePage.NextPage)
	}
	return agentruntime.Page[agentruntime.Agent]{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    remotePage.HasMore || nextCursor != "",
	}, nil
}

func (r *Runtime) ListAgents(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListAgentsQuery) (agentruntime.Page[agentruntime.Agent], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeAgent, "", "", query.IncludeArchived, limit+1, offset)
	if err != nil {
		return agentruntime.Page[agentruntime.Agent]{}, err
	}
	return r.hydrateAgentPage(ctx, rows, limit), nil
}

func (r *Runtime) SearchAgents(ctx context.Context, scope agentruntime.Scope, query agentruntime.SearchAgentsQuery) (agentruntime.Page[agentruntime.Agent], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.SearchOwned(ctx, scope, agentruntime.ResourceTypeAgent, query.Query, query.IncludeArchived, limit+1, offset)
	if err != nil {
		return agentruntime.Page[agentruntime.Agent]{}, err
	}
	return r.hydrateAgentPage(ctx, rows, limit), nil
}

func (r *Runtime) hydrateAgentPage(ctx context.Context, rows []db.RuntimeResourceBinding, limit int) agentruntime.Page[agentruntime.Agent] {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]agentruntime.Agent, 0, len(rows))
	for _, row := range rows {
		var remote qoderAgent
		if err := r.doJSON(ctx, http.MethodGet, "/agents/"+url.PathEscape(row.RemoteID), nil, nil, nil, &remote); err != nil {
			r.logger.WarnContext(ctx, "qoder agent hydrate failed, falling back to binding summary", "oma_id", row.OMAExternalID, "error", err)
			items = append(items, agentFromBindingSummary(row))
			continue
		}
		mapped, err := mapQoderAgent(row.OMAExternalID, remote)
		if err != nil {
			items = append(items, agentFromBindingSummary(row))
			continue
		}
		items = append(items, mapped)
	}
	page := agentruntime.Page[agentruntime.Agent]{
		Items:   items,
		HasMore: hasMore,
	}
	if len(items) > 0 {
		page.FirstID = items[0].ID
		page.LastID = items[len(items)-1].ID
	}
	return page
}

func (r *Runtime) UpdateAgent(ctx context.Context, scope agentruntime.Scope, agentID string, input agentruntime.UpdateAgentInput) (agentruntime.Agent, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, agentID)
	if err != nil {
		return agentruntime.Agent{}, err
	}
	reqBody := qoderUpdateAgentRequest{
		Version:     input.Version,
		Name:        input.Name,
		Description: input.Description,
		Model:       input.Model,
		System:      input.System,
		MCPServers:  input.MCPServers,
		Skills:      input.Skills,
	}
	if len(bytes.TrimSpace(input.Tools)) > 0 {
		translated, err := TranslateToolsToQoder(input.Tools)
		if err != nil {
			return agentruntime.Agent{}, err
		}
		reqBody.Tools = translated
	}
	if input.Metadata != nil {
		reqBody.Metadata = injectWatermark(input.Metadata, scope)
	}
	var remote qoderAgent
	if err := r.doJSON(ctx, http.MethodPost, "/agents/"+url.PathEscape(binding.RemoteID), nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Agent{}, err
	}
	summary, _ := json.Marshal(agentBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
		Model:       string(remote.Model),
		Version:     remote.Version,
	})
	if _, err := r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeAgent, binding.OMAExternalID, "", summary); err != nil {
		r.logger.WarnContext(ctx, "failed to refresh agent binding summary", "oma_id", binding.OMAExternalID, "error", err)
	}
	return mapQoderAgent(binding.OMAExternalID, remote)
}

func (r *Runtime) ArchiveAgent(ctx context.Context, scope agentruntime.Scope, agentID string) (agentruntime.Agent, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, agentID)
	if err != nil {
		return agentruntime.Agent{}, err
	}
	var remote qoderAgent
	if err := r.doJSON(ctx, http.MethodPost, "/agents/"+url.PathEscape(binding.RemoteID)+"/archive", nil, nil, nil, &remote); err != nil {
		return agentruntime.Agent{}, err
	}
	summary, _ := json.Marshal(agentBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
		Model:       string(remote.Model),
		Version:     remote.Version,
	})
	if _, err := r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeAgent, binding.OMAExternalID, "archived", summary); err != nil {
		return agentruntime.Agent{}, err
	}
	return mapQoderAgent(binding.OMAExternalID, remote)
}

func mapQoderAgent(omaID string, remote qoderAgent) (agentruntime.Agent, error) {
	tools, err := TranslateToolsFromQoder(remote.Tools)
	if err != nil {
		return agentruntime.Agent{}, err
	}
	return agentruntime.Agent{
		ID:          omaID,
		Version:     remote.Version,
		Name:        remote.Name,
		Description: remote.Description,
		Model:       string(remote.Model),
		System:      remote.System,
		Tools:       tools,
		MCPServers:  remote.MCPServers,
		Skills:      remote.Skills,
		Metadata:    stripWatermark(remote.Metadata),
		ArchivedAt:  remote.ArchivedAt,
		CreatedAt:   remote.CreatedAt,
		UpdatedAt:   remote.UpdatedAt,
	}, nil
}

func agentFromBindingSummary(row db.RuntimeResourceBinding) agentruntime.Agent {
	var summary agentBindingSummary
	_ = json.Unmarshal(row.Summary, &summary)
	agent := agentruntime.Agent{
		ID:          row.OMAExternalID,
		Name:        summary.Name,
		Description: summary.Description,
		Model:       summary.Model,
		Version:     summary.Version,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.State == "archived" {
		ts := row.UpdatedAt
		agent.ArchivedAt = &ts
	}
	return agent
}

func (r *Runtime) CreateEnvironment(ctx context.Context, scope agentruntime.Scope, input agentruntime.CreateEnvironmentInput) (agentruntime.Environment, error) {
	reqBody := qoderCreateEnvironmentRequest{
		Name:        input.Name,
		Description: input.Description,
		Config:      input.Config,
		Metadata:    injectWatermark(input.Metadata, scope),
	}
	var remote qoderEnvironment
	if err := r.doJSON(ctx, http.MethodPost, "/environments", nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Environment{}, err
	}
	omaID, err := ids.New("env_")
	if err != nil {
		return agentruntime.Environment{}, agentruntime.UnavailableError("failed to generate environment identifier", err)
	}
	summary, _ := json.Marshal(environmentBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeEnvironment,
		OMAExternalID: omaID,
		RemoteID:      remote.ID,
	}, summary); err != nil {
		return agentruntime.Environment{}, err
	}
	return mapQoderEnvironment(omaID, remote), nil
}

func (r *Runtime) GetEnvironment(ctx context.Context, scope agentruntime.Scope, environmentID string) (agentruntime.Environment, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, environmentID)
	if err != nil {
		return agentruntime.Environment{}, err
	}
	var remote qoderEnvironment
	if err := r.doJSON(ctx, http.MethodGet, "/environments/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.Environment{}, err
	}
	return mapQoderEnvironment(binding.OMAExternalID, remote), nil
}

func (r *Runtime) ListEnvironments(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListEnvironmentsQuery) (agentruntime.Page[agentruntime.Environment], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, "", "", query.IncludeArchived, limit+1, offset)
	if err != nil {
		return agentruntime.Page[agentruntime.Environment]{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]agentruntime.Environment, 0, len(rows))
	for _, row := range rows {
		var remote qoderEnvironment
		if err := r.doJSON(ctx, http.MethodGet, "/environments/"+url.PathEscape(row.RemoteID), nil, nil, nil, &remote); err != nil {
			r.logger.WarnContext(ctx, "qoder environment hydrate failed, falling back to binding summary", "oma_id", row.OMAExternalID, "error", err)
			items = append(items, environmentFromBindingSummary(row))
			continue
		}
		items = append(items, mapQoderEnvironment(row.OMAExternalID, remote))
	}
	page := agentruntime.Page[agentruntime.Environment]{
		Items:   items,
		HasMore: hasMore,
	}
	if len(items) > 0 {
		page.FirstID = items[0].ID
		page.LastID = items[len(items)-1].ID
	}
	return page, nil
}

func (r *Runtime) UpdateEnvironment(ctx context.Context, scope agentruntime.Scope, environmentID string, input agentruntime.UpdateEnvironmentInput) (agentruntime.Environment, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, environmentID)
	if err != nil {
		return agentruntime.Environment{}, err
	}
	reqBody := qoderUpdateEnvironmentRequest{
		Name:        input.Name,
		Description: input.Description,
		Config:      input.Config,
	}
	if input.Metadata != nil {
		reqBody.Metadata = injectWatermark(input.Metadata, scope)
	}
	var remote qoderEnvironment
	if err := r.doJSON(ctx, http.MethodPost, "/environments/"+url.PathEscape(binding.RemoteID), nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Environment{}, err
	}
	summary, _ := json.Marshal(environmentBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
	})
	if _, err := r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeEnvironment, binding.OMAExternalID, "", summary); err != nil {
		r.logger.WarnContext(ctx, "failed to refresh environment binding summary", "oma_id", binding.OMAExternalID, "error", err)
	}
	return mapQoderEnvironment(binding.OMAExternalID, remote), nil
}

func (r *Runtime) ArchiveEnvironment(ctx context.Context, scope agentruntime.Scope, environmentID string) (agentruntime.Environment, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, environmentID)
	if err != nil {
		return agentruntime.Environment{}, err
	}
	var remote qoderEnvironment
	if err := r.doJSON(ctx, http.MethodPost, "/environments/"+url.PathEscape(binding.RemoteID)+"/archive", nil, nil, nil, &remote); err != nil {
		return agentruntime.Environment{}, err
	}
	summary, _ := json.Marshal(environmentBindingSummary{
		Name:        remote.Name,
		Description: remote.Description,
	})
	if _, err := r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeEnvironment, binding.OMAExternalID, "archived", summary); err != nil {
		return agentruntime.Environment{}, err
	}
	return mapQoderEnvironment(binding.OMAExternalID, remote), nil
}

func (r *Runtime) DeleteEnvironment(ctx context.Context, scope agentruntime.Scope, environmentID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, environmentID)
	if err != nil {
		return err
	}
	if err := r.doJSON(ctx, http.MethodDelete, "/environments/"+url.PathEscape(binding.RemoteID), nil, nil, nil, nil); err != nil {
		if !isUpstreamNotFound(err) {
			return err
		}
	}
	return r.bindings.SoftDelete(ctx, scope, agentruntime.ResourceTypeEnvironment, binding.OMAExternalID)
}

func mapQoderEnvironment(omaID string, remote qoderEnvironment) agentruntime.Environment {
	return agentruntime.Environment{
		ID:          omaID,
		Name:        remote.Name,
		Description: remote.Description,
		Config:      remote.Config,
		Metadata:    stripWatermark(remote.Metadata),
		ArchivedAt:  remote.ArchivedAt,
		CreatedAt:   remote.CreatedAt,
		UpdatedAt:   remote.UpdatedAt,
	}
}

func environmentFromBindingSummary(row db.RuntimeResourceBinding) agentruntime.Environment {
	var summary environmentBindingSummary
	_ = json.Unmarshal(row.Summary, &summary)
	env := agentruntime.Environment{
		ID:          row.OMAExternalID,
		Name:        summary.Name,
		Description: summary.Description,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.State == "archived" {
		ts := row.UpdatedAt
		env.ArchivedAt = &ts
	}
	return env
}
