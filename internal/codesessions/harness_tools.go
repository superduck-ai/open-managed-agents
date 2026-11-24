package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxtools"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func HarnessSandboxTools(session db.Session) ([]sandboxtools.Definition, error) {
	policies, err := runtimeRootToolsets(session.AgentSnapshot)
	if err != nil {
		return nil, err
	}
	definitions, err := sandboxtools.Definitions()
	if err != nil {
		return nil, err
	}
	enabled := (builtinToolCatalog{}).runtimeTools(policies)
	var allowed []sandboxtools.Definition
	for _, tool := range definitions {
		if slices.Contains(enabled, tool.Name) && resolveAgentToolPermission(policies, tool.RemoteName) == resolvedToolPermissionAllow {
			allowed = append(allowed, tool)
		}
	}
	return allowed, nil
}

func HarnessModelTools(session db.Session) ([]sandboxtools.Definition, error) {
	tools, err := HarnessSandboxTools(session)
	if err != nil {
		return nil, err
	}
	policies, err := runtimeRootToolsets(session.AgentSnapshot)
	if err != nil {
		return nil, err
	}
	if resolveAgentToolPermission(policies, "ask_user_question") != resolvedToolPermissionDeny {
		tools = append(tools, sandboxtools.Definition{
			Name:        sessioncontract.HarnessAskUserQuestion,
			Description: "Ask the user to choose one of two to four options, then wait for their response.",
			InputSchema: json.RawMessage(`{
    "type": "object",
    "required": ["questions"],
    "properties": {
     "questions": {
      "type": "array", "minItems": 1, "maxItems": 1,
      "items": {
       "type": "object",
       "required": ["id", "question", "options"],
       "properties": {
        "id": {"type": "string"},
        "header": {"type": "string"},
        "question": {"type": "string"},
        "options": {
         "type": "array", "minItems": 2, "maxItems": 4,
         "items": {
          "type": "object", "required": ["label"],
          "properties": {"label": {"type": "string"}, "description": {"type": "string"}}
         }
        }
       }
      }
     }
    }
   }`),
		})
	}
	return append(tools, sandboxtools.Definition{
		Name:        sessioncontract.HarnessSubscribe,
		Description: "Create a subscription for events that an external integration sends to this session. Returns the subscription ID to share with the integration.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}, sandboxtools.Definition{
		Name:        sessioncontract.HarnessCancelSubscription,
		Description: "Stop accepting events on an active subscription in this session.",
		InputSchema: json.RawMessage(`{"type":"object","required":["subscription_id"],"properties":{"subscription_id":{"type":"string","format":"uuid"}}}`),
	}), nil
}

func (s *Service) harnessSandboxToolAllowed(ctx context.Context, code db.CodeSession, thread, name string) (bool, error) {
	if !isSandboxToolName(name) || code.SessionExternalID == "" {
		return false, nil
	}
	policies, err := s.currentRuntimeToolsets(ctx, code, "")
	if err != nil {
		return false, err
	}
	enabled := s.builtinTools().runtimeTools(policies)
	if resolveAgentToolPermission(policies, name) != resolvedToolPermissionAllow ||
		!slices.ContainsFunc(enabled, func(alias string) bool { return managedAgentToolName(alias) == name }) {
		return false, nil
	}
	if thread == "" || thread == "root" {
		return true, nil
	}
	child, err := s.db.GetSessionThread(ctx, code.WorkspaceUUID, code.SessionExternalID, thread)
	if err != nil {
		return false, err
	}
	tools, err := HarnessSandboxTools(db.Session{AgentSnapshot: child.AgentSnapshot})
	return slices.ContainsFunc(tools, func(tool sandboxtools.Definition) bool { return tool.RemoteName == name }) &&
		child.Status == "running" && child.ArchivedAt == nil && child.ParentThreadUUID != nil, err
}

func (h *Handler) harnessToolScope(w http.ResponseWriter, r *http.Request) (db.SandboxToolCallScope, db.HarnessTool, db.HarnessTurnLease, bool) {
	credential, err := h.db.GetCodeSessionByOAuthAccessTokenHash(r.Context(), auth.HashAPIKey(auth.ExtractAPIKey(r)))
	if err != nil || credential.ExecutionRuntime != sessioncontract.CloudHarnessRuntime ||
		credential.CodeSessionExternalID != chi.URLParam(r, "code_session_id") {
		h.errorAdapter.Write(w, r, sessionIngressTokenInvalid(err))
		return db.SandboxToolCallScope{}, db.HarnessTool{}, db.HarnessTurnLease{}, false
	}
	turn, turnErr := uuid.Parse(chi.URLParam(r, "turn_uuid"))
	task, taskErr := uuid.Parse(chi.URLParam(r, "task_uuid"))
	if turnErr != nil || taskErr != nil {
		h.errorAdapter.Write(w, r, toolCallInputError(nil))
		return db.SandboxToolCallScope{}, db.HarnessTool{}, db.HarnessTurnLease{}, false
	}
	ref := db.HarnessTurnRef{
		UUID: turn.String(), OrganizationUUID: credential.OrganizationUUID,
		WorkspaceUUID: credential.WorkspaceUUID, SessionUUID: credential.PublicSessionUUID,
	}
	tool, err := h.db.ActiveHarnessTool(r.Context(), ref, task.String(), credential.WorkerEpoch)
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return db.SandboxToolCallScope{}, db.HarnessTool{}, db.HarnessTurnLease{}, false
	}
	lease := db.HarnessTurnLease{Ref: ref, CodeSessionUUID: credential.CodeSessionUUID, Epoch: credential.WorkerEpoch}
	child, err := h.db.HarnessSubagent(r.Context(), lease)
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return db.SandboxToolCallScope{}, db.HarnessTool{}, db.HarnessTurnLease{}, false
	}
	threadID := "root"
	if child != nil {
		threadID = child.ExternalID
	}
	return db.SandboxToolCallScope{
		OrganizationUUID: ref.OrganizationUUID, WorkspaceUUID: ref.WorkspaceUUID,
		CodeSessionID: credential.CodeSessionExternalID, WorkerEpoch: credential.WorkerEpoch,
		ThreadID: threadID, CallID: tool.UUID,
	}, tool, lease, true
}

func (h *Handler) handleHarnessToolSubmit(w http.ResponseWriter, r *http.Request) {
	scope, tool, _, ok := h.harnessToolScope(w, r)
	if !ok {
		return
	}
	if isHarnessSubscriptionTool(tool.Name) || isHarnessDelegationTool(tool.Name) {
		writeToolCallJSON(w, http.StatusOK, struct {
			State string `json:"state"`
		}{State: "succeeded"})
		return
	}
	name := managedAgentToolName(tool.Name)
	request := toolCallSubmission{CallID: tool.UUID, Tool: name, Input: tool.Arguments, TimeoutMS: harnessToolTimeout(name, tool.Arguments)}
	h.submitToolCall(w, r, scope, request, true)
}

func (h *Handler) handleHarnessToolWait(w http.ResponseWriter, r *http.Request) {
	scope, _, _, ok := h.harnessToolScope(w, r)
	if ok {
		h.waitForToolCall(w, r, scope)
	}
}

func harnessToolTimeout(name string, input json.RawMessage) int64 {
	if name != "bash" {
		return 180000
	}
	var args struct {
		Timeout int64 `json:"timeout"`
	}
	if err := json.Unmarshal(input, &args); err != nil || args.Timeout <= 0 {
		args.Timeout = 120
	}
	return min(args.Timeout, 600)*1000 + 30000
}

type harnessToolResultSink interface {
	PublishHarnessToolResult(context.Context, db.CodeSession, db.HarnessTurnLease, string, sessioncontract.HarnessToolResult, json.RawMessage) error
}

type harnessToolResultEvent struct {
	ID            string                `json:"id"`
	Type          string                `json:"type"`
	ToolUseID     string                `json:"tool_use_id"`
	Content       []ModelRequestContent `json:"content"`
	IsError       bool                  `json:"is_error"`
	ProcessedAt   time.Time             `json:"processed_at"`
	OwnerThreadID string                `json:"_owner_session_thread_id,omitempty"`
}

func (h *Handler) handleHarnessToolResult(w http.ResponseWriter, r *http.Request) {
	scope, tool, lease, ok := h.harnessToolScope(w, r)
	if !ok {
		return
	}
	if err := h.completeHarnessTool(r.Context(), scope, tool, lease); err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) completeHarnessTool(ctx context.Context, scope db.SandboxToolCallScope, tool db.HarnessTool, lease db.HarnessTurnLease) error {
	sink, ok := h.service.sink.(harnessToolResultSink)
	if !ok {
		return db.ErrInvalidState
	}
	code, found, err := h.db.GetScopedCodeSession(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionID)
	if err != nil {
		return err
	}
	if !found {
		return db.ErrNotFound
	}
	var result sessioncontract.HarnessToolResult
	if isHarnessDelegationTool(tool.Name) {
		var started bool
		result, started, err = h.harnessDelegationOutcome(ctx, code, lease, tool)
		if started {
			return nil
		}
	} else {
		result, err = h.harnessToolOutcome(ctx, code, scope, tool)
	}
	if err != nil {
		return err
	}
	threadID := scope.ThreadID
	if threadID == "root" {
		threadID = ""
	}
	payload, err := json.Marshal(harnessToolResultEvent{
		ID:   stablePublicEventID(scope.CodeSessionID, tool.UUID+":result"),
		Type: "agent.tool_result", ToolUseID: maevents.ToolUseEventID(scope.CodeSessionID, tool.CallID, threadID), OwnerThreadID: threadID,
		Content: []ModelRequestContent{{Type: "text", Text: &result.Text}}, IsError: result.Failed, ProcessedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return sink.PublishHarnessToolResult(ctx, code, lease, tool.UUID, result, payload)
}

func (h *Handler) harnessToolOutcome(ctx context.Context, code db.CodeSession, scope db.SandboxToolCallScope, tool db.HarnessTool) (sessioncontract.HarnessToolResult, error) {
	switch tool.Name {
	case sessioncontract.HarnessSubscribe:
		return sessioncontract.HarnessToolResult{Text: "Subscription ID: " + tool.UUID}, nil
	case sessioncontract.HarnessCancelSubscription:
		id, err := sessioncontract.ParseHarnessSubscriptionID(tool.Arguments)
		if err != nil {
			return sessioncontract.HarnessToolResult{}, err
		}
		return sessioncontract.HarnessToolResult{Text: "Subscription closed: " + id}, nil
	}
	call, err := h.db.GetSandboxToolCall(ctx, scope)
	if errors.Is(err, db.ErrNotFound) {
		allowed, err := h.service.harnessSandboxToolAllowed(ctx, code, scope.ThreadID, managedAgentToolName(tool.Name))
		if err != nil {
			return sessioncontract.HarnessToolResult{}, err
		}
		if allowed {
			return sessioncontract.HarnessToolResult{}, db.ErrToolCallConflict
		}
		return sessioncontract.HarnessToolResult{Text: "Tool call is no longer allowed by the session policy.", Failed: true}, nil
	}
	if err != nil {
		return sessioncontract.HarnessToolResult{}, err
	}
	if !toolCallDone(call.State) {
		return sessioncontract.HarnessToolResult{}, db.ErrToolCallConflict
	}
	if err := h.restoreToolCallResult(ctx, &call); err != nil {
		return sessioncontract.HarnessToolResult{}, err
	}
	return harnessSandboxResult(call), nil
}

func harnessSandboxResult(call db.SandboxToolCall) sessioncontract.HarnessToolResult {
	result := sessioncontract.HarnessToolResult{Failed: true}
	switch call.State {
	case "expired":
		result.Text = "Tool call expired before execution."
	case "cancelled":
		result.Text = "Tool call interrupted."
	case "lost":
		result.Text = "Remote tool outcome is unknown. Verify external state before repeating operations with side effects."
	default:
		text, invalidContent := harnessSandboxResultText(call.Result)
		result.Text, result.Failed = text, invalidContent || call.State != "succeeded"
		if result.Text == "" {
			result.Text = "(no output)"
			if result.Failed {
				result.Text = "The sandbox tool failed."
			}
		}
	}
	return result
}

func harnessSandboxResultText(payload json.RawMessage) (string, bool) {
	var result struct {
		Content []ModelRequestContent `json:"content"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &result); err != nil {
			return "The sandbox result could not be read. Verify external state before repeating operations with side effects.", true
		}
	}
	var text strings.Builder
	for i, block := range result.Content {
		if block.Type != "text" || block.Text == nil {
			return "The tool returned content that this text session cannot read.", true
		}
		if i > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(*block.Text)
	}
	value := text.String()
	if suffix := "\n[Tool result truncated]"; len(value) > sessioncontract.HarnessToolResultMaxBytes {
		end := sessioncontract.HarnessToolResultMaxBytes - len(suffix)
		for !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end] + suffix
	}
	return value, false
}
