package sessions

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// An accepted tool event keeps its first owner even if a later child transcript
// supplies another copy. New results may follow an unambiguous accepted tool use.
func toolEventCopySpecs(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, eventType, eventID string, payload map[string]any) ([]sessionEventCopySpec, error) {
	if !sessionToolUseEventHasThreadScopedSessionThreadID(eventType) && !isToolResultOrConfirmationEvent(eventType) {
		return nil, nil
	}
	stored, err := tx.GetSessionEvent(ctx, session, eventID)
	if err == nil {
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, stored.ThreadExternalID, false)}, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	if !isToolResultOrConfirmationEvent(eventType) {
		return nil, nil
	}
	// Explicit agent/task provenance is resolved by the caller before any
	// association with another tool event can supply an owner.
	if firstSessionPayloadString(payload, "agent_id", "agentId", "task_id") != "" {
		return nil, nil
	}
	toolUseID := sessionToolReferenceID(payload)
	if toolUseID == "" {
		return nil, nil
	}
	owners, err := tx.ToolUseOwnerThreadIDs(ctx, session, toolUseID)
	if err != nil || len(owners) != 1 {
		return nil, err
	}
	return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, new(owners[0]), false)}, nil
}
