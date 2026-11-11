package db

import (
	"context"
	"github.com/superduck-ai/yourbatis"
)

func archivedThreadPrimaryCopy(event SessionEvent, primary SessionThread) (SessionEvent, error) {
	return sessionThreadStatusCopy(event, primary, "primary")
}

func archivedThreadNeedsPrimaryCopy(event SessionEvent, primary SessionThread) bool {
	return event.EventType == "session.thread_status_terminated" && event.StatusThreadID != primary.ExternalID && event.ThreadExternalID != nil && *event.ThreadExternalID != primary.ExternalID
}

func appendArchivedThreadPrimaryEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent) ([]SessionEvent, error) {
	row, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	primary := row.thread()
	result := append([]SessionEvent(nil), events...)
	for _, event := range events {
		if !archivedThreadNeedsPrimaryCopy(event, primary) {
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

func archiveThreadEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, thread SessionThread, event SessionEvent) (sessionEventBatchResult, error) {
	mapper := NewDSHExecutionMapper(executor)
	code, dsr, err := mapper.FindCode(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	if !dsr || thread.ParentThreadUUID == nil {
		return insertSessionEventsTx(ctx, executor, session, []SessionEvent{event})
	}
	worker, found, err := NewCodeSessionMapper(executor).LockCodeSessionByExternalID(ctx, code.ExternalID)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	if !found || worker.UUID != code.UUID || worker.SessionUUID != session.UUID || worker.OrganizationUUID != session.OrganizationUUID || worker.WorkspaceUUID != session.WorkspaceUUID {
		return sessionEventBatchResult{}, ErrNotFound
	}
	row, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	scope := DSHScope{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionID: session.ExternalID, CodeSessionID: code.ExternalID, CodeSessionUUID: code.UUID}
	status, err := newSessionStatusEvent(event, event.EventType, thread, nil)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	status.ThreadExternalID = &thread.ExternalID
	inserted, err := insertDSHHistory(ctx, executor, &session, row.thread(), []SessionEvent{status})
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	if _, err = mapper.ClosePermissions(ctx, scope, "", thread.ExternalID); err != nil {
		return sessionEventBatchResult{}, err
	}
	reduced, err := reduceDSHState(ctx, executor, &session, row.thread(), scope, "archive:"+event.ExternalID)
	return sessionEventBatchResult{Events: append(inserted, reduced...)}, err
}
