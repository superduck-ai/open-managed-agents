package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireSessionManager(w, r)
	if !ok {
		return
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionMutationRequest](w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if h.isOfficialSDKFixturePrincipal(principal) && h.createUsesOfficialFixtures(body) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return
	}

	agent, snapshot, err := h.resolveAgent(r, principal, body.Agent)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	environmentID, err := parseRequiredRawString(body.EnvironmentID, "environment_id")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceUUID, environmentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Environment not found: "+environmentID))
			return
		}
		h.logger.ErrorContext(r.Context(), "get environment for session", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create session"))
		return
	}
	if env.ArchivedAt != nil {
		writeBadRequest(w, r, errors.New("environment must not be archived"))
		return
	}
	metadata, err := httpapi.NormalizeMetadata(rawOrDefault(body.Metadata, `{}`), validateMetadataEntries)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	title, err := nullableStringOrMissing(body.Title, "title")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	vaultIDs, err := h.normalizeVaultIDs(r, principal, rawOrDefault(body.VaultIDs, `[]`))
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
	resources, err := h.resourcesFromCreate(r, principal, sessionID, body.Resources, now)
	if err != nil {
		h.writeResourceBuildError(w, r, err)
		return
	}
	resourceInputs, err := sessionResourceWriteInputs(resources)
	if err != nil {
		h.writeResourceBuildError(w, r, err)
		return
	}
	workData, _ := httpapi.MarshalRaw(map[string]any{"id": sessionID, "type": "session"})
	created, thread, _, _, err := h.db.CreateSession(r.Context(), db.CreateSessionInput{
		Session: db.Session{
			UUID:                  uuid.NewString(),
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
			Status:                "idle",
			Usage:                 json.RawMessage(`{}`),
			Stats:                 json.RawMessage(`{}`),
			OutcomeEvaluations:    json.RawMessage(`[]`),
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		Thread: db.SessionThread{
			UUID:             uuid.NewString(),
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
			UUID:                  uuid.NewString(),
			ExternalID:            workID,
			OrganizationUUID:      principal.OrganizationUUID,
			WorkspaceUUID:         principal.WorkspaceUUID,
			EnvironmentUUID:       env.UUID,
			EnvironmentExternalID: env.ExternalID,
			Data:                  workData,
			Metadata:              json.RawMessage(`{}`),
			State:                 "queued",
			CreatedAt:             now,
			UpdatedAt:             now,
		},
	})
	if err != nil {
		if writeFileResourcePersistenceError(w, r, err) {
			return
		}
		h.logger.ErrorContext(r.Context(), "create session", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create session"))
		return
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.created", created.ExternalID, nil)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.pending", created.ExternalID, nil)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.status_idled", created.ExternalID, nil)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.thread_created", created.ExternalID, &thread.ExternalID)
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.thread_idled", created.ExternalID, &thread.ExternalID)
	response, err := h.responseFromSession(r, created)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "load session response", "error", err)
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
		h.logger.ErrorContext(r.Context(), "list sessions", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list sessions"))
		return
	}
	data := make([]sessionResponse, 0, len(records))
	for _, record := range records {
		response, err := h.responseFromSession(r, record)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "list session response", "error", err)
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
		h.logger.ErrorContext(r.Context(), "retrieve session response", "error", err)
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
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), false))
		return
	}
	current, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
		return
	}
	if current.ArchivedAt != nil || current.Status != "idle" {
		writeBadRequest(w, r, errors.New("session must be idle and unarchived to update"))
		return
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionMutationRequest](w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if len(body.VaultIDs) > 0 {
		writeBadRequest(w, r, errors.New("vault_ids updates are not supported"))
		return
	}
	next := current
	if len(body.Title) > 0 {
		next.Title, err = nullableStringFromRaw(body.Title, "title")
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if len(body.Metadata) > 0 {
		next.Metadata, err = httpapi.PatchMetadata(next.Metadata, body.Metadata, validateMetadataEntries)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	if len(body.Agent) > 0 {
		next.AgentSnapshot, err = patchSessionAgent(next.AgentSnapshot, body.Agent)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
	}
	next.UpdatedAt = time.Now().UTC()
	updated, err := h.db.UpdateSession(r.Context(), principal.WorkspaceUUID, sessionID, next)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
		return
	}
	event, err := h.sessionUpdatedEvent(updated)
	if err == nil {
		h.appendAndBroadcastInternal(r, updated.ExternalID, []db.SessionEvent{event})
	}
	response, err := h.responseFromSession(r, updated)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "update session response", "error", err)
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
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureSession(time.Now().UTC(), true))
		return
	}
	current, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
		return
	}
	if current.Status == "running" || current.Status == "rescheduling" {
		writeBadRequest(w, r, errors.New("running sessions cannot be archived"))
		return
	}
	archived, err := h.db.ArchiveSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
		return
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.archived", archived.ExternalID, nil)
	response, err := h.responseFromSession(r, archived)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "archive session response", "error", err)
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
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: sessionID, Type: "session_deleted"})
		return
	}
	current, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
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
	deleted, err := h.db.DeleteSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
		return
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.deleted", deleted.ExternalID, nil)
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
	if _, err := h.db.GetSessionThread(r.Context(), workspaceUUIDFromRequest(r), sessionID, threadID); err != nil {
		h.writeThreadLoadError(w, r, err, threadID)
		return
	}
	if err := h.backfillSubagentThreadEventsIfEmpty(r.Context(), session, threadID); err != nil {
		h.logger.ErrorContext(r.Context(), "backfill subagent thread events", "session_id", sessionID, "thread_id", threadID, "error", err)
	}
	h.listEvents(w, r, sessionID, threadID)
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
		h.logger.ErrorContext(r.Context(), "list session events", "error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list events"))
		return
	}
	hiddenPrimaryToolUseIDs, err := h.primaryOrphanToolUseIDsWithChildCopies(r.Context(), sessionID, threadID, records)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list session events child tool projections", "error", err)
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
	body, err := httpapi.DecodeObjectBodyAs[sessionEventsRequest](w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if len(body.Events) == 0 {
		writeBadRequest(w, r, errors.New("events is required"))
		return
	}
	var inputs []json.RawMessage
	if err := json.Unmarshal(body.Events, &inputs); err != nil || len(inputs) == 0 {
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
	normalizedSession := session
	for _, raw := range inputs {
		event, outcomes, changed, err := normalizeInputEvent(normalizedSession, raw, now)
		if err != nil {
			writeBadRequest(w, r, err)
			return
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
			writeBadRequest(w, r, errors.New("archived sessions do not accept new events"))
			return
		}
		h.writeSessionLoadError(w, r, err, sessionID)
		return
	}
	for _, event := range created {
		h.broadcast(event)
	}
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
	body, err := httpapi.DecodeObjectBodyAs[sessionResourceRequest](w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	resource, err := h.resourceFromRequest(r, session, body, time.Now().UTC())
	if err != nil {
		h.writeResourceBuildError(w, r, err)
		return
	}
	resourceInput, err := sessionResourceWriteInput(resource)
	if err != nil {
		h.writeResourceBuildError(w, r, err)
		return
	}
	created, err := h.db.CreateSessionResource(
		r.Context(),
		resourceInput,
	)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
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
	resources, err := h.db.ListSessionResources(r.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list session resources", "error", err)
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
	resource, err := h.db.GetSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID)
	if err != nil {
		h.writeResourceLoadError(w, r, err, resourceID)
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
	current, err := h.db.GetSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID)
	if err != nil {
		h.writeResourceLoadError(w, r, err, resourceID)
		return
	}
	if current.ResourceType != "github_repository" {
		writeBadRequest(w, r, errors.New("only github_repository resources can be updated"))
		return
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionResourceUpdateRequest](w, r, maxSessionBodySize)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	token, err := parseRequiredRawString(body.AuthorizationToken, "authorization_token")
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	secret, _ := httpapi.MarshalRaw(map[string]any{"authorization_token": token})
	updated, err := h.db.UpdateSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID, current.Payload, secret)
	if err != nil {
		h.writeResourceLoadError(w, r, err, resourceID)
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
	if err := h.db.DeleteSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID); err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			h.writeSessionLoadError(w, r, err, sessionID)
			return
		}
		h.writeResourceLoadError(w, r, err, resourceID)
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
		h.logger.ErrorContext(r.Context(), "ensure primary session thread", "session_id", session.ExternalID, "error", err)
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
		WorkspaceUUID:     session.WorkspaceUUID,
		SessionExternalID: session.ExternalID,
		Limit:             limit,
		Cursor:            cursor,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list session threads", "error", err)
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
	thread, err := h.db.GetSessionThread(r.Context(), session.WorkspaceUUID, session.ExternalID, threadID)
	if err != nil {
		h.writeThreadLoadError(w, r, err, threadID)
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
	session, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
		return
	}
	thread, err := h.db.ArchiveSessionThread(r.Context(), principal.WorkspaceUUID, session.ExternalID, threadID)
	if err != nil {
		h.writeThreadLoadError(w, r, err, threadID)
		return
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.thread_terminated", session.ExternalID, &thread.ExternalID)
	httpapi.WriteJSON(w, http.StatusOK, responseFromThread(thread))
}
