package sessions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

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
	if writeFileResourcePersistenceError(w, r, err) {
		return
	}
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
	if writeFileResourcePersistenceError(w, r, err) {
		return
	}
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

func writeFileResourcePersistenceError(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, db.ErrFileReferenceNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(
			http.StatusNotFound,
			"not_found_error",
			"File referenced by the session resource was not found",
		))
		return true
	}
	if errors.Is(err, db.ErrFilestorePathExists) {
		httpapi.WriteError(w, r, httpapi.NewError(
			http.StatusConflict,
			"conflict_error",
			"File resource mount_path conflicts with the session filesystem",
		))
		return true
	}
	return false
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
