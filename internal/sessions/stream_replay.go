package sessions

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (h *Handler) loadStreamReplay(ctx context.Context, workspaceUUID, sessionID string, connection *streamConnection, cursorID string) ([]sessionStreamEvent, error) {
	if cursorID == "" {
		return nil, nil
	}
	cursor, err := h.db.GetSessionEvent(ctx, workspaceUUID, sessionID, cursorID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, invalidRequest(db.ErrInvalidCursor)
	}
	if err != nil {
		return nil, internalError("Could not load stream cursor", err)
	}
	if !maevents.IsPublicSessionHistoryEvent(cursor.EventType) || !connection.matches(sessionStreamEventFrom(cursor)) {
		return nil, invalidRequest(db.ErrInvalidCursor)
	}
	params := db.ListSessionEventsPageParams{
		WorkspaceUUID: workspaceUUID, SessionExternalID: sessionID,
		ThreadExternalID: connection.threadID, Limit: 1000, Order: "asc",
		Cursor: &db.SessionEventPageCursor{ExternalID: cursorID},
	}
	var replay []sessionStreamEvent
	for {
		records, more, err := h.eventPayloads.ListSessionEventsPage(ctx, params)
		if err != nil {
			return nil, internalError("Could not replay session events", err)
		}
		hidden, err := h.primaryOrphanToolUseIDsWithChildCopies(ctx, sessionID, connection.threadID, records)
		if err != nil {
			return nil, internalError("Could not replay session events", err)
		}
		for _, record := range records {
			if record.EventType == "user.message" && record.ProcessedAt.IsZero() {
				continue
			}
			if maevents.IsPublicSessionHistoryEvent(record.EventType) && !primaryToolProjectionHidden(record, hidden) {
				replay = append(replay, sessionStreamEventFrom(record))
			}
		}
		if !more || len(records) == 0 {
			return replay, nil
		}
		params.Cursor = &db.SessionEventPageCursor{ExternalID: records[len(records)-1].ExternalID}
	}
}

func (c *streamConnection) rememberReplay(cursorID string, replay []sessionStreamEvent) {
	if cursorID == "" {
		return
	}
	c.replayedEventIDs = make(map[string]struct{}, len(replay)+1)
	c.replayedEventIDs[cursorID] = struct{}{}
	for _, event := range replay {
		c.replayedEventIDs[event.ExternalID] = struct{}{}
	}
}
