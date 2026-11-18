package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/superduck-ai/yourbatis"
)

type SessionTitleSource struct {
	WorkspaceUUID string `json:"workspace_uuid"`
	SessionUUID   string `json:"session_uuid"`
	EventUUID     string `json:"event_uuid"`
}

type SessionTitleEnqueuer func(context.Context, *sql.Tx, SessionTitleSource) error

func enqueueFirstSessionTitle(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent, enqueue SessionTitleEnqueuer) error {
	if enqueue == nil || !session.NeedsTitle() {
		return nil
	}
	index := slices.IndexFunc(events, func(event SessionEvent) bool { return event.EventType == "user.message" })
	if index < 0 {
		return nil
	}
	firstUUID, err := NewSessionEventMapper(executor).FirstUserMessageUUID(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil || firstUUID != events[index].UUID {
		return err
	}
	return enqueue(ctx, executor.(*yourbatis.Tx).SQLTx(), SessionTitleSource{
		WorkspaceUUID: session.WorkspaceUUID,
		SessionUUID:   session.UUID,
		EventUUID:     firstUUID,
	})
}

func (s Session) NeedsTitle() bool {
	return s.ArchivedAt == nil && (s.Title == nil || *s.Title == "")
}

func (d *DB) SaveGeneratedSessionTitle(ctx context.Context, session Session, title string, event SessionEvent) (SessionEvent, bool, error) {
	var stored SessionEvent
	var saved bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewSessionMapper(executor)
		current, err := lockSessionForEvents(ctx, mapper, session.WorkspaceUUID, session.ExternalID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !current.NeedsTitle() {
			return nil
		}
		count, err := mapper.SetGeneratedTitle(ctx, current.WorkspaceUUID, current.ExternalID, title, event.CreatedAt)
		if err != nil || count == 0 {
			return err
		}
		primary, err := NewSessionThreadMapper(executor).FindPrimary(ctx, current.WorkspaceUUID, current.ExternalID)
		if err != nil {
			return err
		}
		write, err := insertSessionEventTx(ctx, executor, &current, primary.thread(), event)
		stored, saved = write.Event, write.Inserted
		return err
	})
	return stored, saved && err == nil, err
}
