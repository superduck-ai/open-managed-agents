package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/google/uuid"
)

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
	snapshot, err := agentsnapshot.FromAgent(agent)
	if err != nil {
		return db.Agent{}, nil, err
	}
	return agent, snapshot, nil
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
			payload["checkout"] = agentsnapshot.RawJSONValue(raw, nil)
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
	var err error
	switch eventType {
	case "user.message":
		err = validateContentBlocks(payload, "content", true)
	case "system.message":
		err = validateContentBlocks(payload, "content", true)
	case "user.interrupt":
		return nil
	case "user.tool_confirmation":
		err = validateToolConfirmationPayload(payload)
	case "user.tool_result":
		err = validateToolResultPayload(payload, "tool_use_id")
	case "user.custom_tool_result":
		err = validateToolResultPayload(payload, "custom_tool_use_id")
	case "user.define_outcome":
		err = validateDefineOutcomePayload(payload)
	}
	if err != nil {
		return err
	}
	return validateSessionThreadIDPayload(payload)
}

func validateToolConfirmationPayload(payload map[string]any) error {
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
	return nil
}

func validateToolResultPayload(payload map[string]any, idField string) error {
	if requiredStringValue(payload, idField) == "" {
		return fmt.Errorf("%s is required", idField)
	}
	return validateContentBlocks(payload, "content", false)
}

func validateDefineOutcomePayload(payload map[string]any) error {
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
	return nil
}

func validateSessionThreadIDPayload(payload map[string]any) error {
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
