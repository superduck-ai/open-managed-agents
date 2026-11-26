package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
}

func (h *Handler) resolveExternalAgentRuntime(ctx context.Context, principal auth.Principal) (agentruntime.Runtime, agentruntime.AgentRuntime, agentruntime.Scope, error) {
	if h.runtimeResolver == nil {
		return nil, nil, agentruntime.Scope{}, agentRuntimeUnavailable(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	agentRT, ok := rt.(agentruntime.AgentRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, agentRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, agentRT, scope, nil
}

func (h *Handler) createExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	body, err := httpapi.DecodeObjectBodyAs[agentMutationRequest](w, r, maxAgentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	name, err := parseAgentName(body.Name)
	if err != nil {
		return invalidRequest(err)
	}
	if len(body.Model) == 0 {
		return invalidRequest(errors.New("model is required"))
	}
	model, err := normalizeModel(body.Model)
	if err != nil {
		return invalidRequest(err)
	}
	var description string
	if len(body.Description) > 0 {
		ptr, parseErr := nullableStringFromRaw(body.Description, "description")
		if parseErr != nil {
			return invalidRequest(parseErr)
		}
		if ptr != nil {
			description = *ptr
		}
	}
	var system string
	if len(body.System) > 0 {
		ptr, parseErr := nullableStringFromRaw(body.System, "system")
		if parseErr != nil {
			return invalidRequest(parseErr)
		}
		if ptr != nil {
			system = *ptr
		}
	}
	metadata, err := decodeAgentMetadataMap(body.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	rt, agentRT, scope, err := h.resolveExternalAgentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := agentRT.CreateAgent(r.Context(), scope, agentruntime.CreateAgentInput{
		Name:        name,
		Description: description,
		Model:       model.ID,
		System:      system,
		Tools:       rawOrDefault(body.Tools, `[]`),
		MCPServers:  rawOrDefault(body.MCPServers, `[]`),
		Skills:      rawOrDefault(body.Skills, `[]`),
		Metadata:    metadata,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeAgent(created))
	return nil
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := parseExternalOffsetCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	rt, agentRT, scope, err := h.resolveExternalAgentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := agentRT.ListAgents(r.Context(), scope, agentruntime.ListAgentsQuery{
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponseFromRuntimeAgents(page, offset, limit))
	return nil
}

func (h *Handler) searchExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	body, err := decodeSearchRequest(w, r)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := parseExternalOffsetCursor(derefString(body.Page))
	if err != nil {
		return invalidRequest(err)
	}
	limit := searchLimit(body.Limit)
	rt, agentRT, scope, err := h.resolveExternalAgentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := agentRT.SearchAgents(r.Context(), scope, agentruntime.SearchAgentsQuery{
		Query:           strings.TrimSpace(body.Name),
		IncludeArchived: derefBool(body.IncludeArchived),
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponseFromRuntimeAgents(page, offset, limit))
	return nil
}

func (h *Handler) retrieveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, agentID string) error {
	rawVersion := strings.TrimSpace(r.URL.Query().Get("version"))
	var version int64
	if rawVersion != "" {
		parsed, parseErr := strconv.ParseInt(rawVersion, 10, 64)
		if parseErr != nil || parsed < 1 {
			return invalidRequest(errors.New("version must be at least 1"))
		}
		version = parsed
	}
	rt, agentRT, scope, err := h.resolveExternalAgentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	var agent agentruntime.Agent
	if version > 0 {
		if versionRT, ok := rt.(agentruntime.AgentVersionRuntime); ok {
			agent, err = versionRT.GetAgentVersion(r.Context(), scope, agentID, version)
		} else {
			agent, err = agentRT.GetAgent(r.Context(), scope, agentID)
		}
	} else {
		agent, err = agentRT.GetAgent(r.Context(), scope, agentID)
	}
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeAgent(agent))
	return nil
}

func (h *Handler) updateExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, agentID string) error {
	body, err := httpapi.DecodeObjectBodyAs[agentMutationRequest](w, r, maxAgentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	input, err := buildExternalUpdateAgentInput(body)
	if err != nil {
		return invalidRequest(err)
	}
	rt, agentRT, scope, err := h.resolveExternalAgentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	updated, err := agentRT.UpdateAgent(r.Context(), scope, agentID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeAgent(updated))
	return nil
}

func (h *Handler) archiveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, agentID string) error {
	rt, agentRT, scope, err := h.resolveExternalAgentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	archived, err := agentRT.ArchiveAgent(r.Context(), scope, agentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeAgent(archived))
	return nil
}

func (h *Handler) versionsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, agentID string) error {
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	pageCursor := strings.TrimSpace(r.URL.Query().Get("page"))
	rt, agentRT, scope, err := h.resolveExternalAgentRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if versionRT, ok := rt.(agentruntime.AgentVersionRuntime); ok {
		page, err := versionRT.ListAgentVersions(r.Context(), scope, agentID, agentruntime.ListAgentVersionsQuery{
			Limit: limit,
			Page:  pageCursor,
		})
		if err != nil {
			return err
		}
		data := make([]agentResponse, 0, len(page.Items))
		for _, item := range page.Items {
			data = append(data, responseFromRuntimeAgent(item))
		}
		var nextPage *string
		if page.HasMore && page.NextCursor != "" {
			next := page.NextCursor
			nextPage = &next
		}
		httpapi.WriteJSON(w, http.StatusOK, pageResponse{
			Data:     data,
			NextPage: nextPage,
		})
		return nil
	}
	agent, err := agentRT.GetAgent(r.Context(), scope, agentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{
		Data: []agentResponse{responseFromRuntimeAgent(agent)},
	})
	return nil
}

func buildExternalUpdateAgentInput(body *agentMutationRequest) (agentruntime.UpdateAgentInput, error) {
	var input agentruntime.UpdateAgentInput
	if len(body.Version) > 0 {
		version, err := parseRequiredVersion(body.Version)
		if err != nil {
			return agentruntime.UpdateAgentInput{}, err
		}
		input.Version = int64(version)
	}
	if len(body.Name) > 0 {
		name, err := parseAgentName(body.Name)
		if err != nil {
			return agentruntime.UpdateAgentInput{}, err
		}
		input.Name = &name
	}
	if len(body.Description) > 0 {
		desc, err := nullableStringFromRaw(body.Description, "description")
		if err != nil {
			return agentruntime.UpdateAgentInput{}, err
		}
		value := derefString(desc)
		input.Description = &value
	}
	if len(body.System) > 0 {
		sys, err := nullableStringFromRaw(body.System, "system")
		if err != nil {
			return agentruntime.UpdateAgentInput{}, err
		}
		value := derefString(sys)
		input.System = &value
	}
	if len(body.Model) > 0 {
		model, err := normalizeModel(body.Model)
		if err != nil {
			return agentruntime.UpdateAgentInput{}, err
		}
		input.Model = &model.ID
	}
	if len(body.Tools) > 0 {
		input.Tools = clearableArray(body.Tools)
	}
	if len(body.MCPServers) > 0 {
		input.MCPServers = clearableArray(body.MCPServers)
	}
	if len(body.Skills) > 0 {
		input.Skills = clearableArray(body.Skills)
	}
	if len(body.Metadata) > 0 {
		metadata, err := decodeAgentMetadataMap(body.Metadata)
		if err != nil {
			return agentruntime.UpdateAgentInput{}, err
		}
		input.Metadata = metadata
	}
	return input, nil
}

func decodeAgentMetadataMap(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	normalized, err := httpapi.NormalizeMetadata(raw, validateMetadata)
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	if err := json.Unmarshal(normalized, &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func parseExternalOffsetCursor(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, errors.New("page is invalid")
	}
	return offset, nil
}

func pageResponseFromRuntimeAgents(page agentruntime.Page[agentruntime.Agent], offset int, limit int) pageResponse {
	data := make([]agentResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromRuntimeAgent(item))
	}
	var nextPage *string
	if page.HasMore {
		cursor := page.NextCursor
		if cursor == "" {
			cursor = strconv.Itoa(offset + len(data))
			if len(data) == 0 {
				cursor = strconv.Itoa(offset + limit)
			}
		}
		nextPage = &cursor
	}
	return pageResponse{Data: data, NextPage: nextPage}
}

func responseFromRuntimeAgent(agent agentruntime.Agent) agentResponse {
	version := int(agent.Version)
	if version < 1 {
		version = 1
	}
	var description *string
	if agent.Description != "" {
		value := agent.Description
		description = &value
	}
	var system *string
	if agent.System != "" {
		value := agent.System
		system = &value
	}
	metadataBytes := json.RawMessage(`{}`)
	if len(agent.Metadata) > 0 {
		if encoded, err := json.Marshal(agent.Metadata); err == nil {
			metadataBytes = encoded
		}
	}
	modelBytes := json.RawMessage(`{}`)
	if agent.Model != "" {
		if encoded, err := json.Marshal(normalizedAgentModel{ID: agent.Model, Speed: "standard"}); err == nil {
			modelBytes = encoded
		}
	}
	return agentResponse{
		ID:          agent.ID,
		ArchivedAt:  httpapi.OptionalTime(agent.ArchivedAt),
		CreatedAt:   httpapi.FormatTime(agent.CreatedAt),
		Description: description,
		MCPServers:  httpapi.RawOr(agent.MCPServers, `[]`),
		Metadata:    metadataBytes,
		Model:       modelBytes,
		Multiagent:  json.RawMessage(`null`),
		Name:        agent.Name,
		Skills:      httpapi.RawOr(agent.Skills, `[]`),
		System:      system,
		Tools:       httpapi.RawOr(agent.Tools, `[]`),
		Type:        "agent",
		UpdatedAt:   httpapi.FormatTime(agent.UpdatedAt),
		Version:     version,
	}
}
