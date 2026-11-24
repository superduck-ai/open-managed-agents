package sessions

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type StreamEvent = sessionStreamEvent

type StreamOptions struct {
	ThreadID     string
	AfterEventID string
	WaitForInput bool
	DeltaTypes   map[string]struct{}
}

type EventSubscription struct {
	handler       *Handler
	sessionID     string
	threadID      string
	workspaceUUID string
	waitingInput  bool
	inputBoundary time.Time
	id            int64
	deliveries    <-chan streamDelivery
	connection    *streamConnection
	replay        []sessionStreamEvent
	replayHistory []sessionStreamEvent
	cursorID      string
	replayed      bool
	ctx           context.Context
	cancel        context.CancelFunc
	closeOnce     sync.Once
}

func (h *Handler) SubscribeEvents(ctx context.Context, sessionID string, options StreamOptions) (*EventSubscription, error) {
	session, err := h.authorizeSessionContext(ctx, sessionID, sessionAccessEventsRead)
	if err != nil {
		return nil, err
	}
	var thread db.SessionThread
	if options.ThreadID == "" {
		thread, err = h.ensurePrimarySessionThread(ctx, session)
	} else {
		thread, err = h.db.GetSessionThread(ctx, session.WorkspaceUUID, sessionID, options.ThreadID)
		if err != nil {
			return nil, mapThreadLoadError(err, options.ThreadID)
		}
	}
	if err != nil {
		return nil, mapSessionLoadError(err, sessionID)
	}
	connection := newStreamConnection(thread.ExternalID, thread.ParentThreadUUID == nil, options.DeltaTypes)
	return h.subscribeSession(ctx, session, connection, options)
}

func (h *Handler) subscribeSession(ctx context.Context, session db.Session, connection *streamConnection, options StreamOptions) (*EventSubscription, error) {
	connection.sessionStream = options.ThreadID == ""
	cursorID := options.AfterEventID
	ctx, cancel := context.WithCancel(ctx)
	id, deliveries := h.streams.subscribe(session.WorkspaceUUID, session.ExternalID)
	if err := h.eventBus.Subscribe(ctx, session.ExternalID); err != nil {
		h.streams.unsubscribe(id)
		cancel()
		return nil, internalError("Could not subscribe to session event stream", err)
	}
	sub := &EventSubscription{handler: h, sessionID: session.ExternalID, threadID: connection.threadID, id: id, deliveries: deliveries, connection: connection, cursorID: cursorID, workspaceUUID: session.WorkspaceUUID, ctx: ctx, cancel: cancel}
	if err := sub.initializeReplay(ctx, options.WaitForInput); err != nil {
		sub.Close()
		return nil, err
	}
	context.AfterFunc(ctx, sub.Close)
	return sub, nil
}

func (s *EventSubscription) Receive(ctx context.Context) (StreamEvent, error) {
	return s.receive(ctx, nil)
}

func (s *EventSubscription) ReceiveUntil(ctx context.Context, wake <-chan time.Time) (StreamEvent, error) {
	return s.receive(ctx, wake)
}

func (s *EventSubscription) receive(ctx context.Context, heartbeat <-chan time.Time) (StreamEvent, error) {
	for {
		if err := ctx.Err(); err != nil {
			return StreamEvent{}, err
		}
		if err := s.ctx.Err(); err != nil {
			return StreamEvent{}, err
		}
		select {
		case <-heartbeat:
			return StreamEvent{}, ErrStreamHeartbeat
		default:
		}
		if len(s.replay) > 0 {
			event := s.replay[0]
			s.replay = s.replay[1:]
			if s.connection.accepts(event) {
				return event, nil
			}
			continue
		}
		if !s.replayed {
			s.connection.rememberReplay(s.cursorID, s.replayHistory)
			s.replayHistory = nil
			s.replayed = true
		}
		select {
		case <-heartbeat:
			return StreamEvent{}, ErrStreamHeartbeat
		case <-ctx.Done():
			return StreamEvent{}, ctx.Err()
		case <-s.ctx.Done():
			return StreamEvent{}, s.ctx.Err()
		case delivery, ok := <-s.deliveries:
			if !ok {
				return StreamEvent{}, io.EOF
			}
			if _, reset := delivery.(streamResetDelivery); reset {
				return StreamEvent{}, ErrStreamReset
			}
			value, ok := delivery.(sessionEventDelivery)
			if !ok {
				continue
			}
			event := value.event
			if s.waitingInput {
				if event.EventType == "session.deleted" {
					return StreamEvent{}, io.EOF
				}
				if event.ExternalID != s.cursorID || event.ProcessedAt.IsZero() || !s.connection.matches(event) {
					continue
				}
				if err := s.replayInput(ctx, event); err != nil {
					return StreamEvent{}, err
				}
				continue
			}
			if !s.inputBoundary.IsZero() && !event.ProcessedAt.IsZero() && event.ProcessedAt.Before(s.inputBoundary) {
				continue
			}
			accepted := s.connection.accepts(event)
			if accepted {
				return event, nil
			}
			if event.EventType == "session.deleted" {
				return StreamEvent{}, io.EOF
			}
		}
	}
}

func (s *EventSubscription) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.handler.streams.unsubscribe(s.id)
		if err := s.handler.eventBus.Unsubscribe(s.sessionID); err != nil {
			s.handler.logger.WarnContext(s.ctx, "unsubscribe session event stream", "session_id", s.sessionID, "error", err)
		}
	})
}

func (s *EventSubscription) initializeReplay(ctx context.Context, waitForInput bool) error {
	if waitForInput {
		anchor, err := s.handler.db.GetSessionEvent(ctx, s.workspaceUUID, s.sessionID, s.cursorID)
		if errors.Is(err, db.ErrNotFound) {
			return invalidRequest(db.ErrInvalidCursor)
		}
		if err != nil {
			return internalError("Could not load stream input", err)
		}
		event := sessionStreamEventFrom(anchor)
		if event.EventType != "user.message" || !s.connection.matches(event) {
			return invalidRequest(db.ErrInvalidCursor)
		}
		if event.ProcessedAt.IsZero() {
			s.waitingInput = true
			return nil
		}
		return s.replayInput(ctx, event)
	}
	replay, err := s.handler.loadStreamReplay(ctx, s.workspaceUUID, s.sessionID, s.connection, s.cursorID)
	s.replay, s.replayHistory = replay, replay
	return err
}

func (s *EventSubscription) replayInput(ctx context.Context, anchor sessionStreamEvent) error {
	replay, err := s.handler.loadStreamReplay(ctx, s.workspaceUUID, s.sessionID, s.connection, s.cursorID)
	if err != nil {
		return err
	}
	s.waitingInput = false
	s.inputBoundary = anchor.ProcessedAt
	s.replayed = false
	s.connection.replayedEventIDs = nil
	s.replay = append([]sessionStreamEvent{anchor}, replay...)
	s.replayHistory = s.replay
	return nil
}
