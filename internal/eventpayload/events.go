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
		event.Payload, event.PayloadBlobUUID, err = s.prepare(ctx, organizationUUID, workspaceUUID, event.Payload, summary)
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
	session, found, err := s.database.GetSession(ctx, workspaceUUID, sessionID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	if ifAbsent {
		events, err = s.skipPersistedLargeEvents(ctx, workspaceUUID, sessionID, events)
		if err != nil {
			return nil, err
		}
	}
	prepared, err := s.PreparePublic(ctx, session.OrganizationUUID, workspaceUUID, events)
	if err != nil {
		return nil, err
	}
	var created []db.SessionEvent
	if ifAbsent {
		created, err = s.database.AppendSessionEventsIfAbsent(ctx, workspaceUUID, sessionID, prepared)
	} else {
		created, err = s.database.AppendSessionEvents(ctx, workspaceUUID, sessionID, prepared, outcomes)
	}
	if err != nil {
		return nil, err
	}
	return RestoreCreatedPublic(created, events), nil
}

// Avoid uploading already persisted events during worker replay. The insert still
// handles concurrent duplicates transactionally; their uploads become GC candidates.
func (s *Store) skipPersistedLargeEvents(ctx context.Context, workspaceUUID, sessionID string, events []db.SessionEvent) ([]db.SessionEvent, error) {
	missing := make([]db.SessionEvent, 0, len(events))
	for _, event := range events {
		if !ExceedsThreshold(len(event.Payload)) {
			missing = append(missing, event)
			continue
		}
		_, err := s.database.GetSessionEvent(ctx, workspaceUUID, sessionID, event.ExternalID)
		if errors.Is(err, db.ErrNotFound) {
			missing = append(missing, event)
		} else if err != nil {
			return nil, err
		}
	}
	return missing, nil
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
	prepared, err := s.skipPersistedInternalEvents(ctx, session.WorkspaceUUID, inputs)
	if err != nil {
		return nil, err
	}
	for i := range prepared {
		event := &prepared[i]
		summary, _, err := Summarize(event.Payload, event.EventType)
		if err != nil {
			return nil, err
		}
		event.Payload, event.PayloadBlobUUID, err = s.prepare(ctx, session.OrganizationUUID, session.WorkspaceUUID, event.Payload, summary)
		if err != nil {
			return nil, err
		}
	}
	created, err := s.database.AppendCodeSessionInternalEvents(ctx, session.ExternalID, epoch, prepared)
	if err != nil {
		return nil, err
	}
	originals := make(map[string]json.RawMessage, len(inputs))
	for _, event := range inputs {
		if _, exists := originals[event.ExternalID]; !exists {
			originals[event.ExternalID] = event.Payload
		}
	}
	for i := range created {
		created[i].Payload = originals[created[i].ExternalID]
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

// Preflight avoids S3 writes for replays; the insert still arbitrates concurrent requests.
func (s *Store) skipPersistedInternalEvents(ctx context.Context, workspaceUUID string, inputs []db.AppendCodeSessionInternalEventInput) ([]db.AppendCodeSessionInternalEventInput, error) {
	missing := make([]db.AppendCodeSessionInternalEventInput, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, event := range inputs {
		if event.IdempotencyKey != "" {
			if seen[event.IdempotencyKey] {
				continue
			}
			seen[event.IdempotencyKey] = true
			if ExceedsThreshold(len(event.Payload)) {
				exists, err := s.database.HasCodeSessionInternalEvent(ctx, workspaceUUID, event.IdempotencyKey)
				if err != nil {
					return nil, err
				}
				if exists {
					continue
				}
			}
		}
		missing = append(missing, event)
	}
	return missing, nil
}
