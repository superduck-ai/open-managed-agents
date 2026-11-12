package db

import (
	"context"
	"encoding/json"
	"time"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

func nextSessionPublicationTime(ctx context.Context, executor yourbatis.Executor, workspaceUUID, sessionID string) (time.Time, error) {
	last, found, err := NewSessionEventMapper(executor).LatestProcessed(ctx, workspaceUUID, sessionID)
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if found && last.ProcessedAt != nil && !now.After(*last.ProcessedAt) {
		now = last.ProcessedAt.Add(time.Microsecond)
	}
	return now, nil
}

func stampSessionPublicationTime(ctx context.Context, executor yourbatis.Executor, session Session, event SessionEvent) (SessionEvent, error) {
	if event.ProcessedAt.IsZero() || event.publicationTimeAssigned {
		return event, nil
	}
	var err error
	event.ProcessedAt, err = nextSessionPublicationTime(ctx, executor, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return event, err
	}
	category := maevents.CategoryFor(event.EventType)
	if category == maevents.CategorySessionStatus || category == maevents.CategoryThreadStatus {
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(event.Payload, &fields); err != nil {
			return event, err
		}
		if fields == nil {
			return event, ErrInvalidState
		}
		fields["processed_at"], _ = json.Marshal(event.ProcessedAt.Format(time.RFC3339Nano))
		event.Payload, err = json.Marshal(fields)
	}
	return event, err
}
