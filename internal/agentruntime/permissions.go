package agentruntime

import (
	"context"
	"encoding/json"

	"charm.land/fantasy"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func (t *turn) authorize(ctx context.Context, call fantasy.ToolCall, remote bool) (json.RawMessage, bool, error) {
	if !json.Valid([]byte(call.Input)) {
		return nil, false, ErrInvalidInput
	}
	requestID := t.history.id("permission:" + call.ID)
	waiting := make(chan permissionResponse, 1)
	t.execution.permissionMu.Lock()
	t.execution.permissions[requestID] = waiting
	t.execution.permissionMu.Unlock()
	defer func() {
		t.execution.permissionMu.Lock()
		delete(t.execution.permissions, requestID)
		t.execution.permissionMu.Unlock()
	}()
	toolUseID := t.history.id("provider-tool:" + call.ID)
	permission, err := t.execution.worker.RequestPermission(ctx, codesessions.HostToolRequest{RequestID: requestID, ToolUseID: toolUseID, ToolName: call.Name, ThreadID: t.threadID, Input: json.RawMessage(call.Input)})
	if err != nil {
		return nil, false, err
	}
	resultType := "agent.tool_result"
	if remote {
		resultType = "agent.mcp_tool_result"
	}
	t.tools[call.ID] = toolAssociation{publicID: permission.PublicEventID, resultType: resultType}
	if permission.Behavior == "ask" {
		if err := t.execution.worker.SetState(ctx, "requires_action"); err != nil {
			return nil, false, err
		}
	}
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case response := <-waiting:
		if response.ToolUseID != toolUseID || response.Behavior != "allow" && response.Behavior != "deny" {
			return nil, false, ErrInvalidInput
		}
		if err := t.execution.worker.SetState(ctx, "running"); err != nil {
			return nil, false, err
		}
		if response.Behavior == "deny" {
			return nil, false, nil
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(response.UpdatedInput, &object); err != nil || object == nil {
			return nil, false, ErrInvalidInput
		}
		return response.UpdatedInput, true, nil
	}
}
