package db

import (
	"context"
	"errors"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/superduck-ai/yourbatis"
)

func validateDSHPublicHistory(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, events []SessionEvent, required bool, representations []DSHStoredEvent) error {
	for _, event := range events {
		if _, status := maevents.ThreadStatus(event.EventType); status {
			continue
		}
		stored, err := NewSessionEventMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, event.ExternalID)
		if errors.Is(mapNoRows(err), ErrNotFound) && !required {
			continue
		}
		if errors.Is(mapNoRows(err), ErrNotFound) {
			return ErrDSHPublicHistoryInconsistent
		}
		if err != nil {
			return err
		}
		thread := primary.ExternalID
		if event.ThreadExternalID != nil {
			thread = *event.ThreadExternalID
		}
		equal, err := equalDSHStoredPayload(ctx, executor, session, event, stored, representations)
		if err != nil {
			return err
		}
		if stored.EventType != event.EventType || stored.ThreadExternalID == nil || *stored.ThreadExternalID != thread || !equal {
			if required {
				return ErrDSHPublicHistoryInconsistent
			}
			return ErrDSHPublicConflict
		}
	}
	return nil
}

func replayDSHPublicHistory(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, original, projected []SessionEvent, representations []DSHStoredEvent) ([]SessionEvent, error) {
	err := validateDSHPublicHistory(ctx, executor, session, primary, projected, true, representations)
	if !errors.Is(err, ErrDSHPublicHistoryInconsistent) || len(original) != 1 || !isDSHCoordinationEvent(original[0]) {
		return nil, err
	}
	if err := validateDSHPublicHistory(ctx, executor, session, primary, original, true, representations); err != nil {
		return nil, err
	}
	mirrors := projected[len(original):]
	scope := DSHScope{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID}
	prepared, err := dshEventsForStorage(ctx, executor, scope, mirrors, representations)
	if err != nil {
		return nil, err
	}
	inserted, err := insertDSHHistory(ctx, executor, &session, primary, prepared)
	if err != nil {
		return nil, err
	}
	if err := validateDSHPublicHistory(ctx, executor, session, primary, mirrors, true, representations); err != nil {
		return nil, err
	}
	return inserted, nil
}
