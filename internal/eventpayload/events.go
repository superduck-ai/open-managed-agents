package eventpayload

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Store) PreparePublic(ctx context.Context, organizationUUID, workspaceUUID string, events []db.SessionEvent) ([]db.SessionEvent, error) {
	prepared := append([]db.SessionEvent(nil), events...)
	for i := range prepared {
		event := &prepared[i]
		summary, toolID, err := Summarize(event.Payload, event.EventType)
		if err != nil {
			return nil, err
		}
		event.ToolUseID = toolID
		event.Payload, event.PayloadBlobUUID, err = s.prepare(ctx, organizationUUID, workspaceUUID, "public/"+event.ExternalID, event.Payload, summary)
		if err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

func (s *Store) RestorePublic(ctx context.Context, event db.SessionEvent) (db.SessionEvent, error) {
	payload, err := s.restore(ctx, event.WorkspaceUUID, event.Payload, event.PayloadBlobUUID)
	if err != nil {
		return db.SessionEvent{}, err
	}
	event.Payload = payload
	return event, nil
}

func (s *Store) RestorePublicTx(ctx context.Context, tx db.ManagedAgentEventTx, event db.SessionEvent) (db.SessionEvent, error) {
	payload, err := s.restoreTx(ctx, tx, event.WorkspaceUUID, event.Payload, event.PayloadBlobUUID)
	if err != nil {
		return db.SessionEvent{}, err
	}
	event.Payload = payload
	return event, nil
}

func (s *Store) RestorePublicPage(ctx context.Context, events []db.SessionEvent) ([]db.SessionEvent, error) {
	restored := append([]db.SessionEvent(nil), events...)
	for i := range restored {
		event, err := s.RestorePublic(ctx, restored[i])
		if err != nil {
			return nil, err
		}
		restored[i] = event
	}
	return restored, nil
}

func (s *Store) ListSessionEventsPage(ctx context.Context, params db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	events, more, err := s.database.ListSessionEventsPage(ctx, params)
	if err != nil {
		return nil, false, err
	}
	events, err = s.RestorePublicPage(ctx, events)
	return events, more, err
}

func (s *Store) GetSessionEvent(ctx context.Context, workspaceUUID, sessionID, eventID string) (db.SessionEvent, error) {
	event, err := s.database.GetSessionEvent(ctx, workspaceUUID, sessionID, eventID)
	if err != nil {
		return db.SessionEvent{}, err
	}
	return s.RestorePublic(ctx, event)
}

func (s *Store) AppendSessionEvents(ctx context.Context, workspaceUUID, sessionID string, events []db.SessionEvent, outcomes json.RawMessage) ([]db.SessionEvent, error) {
	return s.appendPublic(ctx, workspaceUUID, sessionID, events, outcomes, false)
}

func (s *Store) AppendSessionEventsIfAbsent(ctx context.Context, workspaceUUID, sessionID string, events []db.SessionEvent) ([]db.SessionEvent, error) {
	return s.appendPublic(ctx, workspaceUUID, sessionID, events, nil, true)
}

func (s *Store) appendPublic(ctx context.Context, workspaceUUID, sessionID string, events []db.SessionEvent, outcomes json.RawMessage, ifAbsent bool) ([]db.SessionEvent, error) {
	var created []db.SessionEvent
	err := s.WithEventTx(ctx, func(ctx context.Context, tx db.ManagedAgentEventTx) error {
		created = nil
		session, err := tx.LockSessionForEvents(ctx, workspaceUUID, sessionID)
		if err != nil {
			return err
		}
		if ifAbsent {
			created, err = s.AppendPublicTx(ctx, tx, session, events, nil)
		} else {
			prepared, prepareErr := s.PreparePublic(ctx, session.OrganizationUUID, workspaceUUID, events)
			if prepareErr != nil {
				return prepareErr
			}
			created, err = tx.AppendSessionEvents(ctx, session, prepared, outcomes)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return RestoreCreatedPublic(created, events), nil
}

// AppendPublicTx compares full retry bodies before reusing an existing blob.
func (s *Store) AppendPublicTx(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, events []db.SessionEvent, ignoredFields []string) ([]db.SessionEvent, error) {
	var created []db.SessionEvent
	for _, event := range events {
		original := event
		stored, err := tx.GetSessionEvent(ctx, session, event.ExternalID)
		if err == nil {
			restored, err := s.RestorePublicTx(ctx, tx, stored)
			if err != nil {
				return nil, err
			}
			matches, err := tx.EventPayloadsMatch(ctx, restored.Payload, event.Payload, ignoredFields)
			if err != nil {
				return nil, err
			}
			if !matches {
				return nil, db.ErrSessionEventConflict
			}
			event.Payload, event.PayloadBlobUUID, event.ToolUseID = stored.Payload, stored.PayloadBlobUUID, stored.ToolUseID
		} else if errors.Is(err, db.ErrNotFound) {
			prepared, err := s.PreparePublic(ctx, session.OrganizationUUID, session.WorkspaceUUID, []db.SessionEvent{event})
			if err != nil {
				return nil, err
			}
			event = prepared[0]
		} else {
			return nil, err
		}
		inserted, err := tx.AppendSessionEventsIfAbsent(ctx, session, []db.SessionEvent{event}, ignoredFields)
		if err != nil {
			return nil, err
		}
		created = append(created, RestoreCreatedPublic(inserted, []db.SessionEvent{original})...)
	}
	return created, nil
}

// RestoreCreatedPublic reuses original bytes only for rows inserted by this call.
// Idempotent conflicts must load the persisted event instead.
func RestoreCreatedPublic(created, originals []db.SessionEvent) []db.SessionEvent {
	payloads := make(map[string]json.RawMessage, len(originals))
	for _, event := range originals {
		if _, exists := payloads[event.ExternalID]; !exists {
			payloads[event.ExternalID] = event.Payload
		}
	}
	for i := range created {
		if payload, ok := payloads[created[i].ExternalID]; ok {
			created[i].Payload = payload
		}
	}
	return created
}

func (s *Store) AppendInternal(ctx context.Context, session db.CodeSession, epoch int64, inputs []db.AppendCodeSessionInternalEventInput) ([]db.CodeSessionInternalEvent, error) {
	var created []db.CodeSessionInternalEvent
	err := s.WithEventTx(ctx, func(ctx context.Context, tx db.ManagedAgentEventTx) error {
		created = nil
		parent, err := tx.LockSessionForEvents(ctx, session.WorkspaceUUID, session.SessionExternalID)
		if err != nil {
			return err
		}
		worker, err := tx.LockPublicEventWorker(ctx, parent, db.SessionEventWorker{CodeSessionUUID: session.UUID, Epoch: epoch})
		if err != nil {
			return err
		}
		created, err = s.AppendInternalTx(ctx, tx, worker, inputs)
		return err
	})
	return created, err
}

func (s *Store) AppendInternalTx(ctx context.Context, tx db.ManagedAgentEventTx, session db.CodeSession, inputs []db.AppendCodeSessionInternalEventInput) ([]db.CodeSessionInternalEvent, error) {
	var created []db.CodeSessionInternalEvent
	for _, input := range inputs {
		if input.CreatedAt.IsZero() {
			input.CreatedAt = EventTime(ctx)
		}
		original := input.Payload
		stored, found, err := tx.GetCodeSessionInternalEvent(ctx, session.WorkspaceUUID, input.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if found {
			restored, err := s.RestoreInternalTx(ctx, tx, stored)
			if err != nil {
				return nil, err
			}
			matches, err := tx.EventPayloadsMatch(ctx, restored.Payload, input.Payload, nil)
			if err != nil {
				return nil, err
			}
			if !matches {
				return nil, db.ErrCodeSessionInternalEventConflict
			}
			input.Payload, input.PayloadBlobUUID = stored.Payload, stored.PayloadBlobUUID
		} else {
			summary, _, err := Summarize(input.Payload, input.EventType)
			if err != nil {
				return nil, err
			}
			input.Payload, input.PayloadBlobUUID, err = s.prepare(ctx, session.OrganizationUUID, session.WorkspaceUUID, "internal/"+input.IdempotencyKey, input.Payload, summary)
			if err != nil {
				return nil, err
			}
		}
		inserted, err := tx.AppendCodeSessionInternalEvents(ctx, session, []db.AppendCodeSessionInternalEventInput{input})
		if err != nil {
			return nil, err
		}
		for i := range inserted {
			inserted[i].Payload = original
			session.LastInternalSequenceNum = inserted[i].SequenceNum
		}
		created = append(created, inserted...)
	}
	return created, nil
}

func (s *Store) ListCodeSessionInternalEventsPage(ctx context.Context, params db.ListCodeSessionInternalEventsPageParams) ([]db.CodeSessionInternalEvent, bool, error) {
	events, more, err := s.database.ListCodeSessionInternalEventsPage(ctx, params)
	if err != nil {
		return nil, false, err
	}
	for i := range events {
		events[i].Payload, err = s.restore(ctx, events[i].WorkspaceUUID, events[i].Payload, events[i].PayloadBlobUUID)
		if err != nil {
			return nil, false, err
		}
	}
	return events, more, nil
}
