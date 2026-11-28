package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
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
	return tools, nil
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
	if isHarnessDelegationTool(tool.Name) {
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
		return harnessSandboxContent(call)
	}
	return result
}

func harnessSandboxContent(call db.SandboxToolCall) sessioncontract.HarnessToolResult {
	var output struct {
		Content           []ModelRequestContent `json:"content"`
		StructuredContent json.RawMessage       `json:"structuredContent"`
		IsError           bool                  `json:"isError"`
	}
	if len(call.Result) > 0 && json.Unmarshal(call.Result, &output) != nil {
		return sessioncontract.HarnessToolResult{
			Text: "The sandbox result could not be read. Verify external state before repeating operations with side effects.", Failed: true,
		}
	}
	failed := call.State != "succeeded" || output.IsError
	if call.ToolName == "bash" && !failed {
		if stdout, ok := successfulSandboxBashStdout(output.Content, output.StructuredContent); ok {
			return sessioncontract.HarnessToolResult{Text: stdout}
		}
	}
	var text strings.Builder
	for i, block := range output.Content {
		if block.Type != "text" || block.Text == nil {
			return sessioncontract.HarnessToolResult{Text: "The tool returned content that this text session cannot read.", Failed: true}
		}
		if i > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(*block.Text)
	}
	value := text.String()
	if value == "" {
		value = "(no output)"
		if failed {
			value = "The sandbox tool failed."
		}
	}
	if suffix := "\n[Tool result truncated]"; len(value) > sessioncontract.HarnessToolResultMaxBytes {
		end := sessioncontract.HarnessToolResultMaxBytes - len(suffix)
		for !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end] + suffix
	}
	return sessioncontract.HarnessToolResult{Text: value, Failed: failed}
}

func successfulSandboxBashStdout(content []ModelRequestContent, structured json.RawMessage) (string, bool) {
	if len(content) < 1 || len(content) > 2 || content[0].Type != "text" || content[0].Text == nil {
		return "", false
	}
	var summary struct {
		Stdout          *string `json:"stdout"`
		Stderr          *string `json:"stderr"`
		ExitCode        *int    `json:"exit_code"`
		Status          string  `json:"status"`
		TimedOut        *bool   `json:"timed_out"`
		Cancelled       *bool   `json:"cancelled"`
		StdoutTruncated *bool   `json:"stdout_truncated"`
		StderrTruncated *bool   `json:"stderr_truncated"`
		StdoutError     string  `json:"stdout_error"`
		StderrError     string  `json:"stderr_error"`
		Error           string  `json:"error"`
		RawOutput       *string `json:"rawOutput"`
	}
	if json.Unmarshal(structured, &summary) != nil || summary.Stdout == nil || summary.Stderr == nil || summary.ExitCode == nil {
		return "", false
	}
	if summary.Status != "completed" || *summary.ExitCode != 0 || *summary.Stderr != "" || summary.StdoutError != "" ||
		summary.StderrError != "" || summary.Error != "" || summary.RawOutput != nil {
		return "", false
	}
	for _, flag := range []*bool{summary.TimedOut, summary.Cancelled, summary.StdoutTruncated, summary.StderrTruncated} {
		if flag == nil || *flag {
			return "", false
		}
	}
	stdout, preview := *summary.Stdout, *content[0].Text
	if len(stdout) > sessioncontract.HarnessToolResultMaxBytes || preview != stdout && !(stdout == "" && preview == "(no output)") {
		return "", false
	}
	if len(content) == 2 && !matchesSandboxExecutionSummary(content[1], structured) {
		return "", false
	}
	return stdout, true
}

func matchesSandboxExecutionSummary(block ModelRequestContent, structured json.RawMessage) bool {
	if block.Type != "text" || block.Text == nil {
		return false
	}
	var preview, summary any
	return json.Unmarshal([]byte(*block.Text), &preview) == nil &&
		json.Unmarshal(structured, &summary) == nil && reflect.DeepEqual(preview, summary)
}
