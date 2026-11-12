package sessions

import (
	"context"
	"errors"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type committedStreamOrder struct {
	last     int64
	boundary func(context.Context) (int64, error)
	history  func(ctx context.Context, after, through int64) ([]sessionStreamEvent, error)
}

func (h *Handler) newCommittedStreamOrder(session db.Session, connection *streamConnection, last int64) *committedStreamOrder {
	return &committedStreamOrder{
		last:     last,
		boundary: func(ctx context.Context) (int64, error) { return h.committedBoundary(ctx, session) },
		history: func(ctx context.Context, after, through int64) ([]sessionStreamEvent, error) {
			return h.readStreamHistory(ctx, session.WorkspaceUUID, session.ExternalID, connection, db.ListSessionEventsPageParams{
				ProcessedOnly: true, AfterSeq: &after, ThroughSeq: &through,
			})
		},
	}
}

type streamCursor struct {
	page    *db.SessionEventPageCursor
	eventID string
	input   bool
}

func (h *Handler) resolveStreamCursor(ctx context.Context, session db.Session, connection *streamConnection, eventID string) (streamCursor, error) {
	if eventID == "" {
		return streamCursor{}, nil
	}
	cursor, err := h.db.GetSessionEvent(ctx, session.WorkspaceUUID, session.ExternalID, eventID)
	if errors.Is(err, db.ErrNotFound) {
		return streamCursor{}, invalidRequest(db.ErrInvalidCursor)
	}
	if err != nil {
		return streamCursor{}, internalError("Could not load stream cursor", err)
	}
	if !maevents.IsPublicSessionHistoryEvent(cursor.EventType) || !connection.matches(sessionStreamEventFrom(cursor)) {
		return streamCursor{}, invalidRequest(db.ErrInvalidCursor)
	}
	input := maevents.IsPublicWorkerInputEvent(cursor.EventType)
	return streamCursor{page: &db.SessionEventPageCursor{ExternalID: eventID, AtQueuedPosition: input}, eventID: eventID, input: input}, nil
}

func (h *Handler) committedBoundary(ctx context.Context, session db.Session) (int64, error) {
	seq, err := h.db.SessionLastEventSeq(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return 0, internalError("Could not read session event position", err)
	}
	return seq, nil
}

func (o *committedStreamOrder) fill(ctx context.Context, connection *streamConnection, through int64) ([]sessionStreamEvent, bool, error) {
	events, err := o.history(ctx, o.last, through)
	if err != nil {
		return nil, false, err
	}
	o.last = through
	return connection.acceptCommitted(events), slices.ContainsFunc(events, isSessionDeletion), nil
}

func (o *committedStreamOrder) catchUp(ctx context.Context, connection *streamConnection) ([]sessionStreamEvent, bool, error) {
	boundary, err := o.boundary(ctx)
	if err != nil || boundary <= o.last {
		return nil, false, err
	}
	return o.fill(ctx, connection, boundary)
}

func (o *committedStreamOrder) events(ctx context.Context, connection *streamConnection, delivery streamDelivery) ([]sessionStreamEvent, bool, error) {
	notification, isEvent := delivery.(sessionEventDelivery)
	var ordered []sessionStreamEvent
	var filledDeletion bool
	var err error
	switch {
	case !isEvent:
		ordered, filledDeletion, err = o.catchUp(ctx, connection)
	case notification.event.Seq == 0:
	case notification.event.Seq <= o.last:
		return nil, isSessionDeletion(notification.event), nil
	case notification.event.Seq == o.last+1:
		o.last = notification.event.Seq
	default:
		ordered, filledDeletion, err = o.fill(ctx, connection, notification.event.Seq)
	}
	if err != nil {
		return nil, false, err
	}
	events, deleted := connection.passthrough(delivery)
	return append(ordered, events...), deleted || filledDeletion, nil
}

func isSessionDeletion(event sessionStreamEvent) bool {
	return event.EventType == "session.deleted"
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

func (c *streamConnection) passthrough(delivery streamDelivery) ([]sessionStreamEvent, bool) {
	event, accepted := c.event(delivery)
	if !accepted {
		return nil, isSessionDeletion(event)
	}
	return []sessionStreamEvent{event}, isSessionDeletion(event)
}
