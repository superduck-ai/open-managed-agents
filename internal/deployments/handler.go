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
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
	"github.com/superduck-ai/yourbatis"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxDeploymentBodySize      = 4 << 20
	managedAgentsBeta          = "managed-agents-2026-04-01"
	defaultAnthropicAPIVersion = "2023-06-01"
)

type Handler struct {
	db        *db.DB
	webhooks  webhookEnqueuer
	scheduler *DeploymentScheduler
	logger    *slog.Logger
	router    chi.Router
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
	ID            string                       `json:"id"`
	Agent         deploymentAgentReference     `json:"agent"`
	ArchivedAt    *string                      `json:"archived_at"`
	CreatedAt     string                       `json:"created_at"`
	Description   string                       `json:"description"`
	EnvironmentID string                       `json:"environment_id"`
	InitialEvents []deploymentInitialEvent     `json:"initial_events"`
	Metadata      map[string]string            `json:"metadata"`
	Name          string                       `json:"name"`
	PausedReason  *deploymentPausedReason      `json:"paused_reason"`
	Resources     []deploymentResourceResponse `json:"resources"`
	Schedule      *deploymentScheduleResponse  `json:"schedule"`
	Status        string                       `json:"status"`
	Type          string                       `json:"type"`
	UpdatedAt     string                       `json:"updated_at"`
	VaultIDs      []string                     `json:"vault_ids"`
}

type deploymentAgentReference struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version int    `json:"version"`
}

type deploymentScheduleResponse struct {
	Type           string   `json:"type"`
	Expression     string   `json:"expression"`
	Timezone       string   `json:"timezone"`
	LastRunAt      *string  `json:"last_run_at"`
	UpcomingRunsAt []string `json:"upcoming_runs_at"`
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

type deploymentCheckoutRequest struct {
	Name json.RawMessage `json:"name"`
	SHA  json.RawMessage `json:"sha"`
	Type json.RawMessage `json:"type"`
}

type deploymentRunResponse struct {
	ID             string                      `json:"id"`
	Agent          deploymentAgentReference    `json:"agent"`
	CreatedAt      string                      `json:"created_at"`
	DeploymentID   string                      `json:"deployment_id"`
	Error          *deploymentRunError         `json:"error"`
	SessionID      *string                     `json:"session_id"`
	TriggerContext deploymentRunTriggerContext `json:"trigger_context"`
	Type           string                      `json:"type"`
}

type deploymentRunError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type deploymentPausedReason struct {
	Type  string              `json:"type"`
	Error *deploymentRunError `json:"error,omitempty"`
}

type deploymentRunTriggerContext struct {
	ScheduledAt string `json:"scheduled_at,omitempty"`
	Type        string `json:"type"`
}

type pageResponse[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

type deploymentCursorPayload struct {
	CreatedAt string `json:"created_at"`
	UUID      string `json:"uuid"`
}

type resolvedAgent struct {
	record   db.Agent
	snapshot json.RawMessage
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

type deploymentInitialEventRequest struct {
	Type          string          `json:"type"`
	Content       json.RawMessage `json:"content,omitempty"`
	Description   string          `json:"description,omitempty"`
	Rubric        json.RawMessage `json:"rubric,omitempty"`
	MaxIterations *int            `json:"max_iterations,omitempty"`
}

type deploymentInitialEvent struct {
	Type          string                   `json:"type"`
	Content       []deploymentContentBlock `json:"content,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Rubric        *deploymentOutcomeRubric `json:"rubric,omitempty"`
	MaxIterations *int                     `json:"max_iterations,omitempty"`
}

type deploymentContentBlock struct {
	Type    string                   `json:"type"`
	Text    string                   `json:"text,omitempty"`
	Source  *deploymentContentSource `json:"source,omitempty"`
	Context string                   `json:"context,omitempty"`
	Title   string                   `json:"title,omitempty"`
}

type deploymentContentSource struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

type deploymentOutcomeRubric struct {
	Type    string `json:"type"`
	FileID  string `json:"file_id,omitempty"`
	Content string `json:"content,omitempty"`
}

type deploymentSessionEventPayload struct {
	ID            string                   `json:"id"`
	ProcessedAt   string                   `json:"processed_at"`
	Type          string                   `json:"type"`
	Content       []deploymentContentBlock `json:"content,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Rubric        *deploymentOutcomeRubric `json:"rubric,omitempty"`
	MaxIterations int                      `json:"max_iterations,omitempty"`
	OutcomeID     string                   `json:"outcome_id,omitempty"`
}

type deploymentOutcomeEvaluation struct {
	ID            string `json:"id"`
	OutcomeID     string `json:"outcome_id"`
	MaxIterations int    `json:"max_iterations"`
	Status        string `json:"status"`
	Type          string `json:"type"`
	UpdatedAt     string `json:"updated_at"`
}

type deploymentAgentSnapshot struct {
	Multiagent *struct {
		Agents []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"agents"`
	} `json:"multiagent"`
	Skills []struct {
		ID      string `json:"skill_id"`
		Type    string `json:"type"`
		Version string `json:"version"`
	} `json:"skills"`
}

func NewHandler(database *db.DB, webhookEvents webhookEnqueuer, scheduler *DeploymentScheduler, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{db: database, webhooks: webhookEvents, scheduler: scheduler, logger: logger}
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
	metadata, err := httpapi.NormalizeMetadata(jsonx.Default(body.Metadata, `{}`), validateMetadataEntries)
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
	resources, resourceSecrets, err := h.normalizeResources(r, principal, jsonx.Default(body.Resources, `[]`))
	if err != nil {
		h.writeResourceBuildError(w, r, err)
		return
	}
	vaultIDs, err := h.normalizeVaultIDs(r, principal, jsonx.Default(body.VaultIDs, `[]`))
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
	deployment := db.Deployment{
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
	}
	var created db.Deployment
	err = h.db.Transaction(r.Context(), func(tx *yourbatis.Tx) error {
		created, err = h.db.CreateDeploymentTx(r.Context(), tx, deployment)
		return err
	})
	if err != nil {
		if errors.Is(err, db.ErrLimitExceeded) {
			writeBadRequest(w, r, fmt.Errorf(
				"an organization may have at most %d scheduled deployments",
				db.MaxScheduledDeploymentsPerOrganization,
			))
			return
		}
		h.logger.ErrorContext(r.Context(), "create deployment", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create deployment"))
		return
	}
	h.updateSchedule(r.Context(), created)
	h.enqueueWebhook(r.Context(), principal, "deployment.created", created.ExternalID, nil)
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
	scheduleRaw := body.Schedule
	if err := applyScheduleUpdate(&next, scheduleRaw); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	now := time.Now().UTC()
	next.UpdatedAt = now
	var updated db.Deployment
	err = h.db.Transaction(r.Context(), func(tx *yourbatis.Tx) error {
		updated, err = h.db.UpdateDeploymentTx(r.Context(), tx, principal.WorkspaceUUID, deploymentID, db.UpdateDeploymentInput{
			Deployment: next, ScheduleProvided: len(scheduleRaw) > 0,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, db.ErrLimitExceeded) {
			writeBadRequest(w, r, fmt.Errorf(
				"an organization may have at most %d scheduled deployments",
				db.MaxScheduledDeploymentsPerOrganization,
			))
			return
		}
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	h.updateSchedule(r.Context(), updated)
	h.enqueueWebhook(r.Context(), principal, "deployment.updated", updated.ExternalID, nil)
	h.writeDeploymentResponse(w, r, updated)
}

func applyScheduleUpdate(next *db.Deployment, raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	schedule, err := normalizeOptionalSchedule(raw)
	if err != nil {
		return err
	}
	next.Schedule = schedule
	return nil
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
	h.updateSchedule(r.Context(), archived)
	h.enqueueWebhook(r.Context(), principal, "deployment.archived", archived.ExternalID, nil)
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
	h.updateSchedule(r.Context(), paused)
	h.enqueueWebhook(r.Context(), principal, "deployment.paused", paused.ExternalID, nil)
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
	h.updateSchedule(r.Context(), unpaused)
	h.enqueueWebhook(r.Context(), principal, "deployment.unpaused", unpaused.ExternalID, nil)
	h.writeDeploymentResponse(w, r, unpaused)
}

func (h *Handler) updateSchedule(ctx context.Context, deployment db.Deployment) {
	if h.scheduler != nil {
		h.scheduler.Update(ctx, deployment)
	}
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
	referenceFailure, err := validateRunReferences(r.Context(), h.db, principal.WorkspaceUUID, deployment)
	if err != nil {
		h.writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	if referenceFailure != nil {
		h.writeRunReferenceFailure(w, r, principal, deployment, referenceFailure)
		return
	}
	now := time.Now().UTC()
	preparedRun, err := prepareDeploymentRun(deployment, now)
	if err != nil {
		if errors.Is(err, errRetryableRunPreparation) {
			h.writeDeploymentLoadError(w, r, err, deploymentID)
			return
		}
		h.writeRunReferenceFailure(w, r, principal, deployment, runError("session_resource_not_found_error", err.Error()))
		return
	}
	run, session, thread, createdEvents, err := h.db.CreateManualDeploymentRun(r.Context(), db.CreateManualDeploymentRunInput{
		DeploymentExternalID: deployment.ExternalID,
		Session:              preparedRun.Session,
		Events:               preparedRun.Events,
		Run: db.DeploymentRun{
			UUID:                uuid.NewString(),
			ExternalID:          preparedRun.RunID,
			CreatedByAPIKeyUUID: principal.APIKeyUUID,
			TriggerType:         "manual",
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
	writeRunResponse(w, r, h.logger, run)
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

func (h *Handler) writeRunReferenceFailure(w http.ResponseWriter, r *http.Request, principal auth.Principal, deployment db.Deployment, runError *deploymentRunError) {
	runID, err := ids.New("drun_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate deployment run ID"))
		return
	}
	now := time.Now().UTC()
	runErrorJSON, err := jsonx.Encode(runError)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create deployment run"))
		return
	}
	run, err := h.db.CreateDeploymentRunFailure(r.Context(), deployment, db.DeploymentRun{
		UUID:                uuid.NewString(),
		ExternalID:          runID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		Error:               runErrorJSON,
		TriggerType:         "manual",
		CreatedAt:           now,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "create deployment run failure", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create deployment run"))
		return
	}
	writeRunResponse(w, r, h.logger, run)
}

func validateRunReferences(ctx context.Context, database *db.DB, workspaceUUID string, deployment db.Deployment) (*deploymentRunError, error) {
	agent, err := database.GetAgent(ctx, workspaceUUID, deployment.AgentExternalID)
	if err != nil {
		return classifyReferenceFailure("agent", err, false)
	}
	if agent.ArchivedAt != nil {
		return classifyReferenceFailure("agent", nil, true)
	}
	return validateRunDependencies(ctx, database, workspaceUUID, deployment)
}

func validateRunDependencies(ctx context.Context, database *db.DB, workspaceUUID string, deployment db.Deployment) (*deploymentRunError, error) {
	var snapshot deploymentAgentSnapshot
	if err := json.Unmarshal(deployment.AgentSnapshot, &snapshot); err != nil {
		return runError("unknown_error", "Stored agent snapshot is invalid"), nil
	}
	if snapshot.Multiagent != nil {
		for _, reference := range snapshot.Multiagent.Agents {
			if reference.Type == "self" {
				continue
			}
			subagent, err := database.GetAgent(ctx, workspaceUUID, reference.ID)
			if err != nil {
				return classifyReferenceFailure("agent", err, false)
			}
			if subagent.ArchivedAt != nil {
				return classifyReferenceFailure("agent", nil, true)
			}
		}
	}
	for _, skill := range snapshot.Skills {
		if skill.Type != "custom" {
			continue
		}
		var err error
		if skill.Version == "latest" {
			_, err = database.GetLatestSkillVersion(ctx, workspaceUUID, skill.ID)
		} else {
			_, err = database.GetSkillVersion(ctx, workspaceUUID, skill.ID, skill.Version)
		}
		if err != nil {
			return classifyReferenceFailure("skill", err, false)
		}
	}
	env, err := database.GetEnvironment(ctx, workspaceUUID, deployment.EnvironmentExternalID)
	if err != nil {
		return classifyReferenceFailure("environment", err, false)
	}
	if env.ArchivedAt != nil {
		return classifyReferenceFailure("environment", nil, true)
	}
	var vaultIDs []string
	if len(deployment.VaultIDs) > 0 && !jsonx.IsNull(deployment.VaultIDs) {
		if err := json.Unmarshal(deployment.VaultIDs, &vaultIDs); err != nil {
			return runError("unknown_error", "Stored vault references are invalid"), nil
		}
	}
	for _, vaultID := range vaultIDs {
		vault, err := database.GetVault(ctx, workspaceUUID, vaultID)
		if err != nil {
			return classifyReferenceFailure("vault", err, false)
		}
		if vault.ArchivedAt != nil {
			return classifyReferenceFailure("vault", nil, true)
		}
	}
	var resources []deploymentResourcePayload
	if len(deployment.Resources) > 0 && !jsonx.IsNull(deployment.Resources) {
		if err := json.Unmarshal(deployment.Resources, &resources); err != nil {
			return runError("unknown_error", "Stored resources are invalid"), nil
		}
	}
	for _, resource := range resources {
		switch resource.Type {
		case "file":
			if _, err := database.GetFile(ctx, workspaceUUID, resource.FileID); err != nil {
				return classifyReferenceFailure("file", err, false)
			}
		case "memory_store":
			store, err := database.GetMemoryStore(ctx, workspaceUUID, resource.MemoryStoreID)
			if err != nil {
				return classifyReferenceFailure("memory_store", err, false)
			}
			if store.ArchivedAt != nil {
				return classifyReferenceFailure("memory_store", nil, true)
			}
		}
	}
	return nil, nil
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
	writeRunResponse(w, r, h.logger, run)
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
		response, err := responseFromRun(record)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "map deployment run response", "error", err, "deployment_run_id", record.ExternalID)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list deployment runs"))
			return
		}
		data = append(data, response)
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
	return resolvedAgent{record: agent, snapshot: snapshot}, nil
}

func parseAgentReference(raw json.RawMessage) (string, int, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
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

func agentReference(id string, version int) deploymentAgentReference {
	return deploymentAgentReference{ID: id, Type: "agent", Version: version}
}

func (h *Handler) normalizeVaultIDs(r *http.Request, principal auth.Principal, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
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
	return jsonx.Encode(ids)
}

func normalizeInitialEvents(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, errors.New("initial_events is required")
	}
	var requests []deploymentInitialEventRequest
	if err := json.Unmarshal(raw, &requests); err != nil {
		return nil, errors.New("initial_events must be an array")
	}
	if len(requests) == 0 || len(requests) > 50 {
		return nil, errors.New("initial_events must contain between 1 and 50 events")
	}
	events := make([]deploymentInitialEvent, 0, len(requests))
	systemMessages := 0
	for index, request := range requests {
		event := deploymentInitialEvent{
			Type: request.Type, Description: request.Description, MaxIterations: request.MaxIterations,
		}
		switch request.Type {
		case "user.message":
			content, err := normalizeMessageContent(request.Content, false)
			if err != nil {
				return nil, err
			}
			event.Content = content
		case "system.message":
			systemMessages++
			if systemMessages > 1 {
				return nil, errors.New("initial_events may contain at most one system.message")
			}
			if index != len(requests)-1 {
				return nil, errors.New("system.message must be the final initial event")
			}
			if index == 0 || requests[index-1].Type != "user.message" {
				return nil, errors.New("system.message must immediately follow user.message")
			}
			content, err := normalizeMessageContent(request.Content, true)
			if err != nil {
				return nil, err
			}
			event.Content = content
		case "user.define_outcome":
			if strings.TrimSpace(request.Description) == "" {
				return nil, errors.New("description must be non-empty")
			}
			rubric, err := normalizeOutcomeRubric(request.Rubric)
			if err != nil {
				return nil, err
			}
			event.Rubric = rubric
			if request.MaxIterations != nil {
				if *request.MaxIterations < 1 {
					return nil, errors.New("max_iterations must be positive")
				}
				if *request.MaxIterations > 20 {
					return nil, errors.New("max_iterations must be at most 20")
				}
			}
		default:
			return nil, errors.New("initial_events type must be user.message, user.define_outcome, or system.message")
		}
		events = append(events, event)
	}
	return jsonx.Encode(events)
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
			return nil, nil, markRunPreparationRetryable(err)
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
				return nil, nil, markRunPreparationRetryable(err)
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
		payloadRaw, err := jsonx.Encode(payload)
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
	outcomesRaw, err := jsonx.Encode(outcomes)
	if err != nil {
		return nil, nil, err
	}
	return events, outcomesRaw, nil
}

func scheduleResponse(scheduleRaw json.RawMessage, lastRunAt *time.Time, now time.Time, archived bool) (*deploymentScheduleResponse, error) {
	if len(scheduleRaw) == 0 || jsonx.IsNull(scheduleRaw) {
		return nil, nil
	}
	schedule, err := parseDeploymentSchedule(scheduleRaw)
	if err != nil {
		return nil, err
	}
	return &deploymentScheduleResponse{
		Type: schedule.config.Type, Expression: schedule.config.Expression, Timezone: schedule.config.Timezone,
		LastRunAt: httpapi.OptionalTime(lastRunAt), UpcomingRunsAt: upcomingRuns(schedule.cron, now, archived),
	}, nil
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
	initialEvents := []deploymentInitialEvent{}
	if len(deployment.InitialEvents) > 0 && !jsonx.IsNull(deployment.InitialEvents) {
		if err := json.Unmarshal(deployment.InitialEvents, &initialEvents); err != nil {
			return deploymentResponse{}, errors.New("stored deployment initial_events are invalid")
		}
	}
	metadata := map[string]string{}
	if len(deployment.Metadata) > 0 && !jsonx.IsNull(deployment.Metadata) {
		if err := json.Unmarshal(deployment.Metadata, &metadata); err != nil {
			return deploymentResponse{}, errors.New("stored deployment metadata is invalid")
		}
	}
	var pausedReason *deploymentPausedReason
	if len(deployment.PausedReason) > 0 && !jsonx.IsNull(deployment.PausedReason) {
		pausedReason = &deploymentPausedReason{}
		if err := json.Unmarshal(deployment.PausedReason, pausedReason); err != nil {
			return deploymentResponse{}, errors.New("stored deployment paused_reason is invalid")
		}
	}
	vaultIDs := []string{}
	if len(deployment.VaultIDs) > 0 && !jsonx.IsNull(deployment.VaultIDs) {
		if err := json.Unmarshal(deployment.VaultIDs, &vaultIDs); err != nil {
			return deploymentResponse{}, errors.New("stored deployment vault_ids are invalid")
		}
	}
	resources, err := deploymentResourcesResponse(deployment.Resources)
	if err != nil {
		return deploymentResponse{}, err
	}
	schedule, err := scheduleResponse(deployment.Schedule, deployment.LastRunAt, now, deployment.ArchivedAt != nil)
	if err != nil {
		return deploymentResponse{}, err
	}
	return deploymentResponse{
		ID:            deployment.ExternalID,
		Agent:         agentReference(deployment.AgentExternalID, deployment.AgentVersion),
		ArchivedAt:    httpapi.OptionalTime(deployment.ArchivedAt),
		CreatedAt:     httpapi.FormatTime(deployment.CreatedAt),
		Description:   description,
		EnvironmentID: deployment.EnvironmentExternalID,
		InitialEvents: initialEvents,
		Metadata:      metadata,
		Name:          deployment.Name,
		PausedReason:  pausedReason,
		Resources:     resources,
		Schedule:      schedule,
		Status:        deployment.Status,
		Type:          "deployment",
		UpdatedAt:     httpapi.FormatTime(deployment.UpdatedAt),
		VaultIDs:      vaultIDs,
	}, nil
}

func responseFromRun(run db.DeploymentRun) (deploymentRunResponse, error) {
	var runError *deploymentRunError
	if len(run.Error) > 0 && !jsonx.IsNull(run.Error) {
		runError = &deploymentRunError{}
		if err := json.Unmarshal(run.Error, runError); err != nil {
			return deploymentRunResponse{}, errors.New("stored deployment run error is invalid")
		}
	}
	triggerContext := deploymentRunTriggerContext{Type: run.TriggerType}
	if run.ScheduledAt != nil {
		triggerContext.ScheduledAt = httpapi.FormatTime(*run.ScheduledAt)
	}
	return deploymentRunResponse{
		ID:             run.ExternalID,
		Agent:          agentReference(run.AgentExternalID, run.AgentVersion),
		CreatedAt:      httpapi.FormatTime(run.CreatedAt),
		DeploymentID:   run.DeploymentExternalID,
		Error:          runError,
		SessionID:      run.SessionExternalID,
		TriggerContext: triggerContext,
		Type:           "deployment_run",
	}, nil
}

func writeRunResponse(w http.ResponseWriter, r *http.Request, logger *slog.Logger, run db.DeploymentRun) {
	response, err := responseFromRun(run)
	if err != nil {
		logger.ErrorContext(r.Context(), "map deployment run response", "error", err, "deployment_run_id", run.ExternalID)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not return deployment run"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func runErrorForReference(resourceType string, err error, archived bool) *deploymentRunError {
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
		case "skill":
			return runError("skill_not_found_error", "Referenced skill is unavailable")
		}
	}
	return runError("unknown_error", "Could not create session")
}

func classifyReferenceFailure(resourceType string, err error, archived bool) (*deploymentRunError, error) {
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	return runErrorForReference(resourceType, err, archived), nil
}

func runError(errorType, message string) *deploymentRunError {
	return &deploymentRunError{Type: errorType, Message: message}
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
		err = markRunPreparationRetryable(err)
		return
	}
	if threadID, err = ids.New("sthr_"); err != nil {
		err = markRunPreparationRetryable(err)
		return
	}
	if workID, err = ids.New("work_"); err != nil {
		err = markRunPreparationRetryable(err)
		return
	}
	if runID, err = ids.New("drun_"); err != nil {
		err = markRunPreparationRetryable(err)
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
	if len(raw) == 0 || jsonx.IsNull(raw) {
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
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s must be a string or null", name)
	}
	return &value, nil
}

func optionalStringWithDefault(raw json.RawMessage, fallback, name string) (string, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
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
	if jsonx.IsNull(raw) {
		return nil, errors.New("metadata must be an object with string or null values")
	}
	metadata := map[string]string{}
	if len(current) > 0 && !jsonx.IsNull(current) {
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
	return jsonx.Encode(metadata)
}

func rejectNullField(raw json.RawMessage, name string) error {
	if len(raw) > 0 && jsonx.IsNull(raw) {
		return fmt.Errorf("%s must not be null", name)
	}
	return nil
}

func normalizeMessageContent(raw json.RawMessage, textOnly bool) ([]deploymentContentBlock, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, errors.New("initial_events content is required")
	}
	var requests []deploymentContentBlockRequest
	if err := json.Unmarshal(raw, &requests); err != nil {
		return nil, errors.New("initial_events content must be an array")
	}
	if len(requests) == 0 {
		return nil, errors.New("initial_events content must contain at least one block")
	}
	blocks := make([]deploymentContentBlock, 0, len(requests))
	for _, request := range requests {
		blockType, err := parseRequiredRawString(request.Type, "content.type")
		if err != nil {
			return nil, err
		}
		if textOnly && blockType != "text" {
			return nil, errors.New("system.message content must contain only text blocks")
		}
		block := deploymentContentBlock{Type: blockType}
		switch blockType {
		case "text":
			block.Text, err = parseRequiredRawString(request.Text, "content.text")
			if err != nil {
				return nil, err
			}
		case "image":
			block.Source, err = normalizeContentSource(request.Source, false)
			if err != nil {
				return nil, err
			}
		case "document":
			block.Source, err = normalizeContentSource(request.Source, true)
			if err != nil {
				return nil, err
			}
			for _, field := range []struct {
				name  string
				raw   json.RawMessage
				value *string
			}{
				{name: "context", raw: request.Context, value: &block.Context},
				{name: "title", raw: request.Title, value: &block.Title},
			} {
				if len(field.raw) > 0 && !jsonx.IsNull(field.raw) {
					*field.value, err = parseRequiredRawString(field.raw, field.name)
					if err != nil {
						return nil, err
					}
				}
			}
		default:
			return nil, errors.New("user.message content type must be text, image, or document")
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func normalizeContentSource(raw json.RawMessage, document bool) (*deploymentContentSource, error) {
	var request deploymentContentSourceRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, errors.New("content source must be an object")
	}
	sourceType, err := parseRequiredRawString(request.Type, "source.type")
	if err != nil {
		return nil, err
	}
	source := &deploymentContentSource{Type: sourceType}
	switch sourceType {
	case "base64":
		source.Data, err = parseRequiredRawString(request.Data, "source.data")
		if err != nil {
			return nil, err
		}
		source.MediaType, err = parseRequiredRawString(request.MediaType, "source.media_type")
	case "url":
		source.URL, err = parseRequiredRawString(request.URL, "source.url")
	case "file":
		source.FileID, err = parseRequiredRawString(request.FileID, "source.file_id")
	case "text":
		if !document {
			return nil, errors.New("image source type must be base64, url, or file")
		}
		source.Data, err = parseRequiredRawString(request.Data, "source.data")
		if err != nil {
			return nil, err
		}
		source.MediaType, err = parseRequiredRawString(request.MediaType, "source.media_type")
		if err != nil {
			return nil, err
		}
		if source.MediaType != "text/plain" {
			return nil, errors.New("text document media_type must be text/plain")
		}
	default:
		if document {
			return nil, errors.New("document source type must be base64, text, url, or file")
		}
		return nil, errors.New("image source type must be base64, url, or file")
	}
	if err != nil {
		return nil, err
	}
	return source, nil
}

func normalizeOutcomeRubric(raw json.RawMessage) (*deploymentOutcomeRubric, error) {
	var request deploymentOutcomeRubricRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, errors.New("user.define_outcome rubric must be an object")
	}
	rubricType, err := parseRequiredRawString(request.Type, "rubric.type")
	if err != nil {
		return nil, err
	}
	rubric := &deploymentOutcomeRubric{Type: rubricType}
	switch rubricType {
	case "file":
		rubric.FileID, err = parseRequiredRawString(request.FileID, "rubric.file_id")
	case "text":
		rubric.Content, err = parseRequiredRawString(request.Content, "rubric.content")
		if err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(rubric.Content) > 262144 {
			return nil, errors.New("user.define_outcome text rubric must be at most 262144 characters")
		}
	default:
		return nil, errors.New("user.define_outcome rubric type must be file or text")
	}
	if err != nil {
		return nil, err
	}
	return rubric, nil
}

func normalizeCheckout(raw json.RawMessage) (*deploymentCheckout, error) {
	var request deploymentCheckoutRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, errors.New("checkout must be an object")
	}
	checkoutType, err := parseRequiredRawString(request.Type, "type")
	if err != nil {
		return nil, err
	}
	checkout := &deploymentCheckout{Type: checkoutType}
	switch checkoutType {
	case "branch":
		checkout.Name, err = parseRequiredRawString(request.Name, "name")
	case "commit":
		checkout.SHA, err = parseRequiredRawString(request.SHA, "sha")
	default:
		err = errors.New("checkout.type must be branch or commit")
	}
	if err != nil {
		return nil, err
	}
	return checkout, nil
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
	data, _ := json.Marshal(deploymentCursorPayload{
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), UUID: recordUUID,
	})
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
	payload, err := jsonx.Decode[deploymentCursorPayload](json.RawMessage(data))
	if err != nil || payload.CreatedAt == "" {
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
