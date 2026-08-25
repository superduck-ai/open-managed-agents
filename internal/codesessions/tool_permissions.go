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
	RequestID       string
	ToolUseID       string
	SessionThreadID string
	Input           map[string]any
}

func (s *Service) handleToolPermissionRequest(ctx context.Context, codeSessionID string, payload *workerControlRequestPayload, meta EventMetadata) error {
	if s == nil {
		return nil
	}
	toolName := payload.Request.ToolName
	permission, identity, err := s.resolveToolPermission(ctx, codeSessionID, toolName)
	if err != nil {
		s.logger.ErrorContext(ctx, "resolve tool permission", "code_session_id", codeSessionID, "tool_name", toolName, "error", err)
		return nil
	}
	s.logger.InfoContext(ctx, "resolved tool permission", "code_session_id", codeSessionID, "tool_name", toolName, "tool_kind", identity.Kind, "server_name", identity.ServerName, "normalized_tool_name", identity.ToolName, "permission", permission)
	switch permission {
	case resolvedToolPermissionAllow:
		return s.respondToToolPermissionRequest(ctx, codeSessionID, toolPermissionRequestFromWorkerEvent(payload, meta), permission, "auto-approve", "", "")
	case resolvedToolPermissionDeny:
		return s.respondToToolPermissionRequest(ctx, codeSessionID, toolPermissionRequestFromWorkerEvent(payload, meta), permission, "auto-deny", "", "")
	case resolvedToolPermissionAsk:
		return s.publishToolPermissionRequiresAction(ctx, codeSessionID, payload, meta, identity)
	default:
		return nil
	}
}

func toolPermissionRequestFromWorkerEvent(payload *workerControlRequestPayload, meta EventMetadata) toolPermissionRequest {
	return toolPermissionRequest{
		RequestID:       firstNonEmpty(requestIDString(meta.RequestID), payload.RequestID),
		ToolUseID:       payload.Request.ToolUseID,
		SessionThreadID: workerOutputSessionThreadID(payload),
		Input:           payload.Request.Input,
	}
}

func toolPermissionRequestFromSessionEvent(event db.SessionEvent) toolPermissionRequest {
	payload := rawObject(event.Payload)
	return toolPermissionRequest{
		RequestID:       stringField(payload, "request_id"),
		ToolUseID:       stringField(payload, "tool_use_id"),
		SessionThreadID: toolPermissionSessionThreadID(payload),
		Input:           objectField(payload, "input"),
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
	return resolveToolPermissionFromAgentSnapshot(session.AgentSnapshot, claudeToolName), parseClaudeToolIdentity(claudeToolName), nil
}

func resolveToolPermissionFromAgentSnapshot(agentSnapshot json.RawMessage, claudeToolName string) resolvedToolPermission {
	snapshot := rawObject(agentSnapshot)
	tools := arrayField(snapshot, "tools")
	identity := parseClaudeToolIdentity(claudeToolName)
	switch identity.Kind {
	case "mcp":
		return resolveMCPToolPermission(tools, identity.ServerName, identity.ToolName)
	case "agent_toolset":
		return resolveAgentToolPermission(tools, identity.ToolName)
	default:
		return resolvedToolPermissionAsk
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
	case "bash":
		return "bash"
	case "edit", "multiedit":
		return "edit"
	case "read":
		return "read"
	case "write":
		return "write"
	case "glob":
		return "glob"
	case "grep":
		return "grep"
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
	request, err := s.toolPermissionRequestForConfirmation(ctx, codeSession, toolUseID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
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
	if behavior == resolvedToolPermissionAllow {
		request.Input = toolConfirmationUpdatedInput(request.Input, payload)
	}
	if err := s.respondToToolPermissionRequest(ctx, codeSession.ExternalID, request, behavior, "tool-confirmation", denyMessage, sessionThreadID); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) toolPermissionRequestForConfirmation(ctx context.Context, codeSession db.CodeSession, toolUseID string) (toolPermissionRequest, error) {
	publicEvent, err := s.db.GetSessionToolPermissionRequest(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, toolUseID)
	if err != nil {
		return toolPermissionRequest{}, err
	}
	request := toolPermissionRequestFromSessionEvent(publicEvent)
	if request.RequestID == "" {
		return toolPermissionRequest{}, db.ErrNotFound
	}
	return request, nil
}

func toolConfirmationUpdatedInput(original map[string]any, confirmation map[string]any) map[string]any {
	input := cloneStringAnyMap(original)
	if updated := objectField(confirmation, "updated_input"); len(updated) > 0 {
		input = cloneStringAnyMap(updated)
	}
	if confirmation != nil {
		if answers, ok := confirmation["answers"].(map[string]any); ok && answers != nil {
			input["answers"] = cloneStringAnyMap(answers)
		}
	}
	return input
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

func (s *Service) publishToolPermissionRequiresAction(ctx context.Context, codeSessionID string, payload *workerControlRequestPayload, meta EventMetadata, identity toolIdentity) error {
	toolName := firstNonEmpty(payload.Request.ToolName, payload.ToolName)
	if toolName == "" {
		return nil
	}
	toolUseID := payload.Request.ToolUseID
	requestID := requestIDString(meta.RequestID)
	if requestID == "" {
		requestID = payload.RequestID
	}
	now := time.Now().UTC()
	seed := firstNonEmpty(meta.IdempotencyKey, meta.PayloadHash, requestID, toolUseID, toolName)
	toolEventType := "agent.tool_use"
	if identity.Kind == "mcp" {
		toolEventType = "agent.mcp_tool_use"
	}
	toolEventID := stablePublicEventID(codeSessionID, seed+"\x00tool_permission_request")
	toolPayload := map[string]any{
		"id":                   toolEventID,
		"uuid":                 stablePublicEventID(codeSessionID, seed+"\x00tool_permission_request_uuid"),
		"type":                 toolEventType,
		"name":                 toolName,
		"tool_name":            toolName,
		"evaluated_permission": string(resolvedToolPermissionAsk),
		"created_at":           formatTime(now),
		"processed_at":         formatTime(now),
	}
	if toolUseID != "" {
		toolPayload["tool_use_id"] = toolUseID
	}
	if requestID != "" {
		toolPayload["request_id"] = requestID
	}
	sessionThreadID := workerOutputSessionThreadID(payload)
	if sessionThreadID != "" {
		toolPayload["session_thread_id"] = sessionThreadID
	}
	if payload.Request.Input != nil {
		toolPayload["input"] = payload.Request.Input
	}
	if identity.Kind == "mcp" {
		toolPayload["mcp_server_name"] = identity.ServerName
		toolPayload["mcp_tool_name"] = identity.ToolName
	}
	blockingEventID := toolEventID
	if sessionThreadID != "" {
		blockingEventID = derivedPrimarySessionEventID(codeSessionID, toolEventID, toolEventType)
	}
	stopReason := map[string]any{
		"event_ids": []string{blockingEventID},
		"type":      "requires_action",
	}
	requiresActionDetails := map[string]any{
		"event_ids": []string{blockingEventID},
		"type":      "requires_action",
		"tool_name": toolName,
	}
	if toolUseID != "" {
		requiresActionDetails["tool_use_id"] = toolUseID
	}
	if sessionThreadID != "" {
		requiresActionDetails["session_thread_id"] = sessionThreadID
	}
	if requestID != "" {
		requiresActionDetails["request_id"] = requestID
	}
	statusTime := now.Add(time.Millisecond)
	statusPayload := map[string]any{
		"id":                      stablePublicEventID(codeSessionID, seed+"\x00tool_permission_requires_action"),
		"uuid":                    stablePublicEventID(codeSessionID, seed+"\x00tool_permission_requires_action_uuid"),
		"type":                    "session.status_idle",
		"stop_reason":             stopReason,
		"requires_action_details": requiresActionDetails,
		"created_at":              formatTime(statusTime),
		"processed_at":            formatTime(statusTime),
	}
	payloads := make([]json.RawMessage, 0, 2)
	for _, payload := range []map[string]any{toolPayload, statusPayload} {
		raw, err := marshalRaw(payload)
		if err != nil {
			return err
		}
		payloads = append(payloads, raw)
	}
	return s.publishWorkerPublicPayloads(ctx, codeSessionID, payloads)
}
