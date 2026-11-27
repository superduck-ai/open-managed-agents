package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

type qoderSessionBudget struct {
	Type          string `json:"type"`
	MaxCreditCost string `json:"max_credit_cost,omitempty"`
}

type qoderSessionUsage struct {
	ModelCredits          float64 `json:"model_credits"`
	SandboxRuntimeCredits float64 `json:"sandbox_runtime_credits"`
	TotalCredits          float64 `json:"total_credits"`
}

type qoderSessionResource struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	FileID        string          `json:"file_id,omitempty"`
	MemoryStoreID string          `json:"memory_store_id,omitempty"`
	MountPath     string          `json:"mount_path,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Raw           json.RawMessage `json:"-"`
}

type qoderAgentField struct {
	ID      string
	Version int64
}

func (a *qoderAgentField) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		var id string
		if err := json.Unmarshal(trimmed, &id); err != nil {
			return err
		}
		a.ID = id
		return nil
	}
	var obj struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return err
	}
	a.ID = obj.ID
	a.Version = obj.Version
	return nil
}

type qoderSession struct {
	ID            string              `json:"id"`
	Agent         qoderAgentField     `json:"agent"`
	AgentID       string              `json:"agent_id"`
	AgentVersion  int64               `json:"agent_version"`
	EnvironmentID string              `json:"environment_id"`
	Title         string              `json:"title"`
	Status        string              `json:"status"`
	VaultIDs      []string            `json:"vault_ids"`
	Metadata      map[string]string   `json:"metadata"`
	Resources     []json.RawMessage   `json:"resources"`
	Budget        *qoderSessionBudget `json:"budget"`
	Usage         *qoderSessionUsage  `json:"usage"`
	ArchivedAt    *time.Time          `json:"archived_at"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

type qoderCreateSessionRequest struct {
	Agent         any                 `json:"agent"`
	EnvironmentID string              `json:"environment_id,omitempty"`
	Title         string              `json:"title,omitempty"`
	VaultIDs      []string            `json:"vault_ids,omitempty"`
	Metadata      map[string]string   `json:"metadata,omitempty"`
	Resources     []json.RawMessage   `json:"resources,omitempty"`
	InitialEvents []json.RawMessage   `json:"initial_events,omitempty"`
	Budget        *qoderSessionBudget `json:"budget,omitempty"`
}

type qoderAgentVersionRef struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

type qoderUpdateSessionRequest struct {
	Title    *string           `json:"title,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type sessionBindingSummary struct {
	Title     string `json:"title,omitempty"`
	Status    string `json:"status,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	Failed    bool   `json:"failed,omitempty"`
}

type qoderSendEventsRequest struct {
	Events []json.RawMessage `json:"events"`
}

type qoderEventsEnvelope struct {
	Data    []json.RawMessage `json:"data"`
	Events  []json.RawMessage `json:"events"`
	FirstID string            `json:"first_id"`
	LastID  string            `json:"last_id"`
	HasMore bool              `json:"has_more"`
}

type qoderSessionResourcesEnvelope struct {
	Data      []json.RawMessage `json:"data"`
	Resources []json.RawMessage `json:"resources"`
}

type qoderThread struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id"`
	Title     string            `json:"title"`
	Status    string            `json:"status"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type qoderThreadsEnvelope struct {
	Data    []qoderThread `json:"data"`
	Threads []qoderThread `json:"threads"`
}

type qoderEventEnvelopeMeta struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	ThreadID    string     `json:"session_thread_id"`
	ProcessedAt *time.Time `json:"processed_at"`
}

func (r *Runtime) CreateSession(ctx context.Context, scope agentruntime.Scope, input agentruntime.CreateSessionInput) (agentruntime.Session, error) {
	agentBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, input.AgentID)
	if err != nil {
		return agentruntime.Session{}, err
	}
	remoteEnvID := ""
	if input.EnvironmentID != "" {
		envBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, input.EnvironmentID)
		if err != nil {
			return agentruntime.Session{}, err
		}
		remoteEnvID = envBinding.RemoteID
	}
	budget, err := translateSessionBudget(input.Budget)
	if err != nil {
		return agentruntime.Session{}, err
	}
	translatedResources, err := r.translateCreateSessionResources(ctx, scope, input.Resources)
	if err != nil {
		return agentruntime.Session{}, err
	}
	translatedVaultIDs, err := r.translateOwnedVaultIDs(ctx, scope, input.VaultIDs)
	if err != nil {
		return agentruntime.Session{}, err
	}
	initialEvents, err := decodeEventPayloads(input.InitialEvents)
	if err != nil {
		return agentruntime.Session{}, err
	}
	var agentField any = agentBinding.RemoteID
	if input.AgentVersion > 0 {
		agentField = qoderAgentVersionRef{
			Type:    "agent",
			ID:      agentBinding.RemoteID,
			Version: input.AgentVersion,
		}
	}
	reqBody := qoderCreateSessionRequest{
		Agent:         agentField,
		EnvironmentID: remoteEnvID,
		Title:         input.Title,
		VaultIDs:      translatedVaultIDs,
		Metadata:      injectWatermark(input.Metadata, scope),
		Resources:     translatedResources,
		InitialEvents: initialEvents,
		Budget:        budget,
	}
	var remote qoderSession
	if err := r.doJSON(ctx, http.MethodPost, "/sessions", nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Session{}, err
	}
	omaID, err := ids.New("sesn_")
	if err != nil {
		return agentruntime.Session{}, agentruntime.UnavailableError("failed to generate session identifier", err)
	}
	var agentSummary agentBindingSummary
	_ = json.Unmarshal(agentBinding.Summary, &agentSummary)
	summary, _ := json.Marshal(sessionBindingSummary{
		Title:     remote.Title,
		Status:    remote.Status,
		AgentID:   input.AgentID,
		AgentName: agentSummary.Name,
		Failed:    isFailedSessionStatus(remote.Status),
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:                agentruntime.ResourceTypeSession,
		OMAExternalID:       omaID,
		RemoteID:            remote.ID,
		ParentOMAExternalID: input.AgentID,
	}, summary); err != nil {
		return agentruntime.Session{}, err
	}
	if remote.Usage != nil {
		r.recordSessionUsage(ctx, scope, omaID, remote.ID, *remote.Usage, remote.UpdatedAt)
	}
	return r.mapQoderSession(ctx, scope, omaID, input.AgentID, input.EnvironmentID, remote), nil
}

func translateSessionBudget(budget *agentruntime.SessionBudget) (*qoderSessionBudget, error) {
	if budget == nil {
		return nil, nil
	}
	if budget.Mode == agentruntime.BudgetModeUSD || strings.TrimSpace(budget.MaxCostUSD) != "" {
		return nil, agentruntime.CapabilityUnsupportedError("session_budget_usd")
	}
	if budget.Mode == agentruntime.BudgetModeCredits || strings.TrimSpace(budget.MaxCreditCost) != "" {
		cost := strings.TrimSpace(budget.MaxCreditCost)
		if cost == "" {
			return nil, agentruntime.InvalidArgumentError("max_credit_cost is required for credits budget", nil)
		}
		return &qoderSessionBudget{
			Type:          "limit",
			MaxCreditCost: cost,
		}, nil
	}
	return nil, nil
}

func (r *Runtime) translateCreateSessionResources(ctx context.Context, scope agentruntime.Scope, inputs []agentruntime.AddSessionResourceInput) ([]json.RawMessage, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(inputs))
	for _, res := range inputs {
		encoded, err := r.encodeSessionResourceInput(ctx, scope, res)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

func (r *Runtime) encodeSessionResourceInput(ctx context.Context, scope agentruntime.Scope, input agentruntime.AddSessionResourceInput) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if len(bytes.TrimSpace(input.Payload)) > 0 {
		if err := json.Unmarshal(input.Payload, &fields); err != nil {
			return nil, agentruntime.InvalidArgumentError("invalid session resource payload", err)
		}
	}
	if input.Type != "" {
		rawType, _ := json.Marshal(input.Type)
		fields["type"] = rawType
	}
	if input.MountPath != "" {
		rawPath, _ := json.Marshal(input.MountPath)
		fields["mount_path"] = rawPath
	}
	fileID := input.FileID
	if fileID == "" {
		if rawFileID, ok := fields["file_id"]; ok {
			_ = json.Unmarshal(rawFileID, &fileID)
		}
	}
	if fileID != "" {
		fileBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeFile, fileID)
		if err != nil {
			return nil, err
		}
		rawRemoteID, _ := json.Marshal(fileBinding.RemoteID)
		fields["file_id"] = rawRemoteID
	}
	memoryStoreID := input.MemoryStoreID
	if memoryStoreID == "" {
		if rawStoreID, ok := fields["memory_store_id"]; ok {
			_ = json.Unmarshal(rawStoreID, &memoryStoreID)
		}
	}
	if memoryStoreID != "" {
		storeBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeMemoryStore, memoryStoreID)
		if err != nil {
			return nil, err
		}
		rawRemoteID, _ := json.Marshal(storeBinding.RemoteID)
		fields["memory_store_id"] = rawRemoteID
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, agentruntime.InvalidArgumentError("failed to encode session resource", err)
	}
	return json.RawMessage(encoded), nil
}

func (r *Runtime) GetSession(ctx context.Context, scope agentruntime.Scope, sessionID string) (agentruntime.Session, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return agentruntime.Session{}, err
	}
	var remote qoderSession
	if err := r.doJSON(ctx, http.MethodGet, "/sessions/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.Session{}, err
	}
	r.refreshSessionBindingSummary(ctx, scope, binding, remote.Title, remote.Status, "")
	if remote.Usage != nil {
		r.recordSessionUsage(ctx, scope, binding.OMAExternalID, binding.RemoteID, *remote.Usage, remote.UpdatedAt)
	}
	return r.mapQoderSession(ctx, scope, binding.OMAExternalID, binding.ParentOMAExternalID, "", remote), nil
}

func (r *Runtime) ListSessions(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListSessionsQuery) (agentruntime.Page[agentruntime.Session], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeSession, "", "", query.IncludeArchived, limit+1, offset)
	if err != nil {
		return agentruntime.Page[agentruntime.Session]{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]agentruntime.Session, 0, len(rows))
	for _, row := range rows {
		var remote qoderSession
		if err := r.doJSON(ctx, http.MethodGet, "/sessions/"+url.PathEscape(row.RemoteID), nil, nil, nil, &remote); err != nil {
			r.logger.WarnContext(ctx, "qoder session hydrate failed, falling back to binding summary", "oma_id", row.OMAExternalID, "error", err)
			items = append(items, sessionFromBindingSummary(row))
			continue
		}
		items = append(items, r.mapQoderSession(ctx, scope, row.OMAExternalID, row.ParentOMAExternalID, "", remote))
	}
	page := agentruntime.Page[agentruntime.Session]{
		Items:   items,
		HasMore: hasMore,
	}
	if len(items) > 0 {
		page.FirstID = items[0].ID
		page.LastID = items[len(items)-1].ID
	}
	return page, nil
}

func (r *Runtime) UpdateSession(ctx context.Context, scope agentruntime.Scope, sessionID string, input agentruntime.UpdateSessionInput) (agentruntime.Session, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return agentruntime.Session{}, err
	}
	reqBody := qoderUpdateSessionRequest{
		Title: input.Title,
	}
	if input.Metadata != nil {
		reqBody.Metadata = injectWatermark(input.Metadata, scope)
	}
	var remote qoderSession
	if err := r.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(binding.RemoteID), nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Session{}, err
	}
	r.refreshSessionBindingSummary(ctx, scope, binding, remote.Title, remote.Status, "")
	return r.mapQoderSession(ctx, scope, binding.OMAExternalID, binding.ParentOMAExternalID, "", remote), nil
}

func (r *Runtime) ArchiveSession(ctx context.Context, scope agentruntime.Scope, sessionID string) (agentruntime.Session, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return agentruntime.Session{}, err
	}
	var remote qoderSession
	if err := r.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(binding.RemoteID)+"/archive", nil, nil, nil, &remote); err != nil {
		return agentruntime.Session{}, err
	}
	r.refreshSessionBindingSummary(ctx, scope, binding, remote.Title, remote.Status, "archived")
	return r.mapQoderSession(ctx, scope, binding.OMAExternalID, binding.ParentOMAExternalID, "", remote), nil
}

func (r *Runtime) DeleteSession(ctx context.Context, scope agentruntime.Scope, sessionID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return err
	}
	if err := r.doJSON(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(binding.RemoteID), nil, nil, nil, nil); err != nil {
		if !isUpstreamNotFound(err) {
			return err
		}
	}
	return r.bindings.SoftDelete(ctx, scope, agentruntime.ResourceTypeSession, binding.OMAExternalID)
}

func (r *Runtime) SendEvents(ctx context.Context, scope agentruntime.Scope, sessionID string, input agentruntime.SendEventsInput) ([]agentruntime.Event, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return nil, err
	}
	decodedEvents, err := decodeEventPayloads(input.Events)
	if err != nil {
		return nil, err
	}
	var rawResp json.RawMessage
	if err := r.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(binding.RemoteID)+"/events", nil, nil, qoderSendEventsRequest{Events: decodedEvents}, &rawResp); err != nil {
		return nil, err
	}
	rawEvents, err := decodeEventsArray(rawResp)
	if err != nil {
		return nil, upstreamTransportError(err)
	}
	return mapEncodedEvents(rawEvents)
}

func (r *Runtime) ListEvents(ctx context.Context, scope agentruntime.Scope, sessionID string, query agentruntime.ListEventsQuery) (agentruntime.Page[agentruntime.Event], error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return agentruntime.Page[agentruntime.Event]{}, err
	}
	targetLimit := query.Limit
	if targetLimit <= 0 {
		targetLimit = maxQoderEventPageLimit
	}
	beforeCursor := DecodeEventID(query.BeforeID)
	afterCursor := DecodeEventID(query.AfterID)
	paginateBackward := query.BeforeID != "" && query.AfterID == ""

	allEvents := make([]agentruntime.Event, 0, min(targetLimit, maxQoderEventPageLimit))
	firstID := ""
	lastID := ""
	hasMore := false

	for len(allEvents) < targetLimit {
		remaining := targetLimit - len(allEvents)
		pageLimit := min(remaining, maxQoderEventPageLimit)
		params := url.Values{}
		params.Set("limit", strconv.Itoa(pageLimit))
		if query.Order != "" {
			params.Set("order", query.Order)
		}
		if query.ThreadID != "" {
			params.Set("session_thread_id", query.ThreadID)
		}
		if beforeCursor != "" {
			params.Set("before_id", beforeCursor)
		}
		if afterCursor != "" {
			params.Set("after_id", afterCursor)
		}

		var env qoderEventsEnvelope
		if err := r.doJSON(ctx, http.MethodGet, "/sessions/"+url.PathEscape(binding.RemoteID)+"/events", params, nil, nil, &env); err != nil {
			return agentruntime.Page[agentruntime.Event]{}, err
		}
		rawItems := env.Data
		if len(rawItems) == 0 {
			rawItems = env.Events
		}
		mapped, err := mapEncodedEvents(rawItems)
		if err != nil {
			return agentruntime.Page[agentruntime.Event]{}, err
		}
		if firstID == "" {
			if env.FirstID != "" {
				firstID = EncodeEventID(env.FirstID)
			} else if len(mapped) > 0 {
				firstID = mapped[0].ID
			}
		}
		allEvents = append(allEvents, mapped...)
		if env.LastID != "" {
			lastID = EncodeEventID(env.LastID)
		} else if len(mapped) > 0 {
			lastID = mapped[len(mapped)-1].ID
		}
		hasMore = env.HasMore
		if !env.HasMore || len(mapped) == 0 || len(allEvents) >= targetLimit {
			break
		}
		if paginateBackward {
			nextBefore := env.FirstID
			if nextBefore == "" {
				nextBefore = DecodeEventID(mapped[0].ID)
			}
			if nextBefore == "" || nextBefore == beforeCursor {
				break
			}
			beforeCursor = nextBefore
		} else {
			nextAfter := env.LastID
			if nextAfter == "" {
				nextAfter = DecodeEventID(mapped[len(mapped)-1].ID)
			}
			if nextAfter == "" || nextAfter == afterCursor {
				break
			}
			afterCursor = nextAfter
		}
	}

	if len(allEvents) > targetLimit {
		allEvents = allEvents[:targetLimit]
		hasMore = true
	}
	if len(allEvents) > 0 {
		if firstID == "" {
			firstID = allEvents[0].ID
		}
		lastID = allEvents[len(allEvents)-1].ID
	}
	return agentruntime.Page[agentruntime.Event]{
		Items:   allEvents,
		FirstID: firstID,
		LastID:  lastID,
		HasMore: hasMore,
	}, nil
}

func (r *Runtime) ListSessionResources(ctx context.Context, scope agentruntime.Scope, sessionID string) ([]agentruntime.SessionResource, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return nil, err
	}
	remoteResources, err := r.fetchRemoteSessionResources(ctx, binding.RemoteID)
	if err != nil {
		return nil, err
	}
	out := make([]agentruntime.SessionResource, 0, len(remoteResources))
	for _, res := range remoteResources {
		out = append(out, r.mapSessionResource(ctx, scope, binding.OMAExternalID, res))
	}
	return out, nil
}

func (r *Runtime) AddSessionResource(ctx context.Context, scope agentruntime.Scope, sessionID string, input agentruntime.AddSessionResourceInput) (agentruntime.SessionResource, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return agentruntime.SessionResource{}, err
	}
	encodedInput, err := r.encodeSessionResourceInput(ctx, scope, input)
	if err != nil {
		return agentruntime.SessionResource{}, err
	}
	var rawResp json.RawMessage
	if err := r.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(binding.RemoteID)+"/resources", nil, nil, encodedInput, &rawResp); err != nil {
		return agentruntime.SessionResource{}, err
	}
	parsed, err := parseQoderSessionResource(rawResp)
	if err != nil {
		return agentruntime.SessionResource{}, upstreamTransportError(err)
	}
	return r.mapSessionResource(ctx, scope, binding.OMAExternalID, parsed), nil
}

func (r *Runtime) UpdateSessionResource(ctx context.Context, scope agentruntime.Scope, sessionID string, resourceID string, input agentruntime.UpdateSessionResourceInput) (agentruntime.SessionResource, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return agentruntime.SessionResource{}, err
	}
	fields := make(map[string]json.RawMessage)
	if len(bytes.TrimSpace(input.Payload)) > 0 {
		if err := json.Unmarshal(input.Payload, &fields); err != nil {
			return agentruntime.SessionResource{}, agentruntime.InvalidArgumentError("invalid session resource update payload", err)
		}
	}
	if input.MountPath != nil {
		rawPath, _ := json.Marshal(*input.MountPath)
		fields["mount_path"] = rawPath
	}
	remoteResourceID := DecodeSessionResourceID(resourceID)
	path := "/sessions/" + url.PathEscape(binding.RemoteID) + "/resources/" + url.PathEscape(remoteResourceID)
	var rawResp json.RawMessage
	if err := r.doJSON(ctx, http.MethodPost, path, nil, nil, fields, &rawResp); err != nil {
		return agentruntime.SessionResource{}, err
	}
	parsed, err := parseQoderSessionResource(rawResp)
	if err != nil {
		return agentruntime.SessionResource{}, upstreamTransportError(err)
	}
	return r.mapSessionResource(ctx, scope, binding.OMAExternalID, parsed), nil
}

func (r *Runtime) DeleteSessionResource(ctx context.Context, scope agentruntime.Scope, sessionID string, resourceID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return err
	}
	remoteResourceID := DecodeSessionResourceID(resourceID)
	path := "/sessions/" + url.PathEscape(binding.RemoteID) + "/resources/" + url.PathEscape(remoteResourceID)
	return r.doJSON(ctx, http.MethodDelete, path, nil, nil, nil, nil)
}

func (r *Runtime) ListThreads(ctx context.Context, scope agentruntime.Scope, sessionID string) ([]agentruntime.Thread, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := r.doJSON(ctx, http.MethodGet, "/sessions/"+url.PathEscape(binding.RemoteID)+"/threads", nil, nil, nil, &raw); err != nil {
		return nil, err
	}
	threads, err := decodeThreadsPayload(raw)
	if err != nil {
		return nil, upstreamTransportError(err)
	}
	out := make([]agentruntime.Thread, 0, len(threads))
	for _, th := range threads {
		out = append(out, mapQoderThread(binding.OMAExternalID, th))
	}
	return out, nil
}

func (r *Runtime) GetThread(ctx context.Context, scope agentruntime.Scope, sessionID string, threadID string) (agentruntime.Thread, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, sessionID)
	if err != nil {
		return agentruntime.Thread{}, err
	}
	path := "/sessions/" + url.PathEscape(binding.RemoteID) + "/threads/" + url.PathEscape(threadID)
	var remote qoderThread
	if err := r.doJSON(ctx, http.MethodGet, path, nil, nil, nil, &remote); err != nil {
		return agentruntime.Thread{}, err
	}
	return mapQoderThread(binding.OMAExternalID, remote), nil
}

func (r *Runtime) fetchRemoteSessionResources(ctx context.Context, remoteSessionID string) ([]qoderSessionResource, error) {
	var raw json.RawMessage
	if err := r.doJSON(ctx, http.MethodGet, "/sessions/"+url.PathEscape(remoteSessionID)+"/resources", nil, nil, nil, &raw); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var items []json.RawMessage
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, upstreamTransportError(err)
		}
	} else {
		var env qoderSessionResourcesEnvelope
		if err := json.Unmarshal(trimmed, &env); err != nil {
			return nil, upstreamTransportError(err)
		}
		if len(env.Data) > 0 {
			items = env.Data
		} else {
			items = env.Resources
		}
	}
	out := make([]qoderSessionResource, 0, len(items))
	for _, item := range items {
		parsed, err := parseQoderSessionResource(item)
		if err != nil {
			continue
		}
		out = append(out, parsed)
	}
	return out, nil
}

func parseQoderSessionResource(raw json.RawMessage) (qoderSessionResource, error) {
	var res qoderSessionResource
	if err := json.Unmarshal(raw, &res); err != nil {
		return qoderSessionResource{}, err
	}
	res.Raw = raw
	return res, nil
}

func (r *Runtime) mapSessionResource(ctx context.Context, scope agentruntime.Scope, omaSessionID string, res qoderSessionResource) agentruntime.SessionResource {
	omaResourceID := EncodeSessionResourceID(res.ID)
	omaFileID := res.FileID
	if res.FileID != "" {
		if fb, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeFile, res.FileID); err == nil {
			omaFileID = fb.OMAExternalID
		}
	}
	omaMemoryStoreID := res.MemoryStoreID
	if res.MemoryStoreID != "" {
		if mb, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeMemoryStore, res.MemoryStoreID); err == nil {
			omaMemoryStoreID = mb.OMAExternalID
		}
	}
	payload := rewriteResourcePayloadIDs(res.Raw, omaResourceID, omaFileID, omaMemoryStoreID)
	return agentruntime.SessionResource{
		ID:            omaResourceID,
		SessionID:     omaSessionID,
		Type:          res.Type,
		FileID:        omaFileID,
		MemoryStoreID: omaMemoryStoreID,
		MountPath:     res.MountPath,
		Payload:       payload,
		CreatedAt:     res.CreatedAt,
		UpdatedAt:     res.UpdatedAt,
	}
}

func rewriteResourcePayloadIDs(raw json.RawMessage, omaResourceID, omaFileID, omaMemoryStoreID string) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}
	if omaResourceID != "" {
		if encoded, err := json.Marshal(omaResourceID); err == nil {
			fields["id"] = encoded
		}
	}
	if omaFileID != "" {
		if _, has := fields["file_id"]; has {
			if encoded, err := json.Marshal(omaFileID); err == nil {
				fields["file_id"] = encoded
			}
		}
	}
	if omaMemoryStoreID != "" {
		if _, has := fields["memory_store_id"]; has {
			if encoded, err := json.Marshal(omaMemoryStoreID); err == nil {
				fields["memory_store_id"] = encoded
			}
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return json.RawMessage(encoded)
}

func (r *Runtime) mapQoderSession(
	ctx context.Context,
	scope agentruntime.Scope,
	omaSessionID string,
	omaAgentID string,
	omaEnvID string,
	remote qoderSession,
) agentruntime.Session {
	resolvedAgentID := omaAgentID
	if resolvedAgentID == "" {
		remoteAgentID := remote.Agent.ID
		if remoteAgentID == "" {
			remoteAgentID = remote.AgentID
		}
		if remoteAgentID != "" {
			if ab, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeAgent, remoteAgentID); err == nil {
				resolvedAgentID = ab.OMAExternalID
			}
		}
	}
	resolvedEnvID := omaEnvID
	if resolvedEnvID == "" && remote.EnvironmentID != "" {
		if eb, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeEnvironment, remote.EnvironmentID); err == nil {
			resolvedEnvID = eb.OMAExternalID
		}
	}
	agentVersion := remote.Agent.Version
	if agentVersion == 0 {
		agentVersion = remote.AgentVersion
	}
	resources := make([]agentruntime.SessionResource, 0, len(remote.Resources))
	for _, rawRes := range remote.Resources {
		if parsed, err := parseQoderSessionResource(rawRes); err == nil {
			resources = append(resources, r.mapSessionResource(ctx, scope, omaSessionID, parsed))
		}
	}
	var budget *agentruntime.SessionBudget
	if remote.Budget != nil && remote.Budget.MaxCreditCost != "" {
		budget = &agentruntime.SessionBudget{
			Mode:          agentruntime.BudgetModeCredits,
			MaxCreditCost: remote.Budget.MaxCreditCost,
		}
	}
	var usage *agentruntime.SessionUsage
	if remote.Usage != nil {
		total := remote.Usage.TotalCredits
		if total == 0 {
			total = remote.Usage.ModelCredits + remote.Usage.SandboxRuntimeCredits
		}
		usage = &agentruntime.SessionUsage{
			ModelCredits:          remote.Usage.ModelCredits,
			SandboxRuntimeCredits: remote.Usage.SandboxRuntimeCredits,
			TotalCredits:          total,
		}
	}
	return agentruntime.Session{
		ID:            omaSessionID,
		AgentID:       resolvedAgentID,
		AgentVersion:  agentVersion,
		EnvironmentID: resolvedEnvID,
		Title:         remote.Title,
		Status:        remote.Status,
		VaultIDs:      r.mapRemoteVaultIDs(ctx, scope, remote.VaultIDs),
		Metadata:      stripWatermark(remote.Metadata),
		Resources:     resources,
		Budget:        budget,
		Usage:         usage,
		ArchivedAt:    remote.ArchivedAt,
		CreatedAt:     remote.CreatedAt,
		UpdatedAt:     remote.UpdatedAt,
	}
}

func sessionFromBindingSummary(row db.RuntimeResourceBinding) agentruntime.Session {
	var summary sessionBindingSummary
	_ = json.Unmarshal(row.Summary, &summary)
	agentID := summary.AgentID
	if agentID == "" {
		agentID = row.ParentOMAExternalID
	}
	sess := agentruntime.Session{
		ID:        row.OMAExternalID,
		AgentID:   agentID,
		Title:     summary.Title,
		Status:    summary.Status,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.State == "archived" {
		ts := row.UpdatedAt
		sess.ArchivedAt = &ts
	}
	return sess
}

func (r *Runtime) refreshSessionBindingSummary(
	ctx context.Context,
	scope agentruntime.Scope,
	binding db.RuntimeResourceBinding,
	title string,
	status string,
	state string,
) {
	var existing sessionBindingSummary
	_ = json.Unmarshal(binding.Summary, &existing)
	if title != "" {
		existing.Title = title
	}
	if status != "" {
		existing.Status = status
		existing.Failed = existing.Failed || isFailedSessionStatus(status)
	}
	if existing.AgentID == "" {
		existing.AgentID = binding.ParentOMAExternalID
	}
	encoded, err := json.Marshal(existing)
	if err != nil {
		return
	}
	if _, err := r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeSession, binding.OMAExternalID, state, encoded); err != nil {
		r.logger.WarnContext(ctx, "failed to refresh session binding summary", "session_id", binding.OMAExternalID, "error", err)
	}
}

func isFailedSessionStatus(status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	return normalized == "failed" || normalized == "error" || normalized == "terminated"
}

func (r *Runtime) recordSessionUsage(
	ctx context.Context,
	scope agentruntime.Scope,
	omaSessionID string,
	remoteSessionID string,
	usage qoderSessionUsage,
	sampledAt time.Time,
) {
	if r.usageStore == nil {
		return
	}
	total := usage.TotalCredits
	if total == 0 {
		total = usage.ModelCredits + usage.SandboxRuntimeCredits
	}
	if total <= 0 {
		return
	}
	key := scope.WorkspaceUUID + "|" + omaSessionID
	r.usageMu.Lock()
	prev, seen := r.lastCredits[key]
	if seen && total <= prev {
		r.usageMu.Unlock()
		return
	}
	delta := total - prev
	if delta < 0 {
		delta = 0
	}
	r.lastCredits[key] = total
	r.usageMu.Unlock()

	rawUsage, _ := json.Marshal(usage)
	if sampledAt.IsZero() {
		sampledAt = time.Now().UTC().Truncate(time.Millisecond)
	}
	if _, err := r.usageStore.RecordProviderUsage(ctx, db.ProviderUsageRecord{
		OrganizationUUID:   scope.OrganizationUUID,
		WorkspaceUUID:      scope.WorkspaceUUID,
		RuntimeProfileUUID: scope.RuntimeProfileUUID,
		ProviderKind:       agentruntime.ProviderQoderCloud.String(),
		SessionExternalID:  omaSessionID,
		RemoteSessionID:    remoteSessionID,
		MetricUnit:         "credits",
		CumulativeAmount:   total,
		DeltaAmount:        delta,
		RawUsage:           rawUsage,
		SampledAt:          sampledAt,
	}); err != nil {
		r.logger.WarnContext(ctx, "failed to record qoder provider usage", "session_id", omaSessionID, "error", err)
	}
}

func decodeEventPayloads(events []json.RawMessage) ([]json.RawMessage, error) {
	if len(events) == 0 {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(events))
	for _, ev := range events {
		decoded, err := DecodeEventPayload(ev)
		if err != nil {
			return nil, agentruntime.InvalidArgumentError("invalid event payload", err)
		}
		out = append(out, decoded)
	}
	return out, nil
}

func decodeEventsArray(raw json.RawMessage) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	var env qoderEventsEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, err
	}
	if len(env.Data) > 0 {
		return env.Data, nil
	}
	return env.Events, nil
}

func mapEncodedEvents(rawEvents []json.RawMessage) ([]agentruntime.Event, error) {
	out := make([]agentruntime.Event, 0, len(rawEvents))
	for _, raw := range rawEvents {
		encoded, err := EncodeEventPayload(raw)
		if err != nil {
			return nil, upstreamTransportError(err)
		}
		var meta qoderEventEnvelopeMeta
		_ = json.Unmarshal(encoded, &meta)
		out = append(out, agentruntime.Event{
			ID:          meta.ID,
			Type:        meta.Type,
			ThreadID:    meta.ThreadID,
			ProcessedAt: meta.ProcessedAt,
			Payload:     encoded,
		})
	}
	return out, nil
}

func decodeThreadsPayload(raw json.RawMessage) ([]qoderThread, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []qoderThread
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var env qoderThreadsEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, err
	}
	if len(env.Data) > 0 {
		return env.Data, nil
	}
	return env.Threads, nil
}

func mapQoderThread(omaSessionID string, th qoderThread) agentruntime.Thread {
	return agentruntime.Thread{
		ID:        th.ID,
		SessionID: omaSessionID,
		Title:     th.Title,
		Status:    th.Status,
		Metadata:  stripWatermark(th.Metadata),
		CreatedAt: th.CreatedAt,
		UpdatedAt: th.UpdatedAt,
	}
}
