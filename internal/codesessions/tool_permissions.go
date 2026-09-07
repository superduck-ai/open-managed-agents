package codesessions

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"maps"
	"slices"
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

func (s *Service) appendToolPermissionRequest(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, worker db.CodeSession, prepared preparedControlAction) ([]db.SessionEvent, []db.AppendCodeSessionEventInput, error) {
	toolName := firstNonEmpty(prepared.request.Request.ToolName, prepared.request.ToolName)
	permission := resolveToolPermissionFromAgentSnapshot(session.AgentSnapshot, toolName)
	identity := parseClaudeToolIdentity(toolName)
	request, payloads, err := toolPermissionPublicPayloads(worker.ExternalID, &prepared.request, prepared.metadata, identity, permission)
	if err != nil || len(payloads) == 0 {
		return nil, nil, err
	}
	created, err := s.sink.AppendCodeSessionEvents(ctx, tx, session, worker.ExternalID, payloads)
	if err != nil {
		return nil, nil, err
	}
	if permission == resolvedToolPermissionAsk {
		newRequest := slices.ContainsFunc(created, func(event db.SessionEvent) bool { return event.ExternalID == request.PublicEventID })
		if !newRequest {
			// A confirmed request has already had its metadata removed. Replaying
			// its public events must not resurrect it or move the Session to idle.
			if len(created) > 0 {
				return nil, nil, db.ErrSessionEventConflict
			}
			return nil, nil, nil
		}
		metadata, err := marshalRaw(map[string]toolPermissionRequest{toolPermissionRequestMetadataKey(request.PublicEventID): request})
		if err != nil {
			return nil, nil, err
		}
		return created, nil, tx.MergeWorkerMetadata(ctx, worker, metadata)
	}
	source := "auto-approve"
	if permission == resolvedToolPermissionDeny {
		source = "auto-deny"
	}
	input, err := toolPermissionResponseInput(worker.ExternalID, request, permission, source, "", "")
	if err != nil {
		return nil, nil, err
	}
	return created, []db.AppendCodeSessionEventInput{input}, nil
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

type userToolConfirmationPayload struct {
	ToolUseID   string `json:"tool_use_id"`
	Result      string `json:"result"`
	DenyMessage string `json:"deny_message"`
	workerThreadReference
	Request  workerThreadReference `json:"request"`
	Data     workerThreadReference `json:"data"`
	Metadata workerThreadReference `json:"metadata"`
}

// A nil response means the public event should use the ordinary worker input path.
func controlResponseForToolConfirmation(codeSession db.CodeSession, event db.SessionEvent) (*db.AppendCodeSessionEventInput, string, error) {
	var payload userToolConfirmationPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return nil, "", err
	}
	request, err := toolPermissionRequestFromMetadata(codeSession.WorkerExternalMetadata, payload.ToolUseID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if request.EventType == "agent.custom_tool_use" {
		return nil, "", nil
	}
	var behavior resolvedToolPermission
	switch payload.Result {
	case "allow":
		behavior = resolvedToolPermissionAllow
	case "deny":
		behavior = resolvedToolPermissionDeny
	default:
		return nil, "", nil
	}
	input, err := toolPermissionResponseInput(codeSession.ExternalID, request, behavior, "tool-confirmation", payload.DenyMessage, firstNonEmpty(
		payload.SessionThreadID, payload.ThreadID,
		payload.Request.SessionThreadID, payload.Request.ThreadID,
		payload.Data.SessionThreadID, payload.Data.ThreadID,
		payload.Metadata.SessionThreadID, payload.Metadata.ThreadID,
	))
	return &input, request.PublicEventID, err
}

const legacyToolPermissionRequestMetadataKey = "managed_agent_tool_permission_request"

type legacyToolPermissionMetadata struct {
	Request struct {
		PublicEventID string `json:"public_event_id"`
	} `json:"managed_agent_tool_permission_request"`
}

func toolPermissionRequestMetadataKey(publicEventID string) string {
	return legacyToolPermissionRequestMetadataKey + ":" + publicEventID
}

// PendingToolActionEventIDs reads the same pending requests that confirmations
// consume. Unrelated worker metadata is not part of the public waiting state.
func PendingToolActionEventIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	var ids []string
	for key, value := range metadata {
		if key != legacyToolPermissionRequestMetadataKey && !strings.HasPrefix(key, legacyToolPermissionRequestMetadataKey+":") {
			continue
		}
		var request toolPermissionRequest
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&request); err != nil {
			return nil, err
		}
		if request.PublicEventID != "" && request.RequestID != "" && request.ToolUseID != "" {
			ids = append(ids, request.PublicEventID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
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
	decoder := json.NewDecoder(bytes.NewReader(requestRaw))
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		return toolPermissionRequest{}, err
	}
	if request.PublicEventID != publicEventID || request.RequestID == "" || request.ToolUseID == "" {
		return toolPermissionRequest{}, db.ErrNotFound
	}
	return request, nil
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

func controlResponseForCustomToolResult(codeSession db.CodeSession, event db.SessionEvent) (*db.AppendCodeSessionEventInput, string, error) {
	var payload userCustomToolResultPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return nil, "", err
	}
	request, err := toolPermissionRequestFromMetadata(codeSession.WorkerExternalMetadata, payload.CustomToolUseID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if request.EventType != "agent.custom_tool_use" {
		return nil, "", nil
	}
	behavior := resolvedToolPermissionAllow
	denyMessage := ""
	if payload.IsError {
		behavior = resolvedToolPermissionDeny
		denyMessage = customToolResultText(payload)
	} else {
		answers, err := customToolResultAnswers(payload)
		if err != nil {
			return nil, "", err
		}
		request.Input = cloneStringAnyMap(request.Input)
		request.Input["answers"] = answers
	}
	input, err := toolPermissionResponseInput(codeSession.ExternalID, request, behavior, "custom-tool-result", denyMessage, payload.SessionThreadID)
	return &input, request.PublicEventID, err
}

func customToolResultAnswers(payload userCustomToolResultPayload) (map[string]any, error) {
	text := customToolResultText(payload)
	if text == "" {
		return map[string]any{}, nil
	}
	var answers map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&answers); err != nil || answers == nil || !json.Valid([]byte(text)) {
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

func toolPermissionResponseInput(codeSessionID string, request toolPermissionRequest, behavior resolvedToolPermission, source string, denyMessage string, sessionThreadID string) (db.AppendCodeSessionEventInput, error) {
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
		return db.AppendCodeSessionEventInput{}, err
	}
	return newInboundEventInput(codeSessionID, payload, source)
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
		"id":                stablePublicEventID(codeSessionID, request.RequestID+"\x00tool_permission_thread_requires_action"),
		"type":              "session.thread_status_idle",
		"session_thread_id": request.SessionThreadID,
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
