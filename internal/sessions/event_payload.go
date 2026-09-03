package sessions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

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

func parseRequiredRawString(raw json.RawMessage, name string) (string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return "", fmt.Errorf("%s is required", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}

func nullableStringOrMissing(raw json.RawMessage, name string) (*string, error) {
	if len(raw) == 0 {
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

func validateMetadataEntries(metadata map[string]string) error {
	return httpapi.ValidateMetadataEntryLimit(metadata, 16, "metadata may contain at most 16 entries")
}

func rawOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) > 0 {
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
	workspaceID := records[0].WorkspaceUUID
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
	return eventPayloadForResponse(event.Payload, event.CreatedAt, event.ProcessedAt, threadID)
}

func eventPayloadForResponse(payloadRaw json.RawMessage, createdAt, processedAt time.Time, threadID string) json.RawMessage {
	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return payloadRaw
	}
	changed := ensureSessionEventTimeField(payload, "created_at", createdAt)
	changed = ensureSessionEventTimeField(payload, "processed_at", processedAt) || changed
	if strings.TrimSpace(threadID) != "" && !hasSessionThreadOwnerField(payload) {
		payload["session_thread_id"] = strings.TrimSpace(threadID)
		changed = true
	}
	if !changed {
		return payloadRaw
	}
	raw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return payloadRaw
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
	return encodeCursor(session.CreatedAt, session.UUID)
}

func encodeEventCursor(event db.SessionEvent) string {
	return encodeCursor(event.CreatedAt, event.UUID)
}

func encodeThreadCursor(thread db.SessionThread) string {
	return encodeCursor(thread.CreatedAt, thread.UUID)
}

func encodeCursor(createdAt time.Time, resourceUUID string) string {
	data, _ := json.Marshal(map[string]any{"created_at": createdAt.UTC().Format(time.RFC3339Nano), "uuid": resourceUUID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeSessionCursor(raw string) (*db.SessionPageCursor, error) {
	createdAt, resourceUUID, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.SessionPageCursor{CreatedAt: *createdAt, UUID: resourceUUID}, nil
}

func decodeEventCursor(raw string) (*db.SessionEventPageCursor, error) {
	createdAt, resourceUUID, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.SessionEventPageCursor{CreatedAt: *createdAt, UUID: resourceUUID}, nil
}

func decodeThreadCursor(raw string) (*db.SessionThreadPageCursor, error) {
	createdAt, resourceUUID, err := decodeCursor(raw)
	if err != nil || createdAt == nil {
		return nil, err
	}
	return &db.SessionThreadPageCursor{CreatedAt: *createdAt, UUID: resourceUUID}, nil
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
	if _, err := uuid.Parse(payload.UUID); err != nil {
		return nil, "", errors.New("page cursor is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, "", errors.New("page cursor is invalid")
	}
	createdAt = createdAt.UTC()
	return &createdAt, payload.UUID, nil
}
