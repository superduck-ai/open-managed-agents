package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/google/uuid"
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

func (s *Service) handleToolPermissionRequest(ctx context.Context, codeSessionID string, object map[string]any, meta EventMetadata) error {
	if s == nil {
		return nil
	}
	request, _ := object["request"].(map[string]any)
	toolName := stringField(request, "tool_name")
	permission, identity, err := s.resolveToolPermission(ctx, codeSessionID, toolName)
	if err != nil {
		log.Printf("resolve tool permission code_session_id=%s tool_name=%q: %v", codeSessionID, toolName, err)
		return nil
	}
	log.Printf("resolved tool permission code_session_id=%s tool_name=%q kind=%s server=%s normalized_tool=%s permission=%s",
		codeSessionID, toolName, identity.Kind, identity.ServerName, identity.ToolName, permission)
	switch permission {
	case resolvedToolPermissionAllow:
		return s.respondToToolPermissionRequest(ctx, codeSessionID, object, meta, permission, "auto-approve", "", "")
	case resolvedToolPermissionDeny:
		return s.respondToToolPermissionRequest(ctx, codeSessionID, object, meta, permission, "auto-deny", "", "")
	case resolvedToolPermissionAsk:
		return s.publishToolPermissionRequiresAction(ctx, codeSessionID, object, meta, identity)
	default:
		return nil
	}
}

func (s *Service) resolveToolPermission(ctx context.Context, codeSessionID string, claudeToolName string) (resolvedToolPermission, toolIdentity, error) {
	codeSession, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return resolvedToolPermissionAsk, parseClaudeToolIdentity(claudeToolName), err
	}
	session, err := s.db.GetSession(ctx, codeSession.WorkspaceID, codeSession.SessionExternalID)
	if err != nil {
		return resolvedToolPermissionAsk, parseClaudeToolIdentity(claudeToolName), err
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
	request, publicEventThreadID, err := s.toolPermissionRequestForConfirmation(ctx, codeSession, toolUseID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	requestObject, err := decodeJSONObject(request.Payload)
	if err != nil {
		return false, err
	}
	meta, err := BuildEventMetadata(codeSession.ExternalID, "outbound", request.Payload)
	if err != nil {
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
	sessionThreadID := firstNonEmpty(toolPermissionSessionThreadID(payload), publicEventThreadID, toolPermissionSessionThreadID(requestObject))
	if err := s.respondToToolPermissionRequest(ctx, codeSession.ExternalID, requestObject, meta, behavior, "tool-confirmation", denyMessage, sessionThreadID); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) toolPermissionRequestForConfirmation(ctx context.Context, codeSession db.CodeSession, toolUseID string) (db.CodeSessionEvent, string, error) {
	request, err := s.db.GetLatestCodeSessionToolPermissionRequest(ctx, codeSession.ExternalID, toolUseID)
	if err == nil || !errors.Is(err, db.ErrNotFound) {
		return request, "", err
	}
	publicEvent, err := s.db.GetSessionEvent(ctx, codeSession.WorkspaceID, codeSession.SessionExternalID, toolUseID)
	if err != nil {
		return db.CodeSessionEvent{}, "", err
	}
	switch publicEvent.EventType {
	case "agent.tool_use", "agent.mcp_tool_use":
	default:
		return db.CodeSessionEvent{}, "", db.ErrNotFound
	}
	publicPayload := rawObject(publicEvent.Payload)
	workerToolUseID := stringField(publicPayload, "tool_use_id")
	if workerToolUseID == "" {
		return db.CodeSessionEvent{}, "", db.ErrNotFound
	}
	request, err = s.db.GetLatestCodeSessionToolPermissionRequest(ctx, codeSession.ExternalID, workerToolUseID)
	return request, toolPermissionSessionThreadID(publicPayload), err
}

func (s *Service) respondToToolPermissionRequest(ctx context.Context, codeSessionID string, object map[string]any, meta EventMetadata, behavior resolvedToolPermission, source string, denyMessage string, sessionThreadID string) error {
	requestID := requestIDString(meta.RequestID)
	if requestID == "" {
		requestID = stringField(object, "request_id")
	}
	if requestID == "" {
		return nil
	}
	request, _ := object["request"].(map[string]any)
	updatedInput := map[string]any{}
	if input, ok := request["input"].(map[string]any); ok && input != nil {
		updatedInput = input
	}
	toolUseID := strings.TrimSpace(stringField(request, "tool_use_id"))
	response := map[string]any{
		"behavior":     string(behavior),
		"updatedInput": updatedInput,
	}
	if toolUseID != "" {
		response["toolUseID"] = toolUseID
	}
	sessionThreadID = firstNonEmpty(sessionThreadID, toolPermissionSessionThreadID(object))
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
		"uuid":       uuid.NewString(),
		"session_id": codeSessionID,
		"created_at": formatTime(now),
		"timestamp":  formatTime(now),
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
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

func (s *Service) publishToolPermissionRequiresAction(ctx context.Context, codeSessionID string, object map[string]any, meta EventMetadata, identity toolIdentity) error {
	request, _ := object["request"].(map[string]any)
	toolName := firstNonEmpty(stringField(request, "tool_name"), stringField(object, "tool_name"))
	if toolName == "" {
		return nil
	}
	toolUseID := stringField(request, "tool_use_id")
	requestID := requestIDString(meta.RequestID)
	if requestID == "" {
		requestID = stringField(object, "request_id")
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
	sessionThreadID := toolPermissionSessionThreadID(object)
	if sessionThreadID != "" {
		toolPayload["session_thread_id"] = sessionThreadID
	}
	if input, ok := request["input"]; ok {
		toolPayload["input"] = input
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
	return s.publishPublicPayloads(ctx, codeSessionID, payloads)
}
