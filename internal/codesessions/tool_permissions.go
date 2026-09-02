package codesessions

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type resolvedToolPermission string

const (
	resolvedToolPermissionAllow resolvedToolPermission = "allow"
	resolvedToolPermissionAsk   resolvedToolPermission = "ask"
	resolvedToolPermissionDeny  resolvedToolPermission = "deny"
)

type toolIdentity struct {
	Kind       string
	ServerName string
	ToolName   string
}

type toolPermissionRequest struct {
	PublicEventID   string         `json:"public_event_id"`
	EventType       string         `json:"event_type"`
	ToolName        string         `json:"tool_name"`
	RequestID       string         `json:"request_id"`
	ToolUseID       string         `json:"provider_tool_use_id"`
	SessionThreadID string         `json:"session_thread_id,omitempty"`
	Input           map[string]any `json:"input"`
}

func (s *Service) handleToolPermissionRequest(ctx context.Context, codeSessionID string, workerEpoch int64, payload *workerControlRequestPayload, meta EventMetadata) error {
	if s == nil {
		return nil
	}
	toolName := firstNonEmpty(payload.Request.ToolName, payload.ToolName)
	permission, identity, err := s.resolveToolPermission(ctx, codeSessionID, toolName)
	if err != nil {
		s.logger.ErrorContext(ctx, "resolve tool permission", "code_session_id", codeSessionID, "tool_name", toolName, "error", err)
		return nil
	}
	s.logger.InfoContext(ctx, "resolved tool permission", "code_session_id", codeSessionID, "tool_name", toolName, "tool_kind", identity.Kind, "server_name", identity.ServerName, "normalized_tool_name", identity.ToolName, "permission", permission)
	request, payloads, err := toolPermissionPublicPayloads(codeSessionID, payload, meta, identity, permission)
	if err != nil {
		return err
	}
	if len(payloads) == 0 {
		return nil
	}
	if permission == resolvedToolPermissionAsk {
		if err := s.persistToolPermissionRequest(ctx, codeSessionID, workerEpoch, request); err != nil {
			return err
		}
	}
	if err := s.publishWorkerPublicPayloads(ctx, codeSessionID, payloads); err != nil {
		return err
	}
	switch permission {
	case resolvedToolPermissionAllow:
		return s.respondToToolPermissionRequest(ctx, codeSessionID, request, permission, "auto-approve", "", "")
	case resolvedToolPermissionDeny:
		return s.respondToToolPermissionRequest(ctx, codeSessionID, request, permission, "auto-deny", "", "")
	case resolvedToolPermissionAsk:
		return nil
	default:
		return nil
	}
}

func toolPermissionRequestFromWorkerEvent(payload *workerControlRequestPayload, meta EventMetadata) toolPermissionRequest {
	return toolPermissionRequest{
		ToolName:        firstNonEmpty(payload.Request.ToolName, payload.ToolName),
		RequestID:       firstNonEmpty(requestIDString(meta.RequestID), payload.RequestID),
		ToolUseID:       payload.Request.ToolUseID,
		SessionThreadID: workerOutputSessionThreadID(payload),
		Input:           payload.Request.Input,
	}
}

func (s *Service) resolveToolPermission(ctx context.Context, codeSessionID string, claudeToolName string) (resolvedToolPermission, toolIdentity, error) {
	codeSession, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return resolvedToolPermissionAsk, parseClaudeToolIdentity(claudeToolName), err
	}
	if !found {
		return resolvedToolPermissionAsk, parseClaudeToolIdentity(claudeToolName), db.ErrNotFound
	}
	session, found, err := s.db.GetSession(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return resolvedToolPermissionAsk, parseClaudeToolIdentity(claudeToolName), err
	}
	if !found {
		return resolvedToolPermissionAsk, parseClaudeToolIdentity(claudeToolName), db.ErrNotFound
	}
	permission, identity := resolveToolPermissionAndIdentityFromAgentSnapshot(session.AgentSnapshot, claudeToolName)
	return permission, identity, nil
}

func resolveToolPermissionFromAgentSnapshot(agentSnapshot json.RawMessage, claudeToolName string) resolvedToolPermission {
	permission, _ := resolveToolPermissionAndIdentityFromAgentSnapshot(agentSnapshot, claudeToolName)
	return permission
}

func resolveToolPermissionAndIdentityFromAgentSnapshot(agentSnapshot json.RawMessage, claudeToolName string) (resolvedToolPermission, toolIdentity) {
	snapshot := rawObject(agentSnapshot)
	tools := arrayField(snapshot, "tools")
	identity := parseClaudeToolIdentity(claudeToolName)
	switch identity.Kind {
	case "mcp":
		return resolveMCPToolPermission(tools, identity.ServerName, identity.ToolName), identity
	case "agent_toolset":
		return resolveAgentToolPermission(tools, identity.ToolName), identity
	default:
		return resolvedToolPermissionAsk, identity
	}
}

func parseClaudeToolIdentity(toolName string) toolIdentity {
	toolName = strings.TrimSpace(toolName)
	if after, ok := strings.CutPrefix(toolName, "mcp__"); ok {
		server, tool, found := strings.Cut(after, "__")
		if found && strings.TrimSpace(server) != "" && strings.TrimSpace(tool) != "" {
			return toolIdentity{Kind: "mcp", ServerName: strings.TrimSpace(server), ToolName: strings.TrimSpace(tool)}
		}
	}
	if managedName := managedAgentToolName(toolName); managedName != "" {
		return toolIdentity{Kind: "agent_toolset", ToolName: managedName}
	}
	return toolIdentity{Kind: "unknown", ToolName: toolName}
}

func managedAgentToolName(claudeToolName string) string {
	switch strings.ToLower(strings.TrimSpace(claudeToolName)) {
	case "task", "agent":
		return "task"
	case "askuserquestion", "ask_user_question":
		return "ask_user_question"
	case "bash":
		return "bash"
	case "croncreate", "cron_create":
		return "cron_create"
	case "crondelete", "cron_delete":
		return "cron_delete"
	case "cronlist", "cron_list":
		return "cron_list"
	case "edit", "multiedit":
		return "edit"
	case "enterplanmode", "enter_plan_mode":
		return "enter_plan_mode"
	case "enterworktree", "enter_worktree":
		return "enter_worktree"
	case "exitplanmode", "exit_plan_mode":
		return "exit_plan_mode"
	case "exitworktree", "exit_worktree":
		return "exit_worktree"
	case "read":
		return "read"
	case "write":
		return "write"
	case "glob":
		return "glob"
	case "grep":
		return "grep"
	case "notebookedit", "notebook_edit":
		return "notebook_edit"
	case "schedulewakeup", "schedule_wakeup":
		return "schedule_wakeup"
	case "skill":
		return "skill"
	case "taskoutput", "task_output":
		return "task_output"
	case "taskstop", "task_stop":
		return "task_stop"
	case "todowrite", "todo_write":
		return "todo_write"
	case "webfetch", "web_fetch":
		return "web_fetch"
	case "websearch", "web_search":
		return "web_search"
	default:
		return ""
	}
}

func resolveMCPToolPermission(tools []any, serverName string, toolName string) resolvedToolPermission {
	for _, value := range tools {
		toolset, ok := value.(map[string]any)
		if !ok || stringField(toolset, "type") != "mcp_toolset" || stringField(toolset, "mcp_server_name") != serverName {
			continue
		}
		if config, ok := findToolConfig(toolset["configs"], toolName); ok {
			return permissionFromToolConfig(config, "always_ask")
		}
		return permissionFromToolConfig(objectField(toolset, "default_config"), "always_ask")
	}
	return resolvedToolPermissionAsk
}

func resolveAgentToolPermission(tools []any, toolName string) resolvedToolPermission {
	for _, value := range tools {
		toolset, ok := value.(map[string]any)
		if !ok || stringField(toolset, "type") != "agent_toolset_20260401" {
			continue
		}
		if config, ok := findToolConfig(toolset["configs"], toolName); ok {
			return permissionFromToolConfig(config, "always_allow")
		}
		return permissionFromToolConfig(objectField(toolset, "default_config"), "always_allow")
	}
	return resolvedToolPermissionAllow
}

func permissionFromToolConfig(config map[string]any, fallbackPolicy string) resolvedToolPermission {
	if enabled, ok := config["enabled"].(bool); ok && !enabled {
		return resolvedToolPermissionDeny
	}
	policy := fallbackPolicy
	if object := objectField(config, "permission_policy"); len(object) > 0 {
		if policyType := stringField(object, "type"); policyType != "" {
			policy = policyType
		}
	}
	switch policy {
	case "always_allow", "allow":
		return resolvedToolPermissionAllow
	case "always_ask", "ask":
		return resolvedToolPermissionAsk
	default:
		return resolvedToolPermissionAsk
	}
}

func findToolConfig(value any, toolName string) (map[string]any, bool) {
	toolName = strings.TrimSpace(toolName)
	for _, item := range arrayValue(value) {
		config, ok := item.(map[string]any)
		if !ok || stringField(config, "name") != toolName {
			continue
		}
		return config, true
	}
	return nil, false
}

func objectField(object map[string]any, field string) map[string]any {
	if object == nil {
		return map[string]any{}
	}
	nested, _ := object[field].(map[string]any)
	if nested == nil {
		return map[string]any{}
	}
	return nested
}

func toolPermissionSessionThreadID(object map[string]any) string {
	request := objectField(object, "request")
	data := objectField(object, "data")
	metadata := objectField(object, "metadata")
	return firstNonEmpty(
		stringField(object, "session_thread_id"),
		stringField(object, "thread_id"),
		stringField(request, "session_thread_id"),
		stringField(request, "thread_id"),
		stringField(data, "session_thread_id"),
		stringField(data, "thread_id"),
		stringField(metadata, "session_thread_id"),
		stringField(metadata, "thread_id"),
	)
}

func workerOutputSessionThreadID(payload *workerControlRequestPayload) string {
	return firstNonEmpty(
		payload.SessionThreadID,
		payload.ThreadID,
		payload.Request.SessionThreadID,
		payload.Request.ThreadID,
		payload.Data.SessionThreadID,
		payload.Data.ThreadID,
		payload.Metadata.SessionThreadID,
		payload.Metadata.ThreadID,
	)
}

func arrayField(object map[string]any, field string) []any {
	if object == nil {
		return nil
	}
	return arrayValue(object[field])
}

func arrayValue(value any) []any {
	items, _ := value.([]any)
	return items
}

func (s *Service) queueControlResponseForToolConfirmation(ctx context.Context, codeSession db.CodeSession, event db.SessionEvent) (bool, error) {
	payload := rawObject(event.Payload)
	toolUseID := stringField(payload, "tool_use_id")
	if toolUseID == "" {
		return false, nil
	}
	request, err := toolPermissionRequestFromMetadata(codeSession.WorkerExternalMetadata, toolUseID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if request.EventType == "agent.custom_tool_use" {
		return false, nil
	}
	var behavior resolvedToolPermission
	switch stringField(payload, "result") {
	case "allow":
		behavior = resolvedToolPermissionAllow
	case "deny":
		behavior = resolvedToolPermissionDeny
	default:
		return false, nil
	}
	denyMessage := stringField(payload, "deny_message")
	sessionThreadID := firstNonEmpty(toolPermissionSessionThreadID(payload), request.SessionThreadID)
	if err := s.respondToToolPermissionRequest(ctx, codeSession.ExternalID, request, behavior, "tool-confirmation", denyMessage, sessionThreadID); err != nil {
		return false, err
	}
	if err := s.clearToolPermissionRequest(ctx, codeSession.ExternalID, codeSession.CurrentWorkerEpoch, request.PublicEventID); err != nil {
		return false, err
	}
	return true, nil
}

const legacyToolPermissionRequestMetadataKey = "managed_agent_tool_permission_request"

func toolPermissionRequestMetadataKey(publicEventID string) string {
	return legacyToolPermissionRequestMetadataKey + ":" + publicEventID
}

func (s *Service) persistToolPermissionRequest(ctx context.Context, codeSessionID string, workerEpoch int64, request toolPermissionRequest) error {
	if workerEpoch <= 0 {
		codeSession, found, err := s.db.GetCodeSession(ctx, codeSessionID)
		if err != nil {
			return err
		}
		if !found {
			return db.ErrNotFound
		}
		workerEpoch = codeSession.CurrentWorkerEpoch
	}
	metadata, err := marshalRaw(map[string]any{toolPermissionRequestMetadataKey(request.PublicEventID): request})
	if err != nil {
		return err
	}
	_, err = s.db.UpdateCodeSessionWorkerState(ctx, codeSessionID, db.UpdateCodeSessionWorkerStateInput{
		WorkerEpoch:         workerEpoch,
		ExternalMetadataSet: true,
		ExternalMetadata:    metadata,
	})
	return err
}

func toolPermissionRequestFromMetadata(raw json.RawMessage, publicEventID string) (toolPermissionRequest, error) {
	if len(raw) == 0 {
		return toolPermissionRequest{}, db.ErrNotFound
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return toolPermissionRequest{}, err
	}
	requestRaw := metadata[toolPermissionRequestMetadataKey(publicEventID)]
	if len(requestRaw) == 0 {
		requestRaw = metadata[legacyToolPermissionRequestMetadataKey]
	}
	var request toolPermissionRequest
	if len(requestRaw) == 0 {
		return toolPermissionRequest{}, db.ErrNotFound
	}
	if err := json.Unmarshal(requestRaw, &request); err != nil {
		return toolPermissionRequest{}, err
	}
	if request.PublicEventID != publicEventID || request.RequestID == "" || request.ToolUseID == "" {
		return toolPermissionRequest{}, db.ErrNotFound
	}
	return request, nil
}

func (s *Service) clearToolPermissionRequest(ctx context.Context, codeSessionID string, workerEpoch int64, publicEventID string) error {
	metadata, err := marshalRaw(map[string]any{toolPermissionRequestMetadataKey(publicEventID): nil})
	if err != nil {
		return err
	}
	_, err = s.db.UpdateCodeSessionWorkerState(ctx, codeSessionID, db.UpdateCodeSessionWorkerStateInput{
		WorkerEpoch:         workerEpoch,
		ExternalMetadataSet: true,
		ExternalMetadata:    metadata,
	})
	return err
}

type userCustomToolResultPayload struct {
	CustomToolUseID string `json:"custom_tool_use_id"`
	Content         []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError         bool   `json:"is_error"`
	SessionThreadID string `json:"session_thread_id"`
}

func (s *Service) queueControlResponseForCustomToolResult(ctx context.Context, codeSession db.CodeSession, event db.SessionEvent) (bool, error) {
	var payload userCustomToolResultPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, err
	}
	request, err := toolPermissionRequestFromMetadata(codeSession.WorkerExternalMetadata, payload.CustomToolUseID)
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if request.EventType != "agent.custom_tool_use" {
		return false, nil
	}
	behavior := resolvedToolPermissionAllow
	denyMessage := ""
	if payload.IsError {
		behavior = resolvedToolPermissionDeny
		denyMessage = customToolResultText(payload)
	} else {
		answers, err := customToolResultAnswers(payload)
		if err != nil {
			return false, err
		}
		request.Input = cloneStringAnyMap(request.Input)
		request.Input["answers"] = answers
	}
	sessionThreadID := firstNonEmpty(payload.SessionThreadID, request.SessionThreadID)
	if err := s.respondToToolPermissionRequest(ctx, codeSession.ExternalID, request, behavior, "custom-tool-result", denyMessage, sessionThreadID); err != nil {
		return false, err
	}
	if err := s.clearToolPermissionRequest(ctx, codeSession.ExternalID, codeSession.CurrentWorkerEpoch, request.PublicEventID); err != nil {
		return false, err
	}
	return true, nil
}

func customToolResultAnswers(payload userCustomToolResultPayload) (map[string]any, error) {
	text := customToolResultText(payload)
	if text == "" {
		return map[string]any{}, nil
	}
	var answers map[string]any
	if err := json.Unmarshal([]byte(text), &answers); err != nil || answers == nil {
		return nil, ErrProtocol
	}
	return answers, nil
}

func customToolResultText(payload userCustomToolResultPayload) string {
	parts := make([]string, 0, len(payload.Content))
	for _, block := range payload.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func cloneStringAnyMap(value map[string]any) map[string]any {
	cloned := maps.Clone(value)
	if cloned == nil {
		return map[string]any{}
	}
	return cloned
}

func (s *Service) respondToToolPermissionRequest(ctx context.Context, codeSessionID string, request toolPermissionRequest, behavior resolvedToolPermission, source string, denyMessage string, sessionThreadID string) error {
	if request.RequestID == "" {
		return nil
	}
	if request.Input == nil {
		request.Input = map[string]any{}
	}
	response := map[string]any{
		"behavior":     string(behavior),
		"updatedInput": request.Input,
	}
	if request.ToolUseID != "" {
		response["toolUseID"] = request.ToolUseID
	}
	sessionThreadID = firstNonEmpty(sessionThreadID, request.SessionThreadID)
	if sessionThreadID != "" {
		response["sessionThreadID"] = sessionThreadID
		response["session_thread_id"] = sessionThreadID
	}
	if behavior == resolvedToolPermissionDeny {
		if strings.TrimSpace(denyMessage) == "" {
			denyMessage = "Tool is denied by the agent permission policy."
		}
		response["message"] = denyMessage
		response["denyMessage"] = denyMessage
	}
	now := time.Now().UTC()
	payloadObject := map[string]any{
		"type":       "control_response",
		"uuid":       controlResponseUUID(codeSessionID, request.RequestID),
		"session_id": codeSessionID,
		"created_at": formatTime(now),
		"timestamp":  formatTime(now),
		"response": map[string]any{
			"subtype":    "success",
			"request_id": request.RequestID,
			"response":   response,
		},
	}
	if sessionThreadID != "" {
		payloadObject["session_thread_id"] = sessionThreadID
	}
	payload, err := marshalRaw(payloadObject)
	if err != nil {
		return err
	}
	// 持久化入站队列是唯一投递路径；当前 CCR v2 worker 通过按 epoch
	// 隔离的事件流接收该响应。
	_, duplicate, err := s.appendInboundPayload(ctx, codeSessionID, payload, source)
	if err != nil || duplicate {
		return err
	}
	return nil
}

// controlResponseUUID preserves the UUIDv5 output previously produced with the
// OID namespace. SHA-1 is required by UUIDv5 and is not used for security here.
func controlResponseUUID(codeSessionID, requestID string) string {
	namespace := uuid.UUID{0x6b, 0xa7, 0xb8, 0x12, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	name := codeSessionID + "\x00control_response\x00" + requestID
	payload := make([]byte, 0, len(namespace)+len(name))
	payload = append(payload, namespace[:]...)
	payload = append(payload, name...)
	digest := sha1.Sum(payload)

	var result uuid.UUID
	copy(result[:], digest[:])
	result[6] = (result[6] & 0x0f) | 0x50
	result[8] = (result[8] & 0x3f) | 0x80
	return result.String()
}

// toolUsePublicEventID translates the private provider tool id into the stable
// public event id used by confirmations and tool results.
func toolUsePublicEventID(codeSessionID string, toolUseID string) string {
	return stablePublicEventID(codeSessionID, "tool_use\x00"+toolUseID)
}

func toolPermissionPublicPayloads(codeSessionID string, payload *workerControlRequestPayload, meta EventMetadata, identity toolIdentity, permission resolvedToolPermission) (toolPermissionRequest, []json.RawMessage, error) {
	request := toolPermissionRequestFromWorkerEvent(payload, meta)
	if request.ToolName == "" || request.ToolUseID == "" || request.RequestID == "" {
		return request, nil, nil
	}
	eventType, publicName := toolPermissionPublicIdentity(request.ToolName, identity)
	toolEventID := toolUsePublicEventID(codeSessionID, request.ToolUseID)
	request.PublicEventID = toolEventID
	request.EventType = eventType
	if request.SessionThreadID != "" {
		request.PublicEventID = derivedPrimarySessionEventID(codeSessionID, toolEventID, eventType)
	}
	now := time.Now().UTC()
	toolPayload := map[string]any{
		"id":           toolEventID,
		"type":         eventType,
		"name":         publicName,
		"input":        cloneStringAnyMap(request.Input),
		"processed_at": formatTime(now),
	}
	if eventType != "agent.custom_tool_use" {
		toolPayload["evaluated_permission"] = string(permission)
	}
	if eventType == "agent.mcp_tool_use" {
		toolPayload["mcp_server_name"] = identity.ServerName
	}
	if request.SessionThreadID != "" {
		toolPayload["session_thread_id"] = request.SessionThreadID
	}
	toolRaw, err := marshalRaw(toolPayload)
	if err != nil {
		return toolPermissionRequest{}, nil, err
	}
	payloads := []json.RawMessage{toolRaw}
	if permission != resolvedToolPermissionAsk {
		return request, payloads, nil
	}
	statusTime := now.Add(time.Millisecond)
	statusRaw, err := marshalRaw(map[string]any{
		"id":   stablePublicEventID(codeSessionID, request.RequestID+"\x00tool_permission_requires_action"),
		"type": "session.status_idle",
		"stop_reason": map[string]any{
			"event_ids": []string{request.PublicEventID},
			"type":      "requires_action",
		},
		"processed_at": formatTime(statusTime),
	})
	if err != nil {
		return toolPermissionRequest{}, nil, err
	}
	return request, append(payloads, statusRaw), nil
}

func toolPermissionPublicIdentity(toolName string, identity toolIdentity) (string, string) {
	if strings.EqualFold(toolName, "AskUserQuestion") {
		return "agent.custom_tool_use", "AskUserQuestion"
	}
	if identity.Kind == "mcp" {
		return "agent.mcp_tool_use", identity.ToolName
	}
	if identity.Kind == "agent_toolset" {
		return "agent.tool_use", identity.ToolName
	}
	return "agent.tool_use", toolName
}
