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

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/webtools"
)

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, err := requireSessionManager(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionCreateRequest](w, r, maxSessionCreateBodySize)
	if err != nil {
		return invalidSessionCreateBody(err)
	}

	agent, snapshot, err := h.resolveAgent(r, principal, body.Agent)
	if err != nil {
		return invalidRequest(err)
	}
	if err := rejectUnpricedSessionBudget(body.Budget, agent.Model); err != nil {
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
	if err := webtools.ValidateSession(env.Config, snapshot); err != nil {
		return invalidRequest(err)
	}
	if err := networkpolicy.ValidateSessionMCPHosts(env.Config, snapshot); err != nil {
		return invalidRequest(err)
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
		return mapVaultBindingError(err)
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
	if err := validateEnvironmentSessionResources(env, resources); err != nil {
		return err
	}
	var workSecret *string
	if selfHostedMemoryCredentialRequired(env, resources) {
		secret, secretErr := auth.NewWorkSecret(auth.WorkTokenScope{WorkspaceUUID: principal.WorkspaceUUID, EnvironmentID: env.ExternalID, WorkID: workID})
		if secretErr != nil {
			return internalError("Could not create work credential", secretErr)
		}
		workSecret = &secret
	}
	resourceInputs, err := sessionResourceWriteInputs(resources)
	if err != nil {
		return mapResourceBuildError(err)
	}
	input := db.CreateSessionInput{
		Session: db.Session{
			UUID:                  uuid.NewV4().String(),
			ExternalID:            sessionID,
			OrganizationUUID:      principal.OrganizationUUID,
			WorkspaceUUID:         principal.WorkspaceUUID,
			CreatedByAPIKeyUUID:   principal.APIKeyUUID,
			RuntimeUserUUID:       principal.UserUUID,
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
			TraceParent:           telemetry.TraceParent(r.Context()),
			UUID:                  uuid.NewV4().String(),
			ExternalID:            workID,
			OrganizationUUID:      principal.OrganizationUUID,
			WorkspaceUUID:         principal.WorkspaceUUID,
			EnvironmentUUID:       env.UUID,
			EnvironmentExternalID: env.ExternalID,
			Metadata:              json.RawMessage(`{}`),
			State:                 "queued",
			Secret:                workSecret,
			CreatedAt:             now,
			UpdatedAt:             now,
		},
	}
	events, err := normalizeSessionInitialEvents(&input.Session, body.InitialEvents, now)
	if err != nil {
		return invalidRequest(err)
	}
	prepared, err := h.eventPayloads.PreparePublic(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, events)
	if err != nil {
		return internalError("Could not prepare initial events", err)
	}
	created, _, _, _, persisted, err := h.db.CreateSessionWithEvents(r.Context(), input, prepared)
	if err != nil {
		if mapped, ok := mapFileResourcePersistenceError(err); ok {
			return mapped
		}
		return internalError("Could not create session", fmt.Errorf("create session %q: %w", sessionID, err))
	}
	if len(events) == 0 {
		h.enqueuePrincipalWebhook(r.Context(), principal, "session.status_idled", created.ExternalID, nil, created.CreatedAt)
	} else {
		persisted = eventpayload.RestoreCreatedPublic(persisted, events)
		h.publishSessionEvents(r.Context(), persisted)
		h.enqueueWebhooksForSessionEvents(r.Context(), created.WorkspaceUUID, created.ExternalID, persisted)
	}
	response, err := h.responseFromSession(r, created)
	if err != nil {
		return internalError("Could not create session", fmt.Errorf("load session %q response: %w", sessionID, err))
	}
	trace.SpanFromContext(r.Context()).SetAttributes(telemetry.AttrSessionID.String(sessionID))
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
	order, err := parseOrder(r, "desc")
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
	if len(body.Budget) > 0 && string(body.Budget) != "null" {
		return invalidRequest(errors.New("a budget cannot be added to a session created without one"))
	}
	if len(body.VaultIDs) > 0 {
		return invalidRequest(errors.New("vault_ids updates are not supported"))
	}
	next := current
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
		env, err := h.db.GetEnvironment(r.Context(), current.WorkspaceUUID, current.EnvironmentExternalID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return environmentNotFound(current.EnvironmentExternalID, err)
			}
			return internalError("Could not update session", fmt.Errorf("get environment: %w", err))
		}
		if err := webtools.ValidateSession(env.Config, next.AgentSnapshot); err != nil {
			return invalidRequest(err)
		}
	}
	updated := current
	var changed bool
	if len(changedSessionFields(current, next)) > 0 {
		next.UpdatedAt = time.Now().UTC()
		updated, changed, err = h.db.UpdateSession(r.Context(), principal.WorkspaceUUID, sessionID, next)
		if err != nil {
			return mapSessionLoadError(err, sessionID)
		}
		if fields := changedSessionFields(current, updated); changed && len(fields) > 0 {
			event, err := h.sessionUpdatedEvent(updated, fields)
			if err == nil {
				h.appendAndBroadcastInternal(r, updated.ExternalID, []db.SessionEvent{event})
			}
		}
	}
	if changed {
		h.enqueuePrincipalWebhook(r.Context(), principal, "session.updated", updated.ExternalID, nil, updated.UpdatedAt)
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
	removal, changed, err := h.db.ArchiveSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	h.finishSessionRemoval(r.Context(), removal)
	archived := removal.Session
	if changed && len(removal.StatusEvents) == 0 {
		h.enqueuePrincipalWebhook(r.Context(), principal, "session.status_terminated", archived.ExternalID, nil, *archived.ArchivedAt)
	}
	h.enqueueSessionSandboxReclaims(r.Context(), archived)
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
	primary, found, err := h.db.GetPrimarySessionThread(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	if !found {
		return mapSessionLoadError(db.ErrNotFound, sessionID)
	}
	removal, err := h.db.DeleteSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	occurredAt := time.Now().UTC()
	h.finishSessionRemoval(r.Context(), removal)
	deleted := removal.Session
	h.enqueueSessionSandboxReclaims(r.Context(), deleted)
	deletedEvent, err := h.simpleSessionEvent("session.deleted", sessionID, &primary.ExternalID)
	if err == nil {
		deletedEvent.WorkspaceUUID = deleted.WorkspaceUUID
		deletedEvent.SessionExternalID = sessionID
		h.publishSessionEvents(r.Context(), []db.SessionEvent{deletedEvent})
	}
	h.enqueuePrincipalWebhook(r.Context(), principal, "session.deleted", deleted.ExternalID, nil, occurredAt)
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: sessionID, Type: "session_deleted"})
	return nil
}

func (h *Handler) enqueueSessionSandboxReclaims(ctx context.Context, session db.Session) {
	if h.sandboxReclaims != nil {
		if err := h.sandboxReclaims.EnqueueRemovedSession(ctx, session); err != nil {
			h.logger.ErrorContext(ctx, "schedule removed session sandbox reclamation", "session_id", session.ExternalID, "error", err)
		}
	}
}

func (h *Handler) finishSessionRemoval(ctx context.Context, removal db.SessionRemoval) {
	h.codeSessions.PurgeWorkerEvents(ctx, removal.CodeSessionIDs)
	h.publishSessionEvents(ctx, removal.StatusEvents)
	h.enqueueWebhooksForSessionEvents(ctx, removal.Session.WorkspaceUUID, removal.Session.ExternalID, removal.StatusEvents)
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
	if _, err := h.authorizeSession(r, sessionID, sessionAccessEventsRead); err != nil {
		return err
	}
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return invalidRequest(err)
	}
	order, err := parseOrder(r, "asc")
	if err != nil {
		return invalidRequest(err)
	}
	cursor, err := decodeEventCursor(r.URL.Query().Get("page"), order)
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
	records, hasMore, err := h.eventPayloads.ListSessionEventsPage(r.Context(), db.ListSessionEventsPageParams{
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
	if errors.Is(err, db.ErrInvalidCursor) {
		return invalidRequest(err)
	}
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
		value := encodeEventCursor(records[len(records)-1], order)
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

	session, err := h.authorizeSession(r, sessionID, sessionAccessEventsSend)
	if err != nil {
		return err
	}
	annotateSessionInputShape(r.Context(), inputs)
	now := time.Now().UTC()
	events := make([]db.SessionEvent, 0, len(inputs))
	var outcomesChanged bool
	normalizedSession := session
	for _, raw := range inputs {
		event, outcomes, changed, err := normalizeInputEvent(normalizedSession, raw, now)
		if err != nil {
			recordSessionEventsRejected(r.Context(), "invalid")
			return invalidRequest(err)
		}
		if changed {
			normalizedSession.OutcomeEvaluations = outcomes
			outcomesChanged = true
		}
		events = append(events, event)
	}
	if err := h.validateMessageFileSources(r.Context(), session, events); err != nil {
		return err
	}
	var outcomeEvaluations json.RawMessage
	if outcomesChanged {
		outcomeEvaluations = normalizedSession.OutcomeEvaluations
	}
	changes, err := h.eventPayloads.AppendSessionEvents(r.Context(), session.WorkspaceUUID, session.ExternalID, events, outcomeEvaluations)
	if err != nil {
		recordSessionEventsRejected(r.Context(), sessionEventRejectionReason(err))
		if errors.Is(err, db.ErrInvalidState) {
			return invalidRequest(errors.New("archived sessions do not accept new events"))
		}
		return mapSessionLoadError(err, sessionID)
	}
	h.codeSessions.PurgeWorkerEvents(r.Context(), changes.RetiredCodeSessionIDs)
	created := changes.Events
	recordAcceptedSessionEvents(r.Context(), created)
	h.publishSessionEvents(r.Context(), created)
	h.enqueueWebhooksForSessionEvents(r.Context(), session.WorkspaceUUID, session.ExternalID, created)
	if h.codeSessions != nil {
		if err := h.codeSessions.QueuePublicSessionEvents(r.Context(), session, changes.WorkerInputEvents()); err != nil {
			h.logger.ErrorContext(r.Context(), "queue session events for code session", "session_id", session.ExternalID, "error", err)
			recordSessionHandoffFailure(r.Context(), err)
			return queueCodeSessionEventsError(err)
		}
	}

	data := make([]json.RawMessage, 0, len(created))
	for _, event := range created {
		if allowedPublicEventType(event.EventType) {
			data = append(data, sessionEventPayload(event))
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, sendEventsResponse{Data: data})
	return nil
}

func (h *Handler) addResourceRoute(w http.ResponseWriter, r *http.Request) error {
	sessionID := chi.URLParam(r, "session_id")

	session, err := h.authorizeSession(r, sessionID, sessionAccessManageResources)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[sessionResourceRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	resourceType, err := parseRequiredRawString(body.Type, "type")
	if err != nil {
		return invalidRequest(err)
	}
	if resourceType == sessionresource.GitRepositoryType {
		return invalidRequest(errors.New("git repositories must be bound when creating the session"))
	}
	if resourceType == sessionresource.MemoryStoreType {
		return invalidRequest(errors.New("memory stores must be bound when creating the session"))
	}
	existing, err := h.db.ListSessionResources(r.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return internalError("Could not list resources", fmt.Errorf("list session %q resources: %w", sessionID, err))
	}
	resource, err := h.resourceFromRequest(r, session, body, time.Now().UTC(), observeSessionMemoryResources(existing))
	if err != nil {
		return mapResourceBuildError(err)
	}
	if resourceType == sessionresource.FileType {
		env, err := h.db.GetEnvironment(r.Context(), session.WorkspaceUUID, session.EnvironmentExternalID)
		if err != nil {
			return resourceEnvironmentLoadError(session.EnvironmentExternalID, err)
		}
		if err := validateEnvironmentSessionResources(env, []normalizedSessionResource{resource}); err != nil {
			return err
		}
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
	// An explicit empty string or null clears the token; an omitted field is rejected.
	if len(body.AuthorizationToken) == 0 {
		return gitTokenUpdateRequiredError()
	}
	token, err := sessionresource.ParseGitTokenInput(body.AuthorizationToken)
	if err != nil {
		return invalidRequest(err)
	}
	secret, err := sessionresource.EncryptGitToken(r.Context(), h.secretService, secrets.ResourceBinding{
		OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID,
	}, token)
	if err != nil {
		return mapResourceBuildError(err)
	}
	// Keep the running repository configuration unchanged; update only credentials.
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

	resource, err := h.db.GetSessionResource(r.Context(), session.WorkspaceUUID, session.ExternalID, resourceID)
	if err != nil {
		return mapResourceLoadError(err, resourceID)
	}
	if resource.ResourceType == sessionresource.GitRepositoryType {
		return invalidRequest(errors.New("git repositories cannot be removed from a session"))
	}
	if resource.ResourceType == sessionresource.MemoryStoreType {
		return invalidRequest(errors.New("memory stores cannot be removed from a session"))
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

	session, err := h.authorizeSession(r, sessionID, sessionAccessRead)
	if err != nil {
		return err
	}
	if _, err := h.ensurePrimarySessionThread(r.Context(), session); err != nil {
		return internalError("Could not list threads", fmt.Errorf("ensure primary thread for session %q: %w", sessionID, err))
	}
	params, err := parseThreadListParams(r, session)
	if err != nil {
		return invalidRequest(err)
	}
	records, hasMore, err := h.db.ListSessionThreadsPage(r.Context(), params)
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

	session, found, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return mapSessionLoadError(err, sessionID)
	}
	if !found {
		return mapSessionLoadError(db.ErrNotFound, sessionID)
	}
	thread, removal, changed, err := h.db.ArchiveSessionThread(r.Context(), principal.WorkspaceUUID, session.ExternalID, threadID)
	if err != nil {
		return mapThreadLoadError(err, threadID)
	}
	h.finishSessionRemoval(r.Context(), removal)
	if changed && thread.ParentThreadUUID != nil && len(removal.StatusEvents) == 0 {
		h.enqueuePrincipalWebhook(r.Context(), principal, "session.thread_terminated", session.ExternalID, &thread.ExternalID, *thread.ArchivedAt)
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromThread(thread))
	return nil
}
