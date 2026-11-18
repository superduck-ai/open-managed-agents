package db

import (
	"context"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

func rejectAcceptedToolControlsTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker codeSessionInputStateRow, events []SessionEvent) error {
	accepted := make(map[string]bool)
	for _, event := range events {
		if event.EventType != "user.tool_confirmation" && event.EventType != "user.custom_tool_result" {
			continue
		}
		threadID := ""
		if event.ThreadExternalID != nil {
			threadID = *event.ThreadExternalID
		}
		references, err := maevents.PendingToolEventReferences(worker.WorkerExternalMetadata, worker.ExternalID, primary.ExternalID, threadID, *event.ToolUseID)
		if err != nil {
			return err
		}
		if accepted[references[0]] {
			return ErrSessionInputConflict
		}
		found, err := NewSessionEventMapper(executor).HasAcceptedToolControl(ctx, session.WorkspaceUUID, session.ExternalID, references[0], references[1])
		if err != nil {
			return err
		}
		if found {
			return ErrSessionInputConflict
		}
		accepted[references[0]] = true
	}
	return nil
}
