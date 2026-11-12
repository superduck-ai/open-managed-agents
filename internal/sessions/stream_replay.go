package sessions

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (h *Handler) loadStreamReplay(ctx context.Context, workspaceUUID, sessionID string, connection *streamConnection, cursorID string, cursorAt *time.Time) ([]sessionStreamEvent, error) {
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
	events, _, err := h.readStreamHistory(ctx, workspaceUUID, sessionID, connection, cursorID, cursorAt, false)
	return events, err
}

func (h *Handler) readStreamHistory(ctx context.Context, workspaceUUID, sessionID string, connection *streamConnection, cursorID string, cursorAt *time.Time, processedOnly bool) ([]sessionStreamEvent, string, error) {
	params := db.ListSessionEventsPageParams{
		WorkspaceUUID: workspaceUUID, SessionExternalID: sessionID,
		ThreadExternalID: connection.threadID, Limit: 1000, Order: "asc",
		ProcessedOnly: processedOnly,
	}
	if cursorID != "" {
		params.Cursor = &db.SessionEventPageCursor{ExternalID: cursorID, OrderingAt: cursorAt}
	}
	var replay []sessionStreamEvent
	for {
		records, more, err := h.eventPayloads.ListSessionEventsPage(ctx, params)
		if err != nil {
			return nil, cursorID, internalError("Could not replay session events", err)
		}
		hidden, err := h.primaryOrphanToolUseIDsWithChildCopies(ctx, sessionID, connection.threadID, records)
		if err != nil {
			return nil, cursorID, internalError("Could not replay session events", err)
		}
		for _, record := range records {
			if record.EventType == "user.message" && record.ProcessedAt.IsZero() {
				continue
			}
			if maevents.IsPublicSessionHistoryEvent(record.EventType) && !primaryToolProjectionHidden(record, hidden) {
				replay = append(replay, sessionStreamEventFrom(record))
			}
		}
		if len(records) > 0 {
			cursorID = records[len(records)-1].ExternalID
		}
		if !more || len(records) == 0 {
			return replay, cursorID, nil
		}
		params.Cursor = &db.SessionEventPageCursor{ExternalID: records[len(records)-1].ExternalID}
	}
}

func (c *streamConnection) rememberReplay(cursorID string, replay []sessionStreamEvent, finalizedCursor bool) {
	if cursorID == "" {
		return
	}
	c.replayedEventIDs = make(map[string]struct{}, len(replay)+1)
	if finalizedCursor {
		c.replayedEventIDs[cursorID] = struct{}{}
	}
	for _, event := range replay {
		if event.ProcessedAt.IsZero() && maevents.IsPublicWorkerInputEvent(event.EventType) {
			continue
		}
		c.replayedEventIDs[event.ExternalID] = struct{}{}
	}
}
