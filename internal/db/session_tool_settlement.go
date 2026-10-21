package db

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

func settleCompletedToolRequestTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker *codeSessionInputStateRow, event SessionEvent) error {
	if event.EventType != "agent.tool_result" && event.EventType != "agent.mcp_tool_result" {
		return nil
	}
	var result struct {
		ToolUseID    string `json:"tool_use_id"`
		MCPToolUseID string `json:"mcp_tool_use_id"`
	}
	if err := jsonv2.Unmarshal(event.Payload, &result); err != nil {
		return err
	}
	reference := result.ToolUseID
	if event.EventType == "agent.mcp_tool_result" {
		reference = result.MCPToolUseID
	}
	if reference == "" {
		return nil
	}
	threadID := primary.ExternalID
	if event.ThreadExternalID != nil {
		threadID = *event.ThreadExternalID
	}
	metadata, err := maevents.SettlePendingToolRequest(worker.WorkerExternalMetadata, worker.ExternalID, primary.ExternalID, threadID, reference)
	if err != nil || bytes.Equal(metadata, worker.WorkerExternalMetadata) {
		return err
	}
	pending, err := maevents.PendingToolEventIDs(metadata, primary.ExternalID, "")
	if err != nil {
		return err
	}
	if _, err := NewCodeSessionMapper(executor).UpdateWorkerToolMetadata(ctx, session.WorkspaceUUID, worker.ExternalID, metadata, len(pending) == 0); err != nil {
		return err
	}
	worker.WorkerExternalMetadata = metadata
	return nil
}
