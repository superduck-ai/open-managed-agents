package db

import (
	"context"
	"github.com/superduck-ai/yourbatis"
)

func archivedThreadPrimaryCopy(event SessionEvent, primary SessionThread) (SessionEvent, error) {
	return sessionThreadStatusCopy(event, primary, "primary")
}

func appendArchivedThreadPrimaryEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent) ([]SessionEvent, error) {
	row, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	primary := row.thread()
	result := append([]SessionEvent(nil), events...)
	for _, event := range events {
		if event.EventType != "session.thread_status_terminated" || event.StatusThreadID == primary.ExternalID {
			continue
		}
		copy, err := archivedThreadPrimaryCopy(event, primary)
		if err != nil {
			return nil, err
		}
		stored, inserted, err := NewSessionEventMapper(executor).InsertIfAbsent(ctx, sessionEventWriteParameters(copy))
		if err != nil {
			return nil, err
		}
		if inserted {
			result = append(result, stored.event())
		}
	}
	return result, nil
}
