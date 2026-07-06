package codesessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/google/uuid"
)

func workerPayloadForPublicEvent(codeSessionID string, raw json.RawMessage, fallback time.Time) (json.RawMessage, error) {
	object, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	eventType := stringField(object, "type")
	now := firstPayloadTime(object, fallback)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	switch eventType {
	case "user.message":
		eventUUID := firstNonEmpty(stringField(object, "uuid"), stringField(object, "id"), uuid.NewString())
		payload := map[string]any{
			"type":               "user",
			"uuid":               eventUUID,
			"session_id":         codeSessionID,
			"created_at":         formatTime(now),
			"timestamp":          formatTime(now),
			"client_platform":    "web_claude_ai",
			"parent_tool_use_id": nil,
			"message": map[string]any{
				"role":    "user",
				"content": claudeContentFromPublicContent(object["content"]),
			},
		}
		if threadID := stringField(object, "session_thread_id"); threadID != "" {
			payload["session_thread_id"] = threadID
		}
		return marshalRaw(payload)
	default:
		payload := cloneMap(object)
		if stringField(payload, "uuid") == "" {
			payload["uuid"] = firstNonEmpty(stringField(payload, "id"), uuid.NewString())
		}
		if stringField(payload, "session_id") == "" {
			payload["session_id"] = codeSessionID
		}
		if stringField(payload, "created_at") == "" {
			payload["created_at"] = formatTime(now)
		}
		if stringField(payload, "timestamp") == "" {
			payload["timestamp"] = payload["created_at"]
		}
		return marshalRaw(payload)
	}
}

func normalizeWorkerOutboundPayload(codeSessionID string, raw json.RawMessage, fallback time.Time) (json.RawMessage, map[string]any, error) {
	object, err := decodeJSONObject(raw)
	if err != nil {
		return nil, nil, err
	}
	eventType := stringField(object, "type")
	if eventType == "" {
		return nil, nil, ErrProtocol
	}
	now := firstPayloadTime(object, fallback)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if eventType != "keep_alive" && stringField(object, "uuid") == "" {
		object["uuid"] = uuid.NewString()
	}
	if stringField(object, "session_id") == "" {
		object["session_id"] = codeSessionID
	}
	if stringField(object, "created_at") == "" {
		object["created_at"] = formatTime(now)
	}
	if stringField(object, "timestamp") == "" {
		object["timestamp"] = object["created_at"]
	}
	raw, err = marshalRaw(object)
	return raw, object, err
}

func normalizeWorkerOutputPayload(codeSessionID string, raw json.RawMessage, fallback time.Time) (json.RawMessage, map[string]any, error) {
	object, err := decodeJSONObject(raw)
	if err != nil {
		return nil, nil, err
	}
	eventType := stringField(object, "type")
	if eventType == "" {
		return nil, nil, fmt.Errorf("%w: missing event type", ErrProtocol)
	}
	if eventType != "keep_alive" && stringField(object, "uuid") == "" {
		return nil, nil, fmt.Errorf("%w: missing event uuid", ErrProtocol)
	}
	now := firstPayloadTime(object, fallback)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if stringField(object, "session_id") == "" {
		object["session_id"] = codeSessionID
	}
	if stringField(object, "created_at") == "" {
		object["created_at"] = formatTime(now)
	}
	if stringField(object, "timestamp") == "" {
		object["timestamp"] = object["created_at"]
	}
	raw, err = marshalRaw(object)
	return raw, object, err
}

func publicPayloadFromWorkerEvent(codeSessionID string, event db.CodeSessionEvent, object map[string]any) (json.RawMessage, bool, error) {
	payloads, ok, err := publicPayloadsFromWorkerEvent(codeSessionID, event, object)
	if err != nil || !ok || len(payloads) == 0 {
		return nil, ok, err
	}
	return payloads[0], true, nil
}

func publicPayloadsFromWorkerEvent(codeSessionID string, event db.CodeSessionEvent, object map[string]any) ([]json.RawMessage, bool, error) {
	candidates, ok := publicPayloadCandidatesFromWorkerEvent(codeSessionID, event, object)
	if !ok {
		return nil, false, nil
	}
	payloads := make([]json.RawMessage, 0, len(candidates))
	for _, candidate := range candidates {
		payload, err := normalizePublicWorkerPayload(codeSessionID, event, candidate.payload, candidate.seedSuffix, candidate.timeOffset)
		if err != nil {
			return nil, false, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, len(payloads) > 0, nil
}

type publicPayloadCandidate struct {
	payload    map[string]any
	seedSuffix string
	timeOffset time.Duration
}

func publicPayloadCandidatesFromWorkerEvent(codeSessionID string, event db.CodeSessionEvent, object map[string]any) ([]publicPayloadCandidate, bool) {
	switch event.EventType {
	case "assistant":
		return assistantPublicPayloadCandidates(object), true
	case "user":
		return userPublicPayloadCandidates(codeSessionID, object), true
	case "system":
		return systemPublicPayloadCandidates(codeSessionID, object), true
	case "result":
		return resultPublicPayloadCandidates(codeSessionID, event, object), true
	default:
		if !maevents.IsWorkerOutputEvent(event.EventType) && !maevents.IsStreamDelta(event.EventType) {
			return nil, false
		}
		return []publicPayloadCandidate{{payload: publicPayloadWithType(object, event.EventType)}}, true
	}
}

func publicPayloadsFromInternalSubagentEvent(codeSessionID string, event db.CodeSessionInternalEvent, threadID string) ([]json.RawMessage, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, nil
	}
	object, err := decodeJSONObject(event.Payload)
	if err != nil {
		return nil, err
	}
	candidates := publicPayloadCandidatesFromInternalSubagentEvent(object)
	if len(candidates) == 0 {
		return nil, nil
	}
	payloads := make([]json.RawMessage, 0, len(candidates))
	for _, candidate := range candidates {
		payload, err := normalizePublicInternalSubagentPayload(codeSessionID, event, threadID, candidate.payload, candidate.seedSuffix, candidate.timeOffset)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

func publicPayloadCandidatesFromInternalSubagentEvent(object map[string]any) []publicPayloadCandidate {
	switch stringField(object, "type") {
	case "assistant":
		return assistantPublicPayloadCandidates(object)
	case "user":
		return internalSubagentUserPayloadCandidates(object)
	case "system":
		return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}
	default:
		return nil
	}
}

func normalizePublicInternalSubagentPayload(codeSessionID string, event db.CodeSessionInternalEvent, threadID string, payload map[string]any, seedSuffix string, timeOffset time.Duration) (json.RawMessage, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	if seedSuffix != "" {
		delete(payload, "uuid")
	}
	if stringField(payload, "id") == "" {
		seed := firstNonEmpty(event.IdempotencyKey, event.PayloadHash, event.ExternalID)
		if seedSuffix != "" {
			seed += "\x00" + seedSuffix
		}
		payload["id"] = stablePublicEventID(codeSessionID, "internal-subagent\x00"+threadID+"\x00"+seed)
	}
	if stringField(payload, "uuid") == "" {
		payload["uuid"] = firstNonEmpty(stringField(payload, "id"), uuid.NewString())
	}
	if stringField(payload, "session_id") == "" {
		payload["session_id"] = codeSessionID
	}
	payload["_owner_session_thread_id"] = threadID
	payload["code_session_internal_event_id"] = event.ExternalID
	if event.AgentID != nil && strings.TrimSpace(*event.AgentID) != "" {
		payload["agent_id"] = strings.TrimSpace(*event.AgentID)
	}
	delete(payload, "agentId")
	delete(payload, "isSidechain")

	createdAt := firstPayloadTime(payload, event.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	createdAt = createdAt.Add(timeOffset)
	processedAt := timeFromString(stringField(payload, "processed_at"))
	if processedAt.IsZero() {
		processedAt = createdAt
	} else {
		processedAt = processedAt.Add(timeOffset)
	}
	payload["created_at"] = formatTime(createdAt)
	payload["processed_at"] = formatTime(processedAt)
	payload["timestamp"] = formatTime(createdAt)
	if _, ok := payload["content"]; !ok {
		if message, ok := payload["message"].(map[string]any); ok {
			if content, ok := message["content"]; ok {
				payload["content"] = publicContentBlocks(content)
			}
		}
	}
	return marshalRaw(payload)
}

func normalizePublicWorkerPayload(codeSessionID string, event db.CodeSessionEvent, payload map[string]any, seedSuffix string, timeOffset time.Duration) (json.RawMessage, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	if seedSuffix != "" {
		delete(payload, "uuid")
	}
	if stringField(payload, "id") == "" {
		seed := firstNonEmpty(event.IdempotencyKey, event.PayloadHash, event.ExternalID)
		if seedSuffix != "" {
			seed += "\x00" + seedSuffix
		}
		payload["id"] = stablePublicEventID(codeSessionID, seed)
	}
	if stringField(payload, "uuid") == "" {
		payload["uuid"] = firstNonEmpty(stringField(payload, "id"), uuid.NewString())
	}
	createdAt := firstPayloadTime(payload, event.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	createdAt = createdAt.Add(timeOffset)
	processedAt := timeFromString(stringField(payload, "processed_at"))
	if processedAt.IsZero() {
		processedAt = createdAt
	} else {
		processedAt = processedAt.Add(timeOffset)
	}
	payload["created_at"] = formatTime(createdAt)
	payload["processed_at"] = formatTime(processedAt)
	payload["timestamp"] = formatTime(createdAt)
	if _, ok := payload["content"]; !ok {
		if message, ok := payload["message"].(map[string]any); ok {
			if content, ok := message["content"]; ok {
				payload["content"] = publicContentBlocks(content)
			}
		}
	}
	return marshalRaw(payload)
}

func publicPayloadWithType(object map[string]any, eventType string) map[string]any {
	payload := cloneMap(object)
	payload["type"] = eventType
	return payload
}

func resultPublicPayloadCandidates(codeSessionID string, event db.CodeSessionEvent, object map[string]any) []publicPayloadCandidate {
	candidates := make([]publicPayloadCandidate, 0, 3)
	modelUsage := firstNonNil(object["model_usage"], object["modelUsage"])
	usage := object["usage"]
	duration := durationFromPayloadMs(object, "duration_api_ms", "duration_ms")
	if modelUsage != nil || usage != nil || duration > 0 {
		seed := firstNonEmpty(event.IdempotencyKey, event.PayloadHash, event.ExternalID)
		startID := ""
		if seed != "" {
			startID = stablePublicEventID(codeSessionID, seed+"\x00result:model_request_start")
		}
		model := firstNonEmpty(stringField(object, "model"), firstModelName(modelUsage))
		start := publicPayloadWithType(object, "span.model_request_start")
		delete(start, "result")
		delete(start, "usage")
		delete(start, "modelUsage")
		delete(start, "model_usage")
		if startID != "" {
			start["id"] = startID
		}
		if model != "" {
			start["model"] = model
		}
		startOffset := -duration
		if startOffset == 0 {
			startOffset = -time.Millisecond
		}
		candidates = append(candidates, publicPayloadCandidate{
			payload:    start,
			seedSuffix: "result:model_request_start",
			timeOffset: startOffset,
		})

		end := publicPayloadWithType(object, "span.model_request_end")
		delete(end, "result")
		delete(end, "modelUsage")
		if seed != "" {
			end["id"] = stablePublicEventID(codeSessionID, seed+"\x00result:model_request_end")
		}
		if startID != "" && stringField(end, "model_request_start_id") == "" {
			end["model_request_start_id"] = startID
		}
		if model != "" {
			end["model"] = model
		}
		if modelUsage != nil {
			end["model_usage"] = modelUsage
		}
		if usage != nil {
			end["usage"] = usage
		}
		candidates = append(candidates, publicPayloadCandidate{
			payload:    end,
			seedSuffix: "result:model_request_end",
		})
	}
	candidates = append(candidates, publicPayloadCandidate{payload: publicPayloadWithType(object, "session.status_idle")})
	return candidates
}

func assistantPublicPayloadCandidates(object map[string]any) []publicPayloadCandidate {
	content := publicContentBlocks(firstNonNil(object["content"], nestedMapValue(object, "message", "content")))
	blocks, ok := content.([]any)
	if !ok || len(blocks) == 0 {
		return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "agent.message")}}
	}
	candidates := make([]publicPayloadCandidate, 0, len(blocks))
	for index, value := range blocks {
		block, ok := value.(map[string]any)
		if !ok {
			candidates = append(candidates, publicPayloadCandidate{
				payload:    publicPayloadWithSingleContentBlock(object, "agent.message", value),
				seedSuffix: fmt.Sprintf("content:%d", index),
				timeOffset: time.Duration(index) * time.Millisecond,
			})
			continue
		}
		blockType := stringField(block, "type")
		eventType := "agent.message"
		switch blockType {
		case "thinking":
			eventType = "agent.thinking"
		case "tool_use":
			eventType = "agent.tool_use"
		}
		payload := publicPayloadWithSingleContentBlock(object, eventType, block)
		if eventType == "agent.tool_use" {
			if toolUseID := stringField(block, "id"); toolUseID != "" {
				payload["tool_use_id"] = toolUseID
			}
			if name := stringField(block, "name"); name != "" {
				payload["name"] = name
				payload["tool_name"] = name
			}
			if input, ok := block["input"]; ok {
				payload["input"] = input
			}
		}
		candidates = append(candidates, publicPayloadCandidate{
			payload:    payload,
			seedSuffix: fmt.Sprintf("content:%d:%s:%s", index, blockType, stringField(block, "id")),
			timeOffset: time.Duration(index) * time.Millisecond,
		})
	}
	return candidates
}

func publicPayloadWithSingleContentBlock(object map[string]any, eventType string, block any) map[string]any {
	payload := publicPayloadWithType(object, eventType)
	content := []any{block}
	payload["content"] = content
	if message, ok := payload["message"].(map[string]any); ok {
		message["content"] = content
	}
	return payload
}

func userPublicPayloadCandidates(codeSessionID string, object map[string]any) []publicPayloadCandidate {
	content := publicContentBlocks(firstNonNil(object["content"], nestedMapValue(object, "message", "content")))
	blocks, ok := content.([]any)
	if !ok {
		return nil
	}
	candidates := make([]publicPayloadCandidate, 0, len(blocks))
	for index, value := range blocks {
		block, ok := value.(map[string]any)
		if !ok || stringField(block, "type") != "tool_result" {
			continue
		}
		toolUseID := stringField(block, "tool_use_id")
		if toolUseID == "" {
			continue
		}
		eventType := "agent.tool_result"
		if claudeToolResultIsAgentThreadMessage(block) {
			eventType = "agent.thread_message_received"
		}
		payload := publicPayloadWithType(object, eventType)
		if eventType == "agent.thread_message_received" {
			payload["from_session_thread_id"] = claudeTaskThreadIDFromKey(codeSessionID, toolUseID)
		}
		payload["tool_use_id"] = toolUseID
		payload["content"] = claudeToolResultContent(block)
		payload["raw_tool_result"] = block
		if isError, ok := block["is_error"]; ok {
			payload["is_error"] = isError
		}
		delete(payload, "message")
		delete(payload, "parent_tool_use_id")
		candidates = append(candidates, publicPayloadCandidate{
			payload:    payload,
			seedSuffix: fmt.Sprintf("user_tool_result:%d:%s", index, toolUseID),
			timeOffset: time.Duration(index) * time.Millisecond,
		})
	}
	return candidates
}

func internalSubagentUserPayloadCandidates(object map[string]any) []publicPayloadCandidate {
	content := publicContentBlocks(firstNonNil(object["content"], nestedMapValue(object, "message", "content")))
	if content == nil {
		return nil
	}
	payload := publicPayloadWithType(object, "user.message")
	payload["content"] = content
	delete(payload, "message")
	delete(payload, "parent_tool_use_id")
	return []publicPayloadCandidate{{payload: payload, seedSuffix: "internal_subagent:user_message"}}
}

func systemPublicPayloadCandidates(codeSessionID string, object map[string]any) []publicPayloadCandidate {
	subtype := stringField(object, "subtype")
	switch subtype {
	case "task_started":
		threadID := claudeTaskThreadID(codeSessionID, object)
		if threadID == "" {
			return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}
		}
		agentName := firstNonEmpty(stringField(object, "description"), stringField(object, "task_type"), "subagent")
		content := claudeTaskContent(object)
		created := publicPayloadWithType(object, "session.thread_created")
		created["session_thread_id"] = threadID
		created["agent_name"] = agentName
		created["task_id"] = stringField(object, "task_id")
		created["tool_use_id"] = stringField(object, "tool_use_id")
		running := publicPayloadWithType(object, "session.thread_status_running")
		running["session_thread_id"] = threadID
		running["agent_name"] = agentName
		running["task_id"] = stringField(object, "task_id")
		running["tool_use_id"] = stringField(object, "tool_use_id")
		sent := publicPayloadWithType(object, "agent.thread_message_sent")
		sent["to_session_thread_id"] = threadID
		sent["to_agent_name"] = agentName
		sent["task_id"] = stringField(object, "task_id")
		sent["tool_use_id"] = stringField(object, "tool_use_id")
		if len(content) > 0 {
			sent["content"] = content
		}
		return []publicPayloadCandidate{
			{payload: created, seedSuffix: "task_started:thread_created:" + threadID},
			{payload: running, seedSuffix: "task_started:thread_running:" + threadID, timeOffset: time.Millisecond},
			{payload: sent, seedSuffix: "task_started:message_sent:" + threadID, timeOffset: 2 * time.Millisecond},
		}
	case "task_notification":
		threadID := claudeTaskThreadID(codeSessionID, object)
		if threadID == "" {
			return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}
		}
		statusEventType := "session.thread_status_idle"
		if status := strings.ToLower(stringField(object, "status")); status == "failed" || status == "error" || status == "terminated" {
			statusEventType = "session.thread_status_terminated"
		}
		status := publicPayloadWithType(object, statusEventType)
		status["session_thread_id"] = threadID
		status["task_id"] = stringField(object, "task_id")
		status["tool_use_id"] = stringField(object, "tool_use_id")
		status["stop_reason"] = map[string]any{
			"type":   firstNonEmpty(stringField(object, "status"), "completed"),
			"detail": stringField(object, "summary"),
		}
		return []publicPayloadCandidate{{payload: status, seedSuffix: "task_notification:thread_status:" + threadID}}
	default:
		return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}
	}
}

func claudeTaskThreadID(codeSessionID string, object map[string]any) string {
	key := firstNonEmpty(stringField(object, "tool_use_id"), stringField(object, "task_id"))
	if key == "" {
		return ""
	}
	return claudeTaskThreadIDFromKey(codeSessionID, key)
}

func claudeTaskThreadIDFromKey(codeSessionID string, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(codeSessionID) + "\x00claude-task\x00" + key))
	return "sthr_" + hex.EncodeToString(sum[:16])
}

func claudeToolResultContent(block map[string]any) []any {
	content, ok := block["content"].([]any)
	if !ok || len(content) == 0 {
		if text := stringField(block, "content"); text != "" {
			return []any{map[string]any{"type": "text", "text": text}}
		}
		return nil
	}
	filtered := make([]any, 0, len(content))
	for _, value := range content {
		record, ok := value.(map[string]any)
		if !ok {
			filtered = append(filtered, value)
			continue
		}
		text := stringField(record, "text")
		if strings.HasPrefix(text, "agentId:") || strings.Contains(text, "<usage>") {
			continue
		}
		filtered = append(filtered, value)
	}
	if len(filtered) > 0 {
		return filtered
	}
	return content
}

func claudeToolResultIsAgentThreadMessage(block map[string]any) bool {
	if firstNonEmpty(stringField(block, "agent_id"), stringField(block, "agentId"), stringField(block, "session_thread_id")) != "" {
		return true
	}
	content, ok := block["content"].([]any)
	if !ok {
		text := stringField(block, "content")
		return strings.Contains(text, "agentId:") || strings.Contains(text, "<usage>")
	}
	for _, value := range content {
		record, ok := value.(map[string]any)
		if !ok {
			continue
		}
		text := stringField(record, "text")
		if strings.HasPrefix(text, "agentId:") || strings.Contains(text, "<usage>") {
			return true
		}
	}
	return false
}

func claudeTaskContent(object map[string]any) []any {
	if prompt := stringField(object, "prompt"); prompt != "" {
		return []any{map[string]any{"type": "text", "text": prompt}}
	}
	if summary := stringField(object, "summary"); summary != "" {
		return []any{map[string]any{"type": "text", "text": summary}}
	}
	return nil
}

func claudeContentFromPublicContent(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		block, ok := item.(map[string]any)
		if !ok || stringField(block, "type") != "text" {
			return value
		}
		if text, ok := block["text"].(string); ok {
			parts = append(parts, text)
			continue
		}
		return value
	}
	return strings.Join(parts, "\n")
}

func publicContentBlocks(value any) any {
	switch content := value.(type) {
	case string:
		return []any{map[string]any{"type": "text", "text": content}}
	case []any:
		return content
	default:
		return value
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstModelName(value any) string {
	usage, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if model := stringField(usage, "model"); model != "" {
		return model
	}
	for key := range usage {
		if strings.TrimSpace(key) != "" {
			return strings.TrimSpace(key)
		}
	}
	return ""
}

func durationFromPayloadMs(object map[string]any, keys ...string) time.Duration {
	for _, key := range keys {
		value, ok := numericPayloadValue(object[key])
		if ok && value > 0 {
			return time.Duration(value * float64(time.Millisecond))
		}
	}
	return 0
}

func numericPayloadValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func nestedMapValue(object map[string]any, keys ...string) any {
	var current any = object
	for _, key := range keys {
		currentObject, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = currentObject[key]
	}
	return current
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		out := make(map[string]any, len(input))
		for key, value := range input {
			out[key] = value
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func firstPayloadTime(object map[string]any, fallback time.Time) time.Time {
	for _, field := range []string{"processed_at", "created_at", "timestamp"} {
		if parsed := timeFromString(stringField(object, field)); !parsed.IsZero() {
			return parsed
		}
	}
	if data, ok := object["data"].(map[string]any); ok {
		if parsed := timeFromString(stringField(data, "timestamp")); !parsed.IsZero() {
			return parsed
		}
	}
	return fallback.UTC()
}

func timeFromString(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
