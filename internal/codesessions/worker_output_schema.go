package codesessions

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Worker output protocol sources in superduck-code:
//   - src/entrypoints/sdk/controlSchemas.ts: StdoutMessageSchema
//   - src/entrypoints/sdk/stdoutTypes.ts: StdoutMessage
//   - src/cli/transports/ccrClient.ts: ClientEvent and toClientEvent
//
// The envelope retains payload as JSON because StdoutMessage is an open union.
// Business code first reads workerPayloadHeader, then decodes a concrete schema
// only when it needs fields specific to that message type.
type workerOutputEvent struct {
	Payload   json.RawMessage `json:"payload"`
	Ephemeral bool            `json:"ephemeral"`
}

type workerPayloadHeader struct {
	Type string `json:"type"`
	UUID string `json:"uuid"`
}

// Public-output mapping schemas come from superduck-code
// src/entrypoints/sdk/coreSchemas.ts:
//   - SDKAssistantMessageSchema
//   - SDKUserMessageSchema / SDKUserMessageReplaySchema
//   - SDKResultSuccessSchema / SDKResultErrorSchema
//   - SDKSystemMessageSchema, SDKTaskStartedMessageSchema and
//     SDKTaskNotificationMessageSchema
//
// message/content and usage values remain RawMessage because the TypeScript
// source intentionally delegates them to Anthropic SDK placeholders or open
// records. The mapper decodes those fields only in the event-type branch that
// interprets them.
type workerOutputMessage struct {
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
}

type workerOutputCommonPayload struct {
	Type            string          `json:"type"`
	UUID            string          `json:"uuid"`
	ID              string          `json:"id"`
	SessionID       string          `json:"session_id"`
	SessionThreadID string          `json:"session_thread_id"`
	CreatedAt       string          `json:"created_at"`
	ProcessedAt     string          `json:"processed_at"`
	Timestamp       string          `json:"timestamp"`
	Content         json.RawMessage `json:"content"`
	Data            json.RawMessage `json:"data"`
}

type workerAssistantOutputPayload struct {
	Type              string              `json:"type"`
	Content           json.RawMessage     `json:"content"`
	ContentBlockIndex *int                `json:"content_block_index"`
	Message           workerOutputMessage `json:"message"`
}

func decodeWorkerAssistantOutputPayload(raw json.RawMessage) (workerAssistantOutputPayload, error) {
	var payload workerAssistantOutputPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return workerAssistantOutputPayload{}, err
	}
	payload.Message.ID = strings.TrimSpace(payload.Message.ID)
	return payload, nil
}

type workerUserOutputPayload struct {
	Type    string              `json:"type"`
	Content json.RawMessage     `json:"content"`
	Message workerOutputMessage `json:"message"`
}

type workerSystemOutputPayload struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	TaskID      string `json:"task_id"`
	ToolUseID   string `json:"tool_use_id"`
	Description string `json:"description"`
	TaskType    string `json:"task_type"`
	Prompt      string `json:"prompt"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
}

type workerResultOutputPayload struct {
	Type          string          `json:"type"`
	Model         string          `json:"model"`
	DurationAPIMs float64         `json:"duration_api_ms"`
	DurationMs    float64         `json:"duration_ms"`
	Usage         json.RawMessage `json:"usage"`
	ModelUsage    json.RawMessage `json:"modelUsage"`
	ModelUsageAlt json.RawMessage `json:"model_usage"`
}

type workerOpaqueOutputPayload struct {
	Type string `json:"type"`
}

func decodeWorkerPayloadHeader(raw json.RawMessage) (workerPayloadHeader, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return workerPayloadHeader{}, invalidWorkerPayload("payload is required", nil)
	}
	var header workerPayloadHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return workerPayloadHeader{}, invalidWorkerPayload("payload must be a json object", err)
	}
	if header.Type == "" {
		return workerPayloadHeader{}, invalidWorkerPayload("payload.type is required", nil)
	}
	if header.Type != "keep_alive" && header.UUID == "" {
		return workerPayloadHeader{}, invalidWorkerPayload("payload.uuid is required", nil)
	}
	return header, nil
}

// TypeScript: controlSchemas.ts SDKControlRequestSchema and
// SDKControlPermissionRequestSchema. Only can_use_tool is interpreted by the
// server; other control_request subtypes remain opaque.
type workerControlRequestPayload struct {
	workerPayloadHeader
	RequestID       string                  `json:"request_id"`
	Request         workerPermissionRequest `json:"request"`
	SessionThreadID string                  `json:"session_thread_id,omitempty"`
	ThreadID        string                  `json:"thread_id,omitempty"`
	ToolName        string                  `json:"tool_name,omitempty"`
	Data            workerThreadReference   `json:"data,omitempty"`
	Metadata        workerThreadReference   `json:"metadata,omitempty"`
}

func decodeWorkerControlRequestPayload(raw json.RawMessage) (workerControlRequestPayload, error) {
	var payload workerControlRequestPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return workerControlRequestPayload{}, err
	}
	return payload, nil
}

type workerPermissionRequest struct {
	Subtype               string                   `json:"subtype"`
	ToolName              string                   `json:"tool_name"`
	ToolUseID             string                   `json:"tool_use_id"`
	Input                 map[string]any           `json:"input"`
	PermissionSuggestions []workerPermissionUpdate `json:"permission_suggestions,omitempty"`
	BlockedPath           string                   `json:"blocked_path,omitempty"`
	DecisionReason        string                   `json:"decision_reason,omitempty"`
	Title                 string                   `json:"title,omitempty"`
	DisplayName           string                   `json:"display_name,omitempty"`
	AgentID               string                   `json:"agent_id,omitempty"`
	Description           string                   `json:"description,omitempty"`
	SessionThreadID       string                   `json:"session_thread_id,omitempty"`
	ThreadID              string                   `json:"thread_id,omitempty"`
}

// Thread references are CCR transport extensions accepted alongside the SDK
// control schema. The permission bridge uses them to preserve subagent scope.
type workerThreadReference struct {
	SessionThreadID string `json:"session_thread_id,omitempty"`
	ThreadID        string `json:"thread_id,omitempty"`
}

// TypeScript: coreSchemas.ts PermissionUpdateSchema and
// PermissionRuleValueSchema.
type workerPermissionUpdate struct {
	Type        string                 `json:"type"`
	Rules       []workerPermissionRule `json:"rules,omitempty"`
	Behavior    string                 `json:"behavior,omitempty"`
	Destination string                 `json:"destination"`
	Mode        string                 `json:"mode,omitempty"`
	Directories []string               `json:"directories,omitempty"`
}

type workerPermissionRule struct {
	ToolName    string `json:"toolName"`
	RuleContent string `json:"ruleContent,omitempty"`
}
