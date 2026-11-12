package db

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"github.com/superduck-ai/yourbatis"
)

type DSHFinish struct {
	Result      string
	FailureCode *string
}

type DSHPublicEvent struct {
	ID           string
	Payload      []byte
	Events       []SessionEvent
	Thread       *SessionThread
	DSHSessionID string
	StoredEvents []DSHStoredEvent
}

type DSHPublishResult struct {
	Accepted   int
	Duplicates int
	Changes    SessionEventChanges
}

type DSHPermission struct {
	RequestID     string
	InputID       string
	DSHSessionID  string
	ToolUseID     string
	ThreadID      string
	Turn          string
	Tool          []byte
	Input         []byte
	Available     *bool
	PublicEventID string
	State         string
	Decision      []byte
}

func lockDSHSession(ctx context.Context, executor yourbatis.Executor, scope DSHScope) (Session, SessionThread, error) {
	session, err := lockSessionForEvents(ctx, NewSessionMapper(executor), scope.WorkspaceUUID, scope.SessionID)
	if err != nil {
		return Session{}, SessionThread{}, err
	}
	if session.OrganizationUUID != scope.OrganizationUUID {
		return Session{}, SessionThread{}, ErrNotFound
	}
	if err := DSHSessionWriteError(session); err != nil {
		return Session{}, SessionThread{}, err
	}
	code, found, err := NewDSHExecutionMapper(executor).FindCode(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID)
	if err != nil {
		return Session{}, SessionThread{}, err
	}
	if !found || code.UUID != scope.CodeSessionUUID {
		return Session{}, SessionThread{}, ErrNotFound
	}
	if err := lockDSHWorker(ctx, executor, scope); err != nil {
		return Session{}, SessionThread{}, err
	}
	primary, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	return session, primary.thread(), err
}

func DSHSessionWriteError(session Session) error {
	if session.Status == "terminated" {
		return ErrDSHSessionTerminated
	}
	if session.ArchivedAt != nil {
		return ErrDSHSessionArchived
	}
	return nil
}

func (d *DB) ValidateDSHControlInputs(ctx context.Context, scope DSHScope, inputIDs []string) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if _, _, err := lockDSHSession(ctx, executor, scope); err != nil {
			return err
		}
		mapper := NewDSHExecutionMapper(executor)
		for _, id := range inputIDs {
			_, found, err := mapper.Execution(ctx, scope, id)
			if err != nil {
				return err
			}
			if found {
				return ErrDSHInputNotEligible
			}
		}
		return nil
	})
}

func startDSHInput(ctx context.Context, executor yourbatis.Executor, scope DSHScope, execution dshInputExecutionRow, budget bool) (SessionEvent, bool, error) {
	mapper := NewDSHExecutionMapper(executor)
	summary, err := mapper.Summary(ctx, scope)
	if err != nil {
		return SessionEvent{}, false, err
	}
	status, _ := dshPrimaryState(summary, nil, budget)
	if status != "running" {
		return SessionEvent{}, false, ErrDSHInputNotEligible
	}
	if summary.ActiveInputID != nil && *summary.ActiveInputID != execution.InputID {
		return SessionEvent{}, false, ErrDSHInputNotEligible
	}
	if _, err := mapper.StartExecution(ctx, scope, execution.InputID); err != nil {
		return SessionEvent{}, false, err
	}
	event, changed, err := NewSessionEventMapper(executor).MarkInputProcessed(ctx, scope.WorkspaceUUID, scope.SessionID, execution.InputID, time.Now().UTC().Truncate(time.Microsecond))
	return event.event(), changed, err
}

func (d *DB) FinishDSHInput(ctx context.Context, scope DSHScope, inputID string, completion DSHFinish) (DSHFinish, SessionEventChanges, error) {
	var changes SessionEventChanges
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, primary, err := lockDSHSession(ctx, executor, scope)
		if err != nil {
			return err
		}
		mapper := NewDSHExecutionMapper(executor)
		execution, found, err := mapper.Execution(ctx, scope, inputID)
		if err != nil {
			return err
		}
		if !found || execution.Status == "accepted" {
			return ErrDSHInputNotEligible
		}
		if execution.Status == "completed" {
			if execution.Result == nil || *execution.Result != completion.Result || !sameDSHParent(execution.FailureCode, completion.FailureCode) {
				return ErrDSHFinishConflict
			}
			return nil
		}
		if _, err := mapper.FinishExecution(ctx, scope, inputID, completion.Result, completion.FailureCode); err != nil {
			return err
		}
		if _, err := mapper.ClosePermissions(ctx, scope, inputID, ""); err != nil {
			return err
		}
		events, err := reduceDSHStateWithBoundary(ctx, executor, &session, primary, scope, "finish:"+inputID, true)
		changes.Events = events
		return err
	})
	return completion, changes, err
}

func (d *DB) PublishDSHEvents(ctx context.Context, scope DSHScope, batch []DSHPublicEvent) (DSHPublishResult, error) {
	var result DSHPublishResult
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, primary, err := lockDSHSession(ctx, executor, scope)
		if err != nil {
			return err
		}
		mapper := NewDSHExecutionMapper(executor)
		for _, item := range batch {
			digest, err := jsoncanonical.Digest(item.Payload)
			if err != nil {
				return ErrInvalidState
			}
			old, found, err := mapper.Receipt(ctx, scope, item.ID)
			if err != nil {
				return err
			}
			if found && old != digest {
				return ErrDSHPublicConflict
			}
			original := item.Events
			if err := prepareDSHCoordinationEvents(ctx, executor, scope, session, primary, &item); err != nil {
				return err
			}
			if found {
				repaired, err := replayDSHPublicHistory(ctx, executor, session, primary, original, item.Events, item.StoredEvents)
				if err != nil {
					return err
				}
				result.Changes.Events = append(result.Changes.Events, repaired...)
				result.Duplicates++
				continue
			}
			if err := validateDSHPublicHistory(ctx, executor, session, primary, item.Events, false, item.StoredEvents); err != nil {
				return err
			}
			if item.Thread != nil {
				if err := createDSHThread(ctx, executor, scope, session, primary, item); err != nil {
					return err
				}
			}
			if _, err := mapper.InsertReceipt(ctx, scope, item.ID, digest); err != nil {
				return err
			}
			for _, event := range item.Events {
				if event.EventType == "session.thread_status_terminated" {
					if _, err := mapper.ClosePermissions(ctx, scope, "", event.StatusThreadID); err != nil {
						return err
					}
				}
			}
			persisted, err := dshEventsForStorage(ctx, executor, scope, item.Events, item.StoredEvents)
			if err != nil {
				return err
			}
			events, err := insertDSHHistory(ctx, executor, &session, primary, persisted)
			if err != nil {
				return err
			}
			if err := validateDSHPublicHistory(ctx, executor, session, primary, item.Events, true, item.StoredEvents); err != nil {
				return err
			}
			result.Changes.Events = append(result.Changes.Events, events...)
			result.Accepted++
		}
		events, err := reduceDSHState(ctx, executor, &session, primary, scope, "public:"+publicSeed(batch))
		result.Changes.Events = append(result.Changes.Events, events...)
		return err
	})
	if err != nil {
		return DSHPublishResult{}, err
	}
	return result, nil
}

func publicSeed(batch []DSHPublicEvent) string {
	ids := make([]string, len(batch))
	for i, item := range batch {
		ids[i] = item.ID
	}
	raw, _ := json.Marshal(ids)
	return string(raw)
}

func insertDSHHistory(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, events []SessionEvent) ([]SessionEvent, error) {
	var result []SessionEvent
	for _, event := range events {
		if event.UUID == "" {
			event.UUID = uuid.NewV4().String()
		}
		write, err := insertSessionEventIfAbsentTx(ctx, executor, session, primary, event)
		if err != nil {
			return nil, err
		}

		if write.Inserted {
			result = append(result, write.Event)
			mirror, err := insertDSHStatusMirror(ctx, executor, session, primary, event)
			if err != nil {
				return nil, err
			}
			result = append(result, mirror...)
		}
	}
	return result, nil
}

func createDSHThread(ctx context.Context, executor yourbatis.Executor, scope DSHScope, session Session, primary SessionThread, item DSHPublicEvent) error {
	parentThread, err := dshParentThread(ctx, executor, scope, session, primary, item.DSHSessionID)
	if err != nil {
		return err
	}
	parentID := parentThread.ExternalID
	thread := *item.Thread
	thread.OrganizationUUID, thread.WorkspaceUUID = session.OrganizationUUID, session.WorkspaceUUID
	thread.SessionUUID, thread.SessionExternalID = session.UUID, session.ExternalID
	thread.ParentThreadUUID, thread.ParentThreadExternalID = &parentThread.UUID, &parentID
	if thread.UUID == "" {
		thread.UUID = uuid.NewV4().String()
	}
	if _, _, err := NewSessionThreadMapper(executor).InsertIfAbsent(ctx, sessionThreadWriteParameters(thread)); err != nil {
		return err
	}
	existing, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, thread.ExternalID)
	if err != nil {
		return mapNoRows(err)
	}
	if existing.ParentThreadUUID == nil || *existing.ParentThreadUUID != parentThread.UUID {
		return ErrDSHPublicConflict
	}
	rows, err := NewDSHExecutionMapper(executor).BindTranscript(ctx, scope, item.DSHSessionID, thread.ExternalID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrDSHPublicConflict
	}
	return nil
}

func dshParentThread(ctx context.Context, executor yourbatis.Executor, scope DSHScope, session Session, primary SessionThread, dshSessionID string) (SessionThread, error) {
	transcript, found, err := NewDSHTranscriptMapper(executor).Find(ctx, scope, dshSessionID)
	if err != nil {
		return SessionThread{}, err
	}
	if !found || transcript.ParentDSHSessionID == nil {
		return SessionThread{}, ErrDSHInputNotEligible
	}
	parent, found, err := NewDSHTranscriptMapper(executor).Find(ctx, scope, *transcript.ParentDSHSessionID)
	if err != nil {
		return SessionThread{}, err
	}
	if !found {
		return SessionThread{}, ErrDSHInputNotEligible
	}
	parentID := primary.ExternalID
	if parent.SessionThreadID != nil {
		parentID = *parent.SessionThreadID
	}
	parentThread, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, parentID)
	return parentThread.thread(), err
}
