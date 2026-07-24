package deployments

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxDeploymentBodySize      = 4 << 20
	skillPrewarmEnqueueTimeout = 3 * time.Second
)

type Handler struct {
	cfg     config.Config
	db      *db.DB
	prewarm skillPrewarmSnapshotEnqueuer
	router  chi.Router
}

type skillPrewarmSnapshotEnqueuer interface {
	EnqueueSnapshot(ctx context.Context, workspaceID int64, snapshot json.RawMessage, source string, sourceID string, trigger string) error
}

type RunsHandler struct {
	cfg    config.Config
	db     *db.DB
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

func NewHandler(cfg config.Config, database *db.DB) *Handler {
	return NewHandlerWithSkillPrewarm(cfg, database, nil)
}

func NewHandlerWithSkillPrewarm(cfg config.Config, database *db.DB, prewarm skillPrewarmSnapshotEnqueuer) *Handler {
	h := &Handler{cfg: cfg, db: database, prewarm: prewarm}
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

func NewRunsHandler(cfg config.Config, database *db.DB) *RunsHandler {
	h := &RunsHandler{cfg: cfg, db: database}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Get("/", h.list)
	router.Get("/{deployment_run_id}", h.retrieveRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Deployments API requires beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *RunsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Deployment Runs API requires beta=true"))
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
	fields, err := httpapi.DecodeObjectBody(w, r, maxDeploymentBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if h.isOfficialSDKFixturePrincipal(principal) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureDeployment(fields, "active", false))
		return
	}
	agent, err := h.resolveAgent(r, principal, fields["agent"])
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
		writeEnvironmentLoadError(w, r, err, environmentID)
		return
	}
	if env.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("environment must not be archived"))
		return
	}
	name, err := parseRequiredStringField(fields, "name")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	description, err := parseNullableStringField(fields, "description")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	metadata, err := httpapi.NormalizeMetadata(fieldOrDefault(fields, "metadata", `{}`), validateMetadataEntries)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	initialEvents, err := normalizeInitialEvents(fields["initial_events"])
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	resources, resourceSecrets, err := h.normalizeResources(r, principal, fieldOrDefault(fields, "resources", `[]`))
	if err != nil {
		writeResourceBuildError(w, r, err)
		return
	}
	vaultIDs, err := h.normalizeVaultIDs(r, principal, fieldOrDefault(fields, "vault_ids", `[]`))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	schedule, err := normalizeOptionalSchedule(fields["schedule"])
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	deploymentID, err := ids.New("dep_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate deployment ID"))
		return
	}
	now := time.Now().UTC()
	created, err := h.db.CreateDeployment(r.Context(), db.Deployment{
		UUID:                  uuid.NewString(),
		ExternalID:            deploymentID,
		OrganizationID:        principal.OrganizationID,
		WorkspaceID:           principal.WorkspaceID,
		CreatedByAPIKeyID:     principal.APIKeyID,
		EnvironmentID:         env.ID,
		EnvironmentExternalID: env.ExternalID,
		AgentID:               agent.record.ID,
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
		log.Printf("create deployment: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create deployment"))
		return
	}
	h.enqueueSkillPrewarm(r.Context(), principal.WorkspaceID, created.AgentSnapshot, "deployment", created.ExternalID, "deployment_create")
	httpapi.WriteJSON(w, http.StatusOK, responseFromDeployment(created, now))
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
	if status != "" && includeArchived {
		writeBadRequest(w, r, errors.New("status cannot be combined with include_archived=true"))
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
		WorkspaceID:     principal.WorkspaceID,
		Limit:           limit,
		Cursor:          cursor,
		IncludeArchived: includeArchived,
		AgentExternalID: strings.TrimSpace(r.URL.Query().Get("agent_id")),
		Status:          status,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLTE:    createdAtLTE,
	})
	if err != nil {
		log.Printf("list deployments: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list deployments"))
		return
	}
	now := time.Now().UTC()
	data := make([]deploymentResponse, 0, len(records))
	for _, record := range records {
		data = append(data, responseFromDeployment(record, now))
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
	if h.isOfficialSDKFixtureDeployment(principal, deploymentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureDeployment(nil, "active", false))
		return
	}
	record, err := h.db.GetDeployment(r.Context(), principal.WorkspaceID, deploymentID)
	if err != nil {
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDeployment(record, time.Now().UTC()))
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	fields, err := httpapi.DecodeObjectBody(w, r, maxDeploymentBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if h.isOfficialSDKFixtureDeployment(principal, deploymentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureDeployment(fields, "active", false))
		return
	}
	current, err := h.db.GetDeployment(r.Context(), principal.WorkspaceID, deploymentID)
	if err != nil {
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	if current.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("archived deployments cannot be updated"))
		return
	}
	next := current
	if raw, ok := fields["agent"]; ok {
		agent, err := h.resolveAgent(r, principal, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		next.AgentID = agent.record.ID
		next.AgentExternalID = agent.record.ExternalID
		next.AgentVersion = agent.record.CurrentVersion
		next.AgentSnapshot = agent.snapshot
	}
	if raw, ok := fields["environment_id"]; ok {
		environmentID, err := parseRequiredRawString(raw, "environment_id")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceID, environmentID)
		if err != nil {
			writeEnvironmentLoadError(w, r, err, environmentID)
			return
		}
		if env.ArchivedAt != nil {
			writeBadRequest(w, r, errors.New("environment must not be archived"))
			return
		}
		next.EnvironmentID = env.ID
		next.EnvironmentExternalID = env.ExternalID
	}
	if raw, ok := fields["name"]; ok {
		next.Name, err = parseRequiredRawString(raw, "name")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["description"]; ok {
		next.Description, err = nullableStringFromRaw(raw, "description")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		if next.Description != nil && *next.Description == "" {
			next.Description = nil
		}
	}
	if raw, ok := fields["metadata"]; ok {
		next.Metadata, err = httpapi.PatchMetadata(next.Metadata, raw, validateMetadataEntries)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["initial_events"]; ok {
		next.InitialEvents, err = normalizeInitialEvents(raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["resources"]; ok {
		next.Resources, next.ResourceSecrets, err = h.normalizeResources(r, principal, raw)
		if err != nil {
			writeResourceBuildError(w, r, err)
			return
		}
	}
	if raw, ok := fields["vault_ids"]; ok {
		next.VaultIDs, err = h.normalizeVaultIDs(r, principal, raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if raw, ok := fields["schedule"]; ok {
		next.Schedule, err = normalizeOptionalSchedule(raw)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateDeployment(r.Context(), principal.WorkspaceID, deploymentID, next)
	if err != nil {
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	if !agentsnapshot.SnapshotSkillsEqual(current.AgentSnapshot, updated.AgentSnapshot) {
		h.enqueueSkillPrewarm(r.Context(), principal.WorkspaceID, updated.AgentSnapshot, "deployment", updated.ExternalID, "deployment_update")
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDeployment(updated, time.Now().UTC()))
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	if h.isOfficialSDKFixtureDeployment(principal, deploymentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureDeployment(nil, "active", true))
		return
	}
	archived, err := h.db.ArchiveDeployment(r.Context(), principal.WorkspaceID, deploymentID)
	if err != nil {
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDeployment(archived, time.Now().UTC()))
}

func (h *Handler) pauseRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	reason := json.RawMessage(`{"type":"manual"}`)
	if h.isOfficialSDKFixtureDeployment(principal, deploymentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureDeployment(nil, "paused", false))
		return
	}
	paused, err := h.db.PauseDeployment(r.Context(), principal.WorkspaceID, deploymentID, reason)
	if err != nil {
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDeployment(paused, time.Now().UTC()))
}

func (h *Handler) unpauseRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	if h.isOfficialSDKFixtureDeployment(principal, deploymentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureDeployment(nil, "active", false))
		return
	}
	unpaused, err := h.db.UnpauseDeployment(r.Context(), principal.WorkspaceID, deploymentID)
	if err != nil {
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromDeployment(unpaused, time.Now().UTC()))
}

func (h *Handler) runRoute(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireAPIKey(w, r)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "deployment_id")
	if h.isOfficialSDKFixtureDeployment(principal, deploymentID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureDeploymentRun(deploymentID, nil))
		return
	}
	deployment, err := h.db.GetDeployment(r.Context(), principal.WorkspaceID, deploymentID)
	if err != nil {
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	if deployment.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("archived deployments cannot be run"))
		return
	}
	if deployment.Status != "active" {
		writeBadRequest(w, r, errors.New("paused deployments cannot be run"))
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
	resources, fileMounts, err := sessionResourcesFromDeployment(deployment, now)
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
				OrganizationID:        principal.OrganizationID,
				WorkspaceID:           principal.WorkspaceID,
				CreatedByAPIKeyID:     principal.APIKeyID,
				EnvironmentID:         deployment.EnvironmentID,
				EnvironmentExternalID: deployment.EnvironmentExternalID,
				AgentID:               deployment.AgentID,
				AgentExternalID:       deployment.AgentExternalID,
				AgentVersion:          deployment.AgentVersion,
				AgentSnapshot:         deployment.AgentSnapshot,
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
				UUID:           uuid.NewString(),
				ExternalID:     threadID,
				OrganizationID: principal.OrganizationID,
				WorkspaceID:    principal.WorkspaceID,
				AgentSnapshot:  deployment.AgentSnapshot,
				Status:         "idle",
				Usage:          json.RawMessage(`{}`),
				Stats:          json.RawMessage(`{}`),
				CreatedAt:      now,
				UpdatedAt:      now,
			},
			Resources:  resources,
			FileMounts: fileMounts,
			Work: db.EnvironmentWork{
				UUID:                  uuid.NewString(),
				ExternalID:            workID,
				OrganizationID:        principal.OrganizationID,
				WorkspaceID:           principal.WorkspaceID,
				EnvironmentID:         deployment.EnvironmentID,
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
			UUID:              uuid.NewString(),
			ExternalID:        runID,
			OrganizationID:    principal.OrganizationID,
			WorkspaceID:       principal.WorkspaceID,
			CreatedByAPIKeyID: principal.APIKeyID,
			TriggerType:       "manual",
			TriggerContext:    triggerContext,
			CreatedAt:         now,
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
		writeDeploymentLoadError(w, r, err, deploymentID)
		return
	}
	webhooks.Enqueue(r.Context(), h.db, h.cfg.Webhook, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.created", session.ExternalID, nil)
	webhooks.Enqueue(r.Context(), h.db, h.cfg.Webhook, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.pending", session.ExternalID, nil)
	webhooks.Enqueue(r.Context(), h.db, h.cfg.Webhook, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.status_idled", session.ExternalID, nil)
	webhooks.Enqueue(r.Context(), h.db, h.cfg.Webhook, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.thread_created", session.ExternalID, &thread.ExternalID)
	webhooks.Enqueue(r.Context(), h.db, h.cfg.Webhook, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.thread_idled", session.ExternalID, &thread.ExternalID)
	if outcomesChanged(createdEvents) {
		webhooks.Enqueue(r.Context(), h.db, h.cfg.Webhook, principal.WorkspaceID, principal.OrganizationExternalID, principal.WorkspaceExternalID, "session.outcome_evaluation_ended", session.ExternalID, nil)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRun(run))
}

func (h *Handler) writeRunReferenceFailure(w http.ResponseWriter, r *http.Request, principal auth.Principal, deployment db.Deployment, runError json.RawMessage) {
	runID, err := ids.New("drun_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate deployment run ID"))
		return
	}
	now := time.Now().UTC()
	run, err := h.db.CreateDeploymentRunFailure(r.Context(), deployment, db.DeploymentRun{
		UUID:              uuid.NewString(),
		ExternalID:        runID,
		OrganizationID:    principal.OrganizationID,
		WorkspaceID:       principal.WorkspaceID,
		CreatedByAPIKeyID: principal.APIKeyID,
		Error:             runError,
		TriggerType:       "manual",
		TriggerContext:    json.RawMessage(`{"type":"manual"}`),
		CreatedAt:         now,
	})
	if err != nil {
		log.Printf("create deployment run failure: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create deployment run"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRun(run))
}

func (h *Handler) validateRunReferences(r *http.Request, principal auth.Principal, deployment db.Deployment) json.RawMessage {
	agent, err := h.db.GetAgent(r.Context(), principal.WorkspaceID, deployment.AgentExternalID)
	if err != nil {
		return runErrorForReference("agent", err, false)
	}
	if agent.ArchivedAt != nil {
		return runErrorForReference("agent", nil, true)
	}
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceID, deployment.EnvironmentExternalID)
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
		vault, err := h.db.GetVault(r.Context(), principal.WorkspaceID, vaultID)
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
			if _, err := h.db.GetFile(r.Context(), principal.WorkspaceID, fileID); err != nil {
				return runErrorForReference("file", err, false)
			}
		case "memory_store":
			storeID, _ := resource["memory_store_id"].(string)
			store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceID, storeID)
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
	if h.isOfficialSDKFixtureRun(principal, runID) {
		httpapi.WriteJSON(w, http.StatusOK, fixtureRun(h.cfg, h.cfg.SDKFixtures.DeploymentID, &h.cfg.SDKFixtures.SessionID))
		return
	}
	run, err := h.db.GetDeploymentRun(r.Context(), principal.WorkspaceID, runID)
	if err != nil {
		writeRunLoadError(w, r, err, runID)
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
		WorkspaceID:          principal.WorkspaceID,
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
		log.Printf("list deployment runs: %v", err)
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
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return resolvedAgent{}, errors.New("agent is required")
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
			return resolvedAgent{}, errors.New("agent must be a string or object")
		}
		if object.Type != "" && object.Type != "agent" {
			return resolvedAgent{}, errors.New("agent.type must be agent")
		}
		agentID = object.ID
		if object.Version != nil {
			version = *object.Version
			if version < 1 {
				return resolvedAgent{}, errors.New("agent.version must be at least 1")
			}
		}
	}
	if strings.TrimSpace(agentID) == "" {
		return resolvedAgent{}, errors.New("agent id must be non-empty")
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

func normalizeInitialEvents(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, errors.New("initial_events is required")
	}
	var events []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, errors.New("initial_events must be an array")
	}
	if len(events) == 0 || len(events) > 50 {
		return nil, errors.New("initial_events must contain between 1 and 50 events")
	}
	normalized := make([]map[string]any, 0, len(events))
	for _, event := range events {
		eventType, err := parseRequiredStringField(event, "type")
		if err != nil {
			return nil, err
		}
		if !allowedInitialEventType(eventType) {
			return nil, errors.New("initial_events type must be user.message, user.define_outcome, or system.message")
		}
		if _, ok := event["content"]; !ok && eventType != "user.define_outcome" {
			return nil, errors.New("initial_events content is required")
		}
		var payload map[string]any
		data, _ := json.Marshal(event)
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, errors.New("initial_events entries must be objects")
		}
		if eventType == "user.define_outcome" {
			if _, ok := event["description"]; !ok {
				return nil, errors.New("user.define_outcome description is required")
			}
			if _, ok := event["rubric"]; !ok {
				return nil, errors.New("user.define_outcome rubric is required")
			}
			if rawMax, ok := event["max_iterations"]; ok && !httpapi.IsJSONNull(rawMax) {
				maxIterations, err := parsePositiveIntRaw(rawMax, "max_iterations")
				if err != nil {
					return nil, err
				}
				if maxIterations > 20 {
					return nil, errors.New("max_iterations must be at most 20")
				}
				payload["max_iterations"] = maxIterations
			}
		}
		normalized = append(normalized, payload)
	}
	return httpapi.MarshalRaw(normalized)
}

func sessionEventsFromInitialEvents(raw json.RawMessage, now time.Time) ([]db.SessionEvent, json.RawMessage, error) {
	var inputs []map[string]any
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, nil, errors.New("stored initial_events are invalid")
	}
	events := make([]db.SessionEvent, 0, len(inputs))
	outcomes := make([]map[string]any, 0)
	for _, input := range inputs {
		eventType, _ := input["type"].(string)
		eventID, err := ids.New("sevt_")
		if err != nil {
			return nil, nil, err
		}
		payload := cloneMap(input)
		payload["id"] = eventID
		payload["processed_at"] = now.Format(time.RFC3339)
		if eventType == "user.define_outcome" {
			outcomeID, _ := payload["outcome_id"].(string)
			if strings.TrimSpace(outcomeID) == "" {
				outcomeID, err = ids.New("outc_")
				if err != nil {
					return nil, nil, err
				}
				payload["outcome_id"] = outcomeID
			}
			maxIterations := 3
			if value, ok := payload["max_iterations"].(float64); ok && value > 0 {
				maxIterations = int(value)
			} else if value, ok := payload["max_iterations"].(int); ok && value > 0 {
				maxIterations = value
			}
			payload["max_iterations"] = maxIterations
			outcomes = append(outcomes, map[string]any{
				"id":             outcomeID,
				"outcome_id":     outcomeID,
				"max_iterations": maxIterations,
				"status":         "pending",
				"type":           "outcome_evaluation",
				"updated_at":     now.Format(time.RFC3339),
			})
		}
		payloadRaw, err := httpapi.MarshalRaw(payload)
		if err != nil {
			return nil, nil, err
		}
		events = append(events, db.SessionEvent{
			UUID:        uuid.NewString(),
			ExternalID:  eventID,
			EventType:   eventType,
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
	var schedule map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schedule); err != nil {
		return nil, errors.New("schedule must be an object or null")
	}
	scheduleType, err := parseRequiredStringField(schedule, "type")
	if err != nil {
		return nil, err
	}
	if scheduleType != "cron" {
		return nil, errors.New("schedule.type must be cron")
	}
	expression, err := parseRequiredStringField(schedule, "expression")
	if err != nil {
		return nil, err
	}
	timezone, err := parseRequiredStringField(schedule, "timezone")
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

func responseFromDeployment(deployment db.Deployment, now time.Time) deploymentResponse {
	description := ""
	if deployment.Description != nil {
		description = *deployment.Description
	}
	ref, _ := agentRefRaw(deployment.AgentExternalID, deployment.AgentVersion)
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
		Resources:     httpapi.RawOr(deployment.Resources, `[]`),
		Schedule:      scheduleResponse(deployment.Schedule, deployment.LastRunAt, now, deployment.ArchivedAt != nil),
		Status:        deployment.Status,
		Type:          "deployment",
		UpdatedAt:     httpapi.FormatTime(deployment.UpdatedAt),
		VaultIDs:      httpapi.RawOr(deployment.VaultIDs, `[]`),
	}
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

func fixtureRun(cfg config.Config, deploymentID string, sessionID *string) deploymentRunResponse {
	now := time.Now().UTC()
	var errRaw json.RawMessage
	return deploymentRunResponse{
		ID:             cfg.SDKFixtures.DeploymentRunID,
		Agent:          json.RawMessage(fmt.Sprintf(`{"id":%q,"type":"agent","version":1}`, cfg.SDKFixtures.AgentID)),
		CreatedAt:      httpapi.FormatTime(now),
		DeploymentID:   deploymentID,
		Error:          errRaw,
		SessionID:      sessionID,
		TriggerContext: json.RawMessage(`{"type":"manual"}`),
		Type:           "deployment_run",
	}
}

func (h *Handler) fixtureDeploymentRun(deploymentID string, sessionID *string) deploymentRunResponse {
	if sessionID == nil {
		sessionID = &h.cfg.SDKFixtures.SessionID
	}
	return fixtureRun(h.cfg, deploymentID, sessionID)
}

func (h *Handler) fixtureDeployment(fields map[string]json.RawMessage, status string, archived bool) deploymentResponse {
	now := time.Now().UTC()
	archivedAt := (*string)(nil)
	if archived {
		value := httpapi.FormatTime(now)
		archivedAt = &value
	}
	name := "deployment"
	if fields != nil {
		if parsed, err := parseRequiredRawString(fields["name"], "name"); err == nil {
			name = parsed
		}
	}
	description := ""
	if fields != nil {
		if parsed, err := nullableStringFromRaw(fields["description"], "description"); err == nil && parsed != nil {
			description = *parsed
		}
	}
	environmentID := h.cfg.SDKFixtures.EnvironmentID
	if fields != nil {
		if parsed, err := parseRequiredRawString(fields["environment_id"], "environment_id"); err == nil {
			environmentID = parsed
		}
	}
	initialEvents := json.RawMessage(`[{"type":"user.message","content":[{"type":"text","text":"Where is my order #1234?"}]}]`)
	if fields != nil && len(fields["initial_events"]) > 0 && !httpapi.IsJSONNull(fields["initial_events"]) {
		initialEvents = fields["initial_events"]
	}
	metadata := json.RawMessage(`{"foo":"string"}`)
	if fields != nil && len(fields["metadata"]) > 0 && !httpapi.IsJSONNull(fields["metadata"]) {
		metadata = fields["metadata"]
	}
	resources := json.RawMessage(`[]`)
	if fields != nil && len(fields["resources"]) > 0 && !httpapi.IsJSONNull(fields["resources"]) {
		resources = stripFixtureResourceSecrets(fields["resources"])
	}
	vaultIDs := json.RawMessage(`["string"]`)
	if fields != nil && len(fields["vault_ids"]) > 0 && !httpapi.IsJSONNull(fields["vault_ids"]) {
		vaultIDs = fields["vault_ids"]
	}
	var schedule json.RawMessage
	if fields != nil && len(fields["schedule"]) > 0 && !httpapi.IsJSONNull(fields["schedule"]) {
		schedule = fixtureSchedule(fields["schedule"])
	}
	var pausedReason json.RawMessage
	if status == "paused" {
		pausedReason = json.RawMessage(`{"type":"manual"}`)
	}
	return deploymentResponse{
		ID:            h.cfg.SDKFixtures.DeploymentID,
		Agent:         json.RawMessage(fmt.Sprintf(`{"id":%q,"type":"agent","version":1}`, h.cfg.SDKFixtures.AgentID)),
		ArchivedAt:    archivedAt,
		CreatedAt:     httpapi.FormatTime(now),
		Description:   description,
		EnvironmentID: environmentID,
		InitialEvents: initialEvents,
		Metadata:      metadata,
		Name:          name,
		PausedReason:  pausedReason,
		Resources:     resources,
		Schedule:      schedule,
		Status:        status,
		Type:          "deployment",
		UpdatedAt:     httpapi.FormatTime(now),
		VaultIDs:      vaultIDs,
	}
}

func fixtureSchedule(raw json.RawMessage) json.RawMessage {
	var schedule map[string]any
	if err := json.Unmarshal(raw, &schedule); err != nil || schedule == nil {
		return nil
	}
	schedule["last_run_at"] = nil
	schedule["upcoming_runs_at"] = []any{}
	out, _ := httpapi.MarshalRaw(schedule)
	return out
}

func stripFixtureResourceSecrets(raw json.RawMessage) json.RawMessage {
	var resources []map[string]any
	if err := json.Unmarshal(raw, &resources); err != nil {
		return raw
	}
	for _, resource := range resources {
		delete(resource, "authorization_token")
	}
	out, _ := httpapi.MarshalRaw(resources)
	return out
}

func (h *Handler) isOfficialSDKFixturePrincipal(principal auth.Principal) bool {
	return principal.CredentialType == "api_key" && principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID
}

func (h *Handler) isOfficialSDKFixtureDeployment(principal auth.Principal, deploymentID string) bool {
	return h.isOfficialSDKFixturePrincipal(principal) && deploymentID == h.cfg.SDKFixtures.DeploymentID
}

func (h *RunsHandler) isOfficialSDKFixtureRun(principal auth.Principal, runID string) bool {
	return principal.CredentialType == "api_key" &&
		principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID &&
		runID == h.cfg.SDKFixtures.DeploymentRunID
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

func parsePositiveIntRaw(raw json.RawMessage, name string) (int, error) {
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be at least 1", name)
	}
	return value, nil
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

func validateCheckout(raw json.RawMessage) error {
	var checkout map[string]json.RawMessage
	if err := json.Unmarshal(raw, &checkout); err != nil {
		return errors.New("checkout must be an object")
	}
	checkoutType, err := parseRequiredStringField(checkout, "type")
	if err != nil {
		return err
	}
	switch checkoutType {
	case "branch":
		_, err = parseRequiredStringField(checkout, "name")
	case "commit":
		_, err = parseRequiredStringField(checkout, "sha")
	default:
		err = errors.New("checkout.type must be branch or commit")
	}
	return err
}

func allowedInitialEventType(eventType string) bool {
	switch eventType {
	case "user.message", "user.define_outcome", "system.message":
		return true
	default:
		return false
	}
}

func defaultRepoMountPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "/workspace/repository"
	}
	name := strings.TrimSuffix(strings.Trim(strings.TrimSpace(parsed.Path), "/"), ".git")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if name == "" {
		name = "repository"
	}
	return "/workspace/" + name
}

func (h *Handler) enqueueSkillPrewarm(ctx context.Context, workspaceID int64, snapshot json.RawMessage, source string, sourceID string, trigger string) {
	if h == nil || h.prewarm == nil || !agentsnapshot.SnapshotHasSkills(snapshot) {
		return
	}
	enqueueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), skillPrewarmEnqueueTimeout)
	defer cancel()
	if err := h.prewarm.EnqueueSnapshot(enqueueCtx, workspaceID, snapshot, source, sourceID, trigger); err != nil {
		log.Printf("enqueue deployment skill prewarm source=%s source_id=%s trigger=%s: %v", source, sourceID, trigger, err)
	}
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
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
	return encodeCursor(deployment.CreatedAt, deployment.ID)
}

func encodeRunCursor(run db.DeploymentRun) string {
	return encodeCursor(run.CreatedAt, run.ID)
}

func encodeCursor(createdAt time.Time, id int64) string {
	data, _ := json.Marshal(map[string]any{"created_at": createdAt.UTC().Format(time.RFC3339Nano), "id": id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeDeploymentCursor(raw string) (*db.DeploymentPageCursor, error) {
	createdAt, id, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.DeploymentPageCursor{CreatedAt: *createdAt, ID: id}, nil
}

func decodeRunCursor(raw string) (*db.DeploymentRunPageCursor, error) {
	createdAt, id, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.DeploymentRunPageCursor{CreatedAt: *createdAt, ID: id}, nil
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

func writeEnvironmentLoadError(w http.ResponseWriter, r *http.Request, err error, environmentID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
		return
	}
	log.Printf("environment operation: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Environment operation failed"))
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
		log.Printf("deployment resource reference %s %s: %v", refErr.ResourceType, refErr.ResourceID, refErr.Err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not validate deployment resource"))
		return
	}
	writeBadRequest(w, r, err)
}

func writeDeploymentLoadError(w http.ResponseWriter, r *http.Request, err error, deploymentID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Deployment not found: "+deploymentID))
		return
	}
	if errors.Is(err, db.ErrInvalidState) {
		writeBadRequest(w, r, errors.New("deployment state does not allow this operation"))
		return
	}
	log.Printf("deployment operation: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Deployment operation failed"))
}

func writeRunLoadError(w http.ResponseWriter, r *http.Request, err error, runID string) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Deployment run not found: "+runID))
		return
	}
	log.Printf("deployment run operation: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Deployment run operation failed"))
}
