package codesessions

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func workerPayloadForPublicEvent(codeSessionID string, raw json.RawMessage, fallback time.Time) (json.RawMessage, error) {
	fields, err := decodeRawJSONObject(raw)
	if err != nil {
		return nil, err
	}
	var schema workerOutputCommonPayload
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("%w: invalid public event payload: %w", ErrProtocol, err)
	}
	now := firstWorkerPayloadTime(schema, fallback)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	switch schema.Type {
	case "user.message":
		eventUUID := firstNonEmpty(schema.UUID, schema.ID, uuid.NewV4().String())
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
				"content": claudeContentFromPublicContent(decodeWorkerOutputValue(schema.Content)),
			},
		}
		if schema.SessionThreadID != "" {
			payload["session_thread_id"] = schema.SessionThreadID
		}
		return marshalRaw(payload)
	default:
		if schema.UUID == "" {
			setRawJSONField(fields, "uuid", firstNonEmpty(schema.ID, uuid.NewV4().String()))
		}
		if schema.SessionID == "" {
			setRawJSONField(fields, "session_id", codeSessionID)
		}
		if schema.CreatedAt == "" {
			setRawJSONField(fields, "created_at", formatTime(now))
		}
		if schema.Timestamp == "" {
			fields["timestamp"] = fields["created_at"]
		}
		return marshalRaw(fields)
	}
}

func normalizeWorkerOutboundPayload(codeSessionID string, raw json.RawMessage, fallback time.Time) (json.RawMessage, error) {
	fields, err := decodeRawJSONObject(raw)
	if err != nil {
		return nil, err
	}
	var schema workerOutputCommonPayload
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("%w: invalid worker payload: %w", ErrProtocol, err)
	}
	if schema.Type == "" {
		return nil, ErrProtocol
	}
	now := firstWorkerPayloadTime(schema, fallback)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if schema.Type != "keep_alive" && schema.UUID == "" {
		setRawJSONField(fields, "uuid", uuid.NewV4().String())
	}
	if schema.SessionID == "" {
		setRawJSONField(fields, "session_id", codeSessionID)
	}
	if schema.CreatedAt == "" {
		setRawJSONField(fields, "created_at", formatTime(now))
	}
	if schema.Timestamp == "" {
		fields["timestamp"] = fields["created_at"]
	}
	return marshalRaw(fields)
}

func publicPayloadFromWorkerEvent(codeSessionID string, event db.CodeSessionEvent, raw json.RawMessage) (json.RawMessage, bool, error) {
	payloads, ok, err := publicPayloadsFromWorkerEvent(codeSessionID, event, raw)
	if err != nil || !ok || len(payloads) == 0 {
		return nil, ok, err
	}
	return payloads[0], true, nil
}

func publicPayloadsFromWorkerEvent(codeSessionID string, event db.CodeSessionEvent, raw json.RawMessage) ([]json.RawMessage, bool, error) {
	candidates, ok, err := publicPayloadCandidatesFromWorkerEvent(codeSessionID, event, raw)
	if err != nil {
		return nil, false, err
	}
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

func publicPayloadCandidatesFromWorkerEvent(codeSessionID string, event db.CodeSessionEvent, raw json.RawMessage) ([]publicPayloadCandidate, bool, error) {
	fields, err := decodeRawJSONObject(raw)
	if err != nil {
		return nil, false, err
	}
	object := materializePublicPayload(fields)
	switch event.EventType {
	case "assistant":
		payload, err := decodeWorkerAssistantOutputPayload(raw)
		if err != nil {
			return nil, false, fmt.Errorf("%w: invalid assistant payload: %w", ErrProtocol, err)
		}
		return assistantPublicPayloadCandidates(codeSessionID, object, payload), true, nil
	case "user":
		var payload workerUserOutputPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, false, fmt.Errorf("%w: invalid user payload: %w", ErrProtocol, err)
		}
		return userPublicPayloadCandidates(codeSessionID, object, payload), true, nil
	case "system":
		var payload workerSystemOutputPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, false, fmt.Errorf("%w: invalid system payload: %w", ErrProtocol, err)
		}
		return systemPublicPayloadCandidates(codeSessionID, object, payload), true, nil
	case "result":
		var payload workerResultOutputPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, false, fmt.Errorf("%w: invalid result payload: %w", ErrProtocol, err)
		}
		return resultPublicPayloadCandidates(codeSessionID, event, object, payload), true, nil
	default:
		if !maevents.IsWorkerOutputEvent(event.EventType) && !maevents.IsStreamDelta(event.EventType) {
			return nil, false, nil
		}
		var payload workerOpaqueOutputPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, false, fmt.Errorf("%w: invalid %s payload: %w", ErrProtocol, event.EventType, err)
		}
		return []publicPayloadCandidate{{payload: publicPayloadWithType(object, payload.Type)}}, true, nil
	}
}

func publicPayloadsFromInternalSubagentEvent(codeSessionID string, event db.CodeSessionInternalEvent, threadID string) ([]json.RawMessage, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, nil
	}
	fields, err := decodeRawJSONObject(event.Payload)
	if err != nil {
		return nil, err
	}
	object := materializePublicPayload(fields)
	var schema workerOutputCommonPayload
	if err := json.Unmarshal(event.Payload, &schema); err != nil {
		return nil, fmt.Errorf("%w: invalid internal subagent payload: %w", ErrProtocol, err)
	}
	candidates, err := publicPayloadCandidatesFromInternalSubagentEvent(codeSessionID, event.Payload, schema.Type, object)
	if err != nil {
		return nil, err
	}
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

func publicPayloadCandidatesFromInternalSubagentEvent(codeSessionID string, raw json.RawMessage, eventType string, object map[string]any) ([]publicPayloadCandidate, error) {
	switch eventType {
	case "assistant":
		payload, err := decodeWorkerAssistantOutputPayload(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid assistant payload: %w", ErrProtocol, err)
		}
		return assistantPublicPayloadCandidates(codeSessionID, object, payload), nil
	case "user":
		var payload workerUserOutputPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("%w: invalid user payload: %w", ErrProtocol, err)
		}
		return internalSubagentUserPayloadCandidates(object, payload), nil
	case "system":
		return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}, nil
	default:
		return nil, nil
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
		payload["uuid"] = firstNonEmpty(stringField(payload, "id"), uuid.NewV4().String())
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
		payload["uuid"] = firstNonEmpty(stringField(payload, "id"), uuid.NewV4().String())
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
	normalizePublicIdleStopReason(payload)
	return marshalRaw(payload)
}

func normalizePublicIdleStopReason(payload map[string]any) {
	switch stringField(payload, "type") {
	case "session.status_idle", "session.thread_status_idle":
	default:
		return
	}
	raw, ok := payload["stop_reason"]
	if !ok || raw == nil {
		payload["stop_reason"] = map[string]any{"type": "end_turn"}
		return
	}
	if reason, ok := raw.(string); ok {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "end_turn"
		}
		payload["stop_reason"] = map[string]any{"type": reason}
		return
	}
	if reason, ok := raw.(map[string]any); ok {
		reasonType := strings.TrimSpace(stringField(reason, "type"))
		if reasonType == "" {
			reasonType = "end_turn"
		}
		normalized := map[string]any{"type": reasonType}
		if reasonType != "requires_action" {
			if detail := strings.TrimSpace(stringField(reason, "detail")); detail != "" {
				normalized["detail"] = detail
			}
		}
		if eventIDs := publicStopReasonEventIDs(reason["event_ids"]); len(eventIDs) > 0 {
			normalized["event_ids"] = eventIDs
		}
		payload["stop_reason"] = normalized
	}
}

func publicStopReasonEventIDs(raw any) []string {
	switch values := raw.(type) {
	case []string:
		eventIDs := make([]string, 0, len(values))
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				eventIDs = append(eventIDs, value)
			}
		}
		return eventIDs
	case []any:
		eventIDs := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				if text = strings.TrimSpace(text); text != "" {
					eventIDs = append(eventIDs, text)
				}
			}
		}
		return eventIDs
	default:
		return nil
	}
}

func publicPayloadWithType(object map[string]any, eventType string) map[string]any {
	payload := cloneMap(object)
	payload["type"] = eventType
	return payload
}

func resultPublicPayloadCandidates(codeSessionID string, event db.CodeSessionEvent, object map[string]any, schema workerResultOutputPayload) []publicPayloadCandidate {
	candidates := make([]publicPayloadCandidate, 0, 3)
	modelUsage := firstNonNil(decodeWorkerOutputValue(schema.ModelUsageAlt), decodeWorkerOutputValue(schema.ModelUsage))
	usage := decodeWorkerOutputValue(schema.Usage)
	durationMs := schema.DurationAPIMs
	if durationMs <= 0 {
		durationMs = schema.DurationMs
	}
	duration := time.Duration(durationMs * float64(time.Millisecond))
	if modelUsage != nil || usage != nil || duration > 0 {
		seed := firstNonEmpty(event.IdempotencyKey, event.PayloadHash, event.ExternalID)
		startID := ""
		if seed != "" {
			startID = stablePublicEventID(codeSessionID, seed+"\x00result:model_request_start")
		}
		model := firstNonEmpty(schema.Model, firstModelName(modelUsage))
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

func assistantPublicPayloadCandidates(codeSessionID string, object map[string]any, schema workerAssistantOutputPayload) []publicPayloadCandidate {
	delete(object, "content_block_index")
	content := publicContentBlocks(workerOutputContent(schema.Content, schema.Message.Content))
	blocks, ok := content.([]any)
	if !ok || len(blocks) == 0 {
		payload := publicPayloadWithType(object, "agent.message")
		if schema.Message.ID != "" {
			payload["id"] = maevents.StableAssistantEventID(codeSessionID, schema.Message.ID, assistantContentBlockIndex(schema, 0, 1), "agent.message")
		}
		return []publicPayloadCandidate{{payload: payload}}
	}
	candidates := make([]publicPayloadCandidate, 0, len(blocks))
	for index, value := range blocks {
		contentBlockIndex := assistantContentBlockIndex(schema, index, len(blocks))
		block, ok := value.(map[string]any)
		if !ok {
			payload := publicPayloadWithSingleContentBlock(object, "agent.message", value)
			if schema.Message.ID != "" {
				payload["id"] = maevents.StableAssistantEventID(codeSessionID, schema.Message.ID, contentBlockIndex, "agent.message")
			}
			candidates = append(candidates, publicPayloadCandidate{
				payload:    payload,
				seedSuffix: fmt.Sprintf("content:%d", index),
				timeOffset: time.Duration(index) * time.Millisecond,
			})
			continue
		}
		blockType := stringField(block, "type")
		if blockType == "tool_use" {
			continue
		}
		eventType := "agent.message"
		switch blockType {
		case "thinking":
			eventType = "agent.thinking"
		}
		payload := publicPayloadWithSingleContentBlock(object, eventType, block)
		if schema.Message.ID != "" && (eventType == "agent.message" || eventType == "agent.thinking") {
			payload["id"] = maevents.StableAssistantEventID(codeSessionID, schema.Message.ID, contentBlockIndex, eventType)
		}
		candidates = append(candidates, publicPayloadCandidate{
			payload:    payload,
			seedSuffix: fmt.Sprintf("content:%d:%s:%s", index, blockType, stringField(block, "id")),
			timeOffset: time.Duration(index) * time.Millisecond,
		})
	}
	return candidates
}

func assistantContentBlockIndex(schema workerAssistantOutputPayload, fallback, blockCount int) int {
	if blockCount == 1 && schema.ContentBlockIndex != nil && *schema.ContentBlockIndex >= 0 {
		return *schema.ContentBlockIndex
	}
	return fallback
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

func userPublicPayloadCandidates(codeSessionID string, object map[string]any, schema workerUserOutputPayload) []publicPayloadCandidate {
	content := publicContentBlocks(workerOutputContent(schema.Content, schema.Message.Content))
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
		providerToolUseID := stringField(block, "tool_use_id")
		if providerToolUseID == "" {
			continue
		}
		toolUseID := toolUsePublicEventID(codeSessionID, providerToolUseID)
		eventType := "agent.tool_result"
		if claudeToolResultIsAgentThreadMessage(block) {
			eventType = "agent.thread_message_received"
		}
		payload := publicPayloadWithType(object, eventType)
		if eventType == "agent.thread_message_received" {
			payload["from_session_thread_id"] = claudeTaskThreadIDFromKey(codeSessionID, providerToolUseID)
		}
		payload["tool_use_id"] = toolUseID
		payload["content"] = claudeToolResultContent(block)
		if isError, ok := block["is_error"]; ok {
			payload["is_error"] = isError
		}
		delete(payload, "message")
		delete(payload, "parent_tool_use_id")
		candidates = append(candidates, publicPayloadCandidate{
			payload:    payload,
			seedSuffix: fmt.Sprintf("user_tool_result:%d:%s", index, providerToolUseID),
			timeOffset: time.Duration(index) * time.Millisecond,
		})
	}
	return candidates
}

func internalSubagentUserPayloadCandidates(object map[string]any, schema workerUserOutputPayload) []publicPayloadCandidate {
	content := publicContentBlocks(workerOutputContent(schema.Content, schema.Message.Content))
	if content == nil {
		return nil
	}
	payload := publicPayloadWithType(object, "user.message")
	payload["content"] = content
	delete(payload, "message")
	delete(payload, "parent_tool_use_id")
	return []publicPayloadCandidate{{payload: payload, seedSuffix: "internal_subagent:user_message"}}
}

func systemPublicPayloadCandidates(codeSessionID string, object map[string]any, schema workerSystemOutputPayload) []publicPayloadCandidate {
	switch schema.Subtype {
	case "task_started":
		threadID := claudeTaskThreadIDFromFields(codeSessionID, schema.ToolUseID, schema.TaskID)
		if threadID == "" {
			return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}
		}
		agentName := firstNonEmpty(schema.Description, schema.TaskType, "subagent")
		content := claudeTaskContentFromFields(schema.Prompt, schema.Summary)
		created := publicPayloadWithType(object, "session.thread_created")
		created["session_thread_id"] = threadID
		created["agent_name"] = agentName
		created["task_id"] = schema.TaskID
		created["tool_use_id"] = schema.ToolUseID
		running := publicPayloadWithType(object, "session.thread_status_running")
		running["session_thread_id"] = threadID
		running["agent_name"] = agentName
		running["task_id"] = schema.TaskID
		running["tool_use_id"] = schema.ToolUseID
		sent := publicPayloadWithType(object, "agent.thread_message_sent")
		sent["to_session_thread_id"] = threadID
		sent["to_agent_name"] = agentName
		sent["task_id"] = schema.TaskID
		sent["tool_use_id"] = schema.ToolUseID
		if len(content) > 0 {
			sent["content"] = content
		}
		return []publicPayloadCandidate{
			{payload: created, seedSuffix: "task_started:thread_created:" + threadID},
			{payload: running, seedSuffix: "task_started:thread_running:" + threadID, timeOffset: time.Millisecond},
			{payload: sent, seedSuffix: "task_started:message_sent:" + threadID, timeOffset: 2 * time.Millisecond},
		}
	case "task_notification":
		threadID := claudeTaskThreadIDFromFields(codeSessionID, schema.ToolUseID, schema.TaskID)
		if threadID == "" {
			return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}
		}
		statusEventType := "session.thread_status_idle"
		workerStatus := strings.ToLower(schema.Status)
		failed := workerStatus == "failed" || workerStatus == "error" || workerStatus == "terminated"
		if failed {
			statusEventType = "session.thread_status_terminated"
		}
		status := publicPayloadWithType(object, statusEventType)
		status["session_thread_id"] = threadID
		status["task_id"] = schema.TaskID
		status["tool_use_id"] = schema.ToolUseID
		status["stop_reason"] = map[string]any{
			"type":   firstNonEmpty(schema.Status, "completed"),
			"detail": schema.Summary,
		}
		if !failed {
			return []publicPayloadCandidate{{payload: status, seedSuffix: "task_notification:thread_status:" + threadID}}
		}
		// error.type 与 stop_reason.type 同源（schema.Status），保持两事件表述一致。
		errorEvent := publicPayloadWithType(object, "session.error")
		errorEvent["session_thread_id"] = threadID
		errorEvent["task_id"] = schema.TaskID
		errorEvent["tool_use_id"] = schema.ToolUseID
		errorEvent["error"] = map[string]any{
			"type":         workerStatus,
			"retry_status": "not_retryable",
			"message":      firstNonEmpty(schema.Summary, "task "+workerStatus),
		}
		return []publicPayloadCandidate{
			{payload: errorEvent, seedSuffix: "task_notification:error:" + threadID},
			{payload: status, seedSuffix: "task_notification:thread_status:" + threadID, timeOffset: time.Millisecond},
		}
	default:
		return []publicPayloadCandidate{{payload: publicPayloadWithType(object, "system.message")}}
	}
}

func claudeTaskThreadIDFromFields(codeSessionID, toolUseID, taskID string) string {
	key := firstNonEmpty(toolUseID, taskID)
	if key == "" {
		return ""
	}
	return claudeTaskThreadIDFromKey(codeSessionID, key)
}

func claudeTaskThreadIDFromKey(codeSessionID string, key string) string {
	return maevents.ClaudeTaskThreadID(codeSessionID, key)
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

func claudeTaskContentFromFields(prompt, summary string) []any {
	if prompt != "" {
		return []any{map[string]any{"type": "text", "text": prompt}}
	}
	if summary != "" {
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

func workerOutputContent(content, messageContent json.RawMessage) any {
	value := decodeWorkerOutputValue(content)
	if value != nil {
		return value
	}
	return decodeWorkerOutputValue(messageContent)
}

func decodeWorkerOutputValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func decodeRawJSONObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty payload", ErrProtocol)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%w: payload must be a json object", ErrProtocol)
	}
	return fields, nil
}

func materializePublicPayload(fields map[string]json.RawMessage) map[string]any {
	// This map is an output-only serialization document. Known worker fields are
	// read from the typed schemas above; raw unknown fields are materialized only
	// so public-event normalization can preserve them while adding output fields.
	payload := make(map[string]any, len(fields))
	for field, raw := range fields {
		payload[field] = decodeWorkerOutputValue(raw)
	}
	return payload
}

func setRawJSONField(fields map[string]json.RawMessage, field string, value any) {
	raw, err := json.Marshal(value)
	if err == nil {
		fields[field] = raw
	}
}

func firstWorkerPayloadTime(payload workerOutputCommonPayload, fallback time.Time) time.Time {
	for _, value := range []string{payload.ProcessedAt, payload.CreatedAt, payload.Timestamp} {
		if parsed := timeFromString(value); !parsed.IsZero() {
			return parsed
		}
	}
	var data struct {
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(payload.Data, &data) == nil {
		if parsed := timeFromString(data.Timestamp); !parsed.IsZero() {
			return parsed
		}
	}
	return fallback.UTC()
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
