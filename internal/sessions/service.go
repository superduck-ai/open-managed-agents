package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireSessionManager(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionMutationRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	if h.isOfficialSDKFixturePrincipal(principal) && h.createUsesOfficialFixtures(body) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return nil
	}

	agent, snapshot, err := h.resolveAgent(r, principal, body.Agent)
	if err != nil {
		return invalidRequest(err)
	}
	environmentID, err := parseRequiredRawString(body.EnvironmentID, "environment_id")
	if err != nil {
		return invalidRequest(err)
	}
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceUUID, environmentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return environmentNotFound(environmentID, err)
		}
		return internalError("Could not create session", fmt.Errorf("get environment %q for session: %w", environmentID, err))
	}
	if env.ArchivedAt != nil {
		return invalidRequest(errors.New("environment must not be archived"))
	}
	metadata, err := httpapi.NormalizeMetadata(rawOrDefault(body.Metadata, `{}`), validateMetadataEntries)
	if err != nil {
		return invalidRequest(err)
	}
	title, err := nullableStringOrMissing(body.Title, "title")
	if err != nil {
		return invalidRequest(err)
	}
	vaultIDs, err := h.normalizeVaultIDs(r, principal, rawOrDefault(body.VaultIDs, `[]`))
	if err != nil {
		return invalidRequest(err)
	}
	budget, err := normalizeBudget(body.Budget)
	if err != nil {
		return invalidRequest(err)
	}

	sessionID, err := ids.New("sesn_")
	if err != nil {
		return internalError("Could not generate session ID", fmt.Errorf("generate session ID: %w", err))
	}
	threadID, err := ids.New("sthr_")
	if err != nil {
		return internalError("Could not generate thread ID", fmt.Errorf("generate thread ID: %w", err))
	}
	workID, err := ids.New("work_")
	if err != nil {
		return internalError("Could not generate work ID", fmt.Errorf("generate work ID: %w", err))
	}
	now := time.Now().UTC()
	resources, err := h.resourcesFromCreate(r, principal, sessionID, body.Resources, now)
	if err != nil {
		return mapResourceBuildError(err)
	}
	resourceInputs, err := sessionResourceWriteInputs(resources)
	if err != nil {
		return mapResourceBuildError(err)
	}
	created, thread, _, _, err := h.db.CreateSession(r.Context(), db.CreateSessionInput{
		Session: db.Session{
			UUID:                  uuid.NewV4().String(),
			ExternalID:            sessionID,
			OrganizationUUID:      principal.OrganizationUUID,
			WorkspaceUUID:         principal.WorkspaceUUID,
			CreatedByAPIKeyUUID:   principal.APIKeyUUID,
			EnvironmentUUID:       env.UUID,
			EnvironmentExternalID: env.ExternalID,
			AgentUUID:             agent.UUID,
			AgentExternalID:       agent.ExternalID,
			AgentVersion:          agent.CurrentVersion,
			AgentSnapshot:         snapshot,
			Title:                 title,
			Metadata:              metadata,
			VaultIDs:              vaultIDs,
			Budget:                budget,
			Status:                "idle",
			Usage:                 json.RawMessage(`{}`),
			Stats:                 json.RawMessage(`{}`),
			OutcomeEvaluations:    json.RawMessage(`[]`),
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		Thread: db.SessionThread{
			UUID:             uuid.NewV4().String(),
			ExternalID:       threadID,
			OrganizationUUID: principal.OrganizationUUID,
			WorkspaceUUID:    principal.WorkspaceUUID,
			AgentSnapshot:    snapshot,
			Status:           "idle",
			Usage:            json.RawMessage(`{}`),
			Stats:            json.RawMessage(`{}`),
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		Resources: resourceInputs,
		Work: db.EnvironmentWork{
			UUID:                  uuid.NewV4().String(),
			ExternalID:            workID,
			OrganizationUUID:      principal.OrganizationUUID,
			WorkspaceUUID:         principal.WorkspaceUUID,
			EnvironmentUUID:       env.UUID,
			EnvironmentExternalID: env.ExternalID,
			Metadata:              json.RawMessage(`{}`),
			State:                 "queued",
			CreatedAt:             now,
			UpdatedAt:             now,
		},
	})
	if err != nil {
		if mapped, ok := mapFileResourcePersistenceError(err); ok {
			return mapped
		}
		return internalError("Could not create session", fmt.Errorf("create session %q: %w", sessionID, err))
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.created", created.ExternalID, nil)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.pending", created.ExternalID, nil)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.status_idled", created.ExternalID, nil)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.thread_created", created.ExternalID, &thread.ExternalID)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.thread_idled", created.ExternalID, &thread.ExternalID)
	response, err := h.responseFromSession(r, created)
	if err != nil {
		return internalError("Could not create session", fmt.Errorf("load session %q response: %w", sessionID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireSessionManager(r)
	if err != nil {
		return err
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeSessionCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	order, err := parseOrder(r)
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	agentVersion, err := parseOptionalPositiveInt(r, "agent_version")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGT, err := httpapi.ParseOptionalTime(r, "created_at[gt]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLT, err := httpapi.ParseOptionalTime(r, "created_at[lt]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		return invalidRequest(err)
	}
	statuses, err := parseRepeatedStatuses(r)
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListSessionsPage(r.Context(), db.ListSessionsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
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
		return internalError("Could not list sessions", fmt.Errorf("list sessions: %w", err))
	}
	data := make([]sessionResponse, 0, len(records))
	for _, record := range records {
		response, err := h.responseFromSession(r, record)
		if err != nil {
			return internalError("Could not list sessions", fmt.Errorf("load session %q response: %w", record.ExternalID, err))
		}
		data = append(data, response)
	}
	var nextPage *string
	if hasMore && len(records) > 0 {
		value := encodeSessionCursor(records[len(records)-1])
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[sessionResponse]{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return nil
	}
	session, err := h.authorizeSession(r, sessionID, sessionAccessRead)
	if err != nil {
		return err
	}
	response, err := h.responseFromSession(r, session)
	if err != nil {
		return internalError("Could not retrieve session", fmt.Errorf("load session %q response: %w", sessionID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireSessionManager(r)
	if err != nil {
		return err
	}
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return nil
	}
	current, found, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	if !found {
		return mapSessionLoadError(db.ErrNotFound, sessionID)
	}
	if current.ArchivedAt != nil || current.Status != "idle" {
		return invalidRequest(errors.New("session must be idle and unarchived to update"))
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionMutationRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	if len(body.VaultIDs) > 0 {
		return invalidRequest(errors.New("vault_ids updates are not supported"))
	}
	next := current
	if len(body.Budget) > 0 {
		budget, err := normalizeBudget(body.Budget)
		if err != nil {
			return invalidRequest(err)
		}
		if budget != nil && len(current.Budget) == 0 {
			return invalidRequest(errors.New("budget can only be added at session creation"))
		}
		next.Budget = budget
	}
	if len(body.Title) > 0 {
		next.Title, err = nullableStringFromRaw(body.Title, "title")
		if err != nil {
			return invalidRequest(err)
		}
	}
	if len(body.Metadata) > 0 {
		next.Metadata, err = httpapi.PatchMetadata(next.Metadata, body.Metadata, validateMetadataEntries)
		if err != nil {
			return invalidRequest(err)
		}
	}
	if len(body.Agent) > 0 {
		next.AgentSnapshot, err = patchSessionAgent(next.AgentSnapshot, body.Agent)
		if err != nil {
			return invalidRequest(err)
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateSession(r.Context(), principal.WorkspaceUUID, sessionID, next)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	event, err := h.sessionUpdatedEvent(updated)
	if err == nil {
		h.appendAndBroadcastInternal(r, updated.ExternalID, []db.SessionEvent{event})
	}
	response, err := h.responseFromSession(r, updated)
	if err != nil {
		return internalError("Could not update session", fmt.Errorf("load updated session %q response: %w", sessionID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) archiveRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireSessionManager(r)
	if err != nil {
		return err
	}
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), true))
		return nil
	}
	current, found, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	if !found {
		return mapSessionLoadError(db.ErrNotFound, sessionID)
	}
	if current.Status == "running" || current.Status == "rescheduling" {
		return invalidRequest(errors.New("running sessions cannot be archived"))
	}
	archived, err := h.db.ArchiveSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.archived", archived.ExternalID, nil)
	response, err := h.responseFromSession(r, archived)
	if err != nil {
		return internalError("Could not archive session", fmt.Errorf("load archived session %q response: %w", sessionID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireSessionManager(r)
	if err != nil {
		return err
	}
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: sessionID, Type: "session_deleted"})
		return nil
	}
	current, found, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	if !found {
		return mapSessionLoadError(db.ErrNotFound, sessionID)
	}
	if current.Status == "running" || current.Status == "rescheduling" {
		return invalidRequest(errors.New("running sessions cannot be deleted"))
	}
	deletedEvent, err := h.simpleSessionEvent("session.deleted", sessionID, nil)
	if err == nil {
		if current.ArchivedAt == nil {
			h.appendAndBroadcastInternal(r, sessionID, []db.SessionEvent{deletedEvent})
		} else {
			deletedEvent.SessionExternalID = sessionID
			h.publishSessionEvents(r.Context(), []db.SessionEvent{deletedEvent})
		}
	}
	deleted, err := h.db.DeleteSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.deleted", deleted.ExternalID, nil)
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: sessionID, Type: "session_deleted"})
	return nil
}

func (h *Handler) listEventsRoute(w http.ResponseWriter, r *http.Request) error {
	return h.listEvents(w, r, chi.URLParam(r, "session_id"), "")
}

func (h *Handler) listThreadEventsRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	session, err := h.authorizeSession(r, sessionID, sessionAccessEventsRead)
	if err != nil {
		return err
	}
	if _, err := h.db.GetSessionThread(r.Context(), workspaceUUIDFromRequest(r), sessionID, threadID); err != nil {
		return mapThreadLoadError(err, threadID)
	}
	if err := h.backfillSubagentThreadEventsIfEmpty(r.Context(), session, threadID); err != nil {
		h.logger.ErrorContext(r.Context(), "backfill subagent thread events", "session_id", sessionID, "thread_id", threadID, "error", err)
	}
	return h.listEvents(w, r, sessionID, threadID)
}

func (h *Handler) backfillSubagentThreadEventsIfEmpty(ctx context.Context, session db.Session, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if h == nil || h.codeSessions == nil || threadID == "" {
		return nil
	}
	existing, _, err := h.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceUUID:     session.WorkspaceUUID,
		SessionExternalID: session.ExternalID,
		ThreadExternalID:  threadID,
		Limit:             1,
		Order:             "asc",
	})
	if err != nil || len(existing) > 0 {
		return err
	}
	codeSession, err := h.db.GetCodeSessionBySessionExternalID(ctx, session.WorkspaceUUID, session.ExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return h.codeSessions.PublishSubagentInternalEvents(ctx, codeSession)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request, sessionID, threadID string) error {
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: []json.RawMessage{}})
		return nil
	}
	if _, err := h.authorizeSession(r, sessionID, sessionAccessEventsRead); err != nil {
		return err
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeEventCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	order, err := parseOrder(r)
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGT, err := httpapi.ParseOptionalTime(r, "created_at[gt]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLT, err := httpapi.ParseOptionalTime(r, "created_at[lt]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListSessionEventsPage(r.Context(), db.ListSessionEventsPageParams{
		WorkspaceUUID:     workspaceUUIDFromRequest(r),
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
		return internalError("Could not list events", fmt.Errorf("list session %q events: %w", sessionID, err))
	}
	hiddenPrimaryToolUseIDs, err := h.primaryOrphanToolUseIDsWithChildCopies(r.Context(), sessionID, threadID, records)
	if err != nil {
		return internalError("Could not list events", fmt.Errorf("list session %q child tool projections: %w", sessionID, err))
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
	return nil
}

func (h *Handler) sendEventsRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	body, err := httpapi.DecodeObjectBodyAs[sessionEventsRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	if len(body.Events) == 0 {
		return invalidRequest(errors.New("events is required"))
	}
	var inputs []json.RawMessage
	if err := json.Unmarshal(body.Events, &inputs); err != nil || len(inputs) == 0 {
		return invalidRequest(errors.New("events must be a non-empty array"))
	}
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		now := time.Now().UTC()
		data := make([]json.RawMessage, 0, len(inputs))
		for _, raw := range inputs {
			payload, err := normalizeFixtureEvent(raw, now)
			if err != nil {
				return invalidRequest(err)
			}
			data = append(data, payload)
		}
		httpapi.WriteJSON(w, http.StatusOK, sendEventsResponse{Data: data})
		return nil
	}
	session, err := h.authorizeSession(r, sessionID, sessionAccessEventsSend)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	events := make([]db.SessionEvent, 0, len(inputs))
	var outcomesChanged bool
	normalizedSession := session
	for _, raw := range inputs {
		event, outcomes, changed, err := normalizeInputEvent(normalizedSession, raw, now)
		if err != nil {
			return invalidRequest(err)
		}
		if changed {
			normalizedSession.OutcomeEvaluations = outcomes
			outcomesChanged = true
		}
		events = append(events, event)
	}
	var outcomeEvaluations json.RawMessage
	if outcomesChanged {
		outcomeEvaluations = normalizedSession.OutcomeEvaluations
	}
	created, err := h.db.AppendSessionEvents(r.Context(), session.WorkspaceUUID, session.ExternalID, events, outcomeEvaluations)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return invalidRequest(errors.New("archived sessions do not accept new events"))
		}
		return mapSessionLoadError(err, sessionID)
	}
	h.publishSessionEvents(r.Context(), created)
	if h.codeSessions != nil {
		if err := h.codeSessions.QueuePublicSessionEvents(r.Context(), session, created); err != nil {
			h.logger.ErrorContext(r.Context(), "queue session events for code session", "session_id", session.ExternalID, "error", err)
		}
	}
	if outcomesChanged {
		h.enqueueWebhook(r.Context(), webhooks.EnqueueInput{
			WorkspaceUUID:       session.WorkspaceUUID,
			OrganizationUUID:    organizationUUIDFromRequest(r),
			WorkspaceExternalID: workspaceExternalIDFromRequest(r),
			EventType:           "session.outcome_evaluation_ended",
			ResourceID:          session.ExternalID,
		})
	}
	data := make([]json.RawMessage, 0, len(created))
	for _, event := range created {
		data = append(data, sessionEventPayload(event))
	}
	httpapi.WriteJSON(w, http.StatusOK, sendEventsResponse{Data: data})
	return nil
}

func (h *Handler) addResourceRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureResource(time.Now().UTC()))
		return nil
	}
	session, err := h.authorizeSession(r, sessionID, sessionAccessManageResources)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionResourceRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	resource, err := h.resourceFromRequest(r, session, body, time.Now().UTC())
	if err != nil {
		return mapResourceBuildError(err)
	}
	resourceInput, err := sessionResourceWriteInput(resource)
	if err != nil {
		return mapResourceBuildError(err)
	}
	created, err := h.db.CreateSessionResource(
		r.Context(),
		resourceInput,
	)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromResource(created))
	return nil
}

func (h *Handler) listResourcesRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: []json.RawMessage{h.fixtureResource(time.Now().UTC())}})
		return nil
	}
	session, err := h.authorizeSession(r, sessionID, sessionAccessRead)
	if err != nil {
		return err
	}
	resources, err := h.db.ListSessionResources(r.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return internalError("Could not list resources", fmt.Errorf("list session %q resources: %w", sessionID, err))
	}
	data := resourcesToResponses(resources)
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: data})
	return nil
}

func (h *Handler) retrieveResourceRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	resourceID := chi.URLParam(r, "resource_id")
	session, err := h.authorizeSession(r, sessionID, sessionAccessRead)
	if err != nil {
		return err
	}
	if h.isFixtureResource(r, sessionID, resourceID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureResource(time.Now().UTC()))
		return nil
	}
	resource, err := h.db.GetSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID)
	if err != nil {
		return mapResourceLoadError(err, resourceID)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromResource(resource))
	return nil
}

func (h *Handler) updateResourceRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	resourceID := chi.URLParam(r, "resource_id")
	session, err := h.authorizeSession(r, sessionID, sessionAccessManageResources)
	if err != nil {
		return err
	}
	if h.isFixtureResource(r, sessionID, resourceID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureResource(time.Now().UTC()))
		return nil
	}
	current, err := h.db.GetSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID)
	if err != nil {
		return mapResourceLoadError(err, resourceID)
	}
	if current.ResourceType != "github_repository" {
		return invalidRequest(errors.New("only github_repository resources can be updated"))
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionResourceUpdateRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	token, err := parseRequiredRawString(body.AuthorizationToken, "authorization_token")
	if err != nil {
		return invalidRequest(err)
	}
	secret, _ := httpapi.MarshalRaw(map[string]any{"authorization_token": token})
	updated, err := h.db.UpdateSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID, current.Payload, secret)
	if err != nil {
		return mapResourceLoadError(err, resourceID)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromResource(updated))
	return nil
}

func (h *Handler) deleteResourceRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	resourceID := chi.URLParam(r, "resource_id")
	session, err := h.authorizeSession(r, sessionID, sessionAccessManageResources)
	if err != nil {
		return err
	}
	if h.isFixtureResource(r, sessionID, resourceID) {
		httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: resourceID, Type: "session_resource_deleted"})
		return nil
	}
	if err := h.db.DeleteSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID); err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return mapSessionLoadError(err, sessionID)
		}
		return mapResourceLoadError(err, resourceID)
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: resourceID, Type: "session_resource_deleted"})
	return nil
}

func (h *Handler) listThreadsRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	if h.isOfficialSDKFixtureSession(r, sessionID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[threadResponse]{Data: []threadResponse{h.fixtureThread(time.Now().UTC(), false)}})
		return nil
	}
	session, err := h.authorizeSession(r, sessionID, sessionAccessRead)
	if err != nil {
		return err
	}
	if _, err := h.ensurePrimarySessionThread(r.Context(), session); err != nil {
		return internalError("Could not list threads", fmt.Errorf("ensure primary thread for session %q: %w", sessionID, err))
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return invalidRequest(err)
	}
	if strings.TrimSpace(r.URL.Query().Get("limit")) == "" {
		limit = 500
	}
	cursor, err := decodeThreadCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListSessionThreadsPage(r.Context(), db.ListSessionThreadsPageParams{
		WorkspaceUUID:     session.WorkspaceUUID,
		SessionExternalID: session.ExternalID,
		Limit:             limit,
		Cursor:            cursor,
	})
	if err != nil {
		return internalError("Could not list threads", fmt.Errorf("list session %q threads: %w", sessionID, err))
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
	return nil
}

func (h *Handler) retrieveThreadRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	session, err := h.authorizeSession(r, sessionID, sessionAccessRead)
	if err != nil {
		return err
	}
	if h.isFixtureThread(r, sessionID, threadID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureThread(time.Now().UTC(), false))
		return nil
	}
	thread, err := h.db.GetSessionThread(r.Context(), session.WorkspaceUUID, session.ExternalID, threadID)
	if err != nil {
		return mapThreadLoadError(err, threadID)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromThread(thread))
	return nil
}

func (h *Handler) archiveThreadRoute(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireSessionManager(r)
	if err != nil {
		return err
	}
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	if h.isFixtureThread(r, sessionID, threadID) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureThread(time.Now().UTC(), true))
		return nil
	}
	session, found, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	if !found {
		return mapSessionLoadError(db.ErrNotFound, sessionID)
	}
	thread, err := h.db.ArchiveSessionThread(r.Context(), principal.WorkspaceUUID, session.ExternalID, threadID)
	if err != nil {
		return mapThreadLoadError(err, threadID)
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.thread_terminated", session.ExternalID, &thread.ExternalID)
	httpapi.WriteJSON(w, http.StatusOK, responseFromThread(thread))
	return nil
}
