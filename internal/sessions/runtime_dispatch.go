package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
}

func (h *Handler) resolveExternalSessionRuntime(ctx context.Context, principal auth.Principal) (agentruntime.Runtime, agentruntime.SessionRuntime, agentruntime.Scope, error) {
	if h.runtimeResolver == nil {
		return nil, nil, agentruntime.Scope{}, sessionRuntimeUnavailable(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	sessionRT, ok := rt.(agentruntime.SessionRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, sessionRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, sessionRT, scope, nil
}

func (h *Handler) createExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	body, err := httpapi.DecodeObjectBodyAs[sessionCreateRequest](w, r, maxSessionCreateBodySize)
	if err != nil {
		return invalidSessionCreateBody(err)
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	input, err := buildExternalCreateSessionInput(body, rt.Capabilities())
	if err != nil {
		return err
	}
	created, err := sessionRT.CreateSession(r.Context(), scope, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeSession(created))
	return nil
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := parseExternalOffset(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := sessionRT.ListSessions(r.Context(), scope, agentruntime.ListSessionsQuery{
		IncludeArchived: includeArchived,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return err
	}
	data := make([]sessionResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromRuntimeSession(item))
	}
	var nextPage *string
	if page.HasMore {
		cursor := page.NextCursor
		if cursor == "" {
			cursor = strconv.Itoa(offset + len(data))
		}
		nextPage = &cursor
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[sessionResponse]{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	session, err := sessionRT.GetSession(r.Context(), scope, sessionID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeSession(session))
	return nil
}

func (h *Handler) updateExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string) error {
	body, err := httpapi.DecodeObjectBodyAs[sessionMutationRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	var input agentruntime.UpdateSessionInput
	if len(body.Title) > 0 {
		titlePtr, parseErr := nullableStringFromRaw(body.Title, "title")
		if parseErr != nil {
			return invalidRequest(parseErr)
		}
		value := ""
		if titlePtr != nil {
			value = *titlePtr
		}
		input.Title = &value
	}
	if len(body.Metadata) > 0 {
		metadata, parseErr := decodeSessionMetadataMap(body.Metadata)
		if parseErr != nil {
			return invalidRequest(parseErr)
		}
		input.Metadata = metadata
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	updated, err := sessionRT.UpdateSession(r.Context(), scope, sessionID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeSession(updated))
	return nil
}

func (h *Handler) archiveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	archived, err := sessionRT.ArchiveSession(r.Context(), scope, sessionID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeSession(archived))
	return nil
}

func (h *Handler) deleteExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := sessionRT.DeleteSession(r.Context(), scope, sessionID); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: sessionID, Type: "session_deleted"})
	return nil
}

func (h *Handler) sendEventsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string, rawEvents json.RawMessage, inputs []json.RawMessage) error {
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	requestHash := hashEventsPayload(rawEvents)
	now := time.Now().UTC()
	if idempotencyKey != "" {
		cached, hit, conflict := h.eventIdempotency.lookup(principal.WorkspaceUUID, sessionID, idempotencyKey, requestHash, now)
		if conflict {
			return sessionIdempotencyConflict()
		}
		if hit {
			httpapi.WriteJSON(w, http.StatusOK, cached)
			return nil
		}
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	sent, err := sessionRT.SendEvents(r.Context(), scope, sessionID, agentruntime.SendEventsInput{Events: inputs})
	if err != nil {
		return err
	}
	data := make([]json.RawMessage, 0, len(sent))
	for _, event := range sent {
		data = append(data, encodeRuntimeEvent(event))
	}
	response := sendEventsResponse{Data: data}
	if idempotencyKey != "" {
		h.eventIdempotency.store(principal.WorkspaceUUID, sessionID, idempotencyKey, requestHash, response, now)
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) listEventsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID, threadID string) error {
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return invalidRequest(err)
	}
	order, err := parseOrder(r, "asc")
	if err != nil {
		return invalidRequest(err)
	}
	query := r.URL.Query()
	afterID := strings.TrimSpace(query.Get("after_id"))
	beforeID := strings.TrimSpace(query.Get("before_id"))
	if page := strings.TrimSpace(query.Get("page")); page != "" && afterID == "" && beforeID == "" {
		if order == "desc" {
			beforeID = page
		} else {
			afterID = page
		}
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := sessionRT.ListEvents(r.Context(), scope, sessionID, agentruntime.ListEventsQuery{
		ThreadID: threadID,
		BeforeID: beforeID,
		AfterID:  afterID,
		Limit:    limit,
		Order:    order,
	})
	if err != nil {
		return err
	}
	data := make([]json.RawMessage, 0, len(page.Items))
	for _, event := range page.Items {
		data = append(data, encodeRuntimeEvent(event))
	}
	var nextPage *string
	if page.HasMore {
		cursor := page.NextCursor
		if cursor == "" {
			cursor = page.LastID
		}
		if cursor != "" {
			nextPage = &cursor
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) streamEventsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID, threadID string, deltaTypeSet map[string]struct{}) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.errorAdapter.Write(w, r, streamingUnsupported())
		return
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()

	deltaTypes := make([]string, 0, len(deltaTypeSet))
	for deltaType := range deltaTypeSet {
		deltaTypes = append(deltaTypes, deltaType)
	}
	slices.Sort(deltaTypes)

	stream, err := sessionRT.StreamEvents(r.Context(), scope, sessionID, agentruntime.StreamOptions{
		LastEventID: strings.TrimSpace(r.Header.Get("Last-Event-ID")),
		ThreadID:    threadID,
		DeltaTypes:  deltaTypes,
	})
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	defer func() { _ = stream.Close() }()
	defer httpapi.ObserveSSEStream(r.Context(), "public")()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		frame, nextErr := stream.Next(r.Context())
		if nextErr != nil {
			if errors.Is(nextErr, io.EOF) || errors.Is(nextErr, context.Canceled) {
				return
			}
			h.logger.WarnContext(r.Context(), "external session stream ended with error", "session_id", sessionID, "error", nextErr)
			return
		}
		writeExternalSSEFrame(w, frame)
		flusher.Flush()
		if frame.Event == "session.deleted" {
			return
		}
	}
}

func writeExternalSSEFrame(w http.ResponseWriter, frame agentruntime.StreamFrame) {
	if frame.ID != "" && !strings.ContainsAny(frame.ID, "\r\n\x00") {
		fmt.Fprintf(w, "id: %s\n", frame.ID)
	}
	if frame.Event != "" && !strings.ContainsAny(frame.Event, "\r\n\x00") {
		fmt.Fprintf(w, "event: %s\n", frame.Event)
	}
	data := frame.Data
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func (h *Handler) listResourcesExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	resources, err := sessionRT.ListSessionResources(r.Context(), scope, sessionID)
	if err != nil {
		return err
	}
	data := make([]json.RawMessage, 0, len(resources))
	for _, res := range resources {
		data = append(data, encodeRuntimeResource(res))
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[json.RawMessage]{Data: data})
	return nil
}

func (h *Handler) addResourceExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string) error {
	body, err := httpapi.DecodeObjectBodyAs[sessionResourceRequest](w, r, maxSessionBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	input, err := buildExternalAddResourceInput(body)
	if err != nil {
		return invalidRequest(err)
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := sessionRT.AddSessionResource(r.Context(), scope, sessionID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, encodeRuntimeResource(created))
	return nil
}

func (h *Handler) retrieveResourceExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID, resourceID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	resources, err := sessionRT.ListSessionResources(r.Context(), scope, sessionID)
	if err != nil {
		return err
	}
	for _, res := range resources {
		if res.ID == resourceID {
			httpapi.WriteJSON(w, http.StatusOK, encodeRuntimeResource(res))
			return nil
		}
	}
	return resourceNotFound(resourceID, nil)
}

func (h *Handler) updateResourceExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID, resourceID string) error {
	rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSessionBodySize))
	if err != nil {
		return invalidRequest(err)
	}
	var patch struct {
		MountPath *string `json:"mount_path"`
	}
	if err := json.Unmarshal(rawBody, &patch); err != nil {
		return invalidRequest(errors.New("request body must be a JSON object"))
	}
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	updated, err := sessionRT.UpdateSessionResource(r.Context(), scope, sessionID, resourceID, agentruntime.UpdateSessionResourceInput{
		MountPath: patch.MountPath,
		Payload:   rawBody,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, encodeRuntimeResource(updated))
	return nil
}

func (h *Handler) deleteResourceExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID, resourceID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if err := sessionRT.DeleteSessionResource(r.Context(), scope, sessionID, resourceID); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, deleteResponse{ID: resourceID, Type: "session_resource_deleted"})
	return nil
}

func (h *Handler) listThreadsExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	threads, err := sessionRT.ListThreads(r.Context(), scope, sessionID)
	if err != nil {
		return err
	}
	data := make([]threadResponse, 0, len(threads))
	for _, thread := range threads {
		data = append(data, responseFromRuntimeThread(thread))
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[threadResponse]{Data: data})
	return nil
}

func (h *Handler) retrieveThreadExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, sessionID, threadID string) error {
	rt, sessionRT, scope, err := h.resolveExternalSessionRuntime(r.Context(), principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	thread, err := sessionRT.GetThread(r.Context(), scope, sessionID, threadID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromRuntimeThread(thread))
	return nil
}

func (h *Handler) checkExternalPortPreview(w http.ResponseWriter, r *http.Request, principal auth.Principal) bool {
	caps := agentruntime.DefaultCapabilities(agentruntime.ProviderKind(principal.RuntimeProvider))
	if h.runtimeResolver != nil {
		if resolved, _, err := h.runtimeResolver.WorkspaceCapabilities(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID); err == nil {
			caps = resolved
		}
	}
	if !caps.PortPreview {
		httpapi.WriteError(w, r, httpapi.NewError(
			http.StatusBadRequest,
			"unsupported_capability_error",
			fmt.Sprintf("Workspace runtime does not support capability %q", agentruntime.CapabilityPortPreview),
		))
		return false
	}
	return true
}

func buildExternalCreateSessionInput(body *sessionCreateRequest, caps agentruntime.CapabilitySet) (agentruntime.CreateSessionInput, error) {
	agentID, agentVersion, isOverride, err := parseExternalSessionAgent(body.Agent)
	if err != nil {
		return agentruntime.CreateSessionInput{}, invalidRequest(err)
	}
	if isOverride && !caps.SessionAgentOverrides {
		return agentruntime.CreateSessionInput{}, agentruntime.CapabilityUnsupportedError(agentruntime.CapabilitySessionAgentOverrides)
	}
	environmentID, err := parseRequiredRawString(body.EnvironmentID, "environment_id")
	if err != nil {
		return agentruntime.CreateSessionInput{}, invalidRequest(err)
	}
	titlePtr, err := nullableStringOrMissing(body.Title, "title")
	if err != nil {
		return agentruntime.CreateSessionInput{}, invalidRequest(err)
	}
	title := ""
	if titlePtr != nil {
		title = *titlePtr
	}
	metadata, err := decodeSessionMetadataMap(body.Metadata)
	if err != nil {
		return agentruntime.CreateSessionInput{}, invalidRequest(err)
	}
	var vaultIDs []string
	if len(body.VaultIDs) > 0 && !httpapi.IsJSONNull(body.VaultIDs) {
		if err := json.Unmarshal(body.VaultIDs, &vaultIDs); err != nil {
			return agentruntime.CreateSessionInput{}, invalidRequest(errors.New("vault_ids must be an array of strings"))
		}
	}
	resources, err := parseExternalCreateResources(body.Resources)
	if err != nil {
		return agentruntime.CreateSessionInput{}, invalidRequest(err)
	}
	var initialEvents []json.RawMessage
	if len(body.InitialEvents) > 0 && !httpapi.IsJSONNull(body.InitialEvents) {
		if err := json.Unmarshal(body.InitialEvents, &initialEvents); err != nil {
			return agentruntime.CreateSessionInput{}, invalidRequest(errors.New("initial_events must be an array"))
		}
	}
	budget, err := parseExternalSessionBudget(body.Budget, caps.SessionBudget)
	if err != nil {
		return agentruntime.CreateSessionInput{}, invalidRequest(err)
	}
	return agentruntime.CreateSessionInput{
		AgentID:       agentID,
		AgentVersion:  agentVersion,
		EnvironmentID: environmentID,
		Title:         title,
		VaultIDs:      vaultIDs,
		Metadata:      metadata,
		Resources:     resources,
		InitialEvents: initialEvents,
		Budget:        budget,
	}, nil
}

func parseExternalSessionAgent(raw json.RawMessage) (string, int64, bool, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return "", 0, false, errors.New("agent is required")
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		trimmed := strings.TrimSpace(asString)
		if trimmed == "" {
			return "", 0, false, errors.New("agent must be non-empty")
		}
		return trimmed, 0, false, nil
	}
	var obj struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Version *int64 `json:"version"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", 0, false, errors.New("agent must be a string or object")
	}
	if obj.Type == "agent_with_overrides" {
		return strings.TrimSpace(obj.ID), 0, true, nil
	}
	if strings.TrimSpace(obj.ID) == "" {
		return "", 0, false, errors.New("agent.id is required")
	}
	var version int64
	if obj.Version != nil {
		version = *obj.Version
	}
	return strings.TrimSpace(obj.ID), version, false, nil
}

func parseExternalSessionBudget(raw json.RawMessage, mode agentruntime.BudgetMode) (*agentruntime.SessionBudget, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	if mode == agentruntime.BudgetModeNone {
		return nil, errors.New("workspace runtime does not support session budgets")
	}
	var payload struct {
		Type          string `json:"type"`
		MaxCreditCost string `json:"max_credit_cost"`
		MaxCostUSD    string `json:"max_cost_usd"`
		MaxListCost   any    `json:"max_list_cost"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("budget must be an object")
	}
	if mode == agentruntime.BudgetModeCredits {
		if payload.MaxCostUSD != "" || payload.MaxListCost != nil {
			return nil, errors.New("workspace runtime requires credits budget (max_credit_cost)")
		}
		if strings.TrimSpace(payload.MaxCreditCost) == "" {
			return nil, errors.New("budget.max_credit_cost is required")
		}
		return &agentruntime.SessionBudget{
			Mode:          agentruntime.BudgetModeCredits,
			MaxCreditCost: strings.TrimSpace(payload.MaxCreditCost),
		}, nil
	}
	return &agentruntime.SessionBudget{
		Mode:       agentruntime.BudgetModeUSD,
		MaxCostUSD: strings.TrimSpace(payload.MaxCostUSD),
	}, nil
}

func parseExternalCreateResources(raw json.RawMessage) ([]agentruntime.AddSessionResourceInput, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.New("resources must be an array")
	}
	result := make([]agentruntime.AddSessionResourceInput, 0, len(items))
	for _, item := range items {
		var req sessionResourceRequest
		if err := json.Unmarshal(item, &req); err != nil {
			return nil, errors.New("each resource must be an object")
		}
		parsed, err := buildExternalAddResourceInput(&req)
		if err != nil {
			return nil, err
		}
		parsed.Payload = item
		result = append(result, parsed)
	}
	return result, nil
}

func buildExternalAddResourceInput(body *sessionResourceRequest) (agentruntime.AddSessionResourceInput, error) {
	resourceType, err := parseRequiredRawString(body.Type, "type")
	if err != nil {
		return agentruntime.AddSessionResourceInput{}, err
	}
	var fileID string
	if len(body.FileID) > 0 && !httpapi.IsJSONNull(body.FileID) {
		if err := json.Unmarshal(body.FileID, &fileID); err != nil {
			return agentruntime.AddSessionResourceInput{}, errors.New("file_id must be a string")
		}
	}
	var mountPath string
	if len(body.MountPath) > 0 && !httpapi.IsJSONNull(body.MountPath) {
		if err := json.Unmarshal(body.MountPath, &mountPath); err != nil {
			return agentruntime.AddSessionResourceInput{}, errors.New("mount_path must be a string")
		}
	}
	payload, _ := json.Marshal(body)
	return agentruntime.AddSessionResourceInput{
		Type:      resourceType,
		FileID:    fileID,
		MountPath: mountPath,
		Payload:   payload,
	}, nil
}

func decodeSessionMetadataMap(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	normalized, err := httpapi.NormalizeMetadata(raw, validateMetadataEntries)
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	if err := json.Unmarshal(normalized, &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func parseExternalOffset(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, errors.New("page is invalid")
	}
	return offset, nil
}

func responseFromRuntimeSession(session agentruntime.Session) sessionResponse {
	agentPayload := map[string]any{
		"id":      session.AgentID,
		"type":    "agent",
		"version": session.AgentVersion,
	}
	agentRaw, _ := json.Marshal(agentPayload)
	metadataRaw := json.RawMessage(`{}`)
	if len(session.Metadata) > 0 {
		if encoded, err := json.Marshal(session.Metadata); err == nil {
			metadataRaw = encoded
		}
	}
	budgetRaw := json.RawMessage(`null`)
	if session.Budget != nil {
		budgetPayload := struct {
			Type          string `json:"type"`
			Mode          string `json:"mode,omitempty"`
			MaxCostUSD    string `json:"max_cost_usd,omitempty"`
			MaxCreditCost string `json:"max_credit_cost,omitempty"`
		}{
			Type:          "limit",
			Mode:          string(session.Budget.Mode),
			MaxCostUSD:    session.Budget.MaxCostUSD,
			MaxCreditCost: session.Budget.MaxCreditCost,
		}
		if encoded, err := json.Marshal(budgetPayload); err == nil {
			budgetRaw = encoded
		}
	}
	usageRaw := json.RawMessage(`{}`)
	if session.Usage != nil {
		usageMap := map[string]any{
			"input_tokens":            session.Usage.InputTokens,
			"output_tokens":           session.Usage.OutputTokens,
			"cost_usd":                session.Usage.CostUSD,
			"model_credits":           session.Usage.ModelCredits,
			"sandbox_runtime_credits": session.Usage.SandboxRuntimeCredits,
			"total_credits":           session.Usage.TotalCredits,
		}
		if encoded, err := json.Marshal(usageMap); err == nil {
			usageRaw = encoded
		}
	}
	resources := make([]json.RawMessage, 0, len(session.Resources))
	for _, res := range session.Resources {
		resources = append(resources, encodeRuntimeResource(res))
	}
	vaultIDs := session.VaultIDs
	if vaultIDs == nil {
		vaultIDs = []string{}
	}
	var title *string
	if session.Title != "" {
		value := session.Title
		title = &value
	}
	return sessionResponse{
		ID:                 session.ID,
		Agent:              agentRaw,
		ArchivedAt:         httpapi.OptionalTime(session.ArchivedAt),
		Budget:             budgetRaw,
		CreatedAt:          httpapi.FormatTime(session.CreatedAt),
		EnvironmentID:      session.EnvironmentID,
		Metadata:           metadataRaw,
		OutcomeEvaluations: json.RawMessage(`[]`),
		Resources:          resources,
		Stats:              json.RawMessage(`{}`),
		Status:             session.Status,
		Title:              title,
		Type:               "session",
		UpdatedAt:          httpapi.FormatTime(session.UpdatedAt),
		Usage:              usageRaw,
		VaultIDs:           vaultIDs,
	}
}

func encodeRuntimeEvent(event agentruntime.Event) json.RawMessage {
	if len(event.Payload) > 0 {
		return event.Payload
	}
	payload := map[string]any{
		"id":   event.ID,
		"type": event.Type,
	}
	if event.ThreadID != "" {
		payload["thread_id"] = event.ThreadID
	}
	if event.ProcessedAt != nil {
		payload["processed_at"] = httpapi.FormatTime(*event.ProcessedAt)
	}
	encoded, _ := json.Marshal(payload)
	return encoded
}

func encodeRuntimeResource(res agentruntime.SessionResource) json.RawMessage {
	if len(res.Payload) > 0 {
		return res.Payload
	}
	payload := map[string]any{
		"id":         res.ID,
		"type":       res.Type,
		"created_at": httpapi.FormatTime(res.CreatedAt),
		"updated_at": httpapi.FormatTime(res.UpdatedAt),
	}
	if res.FileID != "" {
		payload["file_id"] = res.FileID
	}
	if res.MountPath != "" {
		payload["mount_path"] = res.MountPath
	}
	encoded, _ := json.Marshal(payload)
	return encoded
}

func responseFromRuntimeThread(thread agentruntime.Thread) threadResponse {
	return threadResponse{
		ID:        thread.ID,
		Agent:     json.RawMessage(`{}`),
		CreatedAt: httpapi.FormatTime(thread.CreatedAt),
		SessionID: thread.SessionID,
		Stats:     json.RawMessage(`{}`),
		Status:    thread.Status,
		Type:      "session_thread",
		UpdatedAt: httpapi.FormatTime(thread.UpdatedAt),
		Usage:     json.RawMessage(`{}`),
	}
}
