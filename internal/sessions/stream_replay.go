package sessions

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func (h *Handler) loadStreamReplay(ctx context.Context, session db.Session, connection *streamConnection, cursor streamCursor, through int64) ([]sessionStreamEvent, error) {
	if cursor.page == nil {
		return nil, nil
	}
	return h.readStreamHistory(ctx, session.WorkspaceUUID, session.ExternalID, connection, db.ListSessionEventsPageParams{Cursor: cursor.page, ThroughSeq: &through})
}

func (h *Handler) readStreamHistory(ctx context.Context, workspaceUUID, sessionID string, connection *streamConnection, params db.ListSessionEventsPageParams) ([]sessionStreamEvent, error) {
	params.WorkspaceUUID, params.SessionExternalID, params.ThreadExternalID = workspaceUUID, sessionID, connection.threadID
	params.Limit, params.Order = 1000, "asc"
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
