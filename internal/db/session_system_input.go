package db

import maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

func pendingSessionInputTools(primary SessionThread, worker codeSessionInputStateRow, events []SessionEvent) ([]string, error) {
	pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, primary.ExternalID)
	if err != nil {
		return nil, err
	}
	if worker.WorkerStatus != "requires_action" && len(pending) == 0 {
		return pending, nil
	}
	afterResult := false
	for _, event := range events {
		switch event.EventType {
		case "system.message":
			if !afterResult {
				return nil, ErrSessionInputPaused
			}
		case "user.tool_result", "user.custom_tool_result":
			afterResult = true
		default:
			afterResult = false
		}
	}
	return pending, nil
}
