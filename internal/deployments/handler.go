package deployments

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
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxDeploymentBodySize      = 4 << 20
	managedAgentsBeta          = "managed-agents-2026-04-01"
	defaultAnthropicAPIVersion = "2023-06-01"
)

type Handler struct {
	db       *db.DB
	webhooks webhookEnqueuer
	logger   *slog.Logger
	router   chi.Router
}

type webhookEnqueuer interface {
	Enqueue(context.Context, webhooks.EnqueueInput)
}

type RunsHandler struct {
	db     *db.DB
	logger *slog.Logger
	router chi.Router
}

type deploymentResponse struct {
	ID            string          `json:"id"`
	Agent         json.RawMessage `json:"agent"`
	ArchivedAt    *string         `json:"archived_at"`
	CreatedAt     string          `json:"created_at"`
	Description   string          `json:"description"`
	EnvironmentID string          `json:"environment_id"`
	InitialEvents json.RawMessage `json:"initial_events"`
	Metadata      json.RawMessage `json:"metadata"`
	Name          string          `json:"name"`
	PausedReason  json.RawMessage `json:"paused_reason"`
	Resources     json.RawMessage `json:"resources"`
	Schedule      json.RawMessage `json:"schedule"`
	Status        string          `json:"status"`
	Type          string          `json:"type"`
	UpdatedAt     string          `json:"updated_at"`
	VaultIDs      json.RawMessage `json:"vault_ids"`
}

type deploymentMutationRequest struct {
	Agent         json.RawMessage `json:"agent"`
	Description   json.RawMessage `json:"description"`
	EnvironmentID json.RawMessage `json:"environment_id"`
	InitialEvents json.RawMessage `json:"initial_events"`
	Metadata      json.RawMessage `json:"metadata"`
	Name          json.RawMessage `json:"name"`
	Resources     json.RawMessage `json:"resources"`
	Schedule      json.RawMessage `json:"schedule"`
	VaultIDs      json.RawMessage `json:"vault_ids"`
}

type deploymentScheduleRequest struct {
	Expression json.RawMessage `json:"expression"`
	Timezone   json.RawMessage `json:"timezone"`
	Type       json.RawMessage `json:"type"`
}

type deploymentCheckoutRequest struct {
	Name json.RawMessage `json:"name"`
	SHA  json.RawMessage `json:"sha"`
	Type json.RawMessage `json:"type"`
}

type deploymentRunResponse struct {
	ID             string          `json:"id"`
	Agent          json.RawMessage `json:"agent"`
	CreatedAt      string          `json:"created_at"`
	DeploymentID   string          `json:"deployment_id"`
	Error          json.RawMessage `json:"error"`
	SessionID      *string         `json:"session_id"`
	TriggerContext json.RawMessage `json:"trigger_context"`
	Type           string          `json:"type"`
}

type pageResponse[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

type resolvedAgent struct {
	record   db.Agent
	snapshot json.RawMessage
	ref      json.RawMessage
}

type deploymentContentBlockRequest struct {
	Type    json.RawMessage `json:"type"`
	Text    json.RawMessage `json:"text"`
	Source  json.RawMessage `json:"source"`
	Context json.RawMessage `json:"context"`
	Title   json.RawMessage `json:"title"`
}

type deploymentContentSourceRequest struct {
	Type      json.RawMessage `json:"type"`
	Data      json.RawMessage `json:"data"`
	MediaType json.RawMessage `json:"media_type"`
	URL       json.RawMessage `json:"url"`
	FileID    json.RawMessage `json:"file_id"`
}

type deploymentOutcomeRubricRequest struct {
	Type    json.RawMessage `json:"type"`
	FileID  json.RawMessage `json:"file_id"`
	Content json.RawMessage `json:"content"`
}

type deploymentInitialEvent struct {
	Type          string          `json:"type"`
	Content       json.RawMessage `json:"content,omitempty"`
	Description   string          `json:"description,omitempty"`
	Rubric        json.RawMessage `json:"rubric,omitempty"`
	MaxIterations *int            `json:"max_iterations,omitempty"`
}

type deploymentSessionEventPayload struct {
	ID            string          `json:"id"`
	ProcessedAt   string          `json:"processed_at"`
	Type          string          `json:"type"`
	Content       json.RawMessage `json:"content,omitempty"`
	Description   string          `json:"description,omitempty"`
	Rubric        json.RawMessage `json:"rubric,omitempty"`
	MaxIterations int             `json:"max_iterations,omitempty"`
	OutcomeID     string          `json:"outcome_id,omitempty"`
}

type deploymentOutcomeEvaluation struct {
	ID            string `json:"id"`
	OutcomeID     string `json:"outcome_id"`
	MaxIterations int    `json:"max_iterations"`
	Status        string `json:"status"`
	Type          string `json:"type"`
	UpdatedAt     string `json:"updated_at"`
}

func NewHandler(database *db.DB, webhookEvents webhookEnqueuer, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{db: database, webhooks: webhookEvents, logger: logger}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.create)
	router.Get("/", h.list)
	router.Route("/{deployment_id}", func(r chi.Router) {
		r.Get("/", h.retrieveRoute)
		r.Post("/", h.updateRoute)
		r.Post("/archive", h.archiveRoute)
		r.Post("/pause", h.pauseRoute)
		r.Post("/run", h.runRoute)
		r.Post("/unpause", h.unpauseRoute)
	})
	h.router = router
	return h
}

func NewRunsHandler(database *db.DB, logger *slog.Logger) *RunsHandler {
	logger = logging.LoggerOrDefault(logger)
	h := &RunsHandler{db: database, logger: logger}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Get("/", h.list)
	router.Get("/{deployment_run_id}", h.retrieveRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !deploymentAPIContractEnabled(r) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Deployments API requires anthropic-version and anthropic-beta: "+managedAgentsBeta))
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *RunsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !deploymentAPIContractEnabled(r) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Deployment Runs API requires anthropic-version and anthropic-beta: "+managedAgentsBeta))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	body, err := httpapi.DecodeObjectBodyAs[deploymentMutationRequest](w, r, maxDeploymentBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "metadata", raw: body.Metadata},
		{name: "resources", raw: body.Resources},
		{name: "vault_ids", raw: body.VaultIDs},
	} {
		if err := rejectNullField(field.raw, field.name); err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	environmentID, err := parseRequiredRawString(body.EnvironmentID, "environment_id")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	name, err := parseRequiredRawString(body.Name, "name")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	description, err := parseNullableString(body.Description, "description")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	metadata, err := httpapi.NormalizeMetadata(rawOrDefault(body.Metadata, `{}`), validateMetadataEntries)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	initialEvents, err := normalizeInitialEvents(body.InitialEvents)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	schedule, err := normalizeOptionalSchedule(body.Schedule)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	agent, err := h.resolveAgent(r, principal, body.Agent)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceUUID, environmentID)
	if err != nil {
		h.writeEnvironmentLoadError(w, r, err, environmentID)
		return
	}
	if env.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("environment must not be archived"))
		return
	}
	resources, resourceSecrets, err := h.normalizeResources(r, principal, rawOrDefault(body.Resources, `[]`))
	if err != nil {
		h.writeResourceBuildError(w, r, err)
		return
	}
	vaultIDs, err := h.normalizeVaultIDs(r, principal, rawOrDefault(body.VaultIDs, `[]`))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	deploymentID, err := ids.New("depl_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate deployment ID"))
		return
	}
	now := time.Now().UTC()
	created, err := h.db.CreateDeployment(r.Context(), db.Deployment{
		UUID:                  uuid.NewString(),
		ExternalID:            deploymentID,
		OrganizationUUID:      principal.OrganizationUUID,
		WorkspaceUUID:         principal.WorkspaceUUID,
		CreatedByAPIKeyUUID:   principal.APIKeyUUID,
		EnvironmentUUID:       env.UUID,
		EnvironmentExternalID: env.ExternalID,
		AgentUUID:             agent.record.UUID,
		AgentExternalID:       agent.record.ExternalID,
		AgentVersion:          agent.record.CurrentVersion,
		AgentSnapshot:         agent.snapshot,
		Name:                  name,
		Description:           description,
		Metadata:              metadata,
		InitialEvents:         initialEvents,
		Resources:             resources,
		ResourceSecrets:       resourceSecrets,
		VaultIDs:              vaultIDs,
		Schedule:              schedule,
		Status:                "active",
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "create deployment", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create deployment"))
		return
	}
	h.writeDeploymentResponse(w, r, created)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeDeploymentCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "active" && status != "paused" {
		writeBadRequest(w, r, errors.New("status must be active or paused"))
		return
	}
	if status != "" && r.URL.Query().Has("include_archived") {
		writeBadRequest(w, r, errors.New("status cannot be combined with include_archived"))
		return
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	records, hasMore, err := h.db.ListDeploymentsPage(r.Context(), db.ListDeploymentsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
		AgentExternalID: strings.TrimSpace(r.URL.Query().Get("agent_id")),
		Status:          status,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLTE:    createdAtLTE,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list deployments", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list deployments"))
		return
	}
	now := time.Now().UTC()
	data := make([]deploymentResponse, 0, len(records))
	for _, record := range records {
		response, err := responseFromDeployment(record, now)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "map deployment response", "error", err, "deployment_id", record.ExternalID)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list deployments"))
			return
		}
		data = append(data, response)
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeDeploymentCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[deploymentResponse]{Data: data, NextPage: nextPage})
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	record, err := h.db.GetDeployment(r.Context(), principal.WorkspaceUUID, deploymentID)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	h.writeDeploymentResponse(w, r, record)
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	body, err := httpapi.DecodeObjectBodyAs[deploymentMutationRequest](w, r, maxDeploymentBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	current, err := h.db.GetDeployment(r.Context(), principal.WorkspaceUUID, deploymentID)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	if current.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("archived deployments cannot be updated"))
		return
	}
	next := current
	if len(body.Agent) > 0 {
		agent, err := h.resolveAgent(r, principal, body.Agent)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		next.AgentUUID = agent.record.UUID
		next.AgentExternalID = agent.record.ExternalID
		next.AgentVersion = agent.record.CurrentVersion
		next.AgentSnapshot = agent.snapshot
	}
	if len(body.EnvironmentID) > 0 {
		environmentID, err := parseRequiredRawString(body.EnvironmentID, "environment_id")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceUUID, environmentID)
		if err != nil {
			h.writeEnvironmentLoadError(w, r, err, environmentID)
			return
		}
		if env.ArchivedAt != nil {
			writeBadRequest(w, r, errors.New("environment must not be archived"))
			return
		}
		next.EnvironmentUUID = env.UUID
		next.EnvironmentExternalID = env.ExternalID
	}
	if len(body.Name) > 0 {
		next.Name, err = parseRequiredRawString(body.Name, "name")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if len(body.Description) > 0 {
		next.Description, err = nullableStringFromRaw(body.Description, "description")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		if next.Description != nil && *next.Description == "" {
			next.Description = nil
		}
	}
	if len(body.Metadata) > 0 {
		next.Metadata, err = patchDeploymentMetadata(next.Metadata, body.Metadata)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if len(body.InitialEvents) > 0 {
		next.InitialEvents, err = normalizeInitialEvents(body.InitialEvents)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if len(body.Resources) > 0 {
		next.Resources, next.ResourceSecrets, err = h.normalizeResources(r, principal, body.Resources)
		if err != nil {
			h.writeResourceBuildError(w, r, err)
			return
		}
	}
	if len(body.VaultIDs) > 0 {
		next.VaultIDs, err = h.normalizeVaultIDs(r, principal, body.VaultIDs)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if len(body.Schedule) > 0 {
		next.Schedule, err = normalizeOptionalSchedule(body.Schedule)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateDeployment(r.Context(), principal.WorkspaceUUID, deploymentID, next)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	h.writeDeploymentResponse(w, r, updated)
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	archived, err := h.db.ArchiveDeployment(r.Context(), principal.WorkspaceUUID, deploymentID)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	h.writeDeploymentResponse(w, r, archived)
}

func (h *Handler) pauseRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	reason := json.RawMessage(`{"type":"manual"}`)
	paused, err := h.db.PauseDeployment(r.Context(), principal.WorkspaceUUID, deploymentID, reason)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	h.writeDeploymentResponse(w, r, paused)
}

func (h *Handler) unpauseRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	unpaused, err := h.db.UnpauseDeployment(r.Context(), principal.WorkspaceUUID, deploymentID)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	h.writeDeploymentResponse(w, r, unpaused)
}

func (h *Handler) runRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	deployment, err := h.db.GetDeployment(r.Context(), principal.WorkspaceUUID, deploymentID)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	if deployment.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("archived deployments cannot be run"))
		return
	}
	if referenceError := h.validateRunReferences(r, principal, deployment); referenceError != nil {
		h.writeRunReferenceFailure(w, r, principal, deployment, referenceError)
		return
	}
	sessionID, threadID, workID, runID, err := newRunIDs()
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate deployment run IDs"))
		return
	}
	now := time.Now().UTC()
	events, outcomes, err := sessionEventsFromInitialEvents(deployment.InitialEvents, now)
	if err != nil {
		h.writeRunReferenceFailure(w, r, principal, deployment, runError("unknown_error", err.Error()))
		return
	}
	resources, err := sessionResourcesFromDeployment(deployment, now)
	if err != nil {
		h.writeRunReferenceFailure(w, r, principal, deployment, runError("session_resource_not_found_error", err.Error()))
		return
	}
	deploymentIDCopy := deployment.ExternalID
	workData, _ := httpapi.MarshalRaw(map[string]any{"id": sessionID, "type": "session"})
	triggerContext := json.RawMessage(`{"type":"manual"}`)
	run, session, thread, createdEvents, err := h.db.CreateManualDeploymentRun(r.Context(), db.CreateManualDeploymentRunInput{
		DeploymentExternalID: deployment.ExternalID,
		Session: db.CreateSessionInput{
			Session: db.Session{
				UUID:                  uuid.NewString(),
				ExternalID:            sessionID,
				OrganizationUUID:      principal.OrganizationUUID,
				WorkspaceUUID:         principal.WorkspaceUUID,
				CreatedByAPIKeyUUID:   principal.APIKeyUUID,
				EnvironmentUUID:       deployment.EnvironmentUUID,
				EnvironmentExternalID: deployment.EnvironmentExternalID,
				AgentUUID:             deployment.AgentUUID,
				AgentExternalID:       deployment.AgentExternalID,
				AgentVersion:          deployment.AgentVersion,
				AgentSnapshot:         deployment.AgentSnapshot,
				DeploymentUUID:        &deployment.UUID,
				DeploymentID:          &deploymentIDCopy,
				Metadata:              httpapi.RawOr(deployment.Metadata, `{}`),
				VaultIDs:              httpapi.RawOr(deployment.VaultIDs, `[]`),
				Status:                "idle",
				Usage:                 json.RawMessage(`{}`),
				Stats:                 json.RawMessage(`{}`),
				OutcomeEvaluations:    outcomes,
				CreatedAt:             now,
				UpdatedAt:             now,
			},
			Thread: db.SessionThread{
				UUID:             uuid.NewString(),
				ExternalID:       threadID,
				OrganizationUUID: principal.OrganizationUUID,
				WorkspaceUUID:    principal.WorkspaceUUID,
				AgentSnapshot:    deployment.AgentSnapshot,
				Status:           "idle",
				Usage:            json.RawMessage(`{}`),
				Stats:            json.RawMessage(`{}`),
				CreatedAt:        now,
				UpdatedAt:        now,
			},
			Resources: resources,
			Work: db.EnvironmentWork{
				UUID:                  uuid.NewString(),
				ExternalID:            workID,
				OrganizationUUID:      principal.OrganizationUUID,
				WorkspaceUUID:         principal.WorkspaceUUID,
				EnvironmentUUID:       deployment.EnvironmentUUID,
				EnvironmentExternalID: deployment.EnvironmentExternalID,
				Data:                  workData,
				Metadata:              json.RawMessage(`{}`),
				State:                 "queued",
				CreatedAt:             now,
				UpdatedAt:             now,
			},
		},
		Events: events,
		Run: db.DeploymentRun{
			UUID:                uuid.NewString(),
			ExternalID:          runID,
			OrganizationUUID:    principal.OrganizationUUID,
			WorkspaceUUID:       principal.WorkspaceUUID,
			CreatedByAPIKeyUUID: principal.APIKeyUUID,
			TriggerType:         "manual",
			TriggerContext:      triggerContext,
			CreatedAt:           now,
		},
		Now: now,
	})
	if err != nil {
		if errors.Is(err, db.ErrFileReferenceNotFound) {
			h.writeRunReferenceFailure(
				w,
				r,
				principal,
				deployment,
				runErrorForReference("file", db.ErrNotFound, false),
			)
			return
		}
		if errors.Is(err, db.ErrFilestorePathExists) {
			httpapi.WriteError(w, r, httpapi.NewError(
				http.StatusConflict,
				"conflict_error",
				"File resource mount_path conflicts with the session filesystem",
			))
			return
		}
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	h.enqueueWebhook(r.Context(), principal, "session.created", session.ExternalID, nil)
	h.enqueueWebhook(r.Context(), principal, "session.pending", session.ExternalID, nil)
	h.enqueueWebhook(r.Context(), principal, "session.status_idled", session.ExternalID, nil)
	h.enqueueWebhook(r.Context(), principal, "session.thread_created", session.ExternalID, &thread.ExternalID)
	h.enqueueWebhook(r.Context(), principal, "session.thread_idled", session.ExternalID, &thread.ExternalID)
	if outcomesChanged(createdEvents) {
		h.enqueueWebhook(r.Context(), principal, "session.outcome_evaluation_ended", session.ExternalID, nil)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRun(run))
}

func (h *Handler) enqueueWebhook(ctx context.Context, principal auth.Principal, eventType, resourceID string, sessionThreadID *string) {
	if h.webhooks == nil {
		return
	}
	h.webhooks.Enqueue(ctx, webhooks.EnqueueInput{
		WorkspaceUUID:       principal.WorkspaceUUID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceExternalID: principal.WorkspaceExternalID,
		EventType:           eventType,
		ResourceID:          resourceID,
		Options:             webhooks.EventOptions{SessionThreadID: sessionThreadID},
	})
}

func (h *Handler) writeRunReferenceFailure(w http.ResponseWriter, r *http.Request, principal auth.Principal, deployment db.Deployment, runError json.RawMessage) {
	runID, err := ids.New("drun_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate deployment run ID"))
		return
	}
	now := time.Now().UTC()
	run, err := h.db.CreateDeploymentRunFailure(r.Context(), deployment, db.DeploymentRun{
		UUID:                uuid.NewString(),
		ExternalID:          runID,
		OrganizationUUID:    principal.OrganizationUUID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		Error:               runError,
		TriggerType:         "manual",
		TriggerContext:      json.RawMessage(`{"type":"manual"}`),
		CreatedAt:           now,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "create deployment run failure", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create deployment run"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRun(run))
}

func (h *Handler) validateRunReferences(r *http.Request, principal auth.Principal, deployment db.Deployment) json.RawMessage {
	agent, err := h.db.GetAgent(r.Context(), principal.WorkspaceUUID, deployment.AgentExternalID)
	if err != nil {
		return runErrorForReference("agent", err, false)
	}
	if agent.ArchivedAt != nil {
		return runErrorForReference("agent", nil, true)
	}
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceUUID, deployment.EnvironmentExternalID)
	if err != nil {
		return runErrorForReference("environment", err, false)
	}
	if env.ArchivedAt != nil {
		return runErrorForReference("environment", nil, true)
	}
	var vaultIDs []string
	if len(deployment.VaultIDs) > 0 && !httpapi.IsJSONNull(deployment.VaultIDs) {
		if err := json.Unmarshal(deployment.VaultIDs, &vaultIDs); err != nil {
			return runError("unknown_error", "Stored vault references are invalid")
		}
	}
	for _, vaultID := range vaultIDs {
		vault, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, vaultID)
		if err != nil {
			return runErrorForReference("vault", err, false)
		}
		if vault.ArchivedAt != nil {
			return runErrorForReference("vault", nil, true)
		}
	}
	var resources []map[string]any
	if len(deployment.Resources) > 0 && !httpapi.IsJSONNull(deployment.Resources) {
		if err := json.Unmarshal(deployment.Resources, &resources); err != nil {
			return runError("unknown_error", "Stored resources are invalid")
		}
	}
	for _, resource := range resources {
		resourceType, _ := resource["type"].(string)
		switch resourceType {
		case "file":
			fileID, _ := resource["file_id"].(string)
			if _, err := h.db.GetFile(r.Context(), principal.WorkspaceUUID, fileID); err != nil {
				return runErrorForReference("file", err, false)
			}
		case "memory_store":
			storeID, _ := resource["memory_store_id"].(string)
			store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceUUID, storeID)
			if err != nil {
				return runErrorForReference("memory_store", err, false)
			}
			if store.ArchivedAt != nil {
				return runErrorForReference("memory_store", nil, true)
			}
		}
	}
	return nil
}

func (h *RunsHandler) retrieveRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	runID := chi.URLParam(r, "deployment_run_id")
	run, err := h.db.GetDeploymentRun(r.Context(), principal.WorkspaceUUID, runID)
	if err != nil {
		h.writeRunLoadError(w, r, err, runID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRun(run))
}

func (h *RunsHandler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireWorkspaceCredential(w, r)
	if !ok {
		return
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	cursor, err := decodeRunCursor(r.URL.Query().Get("page"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	triggerType := strings.TrimSpace(r.URL.Query().Get("trigger_type"))
	if triggerType != "" && triggerType != "manual" && triggerType != "schedule" {
		writeBadRequest(w, r, errors.New("trigger_type must be manual or schedule"))
		return
	}
	hasError, err := parseOptionalBoolPointer(r, "has_error")
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
	records, hasMore, err := h.db.ListDeploymentRunsPage(r.Context(), db.ListDeploymentRunsPageParams{
		WorkspaceUUID:        principal.WorkspaceUUID,
		Limit:                limit,
		Cursor:               cursor,
		DeploymentExternalID: strings.TrimSpace(r.URL.Query().Get("deployment_id")),
		TriggerType:          triggerType,
		HasError:             hasError,
		CreatedAtGT:          createdAtGT,
		CreatedAtGTE:         createdAtGTE,
		CreatedAtLT:          createdAtLT,
		CreatedAtLTE:         createdAtLTE,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list deployment runs", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list deployment runs"))
		return
	}
	data := make([]deploymentRunResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromRun(record))
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeRunCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[deploymentRunResponse]{Data: data, NextPage: nextPage})
}

func (h *Handler) resolveAgent(r *http.Request, principal auth.Principal, raw json.RawMessage) (resolvedAgent, error) {
	agentID, version, err := parseAgentReference(raw)
	if err != nil {
		return resolvedAgent{}, err
	}
	var agent db.Agent
	if version > 0 {
		agent, err = h.db.GetAgentVersion(r.Context(), principal.WorkspaceUUID, agentID, version)
	} else {
		agent, err = h.db.GetAgent(r.Context(), principal.WorkspaceUUID, agentID)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return resolvedAgent{}, errors.New("agent not found")
		}
		return resolvedAgent{}, err
	}
	if agent.ArchivedAt != nil {
		return resolvedAgent{}, errors.New("agent must not be archived")
	}
	snapshot, err := agentsnapshot.FromAgent(agent)
	if err != nil {
		return resolvedAgent{}, err
	}
	ref, err := agentRefRaw(agent.ExternalID, agent.CurrentVersion)
	if err != nil {
		return resolvedAgent{}, err
	}
	return resolvedAgent{record: agent, snapshot: snapshot, ref: ref}, nil
}

func parseAgentReference(raw json.RawMessage) (string, int, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return "", 0, errors.New("agent is required")
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
			return "", 0, errors.New("agent must be a string or object")
		}
		if object.Type != "agent" {
			return "", 0, errors.New("agent.type is required and must be agent")
		}
		agentID = object.ID
		if object.Version != nil {
			version = *object.Version
			if version < 1 {
				return "", 0, errors.New("agent.version must be at least 1")
			}
		}
	}
	if strings.TrimSpace(agentID) == "" {
		return "", 0, errors.New("agent id must be non-empty")
	}
	return agentID, version, nil
}

func agentRefRaw(id string, version int) (json.RawMessage, error) {
	return httpapi.MarshalRaw(map[string]any{"id": id, "type": "agent", "version": version})
}

func (h *Handler) normalizeVaultIDs(r *http.Request, principal auth.Principal, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, errors.New("vault_ids must be an array of strings")
	}
	if len(ids) > 50 {
		return nil, errors.New("vault_ids may contain at most 50 entries")
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("vault_ids must contain non-empty strings")
		}
		vault, err := h.db.GetVault(r.Context(), principal.WorkspaceUUID, id)
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

func normalizeInitialEvents(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, errors.New("initial_events is required")
	}
	var events []deploymentInitialEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, errors.New("initial_events must be an array")
	}
	if len(events) == 0 || len(events) > 50 {
		return nil, errors.New("initial_events must contain between 1 and 50 events")
	}
	systemMessages := 0
	for index := range events {
		event := &events[index]
		switch event.Type {
		case "user.message":
			if err := validateMessageContent(event.Content, false); err != nil {
				return nil, err
			}
		case "system.message":
			systemMessages++
			if systemMessages > 1 {
				return nil, errors.New("initial_events may contain at most one system.message")
			}
			if index != len(events)-1 {
				return nil, errors.New("system.message must be the final initial event")
			}
			if index == 0 || events[index-1].Type != "user.message" {
				return nil, errors.New("system.message must immediately follow user.message")
			}
			if err := validateMessageContent(event.Content, true); err != nil {
				return nil, err
			}
		case "user.define_outcome":
			if strings.TrimSpace(event.Description) == "" {
				return nil, errors.New("description must be non-empty")
			}
			if err := validateOutcomeRubric(event.Rubric); err != nil {
				return nil, err
			}
			if event.MaxIterations != nil {
				if *event.MaxIterations < 1 {
					return nil, errors.New("max_iterations must be positive")
				}
				if *event.MaxIterations > 20 {
					return nil, errors.New("max_iterations must be at most 20")
				}
			}
		default:
			return nil, errors.New("initial_events type must be user.message, user.define_outcome, or system.message")
		}
	}
	return httpapi.MarshalRaw(events)
}

func sessionEventsFromInitialEvents(raw json.RawMessage, now time.Time) ([]db.SessionEvent, json.RawMessage, error) {
	var inputs []deploymentInitialEvent
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, nil, errors.New("stored initial_events are invalid")
	}
	events := make([]db.SessionEvent, 0, len(inputs))
	outcomes := make([]deploymentOutcomeEvaluation, 0)
	for _, input := range inputs {
		eventID, err := ids.New("sevt_")
		if err != nil {
			return nil, nil, err
		}
		payload := deploymentSessionEventPayload{
			ID:          eventID,
			ProcessedAt: now.Format(time.RFC3339),
			Type:        input.Type,
			Content:     input.Content,
			Description: input.Description,
			Rubric:      input.Rubric,
		}
		if input.Type == "user.define_outcome" {
			outcomeID, err := ids.New("outc_")
			if err != nil {
				return nil, nil, err
			}
			maxIterations := 3
			if input.MaxIterations != nil {
				maxIterations = *input.MaxIterations
			}
			payload.OutcomeID = outcomeID
			payload.MaxIterations = maxIterations
			outcomes = append(outcomes, deploymentOutcomeEvaluation{
				ID:            outcomeID,
				OutcomeID:     outcomeID,
				MaxIterations: maxIterations,
				Status:        "pending",
				Type:          "outcome_evaluation",
				UpdatedAt:     now.Format(time.RFC3339),
			})
		}
		payloadRaw, err := httpapi.MarshalRaw(payload)
		if err != nil {
			return nil, nil, err
		}
		events = append(events, db.SessionEvent{
			UUID:        uuid.NewString(),
			ExternalID:  eventID,
			EventType:   input.Type,
			Payload:     payloadRaw,
			ProcessedAt: now,
			CreatedAt:   now,
		})
	}
	outcomesRaw, err := httpapi.MarshalRaw(outcomes)
	if err != nil {
		return nil, nil, err
	}
	return events, outcomesRaw, nil
}

func normalizeOptionalSchedule(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var schedule deploymentScheduleRequest
	if err := json.Unmarshal(raw, &schedule); err != nil {
		return nil, errors.New("schedule must be an object or null")
	}
	scheduleType, err := parseRequiredRawString(schedule.Type, "type")
	if err != nil {
		return nil, err
	}
	if scheduleType != "cron" {
		return nil, errors.New("schedule.type must be cron")
	}
	expression, err := parseRequiredRawString(schedule.Expression, "expression")
	if err != nil {
		return nil, err
	}
	timezone, err := parseRequiredRawString(schedule.Timezone, "timezone")
	if err != nil {
		return nil, err
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, errors.New("schedule.timezone must be a valid IANA timezone")
	}
	if _, err := parseCronExpression(expression); err != nil {
		return nil, fmt.Errorf("schedule.expression %v", err)
	}
	return httpapi.MarshalRaw(map[string]any{
		"type":       "cron",
		"expression": expression,
		"timezone":   timezone,
	})
}

type cronExpression struct {
	Minute     cronField
	Hour       cronField
	DayOfMonth cronField
	Month      cronField
	DayOfWeek  cronField
}

type cronField struct {
	Values   map[int]bool
	Wildcard bool
}

func parseCronExpression(expression string) (cronExpression, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return cronExpression{}, errors.New("must be a 5-field POSIX cron expression")
	}
	var parsed cronExpression
	var err error
	if parsed.Minute, err = parseCronField(parts[0], 0, 59, false); err != nil {
		return cronExpression{}, fmt.Errorf("minute %w", err)
	}
	if parsed.Hour, err = parseCronField(parts[1], 0, 23, false); err != nil {
		return cronExpression{}, fmt.Errorf("hour %w", err)
	}
	if parsed.DayOfMonth, err = parseCronField(parts[2], 1, 31, false); err != nil {
		return cronExpression{}, fmt.Errorf("day-of-month %w", err)
	}
	if parsed.Month, err = parseCronField(parts[3], 1, 12, false); err != nil {
		return cronExpression{}, fmt.Errorf("month %w", err)
	}
	if parsed.DayOfWeek, err = parseCronField(parts[4], 0, 7, true); err != nil {
		return cronExpression{}, fmt.Errorf("day-of-week %w", err)
	}
	return parsed, nil
}

func parseCronField(raw string, minValue, maxValue int, normalizeSunday bool) (cronField, error) {
	if raw == "" {
		return cronField{}, errors.New("is empty")
	}
	if strings.ContainsAny(raw, "LW#?@") {
		return cronField{}, errors.New("contains unsupported syntax")
	}
	field := cronField{Values: map[int]bool{}, Wildcard: raw == "*"}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return cronField{}, errors.New("contains an empty list item")
		}
		step := 1
		rangePart := part
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return cronField{}, errors.New("contains invalid step syntax")
			}
			rangePart = pieces[0]
			parsedStep, err := strconv.Atoi(pieces[1])
			if err != nil || parsedStep <= 0 {
				return cronField{}, errors.New("step must be positive")
			}
			step = parsedStep
		}
		start, end := minValue, maxValue
		if rangePart != "*" {
			if strings.Contains(rangePart, "-") {
				pieces := strings.Split(rangePart, "-")
				if len(pieces) != 2 {
					return cronField{}, errors.New("contains invalid range syntax")
				}
				var err error
				start, err = strconv.Atoi(pieces[0])
				if err != nil {
					return cronField{}, errors.New("range start must be numeric")
				}
				end, err = strconv.Atoi(pieces[1])
				if err != nil {
					return cronField{}, errors.New("range end must be numeric")
				}
				if start > end {
					return cronField{}, errors.New("range start must be <= range end")
				}
			} else {
				value, err := strconv.Atoi(rangePart)
				if err != nil {
					return cronField{}, errors.New("value must be numeric")
				}
				start, end = value, value
			}
		}
		if start < minValue || end > maxValue {
			return cronField{}, fmt.Errorf("value must be between %d and %d", minValue, maxValue)
		}
		for value := start; value <= end; value += step {
			if normalizeSunday && value == 7 {
				field.Values[0] = true
			} else {
				field.Values[value] = true
			}
		}
	}
	return field, nil
}

func upcomingRuns(scheduleRaw json.RawMessage, lastRunAt *time.Time, now time.Time, archived bool) json.RawMessage {
	if len(scheduleRaw) == 0 || httpapi.IsJSONNull(scheduleRaw) || archived {
		return nil
	}
	var schedule struct {
		Type       string `json:"type"`
		Expression string `json:"expression"`
		Timezone   string `json:"timezone"`
	}
	if err := json.Unmarshal(scheduleRaw, &schedule); err != nil || schedule.Type != "cron" {
		return nil
	}
	cron, err := parseCronExpression(schedule.Expression)
	if err != nil {
		return nil
	}
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return nil
	}
	cursor := now.In(loc).Add(time.Minute).Truncate(time.Minute)
	deadline := cursor.Add(366 * 24 * time.Hour)
	values := make([]string, 0, 5)
	for cursor.Before(deadline) && len(values) < 5 {
		if cronMatches(cron, cursor) {
			values = append(values, cursor.UTC().Format(time.RFC3339))
		}
		cursor = cursor.Add(time.Minute)
	}
	raw, _ := httpapi.MarshalRaw(values)
	return raw
}

func cronMatches(expr cronExpression, t time.Time) bool {
	dayOfWeek := int(t.Weekday())
	domMatches := expr.DayOfMonth.Values[t.Day()]
	dowMatches := expr.DayOfWeek.Values[dayOfWeek]
	dayMatches := domMatches && dowMatches
	if expr.DayOfMonth.Wildcard && !expr.DayOfWeek.Wildcard {
		dayMatches = dowMatches
	} else if !expr.DayOfMonth.Wildcard && expr.DayOfWeek.Wildcard {
		dayMatches = domMatches
	} else if !expr.DayOfMonth.Wildcard && !expr.DayOfWeek.Wildcard {
		dayMatches = domMatches || dowMatches
	}
	return expr.Minute.Values[t.Minute()] &&
		expr.Hour.Values[t.Hour()] &&
		expr.Month.Values[int(t.Month())] &&
		dayMatches
}

func scheduleResponse(scheduleRaw json.RawMessage, lastRunAt *time.Time, now time.Time, archived bool) json.RawMessage {
	if len(scheduleRaw) == 0 || httpapi.IsJSONNull(scheduleRaw) {
		return nil
	}
	var schedule map[string]any
	if err := json.Unmarshal(scheduleRaw, &schedule); err != nil || schedule == nil {
		return nil
	}
	schedule["last_run_at"] = nil
	if lastRunAt != nil {
		schedule["last_run_at"] = httpapi.FormatTime(*lastRunAt)
	}
	schedule["upcoming_runs_at"] = agentsnapshot.RawJSONValue(upcomingRuns(scheduleRaw, lastRunAt, now, archived), []any{})
	raw, _ := httpapi.MarshalRaw(schedule)
	return raw
}

func (h *Handler) writeDeploymentResponse(w http.ResponseWriter, r *http.Request, deployment db.Deployment) {
	response, err := responseFromDeployment(deployment, time.Now().UTC())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "map deployment response", "error", err, "deployment_id", deployment.ExternalID)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not return deployment"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func responseFromDeployment(deployment db.Deployment, now time.Time) (deploymentResponse, error) {
	description := ""
	if deployment.Description != nil {
		description = *deployment.Description
	}
	ref, err := agentRefRaw(deployment.AgentExternalID, deployment.AgentVersion)
	if err != nil {
		return deploymentResponse{}, err
	}
	resources, err := deploymentResourcesResponse(deployment.Resources)
	if err != nil {
		return deploymentResponse{}, err
	}
	return deploymentResponse{
		ID:            deployment.ExternalID,
		Agent:         ref,
		ArchivedAt:    httpapi.OptionalTime(deployment.ArchivedAt),
		CreatedAt:     httpapi.FormatTime(deployment.CreatedAt),
		Description:   description,
		EnvironmentID: deployment.EnvironmentExternalID,
		InitialEvents: httpapi.RawOr(deployment.InitialEvents, `[]`),
		Metadata:      httpapi.RawOr(deployment.Metadata, `{}`),
		Name:          deployment.Name,
		PausedReason:  deployment.PausedReason,
		Resources:     resources,
		Schedule:      scheduleResponse(deployment.Schedule, deployment.LastRunAt, now, deployment.ArchivedAt != nil),
		Status:        deployment.Status,
		Type:          "deployment",
		UpdatedAt:     httpapi.FormatTime(deployment.UpdatedAt),
		VaultIDs:      httpapi.RawOr(deployment.VaultIDs, `[]`),
	}, nil
}

func responseFromRun(run db.DeploymentRun) deploymentRunResponse {
	ref, _ := agentRefRaw(run.AgentExternalID, run.AgentVersion)
	return deploymentRunResponse{
		ID:             run.ExternalID,
		Agent:          ref,
		CreatedAt:      httpapi.FormatTime(run.CreatedAt),
		DeploymentID:   run.DeploymentExternalID,
		Error:          run.Error,
		SessionID:      run.SessionExternalID,
		TriggerContext: httpapi.RawOr(run.TriggerContext, `{}`),
		Type:           "deployment_run",
	}
}

func runErrorForReference(resourceType string, err error, archived bool) json.RawMessage {
	if archived {
		switch resourceType {
		case "environment":
			return runError("environment_archived_error", "Environment is archived")
		case "agent":
			return runError("agent_archived_error", "Agent is archived")
		case "vault":
			return runError("vault_archived_error", "Vault is archived")
		case "file":
			return runError("file_not_found_error", "File is unavailable")
		case "memory_store":
			return runError("memory_store_archived_error", "Memory store is archived")
		}
	}
	if errors.Is(err, db.ErrNotFound) {
		switch resourceType {
		case "agent":
			return runError("agent_archived_error", "Agent not found")
		case "environment":
			return runError("environment_not_found_error", "Environment not found")
		case "vault":
			return runError("vault_not_found_error", "Vault not found")
		case "file":
			return runError("file_not_found_error", "File not found")
		case "memory_store":
			return runError("session_resource_not_found_error", "Memory store not found")
		}
	}
	return runError("unknown_error", "Could not create session")
}

func runError(errorType, message string) json.RawMessage {
	raw, _ := httpapi.MarshalRaw(map[string]any{"type": errorType, "message": message})
	return raw
}

func outcomesChanged(events []db.SessionEvent) bool {
	for _, event := range events {
		if event.EventType == "user.define_outcome" {
			return true
		}
	}
	return false
}

func newRunIDs() (sessionID, threadID, workID, runID string, err error) {
	if sessionID, err = ids.New("sesn_"); err != nil {
		return
	}
	if threadID, err = ids.New("sthr_"); err != nil {
		return
	}
	if workID, err = ids.New("work_"); err != nil {
		return
	}
	if runID, err = ids.New("drun_"); err != nil {
		return
	}
	return
}

func requireAPIKey(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	if !isWorkspaceCredential(principal) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Credential cannot access deployments"))
		return auth.Principal{}, false
	}
	return principal, true
}

func requireWorkspaceCredential(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	if !isWorkspaceCredential(principal) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Credential cannot access deployment runs"))
		return auth.Principal{}, false
	}
	return principal, true
}

func isWorkspaceCredential(principal auth.Principal) bool {
	return principal.CredentialType == auth.CredentialTypeAPIKey ||
		principal.CredentialType == auth.CredentialTypePlatformSession
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

func parseNullableString(raw json.RawMessage, name string) (*string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return nullableStringFromRaw(raw, name)
}

func nullableStringFromRaw(raw json.RawMessage, name string) (*string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
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
	return parseRequiredRawString(raw, name)
}

func validateMetadataEntries(metadata map[string]string) error {
	if err := httpapi.ValidateMetadataEntryLimit(metadata, 16, "metadata may contain at most 16 entries"); err != nil {
		return err
	}
	for key, value := range metadata {
		if key == "" || utf8.RuneCountInString(key) > 64 {
			return errors.New("metadata keys must be between 1 and 64 characters")
		}
		if utf8.RuneCountInString(value) > 512 {
			return errors.New("metadata values must be at most 512 characters")
		}
	}
	return nil
}

func patchDeploymentMetadata(current, raw json.RawMessage) (json.RawMessage, error) {
	if httpapi.IsJSONNull(raw) {
		return nil, errors.New("metadata must be an object with string or null values")
	}
	metadata := map[string]string{}
	if len(current) > 0 && !httpapi.IsJSONNull(current) {
		if err := json.Unmarshal(current, &metadata); err != nil {
			return nil, errors.New("stored metadata is invalid")
		}
	}
	var patch map[string]*string
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, errors.New("metadata must be an object with string or null values")
	}
	for key, value := range patch {
		if value == nil {
			delete(metadata, key)
			continue
		}
		metadata[key] = *value
	}
	if err := validateMetadataEntries(metadata); err != nil {
		return nil, err
	}
	return httpapi.MarshalRaw(metadata)
}

func rejectNullField(raw json.RawMessage, name string) error {
	if len(raw) > 0 && httpapi.IsJSONNull(raw) {
		return fmt.Errorf("%s must not be null", name)
	}
	return nil
}

func validateMessageContent(raw json.RawMessage, textOnly bool) error {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return errors.New("initial_events content is required")
	}
	var blocks []deploymentContentBlockRequest
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return errors.New("initial_events content must be an array")
	}
	if len(blocks) == 0 {
		return errors.New("initial_events content must contain at least one block")
	}
	for _, block := range blocks {
		blockType, err := parseRequiredRawString(block.Type, "content.type")
		if err != nil {
			return err
		}
		if textOnly && blockType != "text" {
			return errors.New("system.message content must contain only text blocks")
		}
		switch blockType {
		case "text":
			if _, err := parseRequiredRawString(block.Text, "content.text"); err != nil {
				return err
			}
		case "image":
			if err := validateContentSource(block.Source, false); err != nil {
				return err
			}
		case "document":
			if err := validateContentSource(block.Source, true); err != nil {
				return err
			}
			for _, field := range []struct {
				name string
				raw  json.RawMessage
			}{{name: "context", raw: block.Context}, {name: "title", raw: block.Title}} {
				if len(field.raw) > 0 && !httpapi.IsJSONNull(field.raw) {
					if _, err := parseRequiredRawString(field.raw, field.name); err != nil {
						return err
					}
				}
			}
		default:
			return errors.New("user.message content type must be text, image, or document")
		}
	}
	return nil
}

func validateContentSource(raw json.RawMessage, document bool) error {
	var source deploymentContentSourceRequest
	if err := json.Unmarshal(raw, &source); err != nil {
		return errors.New("content source must be an object")
	}
	sourceType, err := parseRequiredRawString(source.Type, "source.type")
	if err != nil {
		return err
	}
	switch sourceType {
	case "base64":
		if _, err := parseRequiredRawString(source.Data, "source.data"); err != nil {
			return err
		}
		_, err = parseRequiredRawString(source.MediaType, "source.media_type")
		return err
	case "url":
		_, err = parseRequiredRawString(source.URL, "source.url")
		return err
	case "file":
		_, err = parseRequiredRawString(source.FileID, "source.file_id")
		return err
	case "text":
		if !document {
			return errors.New("image source type must be base64, url, or file")
		}
		if _, err := parseRequiredRawString(source.Data, "source.data"); err != nil {
			return err
		}
		mediaType, err := parseRequiredRawString(source.MediaType, "source.media_type")
		if err != nil {
			return err
		}
		if mediaType != "text/plain" {
			return errors.New("text document media_type must be text/plain")
		}
		return nil
	default:
		if document {
			return errors.New("document source type must be base64, text, url, or file")
		}
		return errors.New("image source type must be base64, url, or file")
	}
}

func validateOutcomeRubric(raw json.RawMessage) error {
	var rubric deploymentOutcomeRubricRequest
	if err := json.Unmarshal(raw, &rubric); err != nil {
		return errors.New("user.define_outcome rubric must be an object")
	}
	rubricType, err := parseRequiredRawString(rubric.Type, "rubric.type")
	if err != nil {
		return err
	}
	switch rubricType {
	case "file":
		_, err = parseRequiredRawString(rubric.FileID, "rubric.file_id")
		return err
	case "text":
		content, err := parseRequiredRawString(rubric.Content, "rubric.content")
		if err != nil {
			return err
		}
		if utf8.RuneCountInString(content) > 262144 {
			return errors.New("user.define_outcome text rubric must be at most 262144 characters")
		}
		return nil
	default:
		return errors.New("user.define_outcome rubric type must be file or text")
	}
}

func rawOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) > 0 {
		return raw
	}
	return json.RawMessage(fallback)
}

func validateCheckout(raw json.RawMessage) error {
	var checkout deploymentCheckoutRequest
	if err := json.Unmarshal(raw, &checkout); err != nil {
		return errors.New("checkout must be an object")
	}
	checkoutType, err := parseRequiredRawString(checkout.Type, "type")
	if err != nil {
		return err
	}
	switch checkoutType {
	case "branch":
		_, err = parseRequiredRawString(checkout.Name, "name")
	case "commit":
		_, err = parseRequiredRawString(checkout.SHA, "sha")
	default:
		err = errors.New("checkout.type must be branch or commit")
	}
	return err
}

func deploymentAPIContractEnabled(r *http.Request) bool {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if ok && principal.CredentialType == auth.CredentialTypePlatformSession && r.URL.Query().Get("beta") == "true" {
		return true
	}
	if strings.TrimSpace(r.Header.Get("anthropic-version")) != defaultAnthropicAPIVersion {
		return false
	}
	for _, value := range r.Header.Values("anthropic-beta") {
		for _, beta := range strings.Split(value, ",") {
			if strings.TrimSpace(beta) == managedAgentsBeta {
				return true
			}
		}
	}
	return false
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

func parseOptionalBoolPointer(r *http.Request, name string) (*bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be true or false", name)
	}
	return &value, nil
}

func encodeDeploymentCursor(deployment db.Deployment) string {
	return encodeCursor(deployment.CreatedAt, deployment.UUID)
}

func encodeRunCursor(run db.DeploymentRun) string {
	return encodeCursor(run.CreatedAt, run.UUID)
}

func encodeCursor(createdAt time.Time, recordUUID string) string {
	data, _ := json.Marshal(map[string]any{"created_at": createdAt.UTC().Format(time.RFC3339Nano), "uuid": recordUUID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeDeploymentCursor(raw string) (*db.DeploymentPageCursor, error) {
	createdAt, recordUUID, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.DeploymentPageCursor{CreatedAt: *createdAt, UUID: recordUUID}, nil
}

func decodeRunCursor(raw string) (*db.DeploymentRunPageCursor, error) {
	createdAt, recordUUID, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.DeploymentRunPageCursor{CreatedAt: *createdAt, UUID: recordUUID}, nil
}

func decodeCursor(raw string) (*time.Time, string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, "", nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, "", errors.New("page cursor is invalid")
	}
	var payload struct {
		CreatedAt string `json:"created_at"`
		UUID      string `json:"uuid"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.CreatedAt == "" {
		return nil, "", errors.New("page cursor is invalid")
	}
	parsedUUID, err := uuid.Parse(payload.UUID)
	if err != nil {
		return nil, "", errors.New("page cursor is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, "", errors.New("page cursor is invalid")
	}
	createdAt = createdAt.UTC()
	return &createdAt, parsedUUID.String(), nil
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
}

func (h *Handler) writeEnvironmentLoadError(w http.ResponseWriter, r *http.Request, err error, environmentID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
		return
	}
	h.logger.ErrorContext(r.Context(), "environment operation", "error", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Environment operation failed"))
}

func (h *Handler) writeResourceBuildError(w http.ResponseWriter, r *http.Request, err error) {
	var refErr resourceReferenceError
	if errors.As(err, &refErr) {
		if refErr.ResourceType == "file" && errors.Is(refErr.Err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found: "+refErr.ResourceID))
			return
		}
		if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Memory store not found: "+refErr.ResourceID))
			return
		}
		if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrInvalidState) {
			writeBadRequest(w, r, errors.New("memory store must not be archived"))
			return
		}
		h.logger.ErrorContext(r.Context(), "deployment resource reference", "resource_type", refErr.ResourceType, "resource_id", refErr.ResourceID, "error", refErr.Err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not validate deployment resource"))
		return
	}
	writeBadRequest(w, r, err)
}

func (h *Handler) writeDeploymentLoadError(w http.ResponseWriter, r *http.Request, err error, deploymentID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Deployment not found: "+deploymentID))
		return
	}
	if errors.Is(err, db.ErrInvalidState) {
		writeBadRequest(w, r, errors.New("deployment state does not allow this operation"))
		return
	}
	h.logger.ErrorContext(r.Context(), "deployment operation", "error", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Deployment operation failed"))
}

func (h *RunsHandler) writeRunLoadError(w http.ResponseWriter, r *http.Request, err error, runID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Deployment run not found: "+runID))
		return
	}
	h.logger.ErrorContext(r.Context(), "deployment run operation", "error", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Deployment run operation failed"))
}
