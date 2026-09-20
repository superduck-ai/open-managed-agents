package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentconfig"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deployments"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
)

const (
	maxAgentBodySize = 4 << 20
)

type Handler struct {
	cfg          config.Config
	db           *db.DB
	deployments  *deployments.Store
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
}

type agentResponse struct {
	ID          string          `json:"id"`
	ArchivedAt  *string         `json:"archived_at"`
	CreatedAt   string          `json:"created_at"`
	Description *string         `json:"description"`
	MCPServers  json.RawMessage `json:"mcp_servers"`
	Metadata    json.RawMessage `json:"metadata"`
	Model       json.RawMessage `json:"model"`
	Multiagent  json.RawMessage `json:"multiagent"`
	Name        string          `json:"name"`
	Skills      json.RawMessage `json:"skills"`
	System      *string         `json:"system"`
	Tools       json.RawMessage `json:"tools"`
	Type        string          `json:"type"`
	UpdatedAt   string          `json:"updated_at"`
	Version     int             `json:"version"`
}

type pageResponse struct {
	Data     []agentResponse `json:"data"`
	NextPage *string         `json:"next_page"`
}

type searchRequest struct {
	Name            string  `json:"name"`
	Limit           *int    `json:"limit"`
	IncludeArchived *bool   `json:"include_archived"`
	Page            *string `json:"page"`
}

type agentMutationRequest struct {
	Name        json.RawMessage `json:"name"`
	Description json.RawMessage `json:"description"`
	System      json.RawMessage `json:"system"`
	Model       json.RawMessage `json:"model"`
	MCPServers  json.RawMessage `json:"mcp_servers"`
	Metadata    json.RawMessage `json:"metadata"`
	Multiagent  json.RawMessage `json:"multiagent"`
	Skills      json.RawMessage `json:"skills"`
	Tools       json.RawMessage `json:"tools"`
	Version     json.RawMessage `json:"version"`
}

type agentState struct {
	Name        string
	Description *string
	System      *string
	Model       json.RawMessage
	MCPServers  json.RawMessage
	Metadata    json.RawMessage
	Multiagent  json.RawMessage
	Skills      json.RawMessage
	Tools       json.RawMessage
}

type agentReference struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version int    `json:"version"`
}

func NewHandler(cfg config.Config, database *db.DB, deploymentStore *deployments.Store, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{cfg: cfg, db: database, deployments: deploymentStore, errorAdapter: httpapi.NewErrorAdapter(logger)}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Post("/", wrap(h.create))
	router.Get("/", wrap(h.list))
	router.Get("/{agent_id}", wrap(h.retrieveRoute))
	router.Post("/{agent_id}", wrap(h.updateRoute))
	router.Post("/{agent_id}/archive", wrap(h.archiveRoute))
	router.Get("/{agent_id}/versions", wrap(h.versionsRoute))
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		h.errorAdapter.Write(w, r, agentsBetaRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return agentRouteNotFound()
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return agentAuthenticationRequired()
	}

	body, err := httpapi.DecodeObjectBodyAs[agentMutationRequest](w, r, maxAgentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	agentID, err := ids.New("agent_")
	if err != nil {
		return internalError("Could not generate agent ID", fmt.Errorf("generate agent ID: %w", err))
	}
	state, err := h.stateFromCreate(r, principal, agentID, body)
	if err != nil {
		return agentMutationError(err)
	}
	versionID, err := ids.New("agentver_")
	if err != nil {
		return internalError("Could not generate agent version ID", fmt.Errorf("generate agent version ID: %w", err))
	}
	now := time.Now().UTC()
	created, err := h.db.CreateAgent(r.Context(), db.Agent{
		UUID:                uuid.NewV4().String(),
		ExternalID:          agentID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		CurrentVersion:      1,
		Name:                state.Name,
		Description:         state.Description,
		System:              state.System,
		Model:               state.Model,
		MCPServers:          state.MCPServers,
		Metadata:            state.Metadata,
		Multiagent:          state.Multiagent,
		Skills:              state.Skills,
		Tools:               state.Tools,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, versionID)
	if err != nil {
		return internalError("Could not create agent", fmt.Errorf("create agent %q: %w", agentID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(created))
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeAgentCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}

	records, hasMore, err := h.db.ListAgentsPage(r.Context(), db.ListAgentsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLTE:    createdAtLTE,
	})
	if err != nil {
		return internalError("Could not list agents", fmt.Errorf("list agents: %w", err))
	}
	data := responsesFromAgents(records)
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeAgentCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	h.errorAdapter.Wrap(h.search)(w, r)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) error {
	if r.URL.Query().Get("beta") != "true" {
		return agentsBetaRequired()
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return agentAuthenticationRequired()
	}
	body, err := decodeSearchRequest(w, r)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeAgentCursor(derefString(body.Page))
	if err != nil {
		return invalidRequest(err)
	}

	records, hasMore, err := h.db.SearchAgentsPage(r.Context(), db.SearchAgentsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		Name:            strings.TrimSpace(body.Name),
		Limit:           searchLimit(body.Limit),
		Cursor:          cursor,
		IncludeArchived: derefBool(body.IncludeArchived),
	})
	if err != nil {
		return internalError("Could not search agents", fmt.Errorf("search agents: %w", err))
	}
	data := responsesFromAgents(records)
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeAgentCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) error {
	return h.retrieve(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, agentID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	rawVersion := strings.TrimSpace(r.URL.Query().Get("version"))
	var record db.Agent
	var err error
	if rawVersion == "" {
		record, err = h.db.GetAgent(r.Context(), principal.WorkspaceUUID, agentID)
	} else {
		version, parseErr := strconv.Atoi(rawVersion)
		if parseErr != nil || version < 1 {
			return invalidRequest(errors.New("version must be at least 1"))
		}
		record, err = h.db.GetAgentVersion(r.Context(), principal.WorkspaceUUID, agentID, version)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureID(principal, agentID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureAgent(agentID, 1, false))
			return nil
		}
		if errors.Is(err, db.ErrNotFound) {
			return agentNotFound(agentID, err)
		}
		return internalError("Could not retrieve agent", fmt.Errorf("retrieve agent %q: %w", agentID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(record))
	return nil
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) error {
	return h.update(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, agentID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, agentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureAgent(agentID, 2, false))
		return nil
	}

	body, err := httpapi.DecodeObjectBodyAs[agentMutationRequest](w, r, maxAgentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	if len(body.Version) == 0 {
		return invalidRequest(errors.New("version is required"))
	}
	expectedVersion, err := parseRequiredVersion(body.Version)
	if err != nil {
		return invalidRequest(err)
	}
	current, err := h.db.GetAgent(r.Context(), principal.WorkspaceUUID, agentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return agentNotFound(agentID, err)
		}
		return internalError("Could not update agent", fmt.Errorf("retrieve agent %q for update: %w", agentID, err))
	}
	nextState, err := h.stateFromUpdate(r, principal, current, body)
	if err != nil {
		return agentMutationError(err)
	}
	versionID, err := ids.New("agentver_")
	if err != nil {
		return internalError("Could not generate agent version ID", fmt.Errorf("generate agent version ID: %w", err))
	}
	updated, err := h.db.UpdateAgent(r.Context(), principal.WorkspaceUUID, agentID, expectedVersion, db.Agent{
		Name:        nextState.Name,
		Description: nextState.Description,
		System:      nextState.System,
		Model:       nextState.Model,
		MCPServers:  nextState.MCPServers,
		Metadata:    nextState.Metadata,
		Multiagent:  nextState.Multiagent,
		Skills:      nextState.Skills,
		Tools:       nextState.Tools,
		UpdatedAt:   time.Now().UTC(),
	}, versionID)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return archivedAgentCannotBeUpdated(err)
		}
		if errors.Is(err, db.ErrVersionConflict) {
			return agentVersionConflict(err)
		}
		if errors.Is(err, db.ErrNotFound) {
			return agentNotFound(agentID, err)
		}
		return internalError("Could not update agent", fmt.Errorf("update agent %q: %w", agentID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(updated))
	return nil
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) error {
	return h.archive(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request, agentID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, agentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureAgent(agentID, 1, true))
		return nil
	}
	record, err := h.deployments.ArchiveAgent(r.Context(), principal.WorkspaceUUID, agentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return agentNotFound(agentID, err)
		}
		return internalError("Could not archive agent", fmt.Errorf("archive agent %q: %w", agentID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(record))
	return nil
}

func (h *Handler) versionsRoute(w http.ResponseWriter, r *http.Request) error {
	return h.versions(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) versions(w http.ResponseWriter, r *http.Request, agentID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, agentID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: []agentResponse{h.fixtureAgent(agentID, 1, false)}})
		return nil
	}
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeVersionCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListAgentVersionsPage(r.Context(), db.ListAgentVersionsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		AgentExternalID: agentID,
		Limit:           limit,
		Cursor:          cursor,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return agentNotFound(agentID, err)
		}
		return internalError("Could not list agent versions", fmt.Errorf("list agent %q versions: %w", agentID, err))
	}
	data := responsesFromAgents(records)
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeVersionCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) stateFromCreate(r *http.Request, principal auth.Principal, agentID string, body *agentMutationRequest) (agentState, error) {
	var state agentState
	name, err := parseRequiredRawString(body.Name, "name")
	if err != nil {
		return agentState{}, err
	}
	state.Name = name
	if len(body.Model) == 0 {
		return agentState{}, errors.New("model is required")
	}
	model, err := h.normalizeConfiguredModel(
		r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, body.Model,
	)
	if err != nil {
		return agentState{}, err
	}
	if state.Model, err = jsonx.Encode(model); err != nil {
		return agentState{}, err
	}
	if len(body.Description) > 0 {
		if state.Description, err = nullableStringFromRaw(body.Description, "description"); err != nil {
			return agentState{}, err
		}
	}
	if len(body.System) > 0 {
		if state.System, err = nullableStringFromRaw(body.System, "system"); err != nil {
			return agentState{}, err
		}
	}
	if state.MCPServers, err = agentconfig.NormalizeMCPServers(rawOrDefault(body.MCPServers, `[]`)); err != nil {
		return agentState{}, err
	}
	if state.Metadata, err = httpapi.NormalizeMetadata(rawOrDefault(body.Metadata, `{}`), validateMetadata); err != nil {
		return agentState{}, err
	}
	if state.Skills, err = agentconfig.NormalizeSkills(rawOrDefault(body.Skills, `[]`)); err != nil {
		return agentState{}, err
	}
	if state.Tools, err = agentconfig.NormalizeTools(rawOrDefault(body.Tools, `[]`), state.MCPServers); err != nil {
		return agentState{}, err
	}
	if state.Multiagent, err = h.normalizeMultiagent(r, principal, agentID, 1, body.Multiagent); err != nil {
		return agentState{}, err
	}
	return state, nil
}

func (h *Handler) stateFromUpdate(r *http.Request, principal auth.Principal, current db.Agent, body *agentMutationRequest) (agentState, error) {
	modelRaw := append(json.RawMessage(nil), current.Model...)
	if len(body.Model) > 0 {
		model, err := h.normalizeConfiguredModel(
			r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, body.Model,
		)
		if err != nil {
			return agentState{}, err
		}
		modelRaw, err = jsonx.Encode(model)
		if err != nil {
			return agentState{}, err
		}
	}
	state := agentState{
		Name:        current.Name,
		Description: current.Description,
		System:      current.System,
		Model:       modelRaw,
		MCPServers:  current.MCPServers,
		Metadata:    current.Metadata,
		Multiagent:  current.Multiagent,
		Skills:      current.Skills,
		Tools:       current.Tools,
	}
	if len(body.Name) > 0 {
		name, err := parseRequiredRawString(body.Name, "name")
		if err != nil {
			return agentState{}, err
		}
		state.Name = name
	}
	if len(body.Description) > 0 {
		description, err := nullableStringFromRaw(body.Description, "description")
		if err != nil {
			return agentState{}, err
		}
		state.Description = description
	}
	if len(body.System) > 0 {
		system, err := nullableStringFromRaw(body.System, "system")
		if err != nil {
			return agentState{}, err
		}
		state.System = system
	}
	if len(body.MCPServers) > 0 {
		mcpServers, err := agentconfig.NormalizeMCPServers(clearableArray(body.MCPServers))
		if err != nil {
			return agentState{}, err
		}
		state.MCPServers = mcpServers
	}
	if len(body.Skills) > 0 {
		skills, err := agentconfig.NormalizeSkills(clearableArray(body.Skills))
		if err != nil {
			return agentState{}, err
		}
		state.Skills = skills
	}
	if len(body.Tools) > 0 {
		tools, err := agentconfig.NormalizeTools(clearableArray(body.Tools), state.MCPServers)
		if err != nil {
			return agentState{}, err
		}
		state.Tools = tools
	} else if len(body.MCPServers) > 0 {
		if _, err := agentconfig.NormalizeTools(jsonx.Default(state.Tools, `[]`), state.MCPServers); err != nil {
			return agentState{}, err
		}
	}
	if len(body.Metadata) > 0 {
		metadata, err := httpapi.PatchMetadata(state.Metadata, body.Metadata, validateMetadata)
		if err != nil {
			return agentState{}, err
		}
		state.Metadata = metadata
	}
	if len(body.Multiagent) > 0 {
		multiagent, err := h.normalizeMultiagent(r, principal, current.ExternalID, current.CurrentVersion+1, body.Multiagent)
		if err != nil {
			return agentState{}, err
		}
		state.Multiagent = multiagent
	}
	return state, nil
}

func (h *Handler) normalizeMultiagent(r *http.Request, principal auth.Principal, selfID string, selfVersion int, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var body struct {
		Type   string            `json:"type"`
		Agents []json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, errors.New("multiagent must be an object")
	}
	if body.Type != "coordinator" {
		return nil, errors.New("multiagent.type must be coordinator")
	}
	if len(body.Agents) < 1 || len(body.Agents) > 20 {
		return nil, errors.New("multiagent.agents must contain between 1 and 20 entries")
	}
	resolved := make([]agentReference, 0, len(body.Agents))
	seen := make(map[string]struct{}, len(body.Agents))
	selfCount := 0
	for _, item := range body.Agents {
		ref, isSelf, err := h.resolveRosterEntry(r, principal, selfID, selfVersion, item)
		if err != nil {
			return nil, err
		}
		if isSelf {
			selfCount++
			if selfCount > 1 {
				return nil, errors.New("multiagent.agents may contain at most one self entry")
			}
		}
		key := ref.ID
		if _, ok := seen[key]; ok {
			return nil, errors.New("multiagent.agents must reference distinct agents")
		}
		seen[key] = struct{}{}
		resolved = append(resolved, ref)
	}
	return jsonx.Encode(map[string]any{"agents": resolved, "type": "coordinator"})
}

func (h *Handler) resolveRosterEntry(r *http.Request, principal auth.Principal, selfID string, selfVersion int, raw json.RawMessage) (agentReference, bool, error) {
	var id string
	var version int
	var rawString string
	if json.Unmarshal(raw, &rawString) == nil {
		id = rawString
	} else {
		var object struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Version *int   `json:"version"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return agentReference{}, false, errors.New("multiagent agent entry must be a string or object")
		}
		switch object.Type {
		case "self":
			return agentReference{ID: selfID, Type: "agent", Version: selfVersion}, true, nil
		case "agent":
			id = object.ID
			if object.Version != nil {
				version = *object.Version
				if version < 1 {
					return agentReference{}, false, errors.New("multiagent agent version must be at least 1")
				}
			}
		default:
			return agentReference{}, false, errors.New("multiagent agent entry type must be agent or self")
		}
	}
	if strings.TrimSpace(id) == "" {
		return agentReference{}, false, errors.New("multiagent agent id must be non-empty")
	}
	if id == selfID && version == 0 {
		return agentReference{ID: selfID, Type: "agent", Version: selfVersion}, false, nil
	}
	if version > 0 {
		if _, err := h.db.GetAgentVersion(r.Context(), principal.WorkspaceUUID, id, version); err != nil {
			if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureReference(principal, id) {
				return agentReference{ID: id, Type: "agent", Version: version}, false, nil
			}
			return agentReference{}, false, errors.New("multiagent referenced agent version not found")
		}
		return agentReference{ID: id, Type: "agent", Version: version}, false, nil
	}
	record, err := h.db.GetAgent(r.Context(), principal.WorkspaceUUID, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureReference(principal, id) {
			return agentReference{ID: id, Type: "agent", Version: 1}, false, nil
		}
		return agentReference{}, false, errors.New("multiagent referenced agent not found")
	}
	if record.ArchivedAt != nil {
		return agentReference{}, false, errors.New("multiagent referenced agent must not be archived")
	}
	return agentReference{ID: id, Type: "agent", Version: record.CurrentVersion}, false, nil
}

func decodeSearchRequest(w http.ResponseWriter, r *http.Request) (*searchRequest, error) {
	body, err := httpapi.DecodeObjectBodyAs[searchRequest](w, r, maxAgentBodySize)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.Name) == "" {
		return nil, errors.New("name is required")
	}
	if body.Limit != nil && (*body.Limit < 0 || *body.Limit > 100) {
		return nil, errors.New("limit must be between 1 and 100")
	}
	return body, nil
}

func searchLimit(limit *int) int {
	if limit == nil || *limit == 0 {
		return 20
	}
	return *limit
}

func derefBool(value *bool) bool {
	return value != nil && *value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func parseRequiredRawString(raw json.RawMessage, name string) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("%s is required", name)
	}
	if httpapi.IsJSONNull(raw) {
		return "", fmt.Errorf("%s cannot be null", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

func nullableStringFromRaw(raw json.RawMessage, name string) (*string, error) {
	if httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s must be a string or null", name)
	}
	return &value, nil
}

func parseRequiredVersion(raw json.RawMessage) (int, error) {
	var version int
	if err := json.Unmarshal(raw, &version); err != nil || version < 1 {
		return 0, errors.New("version must be at least 1")
	}
	return version, nil
}

func (h *Handler) normalizeConfiguredModel(
	ctx context.Context,
	organizationUUID, workspaceUUID string,
	raw json.RawMessage,
) (agentconfig.Model, error) {
	model, err := agentconfig.NormalizeModel(raw)
	if err != nil {
		return agentconfig.Model{}, err
	}
	modelIDs, err := llmproviders.ListModelIDs(ctx, h.db, organizationUUID, workspaceUUID)
	if err != nil {
		return agentconfig.Model{}, configuredModelError(err)
	}
	for _, modelID := range modelIDs {
		if modelID == model.ID {
			return model, nil
		}
	}
	return agentconfig.Model{}, fmt.Errorf("model %q is not configured for this workspace", model.ID)
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > 16 {
		return errors.New("metadata must contain at most 16 keys")
	}
	for key, value := range metadata {
		if key == "" || len(key) > 64 {
			return errors.New("metadata keys must be between 1 and 64 characters")
		}
		if len(value) > 512 {
			return errors.New("metadata values must be at most 512 characters")
		}
	}
	return nil
}

func rawOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) > 0 {
		return raw
	}
	return json.RawMessage(fallback)
}

func clearableArray(raw json.RawMessage) json.RawMessage {
	if httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`)
	}
	return raw
}

func parseOptionalBool(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return value, nil
}

func encodeAgentCursor(agent db.Agent) string {
	data, _ := json.Marshal(map[string]any{
		"created_at": agent.CreatedAt.UTC().Format(time.RFC3339Nano),
		"id":         agent.UUID,
	})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeAgentCursor(raw string) (*db.AgentPageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var cursor struct {
		CreatedAt string `json:"created_at"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.ID == "" || cursor.CreatedAt == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.AgentPageCursor{CreatedAt: createdAt.UTC(), UUID: cursor.ID}, nil
}

func encodeVersionCursor(agent db.Agent) string {
	data, _ := json.Marshal(map[string]any{
		"id":      agent.UUID,
		"version": agent.CurrentVersion,
	})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeVersionCursor(raw string) (*db.AgentVersionPageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var cursor struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.ID == "" || cursor.Version < 1 {
		return nil, errors.New("page is invalid")
	}
	return &db.AgentVersionPageCursor{Version: cursor.Version, UUID: cursor.ID}, nil
}

func responsesFromAgents(records []db.Agent) []agentResponse {
	data := make([]agentResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromAgent(record))
	}
	return data
}

func responseFromAgent(agent db.Agent) agentResponse {
	return agentResponse{
		ID:          agent.ExternalID,
		ArchivedAt:  httpapi.OptionalTime(agent.ArchivedAt),
		CreatedAt:   httpapi.FormatTime(agent.CreatedAt),
		Description: agent.Description,
		MCPServers:  httpapi.RawOr(agent.MCPServers, `[]`),
		Metadata:    httpapi.RawOr(agent.Metadata, `{}`),
		Model:       httpapi.RawOr(agent.Model, `{}`),
		Multiagent:  httpapi.RawOr(agent.Multiagent, `null`),
		Name:        agent.Name,
		Skills:      httpapi.RawOr(agent.Skills, `[]`),
		System:      agent.System,
		Tools:       httpapi.RawOr(agent.Tools, `[]`),
		Type:        "agent",
		UpdatedAt:   httpapi.FormatTime(agent.UpdatedAt),
		Version:     agent.CurrentVersion,
	}
}

func (h *Handler) isOfficialSDKFixtureID(principal auth.Principal, agentID string) bool {
	return principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID && agentID == h.cfg.SDKFixtures.AgentID
}

func (h *Handler) isOfficialSDKFixtureReference(principal auth.Principal, agentID string) bool {
	return principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID &&
		(agentID == h.cfg.SDKFixtures.AgentID || agentID == h.cfg.SDKFixtures.ReferenceAgentID)
}

func (h *Handler) fixtureAgent(agentID string, version int, archived bool) agentResponse {
	now := time.Unix(0, 0).UTC()
	var archivedAt *string
	if archived {
		archivedAt = httpapi.OptionalTime(&now)
	}
	description := "A general-purpose starter agent."
	system := "You are a general-purpose agent that can research, write code, run commands, and use connected tools to complete the user's task end to end."
	return agentResponse{
		ID:          agentID,
		ArchivedAt:  archivedAt,
		CreatedAt:   httpapi.FormatTime(now),
		Description: &description,
		MCPServers:  json.RawMessage(`[{"name":"example-mcp","type":"url","url":"https://example-server.modelcontextprotocol.io/sse"}]`),
		Metadata:    json.RawMessage(`{"foo":"bar"}`),
		Model:       json.RawMessage(`{}`),
		Multiagent:  json.RawMessage(fmt.Sprintf(`{"agents":[{"id":%q,"type":"agent","version":1}],"type":"coordinator"}`, h.cfg.SDKFixtures.ReferenceAgentID)),
		Name:        "My First Agent",
		Skills:      json.RawMessage(`[{"skill_id":"xlsx","type":"anthropic","version":"1"}]`),
		System:      &system,
		Tools:       json.RawMessage(`[{"configs":[{"enabled":true,"name":"bash","permission_policy":{"type":"always_allow"}}],"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}},"type":"agent_toolset_20260401"}]`),
		Type:        "agent",
		UpdatedAt:   httpapi.FormatTime(now),
		Version:     version,
	}
}
