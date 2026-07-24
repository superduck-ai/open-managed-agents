package environments

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxEnvironmentBodySize = 4 << 20

type Handler struct {
	cfg    config.Config
	db     *db.DB
	router chi.Router
}

type environmentResponse struct {
	ID          string          `json:"id"`
	ArchivedAt  *string         `json:"archived_at"`
	Config      json.RawMessage `json:"config"`
	CreatedAt   string          `json:"created_at"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
	Name        string          `json:"name"`
	Scope       string          `json:"scope"`
	State       string          `json:"state"`
	Type        string          `json:"type"`
	UpdatedAt   string          `json:"updated_at"`
}

type environmentPageResponse struct {
	Data     []environmentResponse `json:"data"`
	NextPage *string               `json:"next_page"`
}

type deleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type workResponse struct {
	ID                string          `json:"id"`
	AcknowledgedAt    *string         `json:"acknowledged_at"`
	CreatedAt         string          `json:"created_at"`
	Data              json.RawMessage `json:"data"`
	EnvironmentID     string          `json:"environment_id"`
	LatestHeartbeatAt *string         `json:"latest_heartbeat_at"`
	Metadata          json.RawMessage `json:"metadata"`
	Secret            *string         `json:"secret"`
	StartedAt         *string         `json:"started_at"`
	State             string          `json:"state"`
	StopRequestedAt   *string         `json:"stop_requested_at"`
	StoppedAt         *string         `json:"stopped_at"`
	Type              string          `json:"type"`
}

type workPageResponse struct {
	Data     []workResponse `json:"data"`
	NextPage *string        `json:"next_page"`
}

type heartbeatResponse struct {
	Type          string `json:"type"`
	LastHeartbeat string `json:"last_heartbeat"`
	LeaseExtended bool   `json:"lease_extended"`
	State         string `json:"state"`
	TTLSeconds    int    `json:"ttl_seconds"`
}

type workStatsResponse struct {
	Type           string  `json:"type"`
	Depth          int     `json:"depth"`
	Pending        int     `json:"pending"`
	OldestQueuedAt *string `json:"oldest_queued_at"`
	WorkersPolling *int    `json:"workers_polling"`
}

func NewHandler(cfg config.Config, database *db.DB) *Handler {
	h := &Handler{cfg: cfg, db: database}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.create)
	router.Get("/", h.list)
	router.Get("/{environment_id}", h.retrieveRoute)
	router.Post("/{environment_id}", h.updateRoute)
	router.Post("/{environment_id}/archive", h.archiveRoute)
	router.Delete("/{environment_id}", h.deleteRoute)
	router.Get("/{environment_id}/work/poll", h.pollWorkRoute)
	router.Get("/{environment_id}/work/stats", h.workStatsRoute)
	router.Get("/{environment_id}/work", h.listWorkRoute)
	router.Get("/{environment_id}/work/{work_id}", h.retrieveWorkRoute)
	router.Post("/{environment_id}/work/{work_id}", h.updateWorkRoute)
	router.Post("/{environment_id}/work/{work_id}/ack", h.ackWorkRoute)
	router.Post("/{environment_id}/work/{work_id}/heartbeat", h.heartbeatWorkRoute)
	router.Post("/{environment_id}/work/{work_id}/stop", h.stopWorkRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Environments API requires beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if h.isOfficialSDKPrincipal(principal) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureEnvironment(h.cfg.SDKFixtures.EnvironmentID, false))
		return
	}
	name, err := parseRequiredStringField(fields, "name")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	description, err := parseOptionalDescription(fields)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	metadata, err := normalizeMetadata(fieldOrDefault(fields, "metadata", `{}`))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	scope, err := parseScope(fields["scope"])
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	configRaw, err := normalizeConfigForCreate(fields["config"])
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	envID, err := ids.New("env_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate environment ID"))
		return
	}
	now := time.Now().UTC()
	created, err := h.db.CreateEnvironment(r.Context(), db.Environment{
		UUID:              uuid.NewString(),
		ExternalID:        envID,
		OrganizationID:    principal.OrganizationID,
		WorkspaceID:       principal.WorkspaceID,
		CreatedByAPIKeyID: principal.APIKeyID,
		Name:              name,
		Description:       description,
		Config:            configRaw,
		Metadata:          metadata,
		Scope:             scope,
		Provider:          "e2b",
		ResolvedTemplate:  h.resolvedTemplate(configRaw),
		CreatedAt:         now,
	})
	if err != nil {
		if errors.Is(err, db.ErrDuplicate) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "conflict_error", "Environment name already exists"))
			return
		}
		log.Printf("create environment: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create environment"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromEnvironment(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	limit, err := parseLimit(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeEnvironmentCursor(r.URL.Query().Get("page"))
	if err != nil {
		if h.isOfficialSDKPrincipal(principal) {
			httpapi.WriteJSON(w, http.StatusOK, environmentPageResponse{Data: []environmentResponse{h.fixtureEnvironment(h.cfg.SDKFixtures.EnvironmentID, false)}})
			return
		}
		writeBadRequest(w, r, err)
		return
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListEnvironmentsPage(r.Context(), db.ListEnvironmentsPageParams{
		WorkspaceID:     principal.WorkspaceID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		log.Printf("list environments: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list environments"))
		return
	}
	if h.isOfficialSDKPrincipal(principal) && len(records) == 0 {
		httpapi.WriteJSON(w, http.StatusOK, environmentPageResponse{Data: []environmentResponse{h.fixtureEnvironment(h.cfg.SDKFixtures.EnvironmentID, false)}})
		return
	}
	data := make([]environmentResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromEnvironment(record))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeEnvironmentCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, environmentPageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieve(w, r, chi.URLParam(r, "environment_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, environmentID string) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	record, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceID, environmentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKEnvironmentFixture(principal, environmentID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureEnvironment(environmentID, false))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return
		}
		log.Printf("get environment: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve environment"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromEnvironment(record))
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) {
	h.update(w, r, chi.URLParam(r, "environment_id"))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, environmentID string) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	if h.isOfficialSDKEnvironmentFixture(principal, environmentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureEnvironment(environmentID, false))
		return
	}
	current, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceID, environmentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return
		}
		log.Printf("get environment before update: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update environment"))
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	next := current
	if raw, ok := fields["name"]; ok {
		next.Name, err = parseRequiredRawString(raw, "name")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["description"]; ok {
		next.Description, err = descriptionFromRaw(raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["metadata"]; ok {
		next.Metadata, err = patchMetadata(next.Metadata, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["scope"]; ok {
		next.Scope, err = parseScope(raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["config"]; ok {
		next.Config, err = normalizeConfigForUpdate(current.Config, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		next.ResolvedTemplate = h.resolvedTemplate(next.Config)
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateEnvironment(r.Context(), principal.WorkspaceID, environmentID, next)
	if err != nil {
		if errors.Is(err, db.ErrDuplicate) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "conflict_error", "Environment name already exists"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return
		}
		log.Printf("update environment: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update environment"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromEnvironment(updated))
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) {
	h.archive(w, r, chi.URLParam(r, "environment_id"))
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request, environmentID string) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	if h.isOfficialSDKEnvironmentFixture(principal, environmentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureEnvironment(environmentID, true))
		return
	}
	record, err := h.db.ArchiveEnvironment(r.Context(), principal.WorkspaceID, environmentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return
		}
		log.Printf("archive environment: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not archive environment"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromEnvironment(record))
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	environmentID := chi.URLParam(r, "environment_id")
	if h.isOfficialSDKEnvironmentFixture(principal, environmentID) {
		httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: environmentID, Type: "environment_deleted"})
		return
	}
	if err := h.db.DeleteEnvironment(r.Context(), principal.WorkspaceID, environmentID); err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Environment has active work"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return
		}
		log.Printf("delete environment: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete environment"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: environmentID, Type: "environment_deleted"})
}

func (h *Handler) listWorkRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	if h.isOfficialSDKRequest(r) {
		httpapi.WriteJSON(w, http.StatusOK, workPageResponse{Data: []workResponse{h.fixtureWork(env.ExternalID, h.cfg.SDKFixtures.WorkID, "queued")}})
		return
	}
	limit, err := parseLimit(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeWorkCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListEnvironmentWorkPage(r.Context(), db.ListEnvironmentWorkPageParams{
		WorkspaceID:           env.WorkspaceID,
		EnvironmentExternalID: env.ExternalID,
		Limit:                 limit,
		Cursor:                cursor,
	})
	if err != nil {
		log.Printf("list environment work: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list environment work"))
		return
	}
	data := make([]workResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromWork(record))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeWorkCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, workPageResponse{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveWorkRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	workID := chi.URLParam(r, "work_id")
	if h.isOfficialSDKWorkFixture(r, workID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureWork(env.ExternalID, workID, "queued"))
		return
	}
	record, err := h.db.GetEnvironmentWork(r.Context(), env.WorkspaceID, env.ExternalID, workID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Work not found: "+workID))
			return
		}
		log.Printf("get environment work: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve environment work"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromWork(record))
}

func (h *Handler) updateWorkRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	workID := chi.URLParam(r, "work_id")
	if h.isOfficialSDKWorkFixture(r, workID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureWork(env.ExternalID, workID, "queued"))
		return
	}
	fields, err := decodeObjectBody(w, r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	current, err := h.db.GetEnvironmentWork(r.Context(), env.WorkspaceID, env.ExternalID, workID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Work not found: "+workID))
			return
		}
		log.Printf("get environment work before update: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update environment work"))
		return
	}
	metadata := current.Metadata
	if raw, ok := fields["metadata"]; ok {
		metadata, err = patchMetadata(metadata, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	updated, err := h.db.UpdateEnvironmentWorkMetadata(r.Context(), env.WorkspaceID, env.ExternalID, workID, metadata)
	if err != nil {
		log.Printf("update environment work: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update environment work"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromWork(updated))
}

func (h *Handler) pollWorkRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	if h.isOfficialSDKRequest(r) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureWork(env.ExternalID, h.cfg.SDKFixtures.WorkID, "queued"))
		return
	}
	blockFor, err := parseBlockMS(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	claimFor, err := parseReclaimMS(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	workerID := strings.TrimSpace(r.Header.Get("Anthropic-Worker-ID"))
	deadline := time.Now().Add(blockFor)
	for {
		work, err := h.db.PollEnvironmentWork(r.Context(), env.WorkspaceID, env.ExternalID, workerID, claimFor)
		if err != nil {
			log.Printf("poll environment work: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not poll environment work"))
			return
		}
		if work != nil {
			httpapi.WriteJSON(w, http.StatusOK, responseFromWork(*work))
			return
		}
		if blockFor <= 0 || time.Now().After(deadline) {
			httpapi.WriteJSON(w, http.StatusOK, nil)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (h *Handler) workStatsRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	if h.isOfficialSDKRequest(r) {
		httpapi.WriteJSON(w, http.StatusOK, workStatsResponse{Type: "work_queue_stats"})
		return
	}
	stats, err := h.db.EnvironmentWorkStats(r.Context(), env.WorkspaceID, env.ExternalID)
	if err != nil {
		log.Printf("environment work stats: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve environment work stats"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromStats(stats))
}

func (h *Handler) ackWorkRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	workID := chi.URLParam(r, "work_id")
	if h.isOfficialSDKWorkFixture(r, workID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureWork(env.ExternalID, workID, "starting"))
		return
	}
	record, err := h.db.AckEnvironmentWork(r.Context(), env.WorkspaceID, env.ExternalID, workID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Work not found: "+workID))
			return
		}
		log.Printf("ack environment work: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not ack environment work"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromWork(record))
}

func (h *Handler) heartbeatWorkRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	workID := chi.URLParam(r, "work_id")
	if h.isOfficialSDKWorkFixture(r, workID) {
		httpapi.WriteJSON(w, http.StatusOK, heartbeatResponse{
			Type:          "work_heartbeat",
			LastHeartbeat: formatTime(time.Now().UTC()),
			LeaseExtended: true,
			State:         "active",
			TTLSeconds:    60,
		})
		return
	}
	ttl, err := parseOptionalInt(r, "desired_ttl_seconds")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	expected := strings.TrimSpace(r.URL.Query().Get("expected_last_heartbeat"))
	result, err := h.db.HeartbeatEnvironmentWork(r.Context(), env.WorkspaceID, env.ExternalID, workID, expected, ttl, formatTime)
	if err != nil {
		if errors.Is(err, db.ErrPreconditionFailed) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusPreconditionFailed, "invalid_request_error", "Heartbeat precondition failed"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Work not found: "+workID))
			return
		}
		log.Printf("heartbeat environment work: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not heartbeat environment work"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, heartbeatResponse{
		Type:          "work_heartbeat",
		LastHeartbeat: result.LastHeartbeat,
		LeaseExtended: result.LeaseExtended,
		State:         result.Work.State,
		TTLSeconds:    result.TTLSeconds,
	})
}

func (h *Handler) stopWorkRoute(w http.ResponseWriter, r *http.Request) {
	env, ok := h.authorizeWork(w, r)
	if !ok {
		return
	}
	force := false
	if r.Body != nil {
		fields, err := decodeObjectBody(w, r)
		if err == nil {
			if raw, ok := fields["force"]; ok && !isJSONNull(raw) {
				if err := json.Unmarshal(raw, &force); err != nil {
					writeBadRequest(w, r, errors.New("force must be a boolean"))
					return
				}
			}
		}
	}
	workID := chi.URLParam(r, "work_id")
	if h.isOfficialSDKWorkFixture(r, workID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureWork(env.ExternalID, workID, "stopped"))
		return
	}
	current, err := h.db.GetEnvironmentWork(r.Context(), env.WorkspaceID, env.ExternalID, workID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Work not found: "+workID))
			return
		}
		log.Printf("retrieve environment work before stop: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not stop environment work"))
		return
	}
	if force {
		if err := h.killSandboxForWork(r.Context(), env, current); err != nil {
			log.Printf("kill environment sandbox for work %s: %v", workID, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not stop environment sandbox"))
			return
		}
	}
	record, err := h.db.StopEnvironmentWork(r.Context(), env.WorkspaceID, env.ExternalID, workID, force)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Work not found: "+workID))
			return
		}
		log.Printf("stop environment work: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not stop environment work"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromWork(record))
}

func (h *Handler) killSandboxForWork(ctx context.Context, env db.Environment, work db.EnvironmentWork) error {
	sandbox, err := h.db.GetActiveEnvironmentSandboxForWork(ctx, env.WorkspaceID, env.ExternalID, work.ExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if sandbox.ProviderSandboxID == nil || strings.TrimSpace(*sandbox.ProviderSandboxID) == "" {
		return nil
	}
	providerSandboxID := *sandbox.ProviderSandboxID
	if err := h.db.UpdateEnvironmentSandboxState(ctx, env.WorkspaceID, sandbox.ExternalID, "stopping", &providerSandboxID, nil, nil); err != nil {
		return err
	}
	if err := e2bruntime.NewProvider(h.cfg.E2B).Kill(ctx, providerSandboxID); err != nil {
		message := err.Error()
		_ = h.db.UpdateEnvironmentSandboxState(ctx, env.WorkspaceID, sandbox.ExternalID, "failed", &providerSandboxID, &message, nil)
		return err
	}
	stoppedAt := time.Now().UTC()
	return h.db.UpdateEnvironmentSandboxState(ctx, env.WorkspaceID, sandbox.ExternalID, "stopped", &providerSandboxID, nil, &stoppedAt)
}

func (h *Handler) authorizeWork(w http.ResponseWriter, r *http.Request) (db.Environment, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return db.Environment{}, false
	}
	environmentID := chi.URLParam(r, "environment_id")
	if principal.CredentialType == "environment_key" && principal.EnvironmentExternalID != environmentID {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
		return db.Environment{}, false
	}
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceID, environmentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKEnvironmentFixture(principal, environmentID) {
			return db.Environment{
				ExternalID:       environmentID,
				OrganizationID:   principal.OrganizationID,
				WorkspaceID:      principal.WorkspaceID,
				Name:             "python-data-analysis",
				Description:      "Fixture environment",
				Config:           defaultCloudConfig(),
				Metadata:         json.RawMessage(`{}`),
				Provider:         "e2b",
				ResolvedTemplate: h.resolvedTemplate(nil),
				CreatedAt:        time.Now().UTC(),
				UpdatedAt:        time.Now().UTC(),
			}, true
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return db.Environment{}, false
		}
		log.Printf("authorize environment work: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve environment"))
		return db.Environment{}, false
	}
	return env, true
}

func requireWorkspaceCredential(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !isWorkspaceCredential(principal) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	return principal, true
}

func isWorkspaceCredential(principal auth.Principal) bool {
	return principal.CredentialType == auth.CredentialTypeAPIKey ||
		principal.CredentialType == auth.CredentialTypePlatformSession
}

func (h *Handler) resolvedTemplate(json.RawMessage) string {
	if strings.TrimSpace(h.cfg.E2B.Template) == "" {
		return config.DefaultE2BTemplate
	}
	return h.cfg.E2B.Template
}

func responseFromEnvironment(env db.Environment) environmentResponse {
	scope := "organization"
	if env.Scope != nil && strings.TrimSpace(*env.Scope) != "" {
		scope = *env.Scope
	}
	state := "active"
	if env.ArchivedAt != nil {
		state = "archived"
	}
	return environmentResponse{
		ID:          env.ExternalID,
		ArchivedAt:  optionalTime(env.ArchivedAt),
		Config:      platformEnvironmentConfigForResponse(env.Config),
		CreatedAt:   formatTime(env.CreatedAt),
		Description: env.Description,
		Metadata:    platformEnvironmentMetadataForResponse(env.Metadata),
		Name:        env.Name,
		Scope:       scope,
		State:       state,
		Type:        "environment",
		UpdatedAt:   formatTime(env.UpdatedAt),
	}
}

func responseFromWork(work db.EnvironmentWork) workResponse {
	return workResponse{
		ID:                work.ExternalID,
		AcknowledgedAt:    optionalTime(work.AcknowledgedAt),
		CreatedAt:         formatTime(work.CreatedAt),
		Data:              work.Data,
		EnvironmentID:     work.EnvironmentExternalID,
		LatestHeartbeatAt: optionalTime(work.LatestHeartbeatAt),
		Metadata:          work.Metadata,
		Secret:            work.Secret,
		StartedAt:         optionalTime(work.StartedAt),
		State:             work.State,
		StopRequestedAt:   optionalTime(work.StopRequestedAt),
		StoppedAt:         optionalTime(work.StoppedAt),
		Type:              "work",
	}
}

func responseFromStats(stats db.EnvironmentWorkStats) workStatsResponse {
	var oldest *string
	if stats.OldestQueuedAt != nil {
		value := formatTime(*stats.OldestQueuedAt)
		oldest = &value
	}
	return workStatsResponse{
		Type:           "work_queue_stats",
		Depth:          stats.Depth,
		Pending:        stats.Pending,
		OldestQueuedAt: oldest,
		WorkersPolling: platformWorkersPollingForResponse(stats.WorkersPolling),
	}
}

func (h *Handler) fixtureEnvironment(environmentID string, archived bool) environmentResponse {
	now := formatTime(time.Now().UTC())
	var archivedAt *string
	if archived {
		archivedAt = &now
	}
	return environmentResponse{
		ID:          environmentID,
		ArchivedAt:  archivedAt,
		Config:      platformEnvironmentConfigForResponse(defaultCloudConfig()),
		CreatedAt:   now,
		Description: "Fixture environment",
		Metadata:    json.RawMessage(`{}`),
		Name:        "python-data-analysis",
		Scope:       "organization",
		State:       environmentStateForArchived(archived),
		Type:        "environment",
		UpdatedAt:   now,
	}
}

func environmentStateForArchived(archived bool) string {
	if archived {
		return "archived"
	}
	return "active"
}

func platformEnvironmentConfigForResponse(raw json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return platformDefaultCloudConfigForResponse()
	}
	configType := rawStringOrEmpty(fields["type"])
	switch configType {
	case "self_hosted":
		out, _ := marshalRaw(map[string]any{"type": "self_hosted"})
		return out
	case "", "cloud":
		out, _ := marshalRaw(map[string]any{
			"type":        "cloud",
			"packages":    platformPackagesForResponse(fields["packages"]),
			"networking":  platformNetworkingForResponse(fields["networking"]),
			"init_script": rawStringOrEmpty(fields["init_script"]),
			"environment": platformObjectForResponse(fields["environment"]),
		})
		return out
	default:
		return raw
	}
}

func platformDefaultCloudConfigForResponse() json.RawMessage {
	out, _ := marshalRaw(map[string]any{
		"type":        "cloud",
		"packages":    platformPackagesForResponse(nil),
		"networking":  platformNetworkingForResponse(nil),
		"init_script": "",
		"environment": map[string]any{},
	})
	return out
}

func platformPackagesForResponse(raw json.RawMessage) *environmentPackages {
	packages := emptyPackages()
	if len(raw) > 0 && !isJSONNull(raw) {
		_ = json.Unmarshal(raw, packages)
	}
	packages.Type = managerPackageType
	packages.ensureLists()
	return packages
}

func platformNetworkingForResponse(raw json.RawMessage) map[string]any {
	var fields map[string]json.RawMessage
	if len(raw) > 0 && !isJSONNull(raw) {
		_ = json.Unmarshal(raw, &fields)
	}
	networkType := rawStringOrEmpty(fields["type"])
	if networkType == "" {
		networkType = "limited"
	}
	return map[string]any{
		"type":                   networkType,
		"allow_mcp_servers":      platformBoolForResponse(fields["allow_mcp_servers"]),
		"allow_package_managers": platformBoolForResponse(fields["allow_package_managers"]),
		"allowed_hosts":          platformStringArrayForResponse(fields["allowed_hosts"]),
	}
}

func platformEnvironmentMetadataForResponse(raw json.RawMessage) json.RawMessage {
	return platformObjectRawForResponse(raw)
}

func platformObjectForResponse(raw json.RawMessage) map[string]any {
	var value map[string]any
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{}
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{}
	}
	if value == nil {
		return map[string]any{}
	}
	return value
}

func platformObjectRawForResponse(raw json.RawMessage) json.RawMessage {
	out, _ := marshalRaw(platformObjectForResponse(raw))
	return out
}

func platformStringArrayForResponse(raw json.RawMessage) []string {
	var values []string
	if len(raw) == 0 || isJSONNull(raw) {
		return []string{}
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	if values == nil {
		return []string{}
	}
	return values
}

func platformBoolForResponse(raw json.RawMessage) bool {
	var value bool
	if len(raw) == 0 || isJSONNull(raw) {
		return false
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return value
}

func platformWorkersPollingForResponse(value *int) *int {
	if value != nil {
		return value
	}
	zero := 0
	return &zero
}

func (h *Handler) fixtureWork(environmentID, workID, state string) workResponse {
	now := formatTime(time.Now().UTC())
	return workResponse{
		ID:            workID,
		CreatedAt:     now,
		Data:          json.RawMessage(`{"type":"session","id":"session_id"}`),
		EnvironmentID: environmentID,
		Metadata:      json.RawMessage(`{}`),
		State:         state,
		Type:          "work",
	}
}

func (h *Handler) isOfficialSDKPrincipal(principal auth.Principal) bool {
	return principal.CredentialType == "api_key" &&
		principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID
}

func (h *Handler) isOfficialSDKRequest(r *http.Request) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKPrincipal(principal)
}

func (h *Handler) isOfficialSDKEnvironmentFixture(principal auth.Principal, environmentID string) bool {
	return h.isOfficialSDKPrincipal(principal) &&
		environmentID == h.cfg.SDKFixtures.EnvironmentID
}

func (h *Handler) isOfficialSDKWorkFixture(r *http.Request, workID string) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKPrincipal(principal) &&
		workID == h.cfg.SDKFixtures.WorkID
}

func decodeObjectBody(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEnvironmentBodySize)
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&fields); err != nil {
		return nil, errors.New("Invalid JSON body")
	}
	if fields == nil {
		return nil, errors.New("JSON body must be an object")
	}
	return fields, nil
}

func parseRequiredStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return parseRequiredRawString(raw, name)
}

func parseRequiredRawString(raw json.RawMessage, name string) (string, error) {
	if isJSONNull(raw) {
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

func parseOptionalDescription(fields map[string]json.RawMessage) (string, error) {
	raw, ok := fields["description"]
	if !ok {
		return "", nil
	}
	return descriptionFromRaw(raw)
}

func descriptionFromRaw(raw json.RawMessage) (string, error) {
	if isJSONNull(raw) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("description must be a string or null")
	}
	return value, nil
}

func parseScope(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("scope must be organization, account, or null")
	}
	if value != "organization" && value != "account" {
		return nil, errors.New("scope must be organization or account")
	}
	return &value, nil
}

func normalizeConfigForCreate(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return defaultCloudConfig(), nil
	}
	return normalizeFullConfig(raw)
}

func normalizeConfigForUpdate(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return defaultCloudConfig(), nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config must be an object")
	}
	configType := rawStringOrEmpty(fields["type"])
	if configType == "" {
		configType = rawConfigType(current)
	}
	if configType == "self_hosted" {
		return marshalRaw(map[string]any{"type": "self_hosted"})
	}
	if configType != "cloud" {
		return nil, errors.New("config.type must be cloud or self_hosted")
	}
	base := normalizedCloudValue(defaultCloudConfig())
	if rawConfigType(current) == "cloud" {
		base = normalizedCloudValue(current)
	}
	if rawPackages, ok := fields["packages"]; ok {
		packages, err := normalizePackages(rawPackages)
		if err != nil {
			return nil, err
		}
		base["packages"] = packages
	}
	if rawNetworking, ok := fields["networking"]; ok {
		networking, err := normalizeNetworking(rawNetworking)
		if err != nil {
			return nil, err
		}
		base["networking"] = networking
	}
	return marshalRaw(base)
}

func normalizeFullConfig(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config must be an object")
	}
	configType := rawStringOrEmpty(fields["type"])
	if configType == "" {
		configType = "cloud"
	}
	switch configType {
	case "self_hosted":
		return marshalRaw(map[string]any{"type": "self_hosted"})
	case "cloud":
		packages, err := normalizePackages(fields["packages"])
		if err != nil {
			return nil, err
		}
		networking, err := normalizeNetworking(fields["networking"])
		if err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"type": "cloud", "packages": packages, "networking": networking})
	default:
		return nil, errors.New("config.type must be cloud or self_hosted")
	}
}

func defaultCloudConfig() json.RawMessage {
	raw, _ := marshalRaw(map[string]any{
		"type":       "cloud",
		"packages":   emptyPackages(),
		"networking": map[string]any{"type": "unrestricted"},
	})
	return raw
}

func normalizedCloudValue(raw json.RawMessage) map[string]any {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		value = map[string]any{}
	}
	if _, ok := value["packages"]; !ok {
		value["packages"] = emptyPackages()
	}
	if _, ok := value["networking"]; !ok {
		value["networking"] = map[string]any{"type": "unrestricted"}
	}
	value["type"] = "cloud"
	return value
}

func normalizeNetworking(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{"type": "unrestricted"}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("config.networking must be an object or null")
	}
	networkType := rawStringOrEmpty(fields["type"])
	if networkType == "" {
		networkType = "unrestricted"
	}
	switch networkType {
	case "unrestricted":
		return map[string]any{"type": "unrestricted"}, nil
	case "limited":
		hosts := []string{}
		if rawHosts, ok := fields["allowed_hosts"]; ok && !isJSONNull(rawHosts) {
			values, err := stringArray(rawHosts, "config.networking.allowed_hosts")
			if err != nil {
				return nil, err
			}
			for _, host := range values {
				if err := networkpolicy.ValidateAllowedHost(host); err != nil {
					return nil, err
				}
			}
			hosts = values
		}
		allowMCP, err := optionalBool(fields["allow_mcp_servers"], false, "config.networking.allow_mcp_servers")
		if err != nil {
			return nil, err
		}
		allowPackages, err := optionalBool(fields["allow_package_managers"], false, "config.networking.allow_package_managers")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":                   "limited",
			"allowed_hosts":          hosts,
			"allow_mcp_servers":      allowMCP,
			"allow_package_managers": allowPackages,
		}, nil
	default:
		return nil, errors.New("config.networking.type must be unrestricted or limited")
	}
}

func normalizeMetadata(raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, errors.New("metadata must be an object with string values")
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	return marshalRaw(metadata)
}

func patchMetadata(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	if isJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var patch map[string]*string
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, errors.New("metadata must be an object with string or null values")
	}
	var metadata map[string]string
	if len(current) == 0 || isJSONNull(current) {
		metadata = map[string]string{}
	} else if err := json.Unmarshal(current, &metadata); err != nil {
		return nil, errors.New("existing metadata is invalid")
	}
	for key, value := range patch {
		if value == nil || *value == "" {
			delete(metadata, key)
			continue
		}
		metadata[key] = *value
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	return marshalRaw(metadata)
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > 16 {
		return errors.New("metadata may contain at most 16 entries")
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

func stringArray(raw json.RawMessage, name string) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s entries must be non-empty strings", name)
		}
		if len(value) > 255 {
			return nil, fmt.Errorf("%s entries must be at most 255 characters", name)
		}
	}
	return values, nil
}

func optionalBool(raw json.RawMessage, fallback bool, name string) (bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return fallback, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func rawStringOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func rawConfigType(raw json.RawMessage) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	return rawStringOrEmpty(fields["type"])
}

func fieldOrDefault(fields map[string]json.RawMessage, name, fallback string) json.RawMessage {
	if raw, ok := fields[name]; ok {
		return raw
	}
	return json.RawMessage(fallback)
}

func parseLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, errors.New("limit must be between 1 and 100")
	}
	return limit, nil
}

func parseOptionalBool(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, nil
	}
	switch strings.ToLower(raw) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", name)
	}
}

func parseOptionalInt(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return value, nil
}

func parseBlockMS(r *http.Request) (time.Duration, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("block_ms"))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 999 {
		return 0, errors.New("block_ms must be between 1 and 999")
	}
	return time.Duration(value) * time.Millisecond, nil
}

func parseReclaimMS(r *http.Request) (time.Duration, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("reclaim_older_than_ms"))
	if raw == "" {
		return 5 * time.Second, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("reclaim_older_than_ms must be positive")
	}
	return time.Duration(value) * time.Millisecond, nil
}

func encodeEnvironmentCursor(env db.Environment) string {
	data, _ := json.Marshal(map[string]any{"created_at": formatTime(env.CreatedAt), "id": env.ID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeEnvironmentCursor(raw string) (*db.EnvironmentPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var payload struct {
		CreatedAt string `json:"created_at"`
		ID        int64  `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.ID <= 0 {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.EnvironmentPageCursor{CreatedAt: createdAt, ID: payload.ID}, nil
}

func encodeWorkCursor(work db.EnvironmentWork) string {
	data, _ := json.Marshal(map[string]any{"created_at": formatTime(work.CreatedAt), "id": work.ID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeWorkCursor(raw string) (*db.EnvironmentWorkPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	var payload struct {
		CreatedAt string `json:"created_at"`
		ID        int64  `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.ID <= 0 {
		return nil, errors.New("page is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, errors.New("page is invalid")
	}
	return &db.EnvironmentWorkPageCursor{CreatedAt: createdAt, ID: payload.ID}, nil
}

func isJSONNull(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	return json.Unmarshal(raw, &value) == nil && value == nil
}

func marshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func optionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
}
