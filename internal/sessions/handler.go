package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxSessionBodySize = 4 << 20

type Handler struct {
	cfg          config.Config
	db           *db.DB
	codeSessions *codesessions.Service
	router       chi.Router

	mu          sync.Mutex
	nextSubID   int64
	subscribers map[int64]subscriber
}

type subscriber struct {
	sessionID           string
	threadID            string
	includeStreamDeltas bool
	ch                  chan db.SessionEvent
}

type pageResponse[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

type sessionResponse struct {
	ID                 string            `json:"id"`
	Agent              json.RawMessage   `json:"agent"`
	ArchivedAt         *string           `json:"archived_at"`
	CreatedAt          string            `json:"created_at"`
	DeploymentID       *string           `json:"deployment_id,omitempty"`
	EnvironmentID      string            `json:"environment_id"`
	Metadata           json.RawMessage   `json:"metadata"`
	OutcomeEvaluations json.RawMessage   `json:"outcome_evaluations"`
	Resources          []json.RawMessage `json:"resources"`
	Stats              json.RawMessage   `json:"stats"`
	Status             string            `json:"status"`
	Title              *string           `json:"title"`
	Type               string            `json:"type"`
	UpdatedAt          string            `json:"updated_at"`
	Usage              json.RawMessage   `json:"usage"`
	VaultIDs           json.RawMessage   `json:"vault_ids"`
}

type threadResponse struct {
	ID             string          `json:"id"`
	Agent          json.RawMessage `json:"agent"`
	ArchivedAt     *string         `json:"archived_at"`
	CreatedAt      string          `json:"created_at"`
	ParentThreadID *string         `json:"parent_thread_id"`
	SessionID      string          `json:"session_id"`
	Stats          json.RawMessage `json:"stats"`
	Status         string          `json:"status"`
	Type           string          `json:"type"`
	UpdatedAt      string          `json:"updated_at"`
	Usage          json.RawMessage `json:"usage"`
}

type deleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type sendEventsResponse struct {
	Data []json.RawMessage `json:"data,omitempty"`
}

type resourceReferenceError struct {
	ResourceType string
	ResourceID   string
	Err          error
}

func (e resourceReferenceError) Error() string {
	return e.ResourceType + " reference failed: " + e.ResourceID
}

func (e resourceReferenceError) Unwrap() error {
	return e.Err
}

func NewHandler(cfg config.Config, database *db.DB, codeSessionServices ...*codesessions.Service) *Handler {
	codeSessionService := codesessions.NewService(cfg, database)
	if len(codeSessionServices) > 0 && codeSessionServices[0] != nil {
		codeSessionService = codeSessionServices[0]
	}
	h := &Handler{cfg: cfg, db: database, codeSessions: codeSessionService, subscribers: map[int64]subscriber{}}
	codeSessionService.SetPublicEventSink(h)
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.create)
	router.Get("/", h.list)
	router.Route("/{session_id}", func(r chi.Router) {
		r.Get("/", h.retrieveRoute)
		r.Post("/", h.updateRoute)
		r.Delete("/", h.deleteRoute)
		r.Post("/archive", h.archiveRoute)
		r.Route("/events", func(r chi.Router) {
			r.Get("/", h.listEventsRoute)
			r.Post("/", h.sendEventsRoute)
			r.Get("/stream", h.streamEventsRoute)
		})
		r.Route("/resources", func(r chi.Router) {
			r.Get("/", h.listResourcesRoute)
			r.Post("/", h.addResourceRoute)
			r.Get("/{resource_id}", h.retrieveResourceRoute)
			r.Post("/{resource_id}", h.updateResourceRoute)
			r.Delete("/{resource_id}", h.deleteResourceRoute)
		})
		r.Route("/threads", func(r chi.Router) {
			r.Get("/", h.listThreadsRoute)
			r.Get("/{thread_id}", h.retrieveThreadRoute)
			r.Post("/{thread_id}/archive", h.archiveThreadRoute)
			r.Get("/{thread_id}/events", h.listThreadEventsRoute)
			r.Get("/{thread_id}/stream", h.streamThreadEventsRoute)
		})
	})
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Sessions API requires beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireSessionManager(w, r)
	if !ok {
		return
	}
	fields, err := httpapi.DecodeObjectBody(w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if h.isOfficialSDKFixturePrincipal(principal) && h.createUsesOfficialFixtures(fields) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return
	}

	agent, snapshot, err := h.resolveAgent(r, principal, fields["agent"])
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	environmentID, err := parseRequiredStringField(fields, "environment_id")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceID, environmentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return
		}
		log.Printf("get environment for session: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create session"))
		return
	}
	if env.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("environment must not be archived"))
		return
	}
	metadata, err := httpapi.NormalizeMetadata(fieldOrDefault(fields, "metadata", `{}`), validateMetadataEntries)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	title, err := parseNullableStringField(fields, "title")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	vaultIDs, err := h.normalizeVaultIDs(r, principal, fieldOrDefault(fields, "vault_ids", `[]`))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}

	sessionID, err := ids.New("sesn_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate session ID"))
		return
	}
	threadID, err := ids.New("sthr_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate thread ID"))
		return
	}
	workID, err := ids.New("work_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate work ID"))
		return
	}
	now := time.Now().UTC()
	resources, err := h.resourcesFromCreate(r, principal, sessionID, fields["resources"], now)
	if err != nil {
		writeResourceBuildError(w, r, err)
		return
	}
	workData, _ := httpapi.MarshalRaw(map[string]any{"id": sessionID, "type": "session"})
	created, thread, _, _, err := h.db.CreateSession(r.Context(), db.CreateSessionInput{
		Session: db.Session{
			UUID:                  uuid.NewString(),
			ExternalID:            sessionID,
			OrganizationID:        principal.OrganizationID,
			WorkspaceID:           principal.WorkspaceID,
			CreatedByAPIKeyID:     principal.APIKeyID,
			EnvironmentID:         env.ID,
			EnvironmentExternalID: env.ExternalID,
			AgentID:               agent.ID,
			AgentExternalID:       agent.ExternalID,
			AgentVersion:          agent.CurrentVersion,
			AgentSnapshot:         snapshot,
			Title:                 title,
			Metadata:              metadata,
			VaultIDs:              vaultIDs,
			Status:                "idle",
			Usage:                 json.RawMessage(`{}`),
			Stats:                 json.RawMessage(`{}`),
			OutcomeEvaluations:    json.RawMessage(`[]`),
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		Thread: db.SessionThread{
			UUID:           uuid.NewString(),
			ExternalID:     threadID,
			OrganizationID: principal.OrganizationID,
			WorkspaceID:    principal.WorkspaceID,
			AgentSnapshot:  snapshot,
			Status:         "idle",
			Usage:          json.RawMessage(`{}`),
			Stats:          json.RawMessage(`{}`),
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Resources: resources,
		Work: db.EnvironmentWork{
			UUID:                  uuid.NewString(),
			ExternalID:            workID,
			OrganizationID:        principal.OrganizationID,
			WorkspaceID:           principal.WorkspaceID,
			EnvironmentID:         env.ID,
			EnvironmentExternalID: env.ExternalID,
			Data:                  workData,
			Metadata:              json.RawMessage(`{}`),
			State:                 "queued",
			CreatedAt:             now,
			UpdatedAt:             now,
		},
	})
	if err != nil {
		log.Printf("create session: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create session"))
		return
	}
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.created", created.ExternalID, nil)
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.pending", created.ExternalID, nil)
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.status_idled", created.ExternalID, nil)
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.thread_created", created.ExternalID, &thread.ExternalID)
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.thread_idled", created.ExternalID, &thread.ExternalID)
	response, err := h.responseFromSession(r, created)
	if err != nil {
		log.Printf("load session response: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create session"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireSessionManager(w, r)
	if !ok {
		return
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeSessionCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	order, err := parseOrder(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	agentVersion, err := parseOptionalPositiveInt(r, "agent_version")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtGT, err := httpapi.ParseOptionalTime(r, "created_at[gt]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtLT, err := httpapi.ParseOptionalTime(r, "created_at[lt]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	statuses, err := parseRepeatedStatuses(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListSessionsPage(r.Context(), db.ListSessionsPageParams{
		WorkspaceID:     principal.WorkspaceID,
		Limit:           limit,
		Cursor:          cursor,
		Order:           order,
		IncludeArchived: includeArchived,
		AgentExternalID: strings.TrimSpace(r.URL.Query().Get("agent_id")),
		AgentVersion:    agentVersion,
		DeploymentID:    strings.TrimSpace(r.URL.Query().Get("deployment_id")),
		MemoryStoreID:   strings.TrimSpace(r.URL.Query().Get("memory_store_id")),
		Statuses:        statuses,
		CreatedAtGT:     createdAtGT,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLT:     createdAtLT,
		CreatedAtLTE:    createdAtLTE,
	})
	if err != nil {
		log.Printf("list sessions: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list sessions"))
		return
	}
	data := make([]sessionResponse, 0, len(records))
	for _, record := range records {
		response, err := h.responseFromSession(r, record)
		if err != nil {
			log.Printf("list session response: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list sessions"))
			return
		}
		data = append(data, response)
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeSessionCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[sessionResponse]{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return
	}
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessRead)
	if !ok {
		return
	}
	response, err := h.responseFromSession(r, session)
	if err != nil {
		log.Printf("retrieve session response: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve session"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireSessionManager(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.OfficialSDKFixtureSessionID {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return
	}
	current, err := h.db.GetSession(r.Context(), principal.WorkspaceID, sessionID)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	if current.ArchivedAt != nil || current.Status != "idle" {
		writeBadRequest(w, r, errors.New("session must be idle and unarchived to update"))
		return
	}
	fields, err := httpapi.DecodeObjectBody(w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if _, ok := fields["vault_ids"]; ok {
		writeBadRequest(w, r, errors.New("vault_ids updates are not supported"))
		return
	}
	next := current
	if raw, ok := fields["title"]; ok {
		next.Title, err = nullableStringFromRaw(raw, "title")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["metadata"]; ok {
		next.Metadata, err = httpapi.PatchMetadata(next.Metadata, raw, validateMetadataEntries)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["agent"]; ok {
		next.AgentSnapshot, err = patchSessionAgent(next.AgentSnapshot, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateSession(r.Context(), principal.WorkspaceID, sessionID, next)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	event, err := h.sessionUpdatedEvent(updated)
	if err == nil {
		h.appendAndBroadcastInternal(r, updated.ExternalID, []db.SessionEvent{event})
	}
	response, err := h.responseFromSession(r, updated)
	if err != nil {
		log.Printf("update session response: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not update session"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireSessionManager(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.OfficialSDKFixtureSessionID {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), true))
		return
	}
	current, err := h.db.GetSession(r.Context(), principal.WorkspaceID, sessionID)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	if current.Status == "running" || current.Status == "rescheduling" {
		writeBadRequest(w, r, errors.New("running sessions cannot be archived"))
		return
	}
	archived, err := h.db.ArchiveSession(r.Context(), principal.WorkspaceID, sessionID)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.archived", archived.ExternalID, nil)
	response, err := h.responseFromSession(r, archived)
	if err != nil {
		log.Printf("archive session response: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not archive session"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireSessionManager(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.OfficialSDKFixtureSessionID {
		httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: sessionID, Type: "session_deleted"})
		return
	}
	current, err := h.db.GetSession(r.Context(), principal.WorkspaceID, sessionID)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	if current.Status == "running" || current.Status == "rescheduling" {
		writeBadRequest(w, r, errors.New("running sessions cannot be deleted"))
		return
	}
	deletedEvent, err := h.simpleSessionEvent("session.deleted", sessionID, nil)
	if err == nil {
		if current.ArchivedAt == nil {
			h.appendAndBroadcastInternal(r, sessionID, []db.SessionEvent{deletedEvent})
		} else {
			deletedEvent.SessionExternalID = sessionID
			h.broadcast(deletedEvent)
		}
	}
	deleted, err := h.db.DeleteSession(r.Context(), principal.WorkspaceID, sessionID)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.deleted", deleted.ExternalID, nil)
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: sessionID, Type: "session_deleted"})
}

func (h *Handler) listEventsRoute(w http.ResponseWriter, r *http.Request) {
	h.listEvents(w, r, chi.URLParam(r, "session_id"), "")
}

func (h *Handler) listThreadEventsRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessEventsRead)
	if !ok {
		return
	}
	if _, err := h.db.GetSessionThread(r.Context(), workspaceIDFromRequest(r), sessionID, threadID); err != nil {
		writeThreadLoadError(w, r, err, threadID)
		return
	}
	if err := h.backfillSubagentThreadEventsIfEmpty(r.Context(), session, threadID); err != nil {
		log.Printf("backfill subagent thread events session_id=%s thread_id=%s: %v", sessionID, threadID, err)
	}
	h.listEvents(w, r, sessionID, threadID)
}

func (h *Handler) backfillSubagentThreadEventsIfEmpty(ctx context.Context, session db.Session, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if h == nil || h.codeSessions == nil || threadID == "" {
		return nil
	}
	existing, _, err := h.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceID:       session.WorkspaceID,
		SessionExternalID: session.ExternalID,
		ThreadExternalID:  threadID,
		Limit:             1,
		Order:             "asc",
	})
	if err != nil || len(existing) > 0 {
		return err
	}
	codeSession, err := h.db.GetCodeSessionBySessionExternalID(ctx, session.WorkspaceID, session.ExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return h.codeSessions.PublishSubagentInternalEvents(ctx, codeSession)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request, sessionID, threadID string) {
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: []json.RawMessage{}})
		return
	}
	_, ok := h.authorizeSession(w, r, sessionID, sessionAccessEventsRead)
	if !ok {
		return
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeEventCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	order, err := parseOrder(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtGT, err := httpapi.ParseOptionalTime(r, "created_at[gt]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtLT, err := httpapi.ParseOptionalTime(r, "created_at[lt]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListSessionEventsPage(r.Context(), db.ListSessionEventsPageParams{
		WorkspaceID:       workspaceIDFromRequest(r),
		SessionExternalID: sessionID,
		ThreadExternalID:  threadID,
		PrimaryOnly:       threadID == "",
		Limit:             limit,
		Cursor:            cursor,
		Order:             order,
		Types:             parseRepeatedQuery(r, "types[]", "types"),
		CreatedAtGT:       createdAtGT,
		CreatedAtGTE:      createdAtGTE,
		CreatedAtLT:       createdAtLT,
		CreatedAtLTE:      createdAtLTE,
	})
	if err != nil {
		log.Printf("list session events: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list events"))
		return
	}
	hiddenPrimaryToolUseIDs, err := h.primaryOrphanToolUseIDsWithChildCopies(r.Context(), sessionID, threadID, records)
	if err != nil {
		log.Printf("list session events child tool projections: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list events"))
		return
	}
	data := make([]json.RawMessage, 0, len(records))
	for _, record := range records {
		if !maevents.IsPublicSessionHistoryEvent(record.EventType) {
			continue
		}
		if primaryToolProjectionHidden(record, hiddenPrimaryToolUseIDs) {
			continue
		}
		data = append(data, sessionEventPayloadForResponse(record, threadID))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeEventCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: data, NextPage: nextPage})
}

func (h *Handler) sendEventsRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	fields, err := httpapi.DecodeObjectBody(w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	rawEvents, ok := fields["events"]
	if !ok {
		writeBadRequest(w, r, errors.New("events is required"))
		return
	}
	var inputs []json.RawMessage
	if err := json.Unmarshal(rawEvents, &inputs); err != nil || len(inputs) == 0 {
		writeBadRequest(w, r, errors.New("events must be a non-empty array"))
		return
	}
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		now := time.Now().UTC()
		data := make([]json.RawMessage, 0, len(inputs))
		for _, raw := range inputs {
			payload, err := normalizeFixtureEvent(raw, now)
			if err != nil {
				writeBadRequest(w, r, err)
				return
			}
			data = append(data, payload)
		}
		httpapi.WriteJSON(w, http.StatusOK, sendEventsResponse{Data: data})
		return
	}
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessEventsSend)
	if !ok {
		return
	}
	now := time.Now().UTC()
	events := make([]db.SessionEvent, 0, len(inputs))
	var outcomesChanged bool
	for _, raw := range inputs {
		event, changed, err := h.normalizeInputEvent(r.Context(), session, raw, now)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		outcomesChanged = outcomesChanged || changed
		events = append(events, event)
	}
	created, err := h.db.AppendSessionEvents(r.Context(), session.WorkspaceID, session.ExternalID, events)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			writeBadRequest(w, r, errors.New("archived sessions do not accept new events"))
			return
		}
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	for _, event := range created {
		h.broadcast(event)
	}
	if h.codeSessions != nil {
		if err := h.codeSessions.QueuePublicSessionEvents(r.Context(), session, created); err != nil {
			log.Printf("queue session events for code session session_id=%s: %v", session.ExternalID, err)
		}
	}
	if outcomesChanged {
		webhooks.Enqueue(r.Context(), h.db, h.cfg, session.WorkspaceID, organizationExternalIDFromRequest(r), workspaceExternalIDFromRequest(r), "session.outcome_evaluation_ended", session.ExternalID, nil)
	}
	data := make([]json.RawMessage, 0, len(created))
	for _, event := range created {
		data = append(data, sessionEventPayload(event))
	}
	httpapi.WriteJSON(w, http.StatusOK, sendEventsResponse{Data: data})
}

func (h *Handler) streamEventsRoute(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, chi.URLParam(r, "session_id"), "")
}

func (h *Handler) StreamEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	h.streamEvents(w, r, sessionID, "")
}

func (h *Handler) streamThreadEventsRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	if h.isFixtureThread(r, sessionID, threadID) {
		h.streamEvents(w, r, sessionID, threadID)
		return
	}
	if _, ok := h.authorizeSession(w, r, sessionID, sessionAccessEventsRead); !ok {
		return
	}
	if _, err := h.db.GetSessionThread(r.Context(), workspaceIDFromRequest(r), sessionID, threadID); err != nil {
		writeThreadLoadError(w, r, err, threadID)
		return
	}
	h.streamEvents(w, r, sessionID, threadID)
}

func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request, sessionID, threadID string) {
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessEventsRead)
	if !ok {
		return
	}
	subscribeThreadID := threadID
	if subscribeThreadID == "" {
		primary, err := h.ensurePrimarySessionThread(r.Context(), session)
		if err != nil {
			writeSessionLoadError(w, r, err, sessionID)
			return
		}
		subscribeThreadID = primary.ExternalID
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Streaming is not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	subID, ch := h.subscribe(sessionID, subscribeThreadID, acceptsStreamDeltas(r))
	defer h.unsubscribe(subID)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case event := <-ch:
			writeSSE(w, event, threadID)
			flusher.Flush()
		}
	}
}

func (h *Handler) addResourceRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureResource(time.Now().UTC()))
		return
	}
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessManageResources)
	if !ok {
		return
	}
	fields, err := httpapi.DecodeObjectBody(w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	resource, err := h.resourceFromFields(r, session, fields, time.Now().UTC())
	if err != nil {
		writeResourceBuildError(w, r, err)
		return
	}
	created, err := h.db.CreateSessionResource(r.Context(), resource)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromResource(created))
}

func (h *Handler) listResourcesRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: []json.RawMessage{h.fixtureResource(time.Now().UTC())}})
		return
	}
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessRead)
	if !ok {
		return
	}
	resources, err := h.db.ListSessionResources(r.Context(), session.WorkspaceID, session.ExternalID)
	if err != nil {
		log.Printf("list session resources: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list resources"))
		return
	}
	data := resourcesToResponses(resources)
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: data})
}

func (h *Handler) retrieveResourceRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	resourceID := chi.URLParam(r, "resource_id")
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessRead)
	if !ok {
		return
	}
	if h.isFixtureResource(r, sessionID, resourceID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureResource(time.Now().UTC()))
		return
	}
	resource, err := h.db.GetSessionResource(r.Context(), session.WorkspaceID, session.ExternalID, resourceID)
	if err != nil {
		writeResourceLoadError(w, r, err, resourceID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromResource(resource))
}

func (h *Handler) updateResourceRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	resourceID := chi.URLParam(r, "resource_id")
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessManageResources)
	if !ok {
		return
	}
	if h.isFixtureResource(r, sessionID, resourceID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureResource(time.Now().UTC()))
		return
	}
	current, err := h.db.GetSessionResource(r.Context(), session.WorkspaceID, session.ExternalID, resourceID)
	if err != nil {
		writeResourceLoadError(w, r, err, resourceID)
		return
	}
	if current.ResourceType != "github_repository" {
		writeBadRequest(w, r, errors.New("only github_repository resources can be updated"))
		return
	}
	fields, err := httpapi.DecodeObjectBody(w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	token, err := parseRequiredStringField(fields, "authorization_token")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	secret, _ := httpapi.MarshalRaw(map[string]any{"authorization_token": token})
	updated, err := h.db.UpdateSessionResource(r.Context(), session.WorkspaceID, session.ExternalID, resourceID, current.Payload, secret)
	if err != nil {
		writeResourceLoadError(w, r, err, resourceID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromResource(updated))
}

func (h *Handler) deleteResourceRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	resourceID := chi.URLParam(r, "resource_id")
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessManageResources)
	if !ok {
		return
	}
	if h.isFixtureResource(r, sessionID, resourceID) {
		httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: resourceID, Type: "session_resource_deleted"})
		return
	}
	if err := h.db.DeleteSessionResource(r.Context(), session.WorkspaceID, session.ExternalID, resourceID); err != nil {
		writeResourceLoadError(w, r, err, resourceID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: resourceID, Type: "session_resource_deleted"})
}

func (h *Handler) listThreadsRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[threadResponse]{Data: []threadResponse{h.fixtureThread(time.Now().UTC(), false)}})
		return
	}
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessRead)
	if !ok {
		return
	}
	if _, err := h.ensurePrimarySessionThread(r.Context(), session); err != nil {
		log.Printf("ensure primary session thread session_id=%s: %v", session.ExternalID, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list threads"))
		return
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("limit")) == "" {
		limit = 500
	}
	cursor, err := decodeThreadCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListSessionThreadsPage(r.Context(), db.ListSessionThreadsPageParams{
		WorkspaceID:       session.WorkspaceID,
		SessionExternalID: session.ExternalID,
		Limit:             limit,
		Cursor:            cursor,
	})
	if err != nil {
		log.Printf("list session threads: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list threads"))
		return
	}
	data := make([]threadResponse, 0, len(records))
	for _, thread := range records {
		data = append(data, responseFromThread(thread))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeThreadCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[threadResponse]{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveThreadRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessRead)
	if !ok {
		return
	}
	if h.isFixtureThread(r, sessionID, threadID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureThread(time.Now().UTC(), false))
		return
	}
	thread, err := h.db.GetSessionThread(r.Context(), session.WorkspaceID, session.ExternalID, threadID)
	if err != nil {
		writeThreadLoadError(w, r, err, threadID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromThread(thread))
}

func (h *Handler) archiveThreadRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireSessionManager(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	if h.isFixtureThread(r, sessionID, threadID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureThread(time.Now().UTC(), true))
		return
	}
	session, err := h.db.GetSession(r.Context(), principal.WorkspaceID, sessionID)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return
	}
	thread, err := h.db.ArchiveSessionThread(r.Context(), principal.WorkspaceID, session.ExternalID, threadID)
	if err != nil {
		writeThreadLoadError(w, r, err, threadID)
		return
	}
	webhooks.Enqueue(r.Context(), h.db, h.cfg, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.thread_terminated", session.ExternalID, &thread.ExternalID)
	httpapi.WriteJSON(w, http.StatusOK, responseFromThread(thread))
}

type sessionAccess string

const (
	sessionAccessRead            sessionAccess = "read"
	sessionAccessEventsRead      sessionAccess = "events_read"
	sessionAccessEventsSend      sessionAccess = "events_send"
	sessionAccessManageResources sessionAccess = "manage_resources"
)

func (h *Handler) authorizeSession(w http.ResponseWriter, r *http.Request, sessionID string, access sessionAccess) (db.Session, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return db.Session{}, false
	}
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.OfficialSDKFixtureSessionID {
		return h.fixtureDBSession(principal), true
	}
	session, err := h.db.GetSession(r.Context(), principal.WorkspaceID, sessionID)
	if err != nil {
		writeSessionLoadError(w, r, err, sessionID)
		return db.Session{}, false
	}
	if isSessionManagerCredential(principal) {
		return session, true
	}
	if principal.CredentialType != auth.CredentialTypeEnvironmentKey {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return db.Session{}, false
	}
	if session.EnvironmentExternalID != principal.EnvironmentExternalID {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Environment key cannot access this session"))
		return db.Session{}, false
	}
	switch access {
	case sessionAccessRead, sessionAccessEventsRead, sessionAccessEventsSend:
		return session, true
	default:
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Environment key cannot manage this session"))
		return db.Session{}, false
	}
}

func requireSessionManager(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	if !isSessionManagerCredential(principal) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Environment key cannot manage sessions"))
		return auth.Principal{}, false
	}
	return principal, true
}

func isSessionManagerCredential(principal auth.Principal) bool {
	return principal.CredentialType == auth.CredentialTypeAPIKey ||
		principal.CredentialType == auth.CredentialTypePlatformSession
}

func workspaceIDFromRequest(r *http.Request) int64 {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return principal.WorkspaceID
}

func organizationExternalIDFromRequest(r *http.Request) string {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return principal.OrganizationExternalID
}

func workspaceExternalIDFromRequest(r *http.Request) string {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return principal.WorkspaceExternalID
}

func (h *Handler) resolveAgent(r *http.Request, principal auth.Principal, raw json.RawMessage) (db.Agent, json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return db.Agent{}, nil, errors.New("agent is required")
	}
	var agentID string
	var version int
	if json.Unmarshal(raw, &agentID) != nil {
		var object struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Version *int   `json:"version"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return db.Agent{}, nil, errors.New("agent must be a string or object")
		}
		if object.Type != "" && object.Type != "agent" {
			return db.Agent{}, nil, errors.New("agent.type must be agent")
		}
		agentID = object.ID
		if object.Version != nil {
			version = *object.Version
			if version < 1 {
				return db.Agent{}, nil, errors.New("agent.version must be at least 1")
			}
		}
	}
	if strings.TrimSpace(agentID) == "" {
		return db.Agent{}, nil, errors.New("agent id must be non-empty")
	}
	var agent db.Agent
	var err error
	if version > 0 {
		agent, err = h.db.GetAgentVersion(r.Context(), principal.WorkspaceID, agentID, version)
	} else {
		agent, err = h.db.GetAgent(r.Context(), principal.WorkspaceID, agentID)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.Agent{}, nil, errors.New("agent not found")
		}
		return db.Agent{}, nil, err
	}
	if agent.ArchivedAt != nil {
		return db.Agent{}, nil, errors.New("agent must not be archived")
	}
	snapshot, err := snapshotFromAgent(agent)
	if err != nil {
		return db.Agent{}, nil, err
	}
	return agent, snapshot, nil
}

func snapshotFromAgent(agent db.Agent) (json.RawMessage, error) {
	return httpapi.MarshalRaw(map[string]any{
		"id":          agent.ExternalID,
		"description": agent.Description,
		"mcp_servers": rawJSONValue(agent.MCPServers, []any{}),
		"metadata":    rawJSONValue(agent.Metadata, map[string]any{}),
		"model":       rawJSONValue(agent.Model, map[string]any{}),
		"multiagent":  rawJSONValue(agent.Multiagent, nil),
		"name":        agent.Name,
		"skills":      rawJSONValue(agent.Skills, []any{}),
		"system":      agent.System,
		"tools":       rawJSONValue(agent.Tools, []any{}),
		"type":        "agent",
		"version":     agent.CurrentVersion,
	})
}

func rawJSONValue(raw json.RawMessage, fallback any) any {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return fallback
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

func (h *Handler) normalizeVaultIDs(r *http.Request, principal auth.Principal, raw json.RawMessage) (json.RawMessage, error) {
	if httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, errors.New("vault_ids must be an array of strings")
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("vault_ids must contain non-empty strings")
		}
		vault, err := h.db.GetVault(r.Context(), principal.WorkspaceID, id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, fmt.Errorf("vault not found: %s", id)
			}
			return nil, err
		}
		if vault.ArchivedAt != nil {
			return nil, fmt.Errorf("vault is archived: %s", id)
		}
	}
	return httpapi.MarshalRaw(ids)
}

func (h *Handler) resourcesFromCreate(r *http.Request, principal auth.Principal, sessionID string, raw json.RawMessage, now time.Time) ([]db.SessionResource, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.New("resources must be an array")
	}
	resources := make([]db.SessionResource, 0, len(items))
	session := db.Session{
		ExternalID:     sessionID,
		OrganizationID: principal.OrganizationID,
		WorkspaceID:    principal.WorkspaceID,
	}
	for _, fields := range items {
		resource, err := h.resourceFromFields(r, session, fields, now)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, nil
}

func (h *Handler) resourceFromFields(r *http.Request, session db.Session, fields map[string]json.RawMessage, now time.Time) (db.SessionResource, error) {
	resourceType, err := parseRequiredStringField(fields, "type")
	if err != nil {
		return db.SessionResource{}, err
	}
	resourceID, err := ids.New("sesrsc_")
	if err != nil {
		return db.SessionResource{}, err
	}
	payload := map[string]any{"id": resourceID, "type": resourceType}
	var secret json.RawMessage
	switch resourceType {
	case "file":
		fileID, err := parseRequiredStringField(fields, "file_id")
		if err != nil {
			return db.SessionResource{}, err
		}
		if _, err := h.db.GetFile(r.Context(), session.WorkspaceID, fileID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return db.SessionResource{}, fmt.Errorf("file not found: %s", fileID)
			}
			return db.SessionResource{}, err
		}
		mountPath, err := optionalStringWithDefault(fields["mount_path"], "/mnt/data/"+fileID, "mount_path")
		if err != nil {
			return db.SessionResource{}, err
		}
		payload["file_id"] = fileID
		payload["mount_path"] = mountPath
	case "github_repository":
		url, err := parseRequiredStringField(fields, "url")
		if err != nil {
			return db.SessionResource{}, err
		}
		mountPath, err := optionalStringWithDefault(fields["mount_path"], "/workspace/repository", "mount_path")
		if err != nil {
			return db.SessionResource{}, err
		}
		payload["url"] = url
		payload["mount_path"] = mountPath
		if raw, ok := fields["checkout"]; ok && !httpapi.IsJSONNull(raw) {
			payload["checkout"] = rawJSONValue(raw, nil)
		}
	case "memory_store":
		memoryStoreID, err := parseRequiredStringField(fields, "memory_store_id")
		if err != nil {
			return db.SessionResource{}, err
		}
		store, err := h.db.GetMemoryStore(r.Context(), session.WorkspaceID, memoryStoreID)
		if err != nil {
			return db.SessionResource{}, resourceReferenceError{ResourceType: "memory_store", ResourceID: memoryStoreID, Err: err}
		}
		if store.ArchivedAt != nil {
			return db.SessionResource{}, resourceReferenceError{ResourceType: "memory_store", ResourceID: memoryStoreID, Err: db.ErrInvalidState}
		}
		payload["memory_store_id"] = memoryStoreID
		copyOptionalPayloadString(payload, fields, "access")
		copyOptionalPayloadString(payload, fields, "description")
		copyOptionalPayloadString(payload, fields, "instructions")
		copyOptionalPayloadString(payload, fields, "mount_path")
		copyOptionalPayloadString(payload, fields, "name")
	default:
		return db.SessionResource{}, errors.New("resource type must be file, github_repository, or memory_store")
	}
	payloadRaw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return db.SessionResource{}, err
	}
	return db.SessionResource{
		UUID:              uuid.NewString(),
		ExternalID:        resourceID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionExternalID: session.ExternalID,
		ResourceType:      resourceType,
		Payload:           payloadRaw,
		SecretPayload:     secret,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func (h *Handler) normalizeInputEvent(ctx context.Context, session db.Session, raw json.RawMessage, now time.Time) (db.SessionEvent, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return db.SessionEvent{}, false, errors.New("event must be an object")
	}
	eventType, _ := payload["type"].(string)
	if !allowedPublicEventType(eventType) {
		return db.SessionEvent{}, false, errors.New("event type is not accepted by this endpoint")
	}
	if err := validatePublicInputEvent(eventType, payload); err != nil {
		return db.SessionEvent{}, false, err
	}
	eventID, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, false, err
	}
	payload["id"] = eventID
	payload["processed_at"] = now.Format(time.RFC3339)
	payload["created_at"] = httpapi.FormatTime(now)
	var threadExternalID *string
	if value, ok := payload["session_thread_id"].(string); ok && strings.TrimSpace(value) != "" {
		value = strings.TrimSpace(value)
		threadExternalID = &value
	}
	outcomesChanged := false
	if eventType == "user.define_outcome" {
		outcomeID, _ := payload["outcome_id"].(string)
		if outcomeID == "" {
			outcomeID, err = ids.New("outc_")
			if err != nil {
				return db.SessionEvent{}, false, err
			}
			payload["outcome_id"] = outcomeID
		}
		maxIterations := 3
		if rawMax, ok := payload["max_iterations"].(float64); ok && rawMax > 0 {
			maxIterations = int(rawMax)
		}
		if maxIterations > 20 {
			return db.SessionEvent{}, false, errors.New("max_iterations must be at most 20")
		}
		payload["max_iterations"] = maxIterations
		outcomes, err := appendOutcomeEvaluation(session.OutcomeEvaluations, outcomeID, maxIterations, now)
		if err != nil {
			return db.SessionEvent{}, false, err
		}
		if _, err := h.db.SetSessionOutcomeEvaluations(ctx, session.WorkspaceID, session.ExternalID, outcomes); err != nil {
			return db.SessionEvent{}, false, err
		}
		outcomesChanged = true
	}
	payloadRaw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return db.SessionEvent{}, false, err
	}
	return db.SessionEvent{
		UUID:              uuid.NewString(),
		ExternalID:        eventID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionID:         session.ID,
		SessionExternalID: session.ExternalID,
		ThreadExternalID:  threadExternalID,
		EventType:         eventType,
		Payload:           payloadRaw,
		ProcessedAt:       now,
		CreatedAt:         now,
	}, outcomesChanged, nil
}

func allowedPublicEventType(eventType string) bool {
	return maevents.IsClientInput(eventType)
}

func validatePublicInputEvent(eventType string, payload map[string]any) error {
	switch eventType {
	case "user.message":
		if err := validateContentBlocks(payload, "content", true); err != nil {
			return err
		}
	case "system.message":
		if err := validateContentBlocks(payload, "content", true); err != nil {
			return err
		}
	case "user.interrupt":
		return nil
	case "user.tool_confirmation":
		if requiredStringValue(payload, "tool_use_id") == "" {
			return errors.New("tool_use_id is required")
		}
		result := requiredStringValue(payload, "result")
		if result != "allow" && result != "deny" {
			return errors.New("result must be allow or deny")
		}
		if _, ok := payload["deny_message"]; ok {
			if _, ok := payload["deny_message"].(string); !ok && payload["deny_message"] != nil {
				return errors.New("deny_message must be a string")
			}
		}
	case "user.tool_result":
		if requiredStringValue(payload, "tool_use_id") == "" {
			return errors.New("tool_use_id is required")
		}
		if err := validateContentBlocks(payload, "content", false); err != nil {
			return err
		}
	case "user.custom_tool_result":
		if requiredStringValue(payload, "custom_tool_use_id") == "" {
			return errors.New("custom_tool_use_id is required")
		}
		if err := validateContentBlocks(payload, "content", false); err != nil {
			return err
		}
	case "user.define_outcome":
		if requiredStringValue(payload, "description") == "" {
			return errors.New("description is required")
		}
		rubric, ok := payload["rubric"]
		if !ok || rubric == nil {
			return errors.New("rubric is required")
		}
		if rubricText, ok := rubric.(string); ok && strings.TrimSpace(rubricText) == "" {
			return errors.New("rubric is required")
		}
		if rawMax, ok := payload["max_iterations"]; ok && rawMax != nil {
			max, ok := rawMax.(float64)
			if !ok || max < 1 || max != float64(int(max)) {
				return errors.New("max_iterations must be a positive integer")
			}
		}
	}
	if value, ok := payload["session_thread_id"]; ok && value != nil {
		threadID, ok := value.(string)
		if !ok || strings.TrimSpace(threadID) == "" {
			return errors.New("session_thread_id must be a non-empty string")
		}
	}
	return nil
}

func validateContentBlocks(payload map[string]any, field string, required bool) error {
	value, ok := payload[field]
	if !ok || value == nil {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", field)
	}
	if required && len(items) == 0 {
		return fmt.Errorf("%s must contain at least one item", field)
	}
	if len(items) > 1000 {
		return fmt.Errorf("%s may contain at most 1000 items", field)
	}
	for _, item := range items {
		block, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s items must be objects", field)
		}
		if requiredStringValue(block, "type") == "" {
			return fmt.Errorf("%s item type is required", field)
		}
	}
	return nil
}

func requiredStringValue(payload map[string]any, field string) string {
	value, _ := payload[field].(string)
	return strings.TrimSpace(value)
}

func appendOutcomeEvaluation(raw json.RawMessage, outcomeID string, maxIterations int, now time.Time) (json.RawMessage, error) {
	var outcomes []map[string]any
	if len(raw) > 0 && !httpapi.IsJSONNull(raw) {
		if err := json.Unmarshal(raw, &outcomes); err != nil {
			return nil, errors.New("stored outcome evaluations are invalid")
		}
	}
	outcomes = append(outcomes, map[string]any{
		"id":             outcomeID,
		"outcome_id":     outcomeID,
		"max_iterations": maxIterations,
		"status":         "pending",
		"type":           "outcome_evaluation",
		"updated_at":     now.Format(time.RFC3339),
	})
	return httpapi.MarshalRaw(outcomes)
}

func (h *Handler) responseFromSession(r *http.Request, session db.Session) (sessionResponse, error) {
	resources, err := h.db.ListSessionResources(r.Context(), session.WorkspaceID, session.ExternalID)
	if err != nil {
		return sessionResponse{}, err
	}
	return sessionResponse{
		ID:                 session.ExternalID,
		Agent:              httpapi.RawOr(session.AgentSnapshot, `{}`),
		ArchivedAt:         httpapi.OptionalTime(session.ArchivedAt),
		CreatedAt:          httpapi.FormatTime(session.CreatedAt),
		DeploymentID:       session.DeploymentID,
		EnvironmentID:      session.EnvironmentExternalID,
		Metadata:           httpapi.RawOr(session.Metadata, `{}`),
		OutcomeEvaluations: httpapi.RawOr(session.OutcomeEvaluations, `[]`),
		Resources:          resourcesToResponses(resources),
		Stats:              httpapi.RawOr(session.Stats, `{}`),
		Status:             session.Status,
		Title:              session.Title,
		Type:               "session",
		UpdatedAt:          httpapi.FormatTime(session.UpdatedAt),
		Usage:              httpapi.RawOr(session.Usage, `{}`),
		VaultIDs:           httpapi.RawOr(session.VaultIDs, `[]`),
	}, nil
}

func responseFromThread(thread db.SessionThread) threadResponse {
	return threadResponse{
		ID:             thread.ExternalID,
		Agent:          httpapi.RawOr(thread.AgentSnapshot, `{}`),
		ArchivedAt:     httpapi.OptionalTime(thread.ArchivedAt),
		CreatedAt:      httpapi.FormatTime(thread.CreatedAt),
		ParentThreadID: thread.ParentThreadExternalID,
		SessionID:      thread.SessionExternalID,
		Stats:          httpapi.RawOr(thread.Stats, `{}`),
		Status:         thread.Status,
		Type:           "session_thread",
		UpdatedAt:      httpapi.FormatTime(thread.UpdatedAt),
		Usage:          httpapi.RawOr(thread.Usage, `{}`),
	}
}

func resourcesToResponses(resources []db.SessionResource) []json.RawMessage {
	data := make([]json.RawMessage, 0, len(resources))
	for _, resource := range resources {
		data = append(data, responseFromResource(resource))
	}
	return data
}

func responseFromResource(resource db.SessionResource) json.RawMessage {
	var payload map[string]any
	if err := json.Unmarshal(resource.Payload, &payload); err != nil || payload == nil {
		payload = map[string]any{"id": resource.ExternalID, "type": resource.ResourceType}
	}
	payload["id"] = resource.ExternalID
	payload["type"] = resource.ResourceType
	payload["created_at"] = httpapi.FormatTime(resource.CreatedAt)
	payload["updated_at"] = httpapi.FormatTime(resource.UpdatedAt)
	raw, _ := json.Marshal(payload)
	return raw
}

func (h *Handler) subscribe(sessionID, threadID string, includeStreamDeltas bool) (int64, <-chan db.SessionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSubID++
	id := h.nextSubID
	ch := make(chan db.SessionEvent, 32)
	h.subscribers[id] = subscriber{sessionID: sessionID, threadID: threadID, includeStreamDeltas: includeStreamDeltas, ch: ch}
	return id, ch
}

func (h *Handler) unsubscribe(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subscribers, id)
}

func (h *Handler) broadcast(event db.SessionEvent) {
	if !maevents.IsPublicSessionHistoryEvent(event.EventType) {
		return
	}
	h.broadcastToSubscribers(event, false)
}

func (h *Handler) broadcastStreamDelta(event db.SessionEvent) {
	if !maevents.IsStreamDelta(event.EventType) {
		return
	}
	h.broadcastToSubscribers(event, true)
}

func (h *Handler) broadcastToSubscribers(event db.SessionEvent, streamDelta bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		if streamDelta && !sub.includeStreamDeltas {
			continue
		}
		if sub.sessionID != event.SessionExternalID {
			continue
		}
		if sub.threadID == "" && event.ThreadExternalID != nil {
			continue
		}
		if sub.threadID != "" && (event.ThreadExternalID == nil || *event.ThreadExternalID != sub.threadID) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
		}
	}
}

func acceptsStreamDeltas(r *http.Request) bool {
	return len(parseRepeatedQuery(r, "event_deltas[]", "event_deltas")) > 0
}

func writeSSE(w http.ResponseWriter, event db.SessionEvent, threadID string) {
	fmt.Fprintf(w, "event: %s\n", event.EventType)
	fmt.Fprintf(w, "data: %s\n\n", sessionEventPayloadForResponse(event, threadID))
}

func (h *Handler) appendAndBroadcastInternal(r *http.Request, sessionID string, events []db.SessionEvent) {
	created, err := h.db.AppendSessionEvents(r.Context(), workspaceIDFromRequest(r), sessionID, events)
	if err != nil {
		log.Printf("append internal session events session_id=%s: %v", sessionID, err)
		return
	}
	for _, event := range created {
		h.broadcast(event)
	}
}

func (h *Handler) PublishCodeSessionEvents(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) error {
	if h == nil || len(payloads) == 0 {
		return nil
	}
	session, err := h.db.GetSession(ctx, codeSession.WorkspaceID, codeSession.SessionExternalID)
	if err != nil {
		return err
	}
	var events []db.SessionEvent
	now := time.Now().UTC()
	for _, raw := range payloads {
		if maevents.IsStreamDelta(rawSessionEventType(raw)) {
			event, err := h.streamDeltaEventFromCodeSessionPayload(ctx, session, codeSession.ExternalID, raw, now)
			if err != nil {
				log.Printf("skip code session stream delta session_id=%s code_session_id=%s: %v", session.ExternalID, codeSession.ExternalID, err)
				continue
			}
			h.broadcastStreamDelta(event)
			continue
		}
		batch, err := h.sessionEventsFromCodeSessionPayload(ctx, session, codeSession.ExternalID, raw, now)
		if err != nil {
			log.Printf("skip code session event session_id=%s code_session_id=%s: %v", session.ExternalID, codeSession.ExternalID, err)
			continue
		}
		events = append(events, batch...)
	}
	if len(events) == 0 {
		return nil
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].ProcessedAt.Equal(events[j].ProcessedAt) {
			return events[i].ProcessedAt.Before(events[j].ProcessedAt)
		}
		if !events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].CreatedAt.Before(events[j].CreatedAt)
		}
		return events[i].ExternalID < events[j].ExternalID
	})
	created, err := h.db.AppendSessionEventsIfAbsent(ctx, session.WorkspaceID, session.ExternalID, events)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return nil
		}
		return err
	}
	for _, event := range created {
		h.applySessionEventEffects(ctx, event)
		h.broadcast(event)
	}
	h.enqueueWebhooksForSessionEvents(ctx, session.WorkspaceID, session.ExternalID, created)
	return nil
}

func rawSessionEventType(raw json.RawMessage) string {
	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Type)
}

func (h *Handler) streamDeltaEventFromCodeSessionPayload(ctx context.Context, session db.Session, codeSessionID string, raw json.RawMessage, now time.Time) (db.SessionEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return db.SessionEvent{}, errors.New("code session stream delta must be an object")
	}
	eventType := strings.TrimSpace(sessionPayloadString(payload, "type"))
	if !maevents.IsStreamDelta(eventType) {
		return db.SessionEvent{}, errors.New("code session stream delta type is required")
	}
	eventID := firstSessionPayloadString(payload, "id", "uuid")
	if eventID == "" {
		eventID = stableCodeSessionEventID(codeSessionID, raw)
	}
	payload["id"] = eventID
	if sessionPayloadString(payload, "uuid") == "" {
		payload["uuid"] = eventID
	}
	createdAt := now
	if rawCreatedAt := firstSessionPayloadString(payload, "created_at", "timestamp"); rawCreatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, rawCreatedAt); err == nil {
			createdAt = parsed.UTC()
		}
	}
	if sessionPayloadString(payload, "created_at") == "" {
		payload["created_at"] = httpapi.FormatTime(createdAt)
	}
	threadID := streamDeltaOwnerThreadID(payload)
	delete(payload, "owner_session_thread_id")
	delete(payload, "_owner_session_thread_id")
	if threadID == "" {
		primary, err := h.db.GetPrimarySessionThread(ctx, session.WorkspaceID, session.ExternalID)
		if err != nil {
			return db.SessionEvent{}, err
		}
		threadID = primary.ExternalID
	}
	payloadRaw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{
		UUID:              uuid.NewString(),
		ExternalID:        eventID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionID:         session.ID,
		SessionExternalID: session.ExternalID,
		ThreadExternalID:  &threadID,
		EventType:         eventType,
		Payload:           payloadRaw,
		CreatedAt:         createdAt,
	}, nil
}

func streamDeltaOwnerThreadID(payload map[string]any) string {
	if threadID := firstSessionPayloadString(payload, "owner_session_thread_id", "_owner_session_thread_id", "session_thread_id", "thread_id"); threadID != "" {
		return threadID
	}
	if nested, ok := payload["event"].(map[string]any); ok {
		return firstSessionPayloadString(nested, "owner_session_thread_id", "_owner_session_thread_id", "session_thread_id", "thread_id")
	}
	return ""
}

type sessionEventCopySpec struct {
	OwnerThreadID *string
	Payload       map[string]any
	EventID       string
}

func (h *Handler) sessionEventsFromCodeSessionPayload(ctx context.Context, session db.Session, codeSessionID string, raw json.RawMessage, now time.Time) ([]db.SessionEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("code session event must be an object")
	}
	eventType, _ := payload["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return nil, errors.New("code session event type is required")
	}
	if maevents.IsStreamDelta(eventType) {
		return nil, errors.New("stream delta events are not persisted")
	}
	eventID, _ := payload["id"].(string)
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		eventID = stableCodeSessionEventID(codeSessionID, raw)
		payload["id"] = eventID
	}
	processedAt := now
	if rawProcessedAt, ok := payload["processed_at"].(string); ok && strings.TrimSpace(rawProcessedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(rawProcessedAt)); err == nil {
			processedAt = parsed.UTC()
		}
	}
	payload["processed_at"] = httpapi.FormatTime(processedAt)
	createdAt := processedAt
	if rawCreatedAt, ok := payload["created_at"].(string); ok && strings.TrimSpace(rawCreatedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(rawCreatedAt)); err == nil {
			createdAt = parsed.UTC()
		}
	}
	payload["created_at"] = httpapi.FormatTime(createdAt)
	if err := h.populateThreadCoordinationAgentNames(ctx, session, eventType, payload); err != nil {
		return nil, err
	}
	specs, err := h.sessionEventCopySpecs(ctx, session, codeSessionID, eventType, eventID, payload, createdAt)
	if err != nil {
		return nil, err
	}
	events := make([]db.SessionEvent, 0, len(specs))
	for _, spec := range specs {
		payloadRaw, err := httpapi.MarshalRaw(spec.Payload)
		if err != nil {
			return nil, err
		}
		events = append(events, db.SessionEvent{
			UUID:              uuid.NewString(),
			ExternalID:        spec.EventID,
			OrganizationID:    session.OrganizationID,
			WorkspaceID:       session.WorkspaceID,
			SessionID:         session.ID,
			SessionExternalID: session.ExternalID,
			ThreadExternalID:  spec.OwnerThreadID,
			EventType:         eventType,
			Payload:           payloadRaw,
			ProcessedAt:       processedAt,
			CreatedAt:         createdAt,
		})
	}
	return events, nil
}

func (h *Handler) populateThreadCoordinationAgentNames(ctx context.Context, session db.Session, eventType string, payload map[string]any) error {
	switch eventType {
	case "agent.thread_message_received":
		if sessionPayloadString(payload, "from_agent_name") != "" {
			return nil
		}
		name, err := h.agentNameForSessionThread(ctx, session, sessionPayloadString(payload, "from_session_thread_id"))
		if err != nil {
			return err
		}
		if name != "" {
			payload["from_agent_name"] = name
		}
	case "agent.thread_message_sent":
		if sessionPayloadString(payload, "to_agent_name") != "" {
			return nil
		}
		name, err := h.agentNameForSessionThread(ctx, session, sessionPayloadString(payload, "to_session_thread_id"))
		if err != nil {
			return err
		}
		if name != "" {
			payload["to_agent_name"] = name
		}
	}
	return nil
}

func (h *Handler) agentNameForSessionThread(ctx context.Context, session db.Session, threadID string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	thread, err := h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, threadID)
	if errors.Is(err, db.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	snapshot, ok := rawJSONValue(thread.AgentSnapshot, nil).(map[string]any)
	if !ok {
		return "", nil
	}
	return firstSessionPayloadString(snapshot, "name", "display_name"), nil
}

func shouldInferOwnerThreadFromPayload(eventType string) bool {
	switch maevents.CategoryFor(eventType) {
	case maevents.CategoryAgent, maevents.CategoryTool, maevents.CategorySpan:
		return true
	default:
		return false
	}
}

func (h *Handler) inferOwnerSessionThreadID(ctx context.Context, session db.Session, payload map[string]any) (string, error) {
	candidates := make(map[string]struct{})
	for _, field := range []string{"agent_id", "agentId", "task_id"} {
		if value := sessionPayloadString(payload, field); value != "" {
			candidates[value] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	events, _, err := h.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceID:       session.WorkspaceID,
		SessionExternalID: session.ExternalID,
		PrimaryOnly:       true,
		Limit:             500,
		Order:             "asc",
		Types:             []string{"session.thread_created"},
	})
	if err != nil {
		return "", err
	}
	for _, event := range events {
		var object map[string]any
		if err := json.Unmarshal(event.Payload, &object); err != nil {
			continue
		}
		threadID := sessionPayloadString(object, "session_thread_id")
		if threadID == "" {
			continue
		}
		for _, field := range []string{"agent_id", "agentId", "task_id"} {
			if value := sessionPayloadString(object, field); value != "" {
				if _, ok := candidates[value]; ok {
					// 子线程内部事件有时只带 agent/task 标识。这里把它归还给对应
					// session thread，避免普通工具调用被误写到 primary 线程。
					return threadID, nil
				}
			}
		}
	}
	return "", nil
}

func (h *Handler) sessionEventCopySpecs(ctx context.Context, session db.Session, codeSessionID, eventType, eventID string, payload map[string]any, now time.Time) ([]sessionEventCopySpec, error) {
	ownerThreadID := sessionPayloadString(payload, "owner_session_thread_id")
	if ownerThreadID == "" {
		ownerThreadID = sessionPayloadString(payload, "_owner_session_thread_id")
	}
	delete(payload, "owner_session_thread_id")
	delete(payload, "_owner_session_thread_id")
	if ownerThreadID == "" && shouldInferOwnerThreadFromPayload(eventType) {
		inferredThreadID, err := h.inferOwnerSessionThreadID(ctx, session, payload)
		if err != nil {
			return nil, err
		}
		ownerThreadID = inferredThreadID
	}
	if ownerThreadID != "" {
		if err := h.ensureSessionThread(ctx, session, ownerThreadID, payload, now); err != nil {
			return nil, err
		}
		if h.shouldCrossPostBlockingToolEvent(eventType, payload, ownerThreadID) {
			return h.dualSessionEventCopySpecs(codeSessionID, eventType, eventID, payload, ownerThreadID, true), nil
		}
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, &ownerThreadID, false)}, nil
	}
	threadID := sessionPayloadString(payload, "session_thread_id")
	if eventType == "session.thread_created" {
		if threadID != "" {
			if err := h.ensureSessionThread(ctx, session, threadID, payload, now); err != nil {
				return nil, err
			}
		}
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
	}
	if _, ok := maevents.ThreadStatus(eventType); ok && threadID != "" {
		if err := h.ensureSessionThread(ctx, session, threadID, payload, now); err != nil {
			return nil, err
		}
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
	}
	if h.shouldCrossPostBlockingToolEvent(eventType, payload, threadID) {
		if err := h.ensureSessionThread(ctx, session, threadID, payload, now); err != nil {
			return nil, err
		}
		return h.dualSessionEventCopySpecs(codeSessionID, eventType, eventID, payload, threadID, true), nil
	}
	if maevents.IsPrimaryCoordinationEvent(eventType) {
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
	}
	if threadID != "" {
		if _, err := h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, threadID); err == nil {
			ownerPayload := copySessionEventPayload(payload)
			if sessionToolUseEventHasThreadScopedSessionThreadID(eventType) {
				delete(ownerPayload, "session_thread_id")
			}
			return []sessionEventCopySpec{newSessionEventCopySpec(eventID, ownerPayload, &threadID, false)}, nil
		}
	}
	return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
}

func (h *Handler) dualSessionEventCopySpecs(codeSessionID, eventType, eventID string, payload map[string]any, threadID string, stripThreadIDFromThreadCopy bool) []sessionEventCopySpec {
	threadPayload := copySessionEventPayload(payload)
	if stripThreadIDFromThreadCopy {
		delete(threadPayload, "session_thread_id")
	}
	primaryPayload := copySessionEventPayload(payload)
	primaryPayload["session_thread_id"] = threadID
	primaryEventID := derivedSessionEventID(codeSessionID, eventID, eventType, "primary")
	return []sessionEventCopySpec{
		newSessionEventCopySpec(eventID, threadPayload, &threadID, false),
		newSessionEventCopySpec(primaryEventID, primaryPayload, nil, true),
	}
}

func newSessionEventCopySpec(eventID string, payload map[string]any, ownerThreadID *string, forceID bool) sessionEventCopySpec {
	if forceID {
		payload = copySessionEventPayload(payload)
		payload["id"] = eventID
	}
	return sessionEventCopySpec{OwnerThreadID: ownerThreadID, Payload: payload, EventID: eventID}
}

func derivedSessionEventID(codeSessionID, eventID, eventType, stream string) string {
	sum := sha256.Sum256([]byte(codeSessionID + "\x00" + eventID + "\x00" + eventType + "\x00" + stream))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func copySessionEventPayload(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (h *Handler) isChildSessionThread(ctx context.Context, session db.Session, threadID string) bool {
	primary, err := h.db.GetPrimarySessionThread(ctx, session.WorkspaceID, session.ExternalID)
	return err == nil && primary.ExternalID != threadID
}

func (h *Handler) shouldCrossPostBlockingToolEvent(eventType string, payload map[string]any, threadID string) bool {
	if strings.TrimSpace(threadID) == "" || !sessionToolUseEventHasThreadScopedSessionThreadID(eventType) {
		return false
	}
	if eventType == "agent.custom_tool_use" {
		return true
	}
	permission := strings.ToLower(firstSessionPayloadString(payload, "evaluated_permission", "permission", "permission_decision"))
	return permission == "ask" || permission == "always_ask" || permission == "requires_action"
}

func sessionToolUseEventHasThreadScopedSessionThreadID(eventType string) bool {
	switch eventType {
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		return true
	default:
		return false
	}
}

func firstSessionPayloadString(payload map[string]any, fields ...string) string {
	for _, field := range fields {
		if value := sessionPayloadString(payload, field); value != "" {
			return value
		}
	}
	return ""
}

func (h *Handler) ensurePrimarySessionThread(ctx context.Context, session db.Session) (db.SessionThread, error) {
	thread, err := h.db.GetPrimarySessionThread(ctx, session.WorkspaceID, session.ExternalID)
	if err == nil {
		return thread, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.SessionThread{}, err
	}
	threadID, err := ids.New("sthr_")
	if err != nil {
		return db.SessionThread{}, err
	}
	now := time.Now().UTC()
	return h.db.CreateSessionThreadIfAbsent(ctx, db.SessionThread{
		UUID:              uuid.NewString(),
		ExternalID:        threadID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionID:         session.ID,
		SessionExternalID: session.ExternalID,
		AgentSnapshot:     session.AgentSnapshot,
		Status:            threadStatusForSession(session.Status),
		Usage:             json.RawMessage(`{}`),
		Stats:             json.RawMessage(`{}`),
		CreatedAt:         now,
		UpdatedAt:         now,
	})
}

func threadStatusForSession(status string) string {
	switch strings.TrimSpace(status) {
	case "running", "rescheduling", "idle", "terminated":
		return strings.TrimSpace(status)
	default:
		return "idle"
	}
}

func (h *Handler) ensureSessionThread(ctx context.Context, session db.Session, threadID string, payload map[string]any, now time.Time) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	if _, err := h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, threadID); err == nil {
		return nil
	} else if !errors.Is(err, db.ErrNotFound) {
		return err
	}
	var parentThreadID *int64
	var parentThreadExternalID *string
	parentExternalID := sessionPayloadString(payload, "parent_session_thread_id")
	var parent db.SessionThread
	var err error
	if parentExternalID != "" {
		parent, err = h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, parentExternalID)
	} else {
		parent, err = h.db.GetPrimarySessionThread(ctx, session.WorkspaceID, session.ExternalID)
	}
	if err == nil && parent.ExternalID != threadID {
		parentThreadID = &parent.ID
		value := parent.ExternalID
		parentThreadExternalID = &value
	} else if err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	agentSnapshot, err := sessionThreadAgentSnapshot(payload)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err = h.db.CreateSessionThreadIfAbsent(ctx, db.SessionThread{
		UUID:                   uuid.NewString(),
		ExternalID:             threadID,
		OrganizationID:         session.OrganizationID,
		WorkspaceID:            session.WorkspaceID,
		SessionID:              session.ID,
		SessionExternalID:      session.ExternalID,
		ParentThreadID:         parentThreadID,
		ParentThreadExternalID: parentThreadExternalID,
		AgentSnapshot:          agentSnapshot,
		Status:                 "idle",
		Usage:                  json.RawMessage(`{}`),
		Stats:                  json.RawMessage(`{}`),
		CreatedAt:              now,
		UpdatedAt:              now,
	})
	return err
}

func sessionThreadAgentSnapshot(payload map[string]any) (json.RawMessage, error) {
	if agent, ok := payload["agent"].(map[string]any); ok && agent != nil {
		return httpapi.MarshalRaw(agent)
	}
	agentName := sessionPayloadString(payload, "agent_name")
	if agentName == "" {
		return json.RawMessage(`{}`), nil
	}
	return httpapi.MarshalRaw(map[string]any{"name": agentName})
}

func sessionPayloadString(payload map[string]any, name string) string {
	value, ok := payload[name].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func stableCodeSessionEventID(codeSessionID string, raw json.RawMessage) string {
	sum := sha256.Sum256([]byte(codeSessionID + "\x00" + strings.TrimSpace(string(raw))))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func (h *Handler) applySessionEventEffects(ctx context.Context, event db.SessionEvent) {
	if event.EventType == "session.thread_created" {
		session, err := h.db.GetSession(ctx, event.WorkspaceID, event.SessionExternalID)
		if err != nil {
			if !errors.Is(err, db.ErrNotFound) {
				log.Printf("load session for thread_created session_id=%s: %v", event.SessionExternalID, err)
			}
			return
		}
		if threadID := sessionThreadIDFromEvent(event); threadID != nil {
			var payload map[string]any
			_ = json.Unmarshal(event.Payload, &payload)
			if err := h.ensureSessionThread(ctx, session, *threadID, payload, event.CreatedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
				log.Printf("ensure session thread session_id=%s thread_id=%s: %v", event.SessionExternalID, *threadID, err)
			}
		}
		return
	}
	if status, ok := threadStatusFromEventType(event.EventType); ok {
		threadID := sessionThreadIDFromEvent(event)
		if threadID == nil {
			return
		}
		session, err := h.db.GetSession(ctx, event.WorkspaceID, event.SessionExternalID)
		if err == nil {
			var payload map[string]any
			_ = json.Unmarshal(event.Payload, &payload)
			if err := h.ensureSessionThread(ctx, session, *threadID, payload, event.CreatedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
				log.Printf("ensure session thread for status session_id=%s thread_id=%s: %v", event.SessionExternalID, *threadID, err)
			}
		} else if !errors.Is(err, db.ErrNotFound) {
			log.Printf("load session for thread status session_id=%s: %v", event.SessionExternalID, err)
		}
		if err := h.db.SetSessionThreadStatus(ctx, event.WorkspaceID, event.SessionExternalID, *threadID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
			log.Printf("update session thread status from event session_id=%s thread_id=%s event_type=%s: %v", event.SessionExternalID, *threadID, event.EventType, err)
			return
		}
		h.updateAggregatedSessionStatus(ctx, event.WorkspaceID, event.SessionExternalID)
		return
	}
	status, ok := sessionStatusFromEventType(event.EventType)
	if !ok {
		return
	}
	if err := h.db.SetSessionStatus(ctx, event.WorkspaceID, event.SessionExternalID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("update session status from event session_id=%s event_type=%s: %v", event.SessionExternalID, event.EventType, err)
	}
	thread, err := h.db.GetPrimarySessionThread(ctx, event.WorkspaceID, event.SessionExternalID)
	if err != nil {
		return
	}
	if err := h.db.SetSessionThreadStatus(ctx, event.WorkspaceID, event.SessionExternalID, thread.ExternalID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("update primary session thread status from event session_id=%s thread_id=%s event_type=%s: %v", event.SessionExternalID, thread.ExternalID, event.EventType, err)
	}
}

func sessionStatusFromEventType(eventType string) (string, bool) {
	return maevents.SessionStatus(eventType)
}

func threadStatusFromEventType(eventType string) (string, bool) {
	return maevents.ThreadStatus(eventType)
}

func (h *Handler) updateAggregatedSessionStatus(ctx context.Context, workspaceID int64, sessionID string) {
	threads, err := h.db.ListSessionThreads(ctx, workspaceID, sessionID)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			log.Printf("list session threads for aggregate session_id=%s: %v", sessionID, err)
		}
		return
	}
	if len(threads) == 0 {
		return
	}
	status := "terminated"
	for _, thread := range threads {
		switch thread.Status {
		case "running":
			status = "running"
			if err := h.db.SetSessionStatus(ctx, workspaceID, sessionID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
				log.Printf("aggregate session status session_id=%s: %v", sessionID, err)
			}
			return
		case "rescheduling":
			if status != "running" {
				status = "rescheduling"
			}
		case "idle":
			if status != "running" && status != "rescheduling" {
				status = "idle"
			}
		}
	}
	if err := h.db.SetSessionStatus(ctx, workspaceID, sessionID, status); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("aggregate session status session_id=%s: %v", sessionID, err)
	}
}

func (h *Handler) enqueueWebhooksForSessionEvents(ctx context.Context, workspaceID int64, sessionID string, events []db.SessionEvent) {
	if len(events) == 0 {
		return
	}
	workspaceIDs, err := h.db.GetWorkspaceIdentifiers(ctx, workspaceID)
	if err != nil {
		log.Printf("load workspace identifiers for session webhook session_id=%s: %v", sessionID, err)
		return
	}
	seen := map[string]struct{}{}
	for _, event := range events {
		for _, webhookEvent := range webhookEventsFromSessionEvent(event) {
			key := webhookEvent.EventType + "\x00" + event.CreatedAt.Format(time.RFC3339Nano)
			if webhookEvent.ThreadID != nil {
				key += "\x00" + *webhookEvent.ThreadID
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			webhooks.Enqueue(ctx, h.db, h.cfg, workspaceID, workspaceIDs.OrganizationExternalID, workspaceIDs.WorkspaceExternalID, webhookEvent.EventType, sessionID, webhookEvent.ThreadID)
		}
	}
}

type sessionWebhookEvent struct {
	EventType string
	ThreadID  *string
}

func webhookEventFromSessionEvent(event db.SessionEvent) (string, *string, bool) {
	webhookEvents := webhookEventsFromSessionEvent(event)
	if len(webhookEvents) == 0 {
		return "", nil, false
	}
	return webhookEvents[0].EventType, webhookEvents[0].ThreadID, true
}

func webhookEventsFromSessionEvent(event db.SessionEvent) []sessionWebhookEvent {
	switch event.EventType {
	case "session.status_run_started", "session.status_running", "session.running":
		return []sessionWebhookEvent{{EventType: "session.status_run_started"}}
	case "session.status_idle", "session.status_idled", "session.idled", "session.requires_action":
		return []sessionWebhookEvent{{EventType: "session.status_idled"}}
	case "session.status_rescheduled":
		return []sessionWebhookEvent{{EventType: "session.status_rescheduled"}}
	case "session.status_terminated":
		return []sessionWebhookEvent{{EventType: "session.status_terminated"}}
	case "session.deleted":
		return []sessionWebhookEvent{{EventType: "session.deleted"}}
	case "session.updated":
		return []sessionWebhookEvent{{EventType: "session.updated"}}
	case "session.error":
		return []sessionWebhookEvent{{EventType: "session.error"}}
	case "session.thread_created":
		return []sessionWebhookEvent{{EventType: "session.thread_created", ThreadID: sessionThreadIDFromEvent(event)}}
	case "session.thread_status_running":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{{EventType: "session.thread_status_running", ThreadID: threadID}}
	case "session.thread_status_idle", "session.thread_idled":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{
			{EventType: "session.thread_status_idle", ThreadID: threadID},
			{EventType: "session.thread_idled", ThreadID: threadID},
		}
	case "session.thread_status_rescheduled":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{{EventType: "session.thread_status_rescheduled", ThreadID: threadID}}
	case "session.thread_status_terminated", "session.thread_terminated":
		threadID := sessionThreadIDFromEvent(event)
		return []sessionWebhookEvent{
			{EventType: "session.thread_status_terminated", ThreadID: threadID},
			{EventType: "session.thread_terminated", ThreadID: threadID},
		}
	case "session.outcome_evaluation_ended":
		return []sessionWebhookEvent{{EventType: "session.outcome_evaluation_ended"}}
	default:
		return nil
	}
}

func sessionThreadIDFromEvent(event db.SessionEvent) *string {
	var payload struct {
		SessionThreadID string `json:"session_thread_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err == nil {
		if value := strings.TrimSpace(payload.SessionThreadID); value != "" {
			return &value
		}
	}
	if event.ThreadExternalID != nil && strings.TrimSpace(*event.ThreadExternalID) != "" {
		value := strings.TrimSpace(*event.ThreadExternalID)
		return &value
	}
	return nil
}

func (h *Handler) sessionUpdatedEvent(session db.Session) (db.SessionEvent, error) {
	eventID, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, err
	}
	now := time.Now().UTC()
	payload, err := httpapi.MarshalRaw(map[string]any{
		"id":           eventID,
		"agent":        rawJSONValue(session.AgentSnapshot, nil),
		"created_at":   httpapi.FormatTime(now),
		"metadata":     rawJSONValue(session.Metadata, map[string]any{}),
		"processed_at": now.Format(time.RFC3339),
		"title":        session.Title,
		"type":         "session.updated",
	})
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{
		UUID:              uuid.NewString(),
		ExternalID:        eventID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionID:         session.ID,
		SessionExternalID: session.ExternalID,
		EventType:         "session.updated",
		Payload:           payload,
		ProcessedAt:       now,
		CreatedAt:         now,
	}, nil
}

func (h *Handler) simpleSessionEvent(eventType, sessionID string, threadID *string) (db.SessionEvent, error) {
	eventID, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, err
	}
	now := time.Now().UTC()
	payload := map[string]any{
		"id":           eventID,
		"created_at":   httpapi.FormatTime(now),
		"processed_at": now.Format(time.RFC3339),
		"type":         eventType,
	}
	if threadID != nil {
		payload["session_thread_id"] = *threadID
	}
	raw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{
		UUID:             uuid.NewString(),
		ExternalID:       eventID,
		ThreadExternalID: threadID,
		EventType:        eventType,
		Payload:          raw,
		ProcessedAt:      now,
		CreatedAt:        now,
	}, nil
}

func patchSessionAgent(current json.RawMessage, raw json.RawMessage) (json.RawMessage, error) {
	var snapshot map[string]any
	if err := json.Unmarshal(current, &snapshot); err != nil || snapshot == nil {
		return nil, errors.New("stored session agent is invalid")
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, errors.New("agent must be an object")
	}
	if rawServers, ok := patch["mcp_servers"]; ok {
		if httpapi.IsJSONNull(rawServers) {
			snapshot["mcp_servers"] = []any{}
		} else {
			var servers any
			if err := json.Unmarshal(rawServers, &servers); err != nil {
				return nil, errors.New("agent.mcp_servers must be an array")
			}
			snapshot["mcp_servers"] = servers
		}
	}
	if rawTools, ok := patch["tools"]; ok {
		if httpapi.IsJSONNull(rawTools) {
			snapshot["tools"] = []any{}
		} else {
			var tools any
			if err := json.Unmarshal(rawTools, &tools); err != nil {
				return nil, errors.New("agent.tools must be an array")
			}
			snapshot["tools"] = tools
		}
	}
	return httpapi.MarshalRaw(snapshot)
}

func parseRequiredStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	return parseRequiredRawString(raw, name)
}

func parseRequiredRawString(raw json.RawMessage, name string) (string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return "", fmt.Errorf("%s is required", name)
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

func optionalStringWithDefault(raw json.RawMessage, fallback, name string) (string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return fallback, nil
	}
	value, err := parseRequiredRawString(raw, name)
	if err != nil {
		return "", err
	}
	return value, nil
}

func copyOptionalPayloadString(payload map[string]any, fields map[string]json.RawMessage, name string) {
	raw, ok := fields[name]
	if !ok || httpapi.IsJSONNull(raw) {
		return
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		payload[name] = value
	}
}

func validateMetadataEntries(metadata map[string]string) error {
	return httpapi.ValidateMetadataEntryLimit(metadata, 16, "metadata may contain at most 16 entries")
}

func fieldOrDefault(fields map[string]json.RawMessage, name, fallback string) json.RawMessage {
	if raw, ok := fields[name]; ok {
		return raw
	}
	return json.RawMessage(fallback)
}

func sessionEventPayload(event db.SessionEvent) json.RawMessage {
	return sessionEventPayloadForResponse(event, "")
}

func (h *Handler) primaryOrphanToolUseIDsWithChildCopies(ctx context.Context, sessionID, threadID string, records []db.SessionEvent) (map[string]struct{}, error) {
	if strings.TrimSpace(threadID) != "" || len(records) == 0 {
		return nil, nil
	}
	workspaceID := records[0].WorkspaceID
	seen := make(map[string]struct{})
	toolUseIDs := make([]string, 0)
	for _, record := range records {
		if id := primaryOrphanToolProjectionUseID(record); id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			toolUseIDs = append(toolUseIDs, id)
		}
	}
	if len(toolUseIDs) == 0 {
		return nil, nil
	}
	return h.db.ChildSessionToolUseIDs(ctx, workspaceID, sessionID, toolUseIDs)
}

func primaryToolProjectionHidden(event db.SessionEvent, hiddenToolUseIDs map[string]struct{}) bool {
	if len(hiddenToolUseIDs) == 0 {
		return false
	}
	toolUseID := primaryOrphanToolProjectionUseID(event)
	if toolUseID == "" {
		return false
	}
	_, ok := hiddenToolUseIDs[toolUseID]
	return ok
}

func primaryOrphanToolProjectionUseID(event db.SessionEvent) string {
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ""
	}
	if hasSessionThreadOwnerField(payload) {
		return ""
	}
	eventType := strings.TrimSpace(event.EventType)
	if maevents.IsCrossPostedBlockingEvent(eventType) {
		// 历史数据里曾出现子线程工具事件先被无归属地写入 primary，随后又
		// 以 owner copy 写入子线程。primary 响应隐藏这种 orphan projection。
		return sessionToolUseID(payload)
	}
	if !isToolResultOrConfirmationEvent(eventType) {
		return ""
	}
	// Claude Code 的 tool_result 可能先作为无归属 primary projection 写入，
	// 再由对应的子线程 tool_use 提供真正 owner；响应层按 tool_use_id 过滤。
	return sessionToolReferenceID(payload)
}

func sessionToolUseID(payload map[string]any) string {
	return firstSessionPayloadString(payload, "tool_use_id", "mcp_tool_use_id", "custom_tool_use_id", "id")
}

func sessionToolReferenceID(payload map[string]any) string {
	if id := firstSessionPayloadString(payload, "tool_use_id", "mcp_tool_use_id", "custom_tool_use_id"); id != "" {
		return id
	}
	for _, field := range []string{"raw_tool_result", "message"} {
		if nested, ok := payload[field].(map[string]any); ok {
			if id := firstSessionPayloadString(nested, "tool_use_id", "mcp_tool_use_id", "custom_tool_use_id"); id != "" {
				return id
			}
		}
	}
	if content, ok := payload["content"].([]any); ok {
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if id := firstSessionPayloadString(block, "tool_use_id", "mcp_tool_use_id", "custom_tool_use_id"); id != "" {
				return id
			}
		}
	}
	return ""
}

func isToolResultOrConfirmationEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "agent.tool_result", "agent.mcp_tool_result", "agent.custom_tool_result",
		"user.tool_result", "user.custom_tool_result", "user.tool_confirmation":
		return true
	default:
		return false
	}
}

func sessionEventPayloadForResponse(event db.SessionEvent, threadID string) json.RawMessage {
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return event.Payload
	}
	changed := ensureSessionEventTimeField(payload, "created_at", event.CreatedAt)
	changed = ensureSessionEventTimeField(payload, "processed_at", event.ProcessedAt) || changed
	if strings.TrimSpace(threadID) != "" && !hasSessionThreadOwnerField(payload) {
		payload["session_thread_id"] = strings.TrimSpace(threadID)
		changed = true
	}
	if !changed {
		return event.Payload
	}
	raw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return event.Payload
	}
	return raw
}

func hasSessionThreadOwnerField(payload map[string]any) bool {
	for _, field := range []string{"session_thread_id", "thread_id"} {
		if value, ok := payload[field].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func ensureSessionEventTimeField(payload map[string]any, field string, value time.Time) bool {
	if value.IsZero() {
		return false
	}
	if raw, ok := payload[field].(string); ok && strings.TrimSpace(raw) != "" {
		if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw)); err == nil {
			return false
		}
	}
	payload[field] = httpapi.FormatTime(value)
	return true
}

func parseOrder(r *http.Request) (string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("order"))
	if raw == "" {
		return "desc", nil
	}
	if raw != "asc" && raw != "desc" {
		return "", errors.New("order must be asc or desc")
	}
	return raw, nil
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

func parseOptionalPositiveInt(r *http.Request, name string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return nil, fmt.Errorf("%s must be at least 1", name)
	}
	return &value, nil
}

func parseRepeatedStatuses(r *http.Request) ([]string, error) {
	statuses := parseRepeatedQuery(r, "statuses[]", "statuses")
	for _, status := range statuses {
		switch status {
		case "rescheduling", "running", "idle", "terminated":
		default:
			return nil, errors.New("statuses must contain valid session statuses")
		}
	}
	return statuses, nil
}

func parseRepeatedQuery(r *http.Request, names ...string) []string {
	var values []string
	query := r.URL.Query()
	for _, name := range names {
		for _, value := range query[name] {
			for _, part := range strings.Split(value, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					values = append(values, part)
				}
			}
		}
	}
	return values
}

func encodeSessionCursor(session db.Session) string {
	return encodeCursor(session.CreatedAt, session.ID)
}

func encodeEventCursor(event db.SessionEvent) string {
	return encodeCursor(event.CreatedAt, event.ID)
}

func encodeThreadCursor(thread db.SessionThread) string {
	return encodeCursor(thread.CreatedAt, thread.ID)
}

func encodeCursor(createdAt time.Time, id int64) string {
	data, _ := json.Marshal(map[string]any{"created_at": createdAt.UTC().Format(time.RFC3339Nano), "id": id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeSessionCursor(raw string) (*db.SessionPageCursor, error) {
	createdAt, id, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.SessionPageCursor{CreatedAt: *createdAt, ID: id}, nil
}

func decodeEventCursor(raw string) (*db.SessionEventPageCursor, error) {
	createdAt, id, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.SessionEventPageCursor{CreatedAt: *createdAt, ID: id}, nil
}

func decodeThreadCursor(raw string) (*db.SessionThreadPageCursor, error) {
	createdAt, id, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.SessionThreadPageCursor{CreatedAt: *createdAt, ID: id}, nil
}

func decodeCursor(raw string) (*time.Time, int64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, 0, errors.New("page cursor is invalid")
	}
	var payload struct {
		CreatedAt string `json:"created_at"`
		ID        int64  `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.ID <= 0 || payload.CreatedAt == "" {
		return nil, 0, errors.New("page cursor is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, 0, errors.New("page cursor is invalid")
	}
	createdAt = createdAt.UTC()
	return &createdAt, payload.ID, nil
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
}

func writeResourceBuildError(w http.ResponseWriter, r *http.Request, err error) {
	var refErr resourceReferenceError
	if errors.As(err, &refErr) {
		if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Memory store not found: "+refErr.ResourceID))
			return
		}
		if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrInvalidState) {
			writeBadRequest(w, r, errors.New("memory store must not be archived"))
			return
		}
		log.Printf("session resource reference %s %s: %v", refErr.ResourceType, refErr.ResourceID, refErr.Err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not validate session resource"))
		return
	}
	writeBadRequest(w, r, err)
}

func writeSessionLoadError(w http.ResponseWriter, r *http.Request, err error, sessionID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Session not found: "+sessionID))
		return
	}
	if errors.Is(err, db.ErrInvalidState) {
		writeBadRequest(w, r, errors.New("session state does not allow this operation"))
		return
	}
	log.Printf("session operation: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Session operation failed"))
}

func writeThreadLoadError(w http.ResponseWriter, r *http.Request, err error, threadID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Thread not found: "+threadID))
		return
	}
	log.Printf("thread operation: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Thread operation failed"))
}

func writeResourceLoadError(w http.ResponseWriter, r *http.Request, err error, resourceID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Resource not found: "+resourceID))
		return
	}
	log.Printf("resource operation: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Resource operation failed"))
}

func (h *Handler) isOfficialSDKFixturePrincipal(principal auth.Principal) bool {
	return principal.CredentialType == "api_key" && principal.APIKeyExternalID == h.cfg.OfficialSDKResourceAPIKeyExternalID
}

func (h *Handler) createUsesOfficialFixtures(fields map[string]json.RawMessage) bool {
	agentRaw := fields["agent"]
	env, _ := parseRequiredStringField(fields, "environment_id")
	if env != h.cfg.OfficialSDKFixtureEnvironmentID {
		return false
	}
	var agentID string
	if json.Unmarshal(agentRaw, &agentID) == nil {
		return agentID == h.cfg.OfficialSDKFixtureAgentID
	}
	var object struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(agentRaw, &object)
	return object.ID == h.cfg.OfficialSDKFixtureAgentID
}

func (h *Handler) isFixtureResource(r *http.Request, sessionID, resourceID string) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.OfficialSDKFixtureSessionID && resourceID == h.cfg.OfficialSDKFixtureSessionResourceID
}

func (h *Handler) isFixtureThread(r *http.Request, sessionID, threadID string) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.OfficialSDKFixtureSessionID && threadID == h.cfg.OfficialSDKFixtureSessionThreadID
}

func (h *Handler) isOfficialSDKFixtureSession(r *http.Request, sessionID string) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.OfficialSDKFixtureSessionID
}

func normalizeFixtureEvent(raw json.RawMessage, now time.Time) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("event must be an object")
	}
	eventType, _ := payload["type"].(string)
	if !allowedPublicEventType(eventType) {
		return nil, errors.New("event type is not accepted by this endpoint")
	}
	if err := validatePublicInputEvent(eventType, payload); err != nil {
		return nil, err
	}
	eventID, err := ids.New("sevt_")
	if err != nil {
		return nil, err
	}
	payload["id"] = eventID
	payload["processed_at"] = now.Format(time.RFC3339)
	return httpapi.MarshalRaw(payload)
}

func (h *Handler) fixtureDBSession(principal auth.Principal) db.Session {
	now := time.Now().UTC()
	return db.Session{
		ID:                    1,
		UUID:                  uuid.NewString(),
		ExternalID:            h.cfg.OfficialSDKFixtureSessionID,
		OrganizationID:        principal.OrganizationID,
		WorkspaceID:           principal.WorkspaceID,
		CreatedByAPIKeyID:     principal.APIKeyID,
		EnvironmentExternalID: h.cfg.OfficialSDKFixtureEnvironmentID,
		AgentExternalID:       h.cfg.OfficialSDKFixtureAgentID,
		AgentVersion:          1,
		AgentSnapshot:         h.fixtureAgentSnapshot(),
		Metadata:              json.RawMessage(`{"foo":"string"}`),
		VaultIDs:              json.RawMessage(`["string"]`),
		Status:                "idle",
		Usage:                 json.RawMessage(`{}`),
		Stats:                 json.RawMessage(`{}`),
		OutcomeEvaluations:    json.RawMessage(`[]`),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func (h *Handler) fixtureSession(now time.Time, archived bool) sessionResponse {
	var archivedAt *string
	if archived {
		value := httpapi.FormatTime(now)
		archivedAt = &value
	}
	title := "Order #1234 inquiry"
	return sessionResponse{
		ID:                 h.cfg.OfficialSDKFixtureSessionID,
		Agent:              h.fixtureAgentSnapshot(),
		ArchivedAt:         archivedAt,
		CreatedAt:          httpapi.FormatTime(now),
		EnvironmentID:      h.cfg.OfficialSDKFixtureEnvironmentID,
		Metadata:           json.RawMessage(`{"foo":"string"}`),
		OutcomeEvaluations: json.RawMessage(`[]`),
		Resources:          []json.RawMessage{h.fixtureResource(now)},
		Stats:              json.RawMessage(`{}`),
		Status:             "idle",
		Title:              &title,
		Type:               "session",
		UpdatedAt:          httpapi.FormatTime(now),
		Usage:              json.RawMessage(`{}`),
		VaultIDs:           json.RawMessage(`["string"]`),
	}
}

func (h *Handler) fixtureAgentSnapshot() json.RawMessage {
	raw, _ := httpapi.MarshalRaw(map[string]any{
		"id":          h.cfg.OfficialSDKFixtureAgentID,
		"description": nil,
		"mcp_servers": []any{},
		"model":       map[string]any{"id": "claude-opus-4-6", "speed": "standard"},
		"multiagent":  nil,
		"name":        "fixture agent",
		"skills":      []any{},
		"system":      nil,
		"tools":       []any{},
		"type":        "agent",
		"version":     1,
	})
	return raw
}

func (h *Handler) fixtureResource(now time.Time) json.RawMessage {
	raw, _ := httpapi.MarshalRaw(map[string]any{
		"id":         h.cfg.OfficialSDKFixtureSessionResourceID,
		"created_at": httpapi.FormatTime(now),
		"file_id":    "file_011CNha8iCJcU1wXNR6q4V8w",
		"mount_path": "/uploads/receipt.pdf",
		"type":       "file",
		"updated_at": httpapi.FormatTime(now),
	})
	return raw
}

func (h *Handler) fixtureThread(now time.Time, archived bool) threadResponse {
	var archivedAt *string
	if archived {
		value := httpapi.FormatTime(now)
		archivedAt = &value
	}
	return threadResponse{
		ID:             h.cfg.OfficialSDKFixtureSessionThreadID,
		Agent:          h.fixtureAgentSnapshot(),
		ArchivedAt:     archivedAt,
		CreatedAt:      httpapi.FormatTime(now),
		ParentThreadID: nil,
		SessionID:      h.cfg.OfficialSDKFixtureSessionID,
		Stats:          json.RawMessage(`{}`),
		Status:         "idle",
		Type:           "session_thread",
		UpdatedAt:      httpapi.FormatTime(now),
		Usage:          json.RawMessage(`{}`),
	}
}
