package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxAgentBodySize           = 4 << 20
	skillPrewarmEnqueueTimeout = 3 * time.Second
	mcpCatalogEnqueueTimeout   = 3 * time.Second
)

var customToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Handler struct {
	cfg     config.Config
	db      *db.DB
	prewarm skillPrewarmSnapshotEnqueuer
	mcp     mcpCatalogEnqueuer
	router  chi.Router
}

type skillPrewarmSnapshotEnqueuer interface {
	EnqueueSnapshot(ctx context.Context, workspaceID int64, snapshot json.RawMessage, source string, sourceID string, trigger string) error
}

type mcpCatalogEnqueuer interface {
	EnsureAgent(ctx context.Context, organizationID, workspaceID int64, mcpServers json.RawMessage, trigger string) error
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

func NewHandler(cfg config.Config, database *db.DB) *Handler {
	return NewHandlerWithSkillPrewarm(cfg, database, nil)
}

func NewHandlerWithSkillPrewarm(cfg config.Config, database *db.DB, prewarm skillPrewarmSnapshotEnqueuer) *Handler {
	return NewHandlerWithPrewarmers(cfg, database, prewarm, nil)
}

func NewHandlerWithPrewarmers(cfg config.Config, database *db.DB, prewarm skillPrewarmSnapshotEnqueuer, mcp mcpCatalogEnqueuer) *Handler {
	h := &Handler{cfg: cfg, db: database, prewarm: prewarm, mcp: mcp}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.create)
	router.Get("/", h.list)
	router.Get("/{agent_id}", h.retrieveRoute)
	router.Post("/{agent_id}", h.updateRoute)
	router.Post("/{agent_id}/archive", h.archiveRoute)
	router.Get("/{agent_id}/versions", h.versionsRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Agents API requires beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}

	fields, err := httpapi.DecodeObjectBody(w, r, maxAgentBodySize)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	agentID, err := ids.New("agent_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate agent ID"))
		return
	}
	state, err := h.stateFromCreate(r, principal, agentID, fields)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	versionID, err := ids.New("agentver_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate agent version ID"))
		return
	}
	now := time.Now().UTC()
	created, err := h.db.CreateAgent(r.Context(), db.Agent{
		UUID:              uuid.NewString(),
		ExternalID:        agentID,
		WorkspaceID:       principal.WorkspaceID,
		CreatedByAPIKeyID: principal.APIKeyID,
		CurrentVersion:    1,
		Name:              state.Name,
		Description:       state.Description,
		System:            state.System,
		Model:             state.Model,
		MCPServers:        state.MCPServers,
		Metadata:          state.Metadata,
		Multiagent:        state.Multiagent,
		Skills:            state.Skills,
		Tools:             state.Tools,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, versionID)
	if err != nil {
		log.Printf("create agent: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create agent"))
		return
	}
	h.enqueueSkillPrewarm(r.Context(), principal.WorkspaceID, created, "agent_create")
	h.enqueueMCPCatalog(r.Context(), principal.OrganizationID, principal.WorkspaceID, created.MCPServers, created.ExternalID, "agent_create")
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	cursor, err := decodeAgentCursor(r.URL.Query().Get("page"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}

	records, hasMore, err := h.db.ListAgentsPage(r.Context(), db.ListAgentsPageParams{
		WorkspaceID:     principal.WorkspaceID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLTE:    createdAtLTE,
	})
	if err != nil {
		log.Printf("list agents: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list agents"))
		return
	}
	data := responsesFromAgents(records)
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeAgentCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Agents API requires beta=true"))
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}
	body, err := decodeSearchRequest(w, r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	cursor, err := decodeAgentCursor(derefString(body.Page))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}

	records, hasMore, err := h.db.SearchAgentsPage(r.Context(), db.SearchAgentsPageParams{
		WorkspaceID:     principal.WorkspaceID,
		Name:            strings.TrimSpace(body.Name),
		Limit:           searchLimit(body.Limit),
		Cursor:          cursor,
		IncludeArchived: derefBool(body.IncludeArchived),
	})
	if err != nil {
		log.Printf("search agents: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not search agents"))
		return
	}
	data := responsesFromAgents(records)
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeAgentCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieve(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, agentID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	rawVersion := strings.TrimSpace(r.URL.Query().Get("version"))
	var record db.Agent
	var err error
	if rawVersion == "" {
		record, err = h.db.GetAgent(r.Context(), principal.WorkspaceID, agentID)
	} else {
		version, parseErr := strconv.Atoi(rawVersion)
		if parseErr != nil || version < 1 {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "version must be at least 1"))
			return
		}
		record, err = h.db.GetAgentVersion(r.Context(), principal.WorkspaceID, agentID, version)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureID(principal, agentID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureAgent(agentID, 1, false))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Agent not found: "+agentID))
			return
		}
		log.Printf("get agent: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve agent"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(record))
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) {
	h.update(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, agentID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, agentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureAgent(agentID, 2, false))
		return
	}

	fields, err := httpapi.DecodeObjectBody(w, r, maxAgentBodySize)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	rawVersion, ok := fields["version"]
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "version is required"))
		return
	}
	expectedVersion, err := parseRequiredVersion(rawVersion)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	current, err := h.db.GetAgent(r.Context(), principal.WorkspaceID, agentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Agent not found: "+agentID))
			return
		}
		log.Printf("get agent before update: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update agent"))
		return
	}
	nextState, err := h.stateFromUpdate(r, principal, current, fields)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	versionID, err := ids.New("agentver_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate agent version ID"))
		return
	}
	updated, err := h.db.UpdateAgent(r.Context(), principal.WorkspaceID, agentID, expectedVersion, db.Agent{
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
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Archived agents cannot be updated"))
			return
		}
		if errors.Is(err, db.ErrVersionConflict) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "conflict_error", "Agent version does not match current version"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Agent not found: "+agentID))
			return
		}
		log.Printf("update agent: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update agent"))
		return
	}
	if !agentsnapshot.SameRawJSON(current.Skills, updated.Skills) {
		h.enqueueSkillPrewarm(r.Context(), principal.WorkspaceID, updated, "agent_update")
	}
	if !agentsnapshot.SameRawJSON(current.MCPServers, updated.MCPServers) {
		h.enqueueMCPCatalog(r.Context(), principal.OrganizationID, principal.WorkspaceID, updated.MCPServers, updated.ExternalID, "agent_update")
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(updated))
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) {
	h.archive(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request, agentID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, agentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureAgent(agentID, 1, true))
		return
	}
	record, err := h.db.ArchiveAgent(r.Context(), principal.WorkspaceID, agentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Agent not found: "+agentID))
			return
		}
		log.Printf("archive agent: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not archive agent"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromAgent(record))
}

func (h *Handler) versionsRoute(w http.ResponseWriter, r *http.Request) {
	h.versions(w, r, chi.URLParam(r, "agent_id"))
}

func (h *Handler) versions(w http.ResponseWriter, r *http.Request, agentID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if h.isOfficialSDKFixtureID(principal, agentID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: []agentResponse{h.fixtureAgent(agentID, 1, false)}})
		return
	}
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	cursor, err := decodeVersionCursor(r.URL.Query().Get("page"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	records, hasMore, err := h.db.ListAgentVersionsPage(r.Context(), db.ListAgentVersionsPageParams{
		WorkspaceID:     principal.WorkspaceID,
		AgentExternalID: agentID,
		Limit:           limit,
		Cursor:          cursor,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Agent not found: "+agentID))
			return
		}
		log.Printf("list agent versions: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list agent versions"))
		return
	}
	data := responsesFromAgents(records)
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeVersionCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) stateFromCreate(r *http.Request, principal auth.Principal, agentID string, fields map[string]json.RawMessage) (agentState, error) {
	var state agentState
	name, err := parseRequiredStringField(fields, "name")
	if err != nil {
		return agentState{}, err
	}
	state.Name = name
	modelRaw, ok := fields["model"]
	if !ok {
		return agentState{}, errors.New("model is required")
	}
	model, err := normalizeModel(modelRaw)
	if err != nil {
		return agentState{}, err
	}
	state.Model = model
	if state.Description, err = parseNullableStringField(fields, "description"); err != nil {
		return agentState{}, err
	}
	if state.System, err = parseNullableStringField(fields, "system"); err != nil {
		return agentState{}, err
	}
	if state.MCPServers, err = normalizeMCPServers(fieldOrDefault(fields, "mcp_servers", `[]`)); err != nil {
		return agentState{}, err
	}
	if state.Metadata, err = httpapi.NormalizeMetadata(fieldOrDefault(fields, "metadata", `{}`), validateMetadata); err != nil {
		return agentState{}, err
	}
	if state.Skills, err = normalizeSkills(fieldOrDefault(fields, "skills", `[]`)); err != nil {
		return agentState{}, err
	}
	if state.Tools, err = normalizeTools(fieldOrDefault(fields, "tools", `[]`), state.MCPServers); err != nil {
		return agentState{}, err
	}
	if state.Multiagent, err = h.normalizeMultiagent(r, principal, agentID, 1, fields["multiagent"]); err != nil {
		return agentState{}, err
	}
	return state, nil
}

func (h *Handler) stateFromUpdate(r *http.Request, principal auth.Principal, current db.Agent, fields map[string]json.RawMessage) (agentState, error) {
	state := agentState{
		Name:        current.Name,
		Description: current.Description,
		System:      current.System,
		Model:       current.Model,
		MCPServers:  current.MCPServers,
		Metadata:    current.Metadata,
		Multiagent:  current.Multiagent,
		Skills:      current.Skills,
		Tools:       current.Tools,
	}
	if raw, ok := fields["name"]; ok {
		name, err := parseRequiredRawString(raw, "name")
		if err != nil {
			return agentState{}, err
		}
		state.Name = name
	}
	var err error
	if raw, ok := fields["model"]; ok {
		state.Model, err = normalizeModel(raw)
		if err != nil {
			return agentState{}, err
		}
	}
	if raw, ok := fields["description"]; ok {
		state.Description, err = nullableStringFromRaw(raw, "description")
		if err != nil {
			return agentState{}, err
		}
	}
	if raw, ok := fields["system"]; ok {
		state.System, err = nullableStringFromRaw(raw, "system")
		if err != nil {
			return agentState{}, err
		}
	}
	if raw, ok := fields["mcp_servers"]; ok {
		state.MCPServers, err = normalizeMCPServers(clearableArray(raw))
		if err != nil {
			return agentState{}, err
		}
	}
	if raw, ok := fields["skills"]; ok {
		state.Skills, err = normalizeSkills(clearableArray(raw))
		if err != nil {
			return agentState{}, err
		}
	}
	if raw, ok := fields["tools"]; ok {
		state.Tools, err = normalizeTools(clearableArray(raw), state.MCPServers)
		if err != nil {
			return agentState{}, err
		}
	} else if _, ok := fields["mcp_servers"]; ok {
		if err := validateMCPToolReferences(state.Tools, state.MCPServers); err != nil {
			return agentState{}, err
		}
	}
	if raw, ok := fields["metadata"]; ok {
		state.Metadata, err = httpapi.PatchMetadata(state.Metadata, raw, validateMetadata)
		if err != nil {
			return agentState{}, err
		}
	}
	if raw, ok := fields["multiagent"]; ok {
		state.Multiagent, err = h.normalizeMultiagent(r, principal, current.ExternalID, current.CurrentVersion+1, raw)
		if err != nil {
			return agentState{}, err
		}
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
	return httpapi.MarshalRaw(map[string]any{"agents": resolved, "type": "coordinator"})
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
		if _, err := h.db.GetAgentVersion(r.Context(), principal.WorkspaceID, id, version); err != nil {
			if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureReference(principal, id) {
				return agentReference{ID: id, Type: "agent", Version: version}, false, nil
			}
			return agentReference{}, false, errors.New("multiagent referenced agent version not found")
		}
		return agentReference{ID: id, Type: "agent", Version: version}, false, nil
	}
	record, err := h.db.GetAgent(r.Context(), principal.WorkspaceID, id)
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

func decodeSearchRequest(w http.ResponseWriter, r *http.Request) (searchRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentBodySize)
	var body searchRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		return searchRequest{}, errors.New("Invalid JSON body")
	}
	if strings.TrimSpace(body.Name) == "" {
		return searchRequest{}, errors.New("name is required")
	}
	if body.Limit != nil && (*body.Limit < 0 || *body.Limit > 100) {
		return searchRequest{}, errors.New("limit must be between 1 and 100")
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

func parseRequiredStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return parseRequiredRawString(raw, name)
}

func parseRequiredRawString(raw json.RawMessage, name string) (string, error) {
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

func parseNullableStringField(fields map[string]json.RawMessage, name string) (*string, error) {
	raw, ok := fields[name]
	if !ok {
		return nil, nil
	}
	return nullableStringFromRaw(raw, name)
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

func normalizeModel(raw json.RawMessage) (json.RawMessage, error) {
	if httpapi.IsJSONNull(raw) {
		return nil, errors.New("model cannot be null")
	}
	var modelID string
	if json.Unmarshal(raw, &modelID) == nil {
		if strings.TrimSpace(modelID) == "" {
			return nil, errors.New("model id must be non-empty")
		}
		return httpapi.MarshalRaw(map[string]any{"id": modelID, "speed": "standard"})
	}
	var model map[string]json.RawMessage
	if err := json.Unmarshal(raw, &model); err != nil {
		return nil, errors.New("model must be a string or object")
	}
	rawID, ok := model["id"]
	if !ok {
		return nil, errors.New("model.id is required")
	}
	if err := json.Unmarshal(rawID, &modelID); err != nil || strings.TrimSpace(modelID) == "" {
		return nil, errors.New("model.id must be a non-empty string")
	}
	normalized := map[string]any{"id": modelID, "speed": "standard"}
	if rawSpeed, ok := model["speed"]; ok && !httpapi.IsJSONNull(rawSpeed) {
		var speed string
		if err := json.Unmarshal(rawSpeed, &speed); err != nil || (speed != "standard" && speed != "fast") {
			return nil, errors.New("model.speed must be standard or fast")
		}
		normalized["speed"] = speed
	}
	return httpapi.MarshalRaw(normalized)
}

func normalizeMCPServers(raw json.RawMessage) (json.RawMessage, error) {
	if httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var servers []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, errors.New("mcp_servers must be an array")
	}
	if len(servers) > 20 {
		return nil, errors.New("mcp_servers must contain at most 20 servers")
	}
	seen := map[string]struct{}{}
	normalized := make([]map[string]string, 0, len(servers))
	for _, server := range servers {
		name, err := requiredRawString(server["name"], "mcp_servers.name")
		if err != nil {
			return nil, err
		}
		if len(name) > 255 {
			return nil, errors.New("mcp_servers.name must be at most 255 characters")
		}
		if _, ok := seen[name]; ok {
			return nil, errors.New("mcp_servers.name must be unique")
		}
		seen[name] = struct{}{}
		serverType, err := requiredRawString(server["type"], "mcp_servers.type")
		if err != nil {
			return nil, err
		}
		if serverType != "url" {
			return nil, errors.New("mcp_servers.type must be url")
		}
		url, err := requiredRawString(server["url"], "mcp_servers.url")
		if err != nil {
			return nil, err
		}
		if len(url) > 2048 {
			return nil, errors.New("mcp_servers.url must be at most 2048 characters")
		}
		normalized = append(normalized, map[string]string{"name": name, "type": "url", "url": url})
	}
	return httpapi.MarshalRaw(normalized)
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

func normalizeSkills(raw json.RawMessage) (json.RawMessage, error) {
	if httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var skills []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &skills); err != nil {
		return nil, errors.New("skills must be an array")
	}
	if len(skills) > 20 {
		return nil, errors.New("skills must contain at most 20 skills")
	}
	normalized := make([]map[string]string, 0, len(skills))
	for _, skill := range skills {
		skillType, err := requiredRawString(skill["type"], "skills.type")
		if err != nil {
			return nil, err
		}
		if skillType != "anthropic" && skillType != "custom" {
			return nil, errors.New("skills.type must be anthropic or custom")
		}
		skillID, err := requiredRawString(skill["skill_id"], "skills.skill_id")
		if err != nil {
			return nil, err
		}
		version := "latest"
		if rawVersion, ok := skill["version"]; ok && !httpapi.IsJSONNull(rawVersion) {
			if err := json.Unmarshal(rawVersion, &version); err != nil || version == "" {
				return nil, errors.New("skills.version must be a non-empty string")
			}
		}
		normalized = append(normalized, map[string]string{"skill_id": skillID, "type": skillType, "version": version})
	}
	return httpapi.MarshalRaw(normalized)
}

func normalizeTools(raw json.RawMessage, mcpServers json.RawMessage) (json.RawMessage, error) {
	if httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var tools []map[string]any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, errors.New("tools must be an array")
	}
	total := 0
	serverNames, err := mcpServerNames(mcpServers)
	if err != nil {
		return nil, err
	}
	referencedMCPServers := map[string]struct{}{}
	normalized := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		total++
		toolType, _ := tool["type"].(string)
		switch toolType {
		case "agent_toolset_20260401":
			defaultConfig, err := normalizeDefaultConfig(tool["default_config"], "always_allow")
			if err != nil {
				return nil, err
			}
			configs, err := normalizeAgentToolConfigs(tool["configs"], permissionPolicyType(defaultConfig, "always_allow"))
			if err != nil {
				return nil, err
			}
			total += len(configs)
			normalized = append(normalized, map[string]any{"configs": configs, "default_config": defaultConfig, "type": toolType})
		case "mcp_toolset":
			name, _ := tool["mcp_server_name"].(string)
			if name == "" {
				return nil, errors.New("mcp_toolset.mcp_server_name is required")
			}
			if _, ok := serverNames[name]; !ok {
				return nil, errors.New("mcp_toolset.mcp_server_name must reference an MCP server")
			}
			referencedMCPServers[name] = struct{}{}
			defaultConfig, err := normalizeDefaultConfig(tool["default_config"], "always_ask")
			if err != nil {
				return nil, err
			}
			configs, err := normalizeMCPToolConfigs(tool["configs"], permissionPolicyType(defaultConfig, "always_ask"))
			if err != nil {
				return nil, err
			}
			total += len(configs)
			normalized = append(normalized, map[string]any{"configs": configs, "default_config": defaultConfig, "mcp_server_name": name, "type": toolType})
		case "custom":
			custom, err := normalizeCustomTool(tool)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, custom)
		default:
			return nil, errors.New("tools.type must be agent_toolset_20260401, mcp_toolset, or custom")
		}
	}
	if len(referencedMCPServers) > 0 && len(referencedMCPServers) != len(serverNames) {
		return nil, errors.New("every mcp_servers entry must be referenced by an mcp_toolset")
	}
	if total > 128 {
		return nil, errors.New("tools must contain at most 128 total tools")
	}
	return httpapi.MarshalRaw(normalized)
}

func validateMCPToolReferences(tools json.RawMessage, mcpServers json.RawMessage) error {
	_, err := normalizeTools(tools, mcpServers)
	return err
}

func normalizeAgentToolConfigs(value any, defaultPolicy string) ([]map[string]any, error) {
	if value == nil {
		return []map[string]any{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("tools.configs must be an array")
	}
	var configs []map[string]any
	if err := json.Unmarshal(raw, &configs); err != nil {
		return nil, errors.New("tools.configs must be an array")
	}
	allowed := map[string]struct{}{
		"bash": {}, "edit": {}, "read": {}, "write": {}, "glob": {}, "grep": {}, "web_fetch": {}, "web_search": {},
	}
	normalized := make([]map[string]any, 0, len(configs))
	for _, config := range configs {
		name, _ := config["name"].(string)
		if _, ok := allowed[name]; !ok {
			return nil, errors.New("agent tool config name is invalid")
		}
		enabled, err := boolWithDefault(config["enabled"], true, "tools.configs.enabled")
		if err != nil {
			return nil, err
		}
		policy, err := normalizePermissionPolicy(config["permission_policy"], defaultPolicy)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, map[string]any{"enabled": enabled, "name": name, "permission_policy": policy})
	}
	return normalized, nil
}

func normalizeMCPToolConfigs(value any, defaultPolicy string) ([]map[string]any, error) {
	if value == nil {
		return []map[string]any{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("tools.configs must be an array")
	}
	var configs []map[string]any
	if err := json.Unmarshal(raw, &configs); err != nil {
		return nil, errors.New("tools.configs must be an array")
	}
	normalized := make([]map[string]any, 0, len(configs))
	for _, config := range configs {
		name, _ := config["name"].(string)
		if name == "" || len(name) > 128 {
			return nil, errors.New("mcp tool config name must be between 1 and 128 characters")
		}
		enabled, err := boolWithDefault(config["enabled"], true, "tools.configs.enabled")
		if err != nil {
			return nil, err
		}
		policy, err := normalizePermissionPolicy(config["permission_policy"], defaultPolicy)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, map[string]any{"enabled": enabled, "name": name, "permission_policy": policy})
	}
	return normalized, nil
}

func normalizeDefaultConfig(value any, defaultPolicy string) (map[string]any, error) {
	if value == nil {
		return map[string]any{"enabled": true, "permission_policy": map[string]string{"type": defaultPolicy}}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("default_config must be an object")
	}
	enabled, err := boolWithDefault(object["enabled"], true, "default_config.enabled")
	if err != nil {
		return nil, err
	}
	policy, err := normalizePermissionPolicy(object["permission_policy"], defaultPolicy)
	if err != nil {
		return nil, err
	}
	return map[string]any{"enabled": enabled, "permission_policy": policy}, nil
}

func normalizePermissionPolicy(value any, defaultPolicy string) (map[string]string, error) {
	if value == nil {
		return map[string]string{"type": defaultPolicy}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("permission_policy must be an object")
	}
	policyType, _ := object["type"].(string)
	if policyType != "always_allow" && policyType != "always_ask" {
		return nil, errors.New("permission_policy.type must be always_allow or always_ask")
	}
	return map[string]string{"type": policyType}, nil
}

func permissionPolicyType(config map[string]any, fallback string) string {
	policy, _ := config["permission_policy"].(map[string]string)
	if policy == nil {
		return fallback
	}
	if policy["type"] == "" {
		return fallback
	}
	return policy["type"]
}

func normalizeCustomTool(tool map[string]any) (map[string]any, error) {
	name, _ := tool["name"].(string)
	if !customToolNamePattern.MatchString(name) {
		return nil, errors.New("custom tool name must match ^[A-Za-z0-9_-]{1,128}$")
	}
	description, _ := tool["description"].(string)
	if len(description) < 1 || len(description) > 1024 {
		return nil, errors.New("custom tool description must be between 1 and 1024 characters")
	}
	schema, ok := tool["input_schema"].(map[string]any)
	if !ok {
		return nil, errors.New("custom tool input_schema must be an object")
	}
	schemaType, _ := schema["type"].(string)
	if schemaType != "object" {
		return nil, errors.New("custom tool input_schema.type must be object")
	}
	return map[string]any{"description": description, "input_schema": schema, "name": name, "type": "custom"}, nil
}

func mcpServerNames(raw json.RawMessage) (map[string]struct{}, error) {
	var servers []struct {
		Name string `json:"name"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, errors.New("stored mcp_servers are invalid")
		}
	}
	names := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		names[server.Name] = struct{}{}
	}
	return names, nil
}

func fieldOrDefault(fields map[string]json.RawMessage, name, fallback string) json.RawMessage {
	if raw, ok := fields[name]; ok {
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

func requiredRawString(raw json.RawMessage, name string) (string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return "", fmt.Errorf("%s is required", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return value, nil
}

func boolWithDefault(value any, fallback bool, name string) (bool, error) {
	if value == nil {
		return fallback, nil
	}
	if parsed, ok := value.(bool); ok {
		return parsed, nil
	}
	return false, fmt.Errorf("%s must be a boolean", name)
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
		"id":         agent.ID,
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
		ID        int64  `json:"id"`
	}
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.ID <= 0 || cursor.CreatedAt == "" {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.AgentPageCursor{CreatedAt: createdAt.UTC(), ID: cursor.ID}, nil
}

func encodeVersionCursor(agent db.Agent) string {
	data, _ := json.Marshal(map[string]any{
		"id":      agent.ID,
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
		ID      int64 `json:"id"`
		Version int   `json:"version"`
	}
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.ID <= 0 || cursor.Version < 1 {
		return nil, errors.New("page is invalid")
	}
	return &db.AgentVersionPageCursor{Version: cursor.Version, ID: cursor.ID}, nil
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

func (h *Handler) enqueueSkillPrewarm(ctx context.Context, workspaceID int64, agent db.Agent, trigger string) {
	if h == nil || h.prewarm == nil || !agentsnapshot.SkillsRawHasEntries(agent.Skills) {
		return
	}
	snapshot, err := agentsnapshot.FromAgent(agent)
	if err != nil {
		log.Printf("build agent skill prewarm snapshot agent_id=%s trigger=%s: %v", agent.ExternalID, trigger, err)
		return
	}
	enqueueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), skillPrewarmEnqueueTimeout)
	defer cancel()
	if err := h.prewarm.EnqueueSnapshot(enqueueCtx, workspaceID, snapshot, "agent", agent.ExternalID, trigger); err != nil {
		log.Printf("enqueue agent skill prewarm agent_id=%s trigger=%s: %v", agent.ExternalID, trigger, err)
	}
}

func (h *Handler) enqueueMCPCatalog(ctx context.Context, organizationID, workspaceID int64, mcpServers json.RawMessage, agentID, trigger string) {
	if h == nil || h.mcp == nil {
		return
	}
	requestContext := context.WithoutCancel(ctx)
	servers := append(json.RawMessage(nil), mcpServers...)
	go func() {
		enqueueCtx, cancel := context.WithTimeout(requestContext, mcpCatalogEnqueueTimeout)
		defer cancel()
		if err := h.mcp.EnsureAgent(enqueueCtx, organizationID, workspaceID, servers, trigger); err != nil {
			log.Printf("enqueue agent mcp catalog agent_id=%s trigger=%s: %v", agentID, trigger, err)
		}
	}()
}

func (h *Handler) isOfficialSDKFixtureID(principal auth.Principal, agentID string) bool {
	return principal.APIKeyExternalID == h.cfg.OfficialSDKResourceAPIKeyExternalID && agentID == h.cfg.OfficialSDKFixtureAgentID
}

func (h *Handler) isOfficialSDKFixtureReference(principal auth.Principal, agentID string) bool {
	return principal.APIKeyExternalID == h.cfg.OfficialSDKResourceAPIKeyExternalID &&
		(agentID == h.cfg.OfficialSDKFixtureAgentID || agentID == h.cfg.OfficialSDKFixtureReferenceAgentID)
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
		Model:       json.RawMessage(`{"id":"claude-opus-4-6","speed":"standard"}`),
		Multiagent:  json.RawMessage(fmt.Sprintf(`{"agents":[{"id":%q,"type":"agent","version":1}],"type":"coordinator"}`, h.cfg.OfficialSDKFixtureReferenceAgentID)),
		Name:        "My First Agent",
		Skills:      json.RawMessage(`[{"skill_id":"xlsx","type":"anthropic","version":"1"}]`),
		System:      &system,
		Tools:       json.RawMessage(`[{"configs":[{"enabled":true,"name":"bash","permission_policy":{"type":"always_allow"}}],"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}},"type":"agent_toolset_20260401"}]`),
		Type:        "agent",
		UpdatedAt:   httpapi.FormatTime(now),
		Version:     version,
	}
}
