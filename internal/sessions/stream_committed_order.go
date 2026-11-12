package sessions

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type committedStreamOrder struct {
	session       db.Session
	cursorID      string
	cursorAt      *time.Time
	enabled       bool
	mutableCursor bool
}

func (h *Handler) newCommittedStreamOrder(ctx context.Context, session db.Session, connection *streamConnection, cursorID string) (*committedStreamOrder, error) {
	order := &committedStreamOrder{session: session, cursorID: cursorID}
	if cursorID != "" {
		cursor, err := h.db.GetSessionEvent(ctx, session.WorkspaceUUID, session.ExternalID, cursorID)
		if errors.Is(err, db.ErrNotFound) {
			return nil, invalidRequest(db.ErrInvalidCursor)
		}
		if err != nil {
			return nil, internalError("Could not load stream cursor", err)
		}
		if cursor.EventType == "user.message" || cursor.EventType == "user.tool_confirmation" {
			order.mutableCursor = true
			at := cursor.CreatedAt
			order.cursorAt = &at
		}
	}

	if cursorID == "" {
		events, _, err := h.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, ThreadExternalID: connection.threadID, ProcessedOnly: true, Order: "desc", Limit: 1})
		if err != nil {
			return nil, internalError("Could not initialize stream order", err)
		}
		if len(events) > 0 {
			order.cursorID = events[0].ExternalID
		}
	}
	return order, nil
}

func (h *Handler) committedStreamEvents(ctx context.Context, order *committedStreamOrder, connection *streamConnection, delivery streamDelivery) ([]sessionStreamEvent, error) {
	if notification, ok := delivery.(sessionEventDelivery); ok && maevents.IsStreamDelta(notification.event.EventType) {
		return nil, nil
	}
	if !order.enabled {
		dsr, err := h.db.HasDSHSession(ctx, order.session)
		if err != nil {
			return nil, err
		}
		order.enabled = dsr
	}
	if !order.enabled {
		return nil, nil
	}
	events, next, err := h.readStreamHistory(ctx, order.session.WorkspaceUUID, order.session.ExternalID, connection, order.cursorID, order.cursorAt, true)
	if err != nil {
		return nil, err
	}
	if next != order.cursorID || len(events) > 0 {
		order.cursorAt = nil
	}
	order.cursorID = next
	return events, nil
}

func (c *streamConnection) acceptCommitted(events []sessionStreamEvent) []sessionStreamEvent {
	accepted := make([]sessionStreamEvent, 0, len(events))
	for _, event := range events {
		if !c.accepts(event) {
			continue
		}
		accepted = append(accepted, event)
		if c.replayedEventIDs == nil {
			c.replayedEventIDs = make(map[string]struct{})
		}
		c.replayedEventIDs[event.ExternalID] = struct{}{}
	}
	return accepted
}

func (o *committedStreamOrder) notificationOnly(event sessionStreamEvent) bool {
	return o.enabled && !event.ProcessedAt.IsZero() && maevents.IsPublicSessionHistoryEvent(event.EventType) && event.EventType != "session.deleted"
}

func (c *streamConnection) orderedDelivery(order *committedStreamOrder, committed []sessionStreamEvent, delivery streamDelivery) ([]sessionStreamEvent, bool) {
	events := c.acceptCommitted(committed)
	if notification, ok := delivery.(sessionEventDelivery); ok && order.notificationOnly(notification.event) {
		return events, false
	}
	event, accepted := c.event(delivery)
	if accepted {
		events = append(events, event)
		if !order.enabled && !event.ProcessedAt.IsZero() && maevents.IsPublicSessionHistoryEvent(event.EventType) && event.EventType != "session.deleted" {
			order.cursorID = event.ExternalID
		}
	}
	return events, event.EventType == "session.deleted"
}
