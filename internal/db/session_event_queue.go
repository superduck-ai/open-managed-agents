package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/superduck-ai/yourbatis"
)

type SessionEventDelivery string

const (
	SessionEventDeliveryRealtime      SessionEventDelivery = "realtime"
	SessionEventDeliveryStartupQueued SessionEventDelivery = "startup_queued"
)

type sessionEventQueueIdentityRow struct {
	ID               int64     `db:"id"`
	SessionEventUUID uuid.UUID `db:"session_event_uuid"`
}

type sessionEventQueueInsertRow struct {
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID    uuid.UUID `db:"workspace_uuid"`
	SessionUUID      uuid.UUID `db:"session_uuid"`
	SessionEventUUID uuid.UUID `db:"session_event_uuid"`
}

type sessionUUIDs struct {
	OrganizationUUID uuid.UUID
	WorkspaceUUID    uuid.UUID
	SessionUUID      uuid.UUID
	EnvironmentUUID  uuid.UUID
}

type sessionEventInsertRow struct {
	UUID              uuid.UUID `db:"uuid"`
	ExternalID        string    `db:"external_id"`
	OrganizationUUID  uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID     uuid.UUID `db:"workspace_uuid"`
	SessionUUID       uuid.UUID `db:"session_uuid"`
	SessionExternalID string    `db:"session_external_id"`
	ThreadUUID        uuid.UUID `db:"thread_uuid"`
	ThreadExternalID  string    `db:"thread_external_id"`
	EventType         string    `db:"event_type"`
	Payload           []byte    `db:"payload"`
	ProcessedAt       time.Time `db:"processed_at"`
	CreatedAt         time.Time `db:"created_at"`
}

// AppendSessionEventsForDelivery keeps the existing delivery path outside the
// managed-agent startup window. During startup it accepts exactly one
// user.message with an empty queue and records the public event and temporary
// delivery responsibility in the same transaction.
func (d *DB) AppendSessionEventsForDelivery(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
	events []SessionEvent,
	outcomeEvaluations json.RawMessage,
) ([]SessionEvent, SessionEventDelivery, error) {
	var created []SessionEvent
	delivery := SessionEventDeliveryRealtime
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		sessionMapper := NewSessionMapper(executor)
		sessionThreadMapper := NewSessionThreadMapper(executor)
		sessionEventMapper := NewSessionEventMapper(executor)
		sessionEventQueueMapper := NewSessionEventQueueMapper(executor)
		codeSessionMapper := NewCodeSessionMapper(executor)
		environmentWorkMapper := NewEnvironmentWorkMapper(executor)
		parsedWorkspaceUUID, err := parseDBUUID("workspace_uuid", workspaceUUID)
		if err != nil {
			return err
		}
		sessionRow, found, err := sessionMapper.LockSessionForEvents(
			ctx,
			parsedWorkspaceUUID,
			sessionExternalID,
		)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		session := sessionRow.session()
		if session.ArchivedAt != nil {
			return ErrSessionArchived
		}
		userMessageCount := lo.CountBy(events, func(event SessionEvent) bool {
			return event.EventType == "user.message"
		})
		shouldEnqueue := false
		if userMessageCount > 0 {
			shouldEnqueue, err = shouldQueueForStartup(
				ctx,
				codeSessionMapper,
				environmentWorkMapper,
				session,
			)
			if err != nil {
				return err
			}
		}
		if shouldEnqueue {
			if len(events) != 1 || userMessageCount != 1 {
				return ErrSessionStartupMessageConflict
			}
			sessionUUID, err := parseDBUUID("session_uuid", session.UUID)
			if err != nil {
				return err
			}
			hasQueuedEvents, err := sessionEventQueueMapper.SessionEventQueueExists(ctx, sessionUUID)
			if err != nil {
				return err
			}
			if hasQueuedEvents {
				return ErrSessionStartupMessageConflict
			}
		}

		created, err = insertSessionEventsWithMappers(
			ctx,
			sessionThreadMapper,
			sessionEventMapper,
			session,
			events,
		)
		if err != nil {
			return err
		}
		if shouldEnqueue {
			if err := enqueueSessionEventsTx(
				ctx,
				sessionEventQueueMapper,
				session,
				created,
			); err != nil {
				return err
			}
			delivery = SessionEventDeliveryStartupQueued
		}
		if len(outcomeEvaluations) > 0 {
			updated, err := sessionMapper.SetSessionOutcomeEvaluations(
				ctx,
				parsedWorkspaceUUID,
				session.ExternalID,
				outcomeEvaluations,
			)
			if err != nil {
				return err
			}
			if updated == 0 {
				return ErrNotFound
			}
			if updated != 1 {
				return ErrInvalidState
			}
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return created, delivery, nil
}

func insertSessionEventsWithMappers(
	ctx context.Context,
	sessionThreadMapper SessionThreadMapper,
	sessionEventMapper SessionEventMapper,
	session Session,
	events []SessionEvent,
) ([]SessionEvent, error) {
	parsedUUIDs, err := parseSessionUUIDs(session)
	if err != nil {
		return nil, err
	}
	primaryRow, found, err := sessionThreadMapper.GetPrimarySessionThread(
		ctx,
		parsedUUIDs.WorkspaceUUID,
		session.ExternalID,
	)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	primary := primaryRow.thread()

	created := make([]SessionEvent, 0, len(events))
	for _, input := range events {
		event := input
		event.OrganizationUUID = session.OrganizationUUID
		event.WorkspaceUUID = session.WorkspaceUUID
		event.SessionUUID = session.UUID
		event.SessionExternalID = session.ExternalID

		thread := primary
		if event.ThreadExternalID != nil {
			threadRow, found, err := sessionThreadMapper.GetSessionThreadByExternalID(
				ctx,
				parsedUUIDs.WorkspaceUUID,
				session.ExternalID,
				*event.ThreadExternalID,
			)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, ErrNotFound
			}
			thread = threadRow.thread()
		}
		threadUUID, err := parseDBUUID("thread_uuid", thread.UUID)
		if err != nil {
			return nil, err
		}
		eventUUID, err := parseDBUUID("event_uuid", event.UUID)
		if err != nil {
			return nil, err
		}
		row, err := sessionEventMapper.InsertSessionEvent(ctx, sessionEventInsertRow{
			UUID:              eventUUID,
			ExternalID:        event.ExternalID,
			OrganizationUUID:  parsedUUIDs.OrganizationUUID,
			WorkspaceUUID:     parsedUUIDs.WorkspaceUUID,
			SessionUUID:       parsedUUIDs.SessionUUID,
			SessionExternalID: session.ExternalID,
			ThreadUUID:        threadUUID,
			ThreadExternalID:  thread.ExternalID,
			EventType:         event.EventType,
			Payload:           []byte(event.Payload),
			ProcessedAt:       event.ProcessedAt,
			CreatedAt:         event.CreatedAt,
		})
		if err != nil {
			return nil, err
		}
		created = append(created, row.event())
	}
	return created, nil
}

// ListSessionEventQueueItems returns and locks the public events currently
// referenced by the startup queue for the Session already locked by the
// activation transaction. Callers use this only to validate ownership and
// event type before clearing the queue; inbound content and order come from
// ListSessionEventsForActivation.
func (tx ManagedAgentActivationTx) ListSessionEventQueueItems(
	ctx context.Context,
	session Session,
) ([]SessionEvent, error) {
	return listSessionEventQueueItems(
		ctx,
		tx.sessionEventQueueMapper,
		tx.sessionEventMapper,
		session,
	)
}

func listSessionEventQueueItems(
	ctx context.Context,
	sessionEventQueueMapper SessionEventQueueMapper,
	sessionEventMapper SessionEventMapper,
	session Session,
) ([]SessionEvent, error) {
	sessionUUID, err := parseDBUUID("session_uuid", session.UUID)
	if err != nil {
		return nil, err
	}
	identityRows, err := sessionEventQueueMapper.ListSessionEventQueueIdentities(ctx, sessionUUID)
	if err != nil {
		return nil, err
	}
	if len(identityRows) == 0 {
		return nil, nil
	}

	eventsByUUID, err := sessionEventsByUUIDs(ctx, sessionEventMapper, session, identityRows)
	if err != nil {
		return nil, err
	}

	events := make([]SessionEvent, 0, len(identityRows))
	for _, row := range identityRows {
		event, ok := eventsByUUID[row.SessionEventUUID.String()]
		if !ok {
			return nil, fmt.Errorf(
				"%w: queued event %s does not belong to Session %s",
				ErrInvalidState,
				row.SessionEventUUID.String(),
				session.ExternalID,
			)
		}
		events = append(events, event)
	}
	return events, nil
}

// ListSessionEventsForActivation returns complete public history in stable
// creation order. The database identity is used only to preserve insertion
// order when a batch shares one created_at timestamp.
func (tx ManagedAgentActivationTx) ListSessionEventsForActivation(
	ctx context.Context,
	session Session,
) ([]SessionEvent, error) {
	parsedUUIDs, err := parseSessionUUIDs(session)
	if err != nil {
		return nil, err
	}
	rows, err := tx.sessionEventMapper.ListSessionEventsForActivation(
		ctx,
		parsedUUIDs.OrganizationUUID,
		parsedUUIDs.WorkspaceUUID,
		parsedUUIDs.SessionUUID,
	)
	if err != nil {
		return nil, err
	}
	return lo.Map(rows, func(row sessionEventRow, _ int) SessionEvent {
		return row.event()
	}), nil
}

func sessionEventsByUUIDs(
	ctx context.Context,
	sessionEventMapper SessionEventMapper,
	session Session,
	identityRows []sessionEventQueueIdentityRow,
) (map[string]SessionEvent, error) {
	eventUUIDs := lo.Map(identityRows, func(row sessionEventQueueIdentityRow, _ int) uuid.UUID {
		return row.SessionEventUUID
	})
	sessionUUID, err := parseDBUUID("session_uuid", session.UUID)
	if err != nil {
		return nil, err
	}
	rows, err := sessionEventMapper.ListSessionEventsByUUIDs(
		ctx,
		sessionUUID,
		eventUUIDs,
	)
	if err != nil {
		return nil, err
	}
	events := lo.Map(rows, func(row sessionEventRow, _ int) SessionEvent {
		return row.event()
	})
	byUUID := make(map[string]SessionEvent, len(events))
	for _, event := range events {
		byUUID[event.UUID] = event
	}
	return byUUID, nil
}

func (tx ManagedAgentActivationTx) DeleteSessionEventQueue(
	ctx context.Context,
	sessionUUID string,
) error {
	parsedUUID, err := parseDBUUID("session_uuid", sessionUUID)
	if err != nil {
		return err
	}
	_, err = tx.sessionEventQueueMapper.DeleteSessionEventQueue(ctx, parsedUUID)
	return err
}

// shouldQueueForStartup reports whether a user.message should enter the startup
// queue. Environment type is intentionally not part of this decision: queueing
// depends only on whether the Session still has no active Code Session and still
// has session-scoped environment work in flight.
func shouldQueueForStartup(
	ctx context.Context,
	codeSessionMapper CodeSessionMapper,
	environmentWorkMapper EnvironmentWorkMapper,
	session Session,
) (bool, error) {
	parsedUUIDs, err := parseSessionUUIDs(session)
	if err != nil {
		return false, err
	}
	status, found, err := codeSessionMapper.GetLatestCodeSessionStatus(ctx, parsedUUIDs.SessionUUID)
	if err != nil {
		return false, err
	}
	if found && status != "initializing" {
		return false, nil
	}
	return environmentWorkMapper.StartupEnvironmentWorkExists(
		ctx,
		parsedUUIDs.WorkspaceUUID,
		parsedUUIDs.EnvironmentUUID,
		session.ExternalID,
	)
}

func enqueueSessionEventsTx(
	ctx context.Context,
	sessionEventQueueMapper SessionEventQueueMapper,
	session Session,
	events []SessionEvent,
) error {
	parsedUUIDs, err := parseSessionUUIDs(session)
	if err != nil {
		return err
	}

	rows := make([]sessionEventQueueInsertRow, 0, len(events))
	for _, event := range events {
		if event.EventType != "user.message" {
			continue
		}
		eventUUID, err := parseDBUUID("session_event_uuid", event.UUID)
		if err != nil {
			return err
		}
		rows = append(rows, sessionEventQueueInsertRow{
			OrganizationUUID: parsedUUIDs.OrganizationUUID,
			WorkspaceUUID:    parsedUUIDs.WorkspaceUUID,
			SessionUUID:      parsedUUIDs.SessionUUID,
			SessionEventUUID: eventUUID,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	_, err = sessionEventQueueMapper.EnqueueSessionEvents(ctx, rows)
	return err
}

func parseSessionUUIDs(session Session) (sessionUUIDs, error) {
	organizationUUID, err := parseDBUUID("organization_uuid", session.OrganizationUUID)
	if err != nil {
		return sessionUUIDs{}, err
	}
	workspaceUUID, err := parseDBUUID("workspace_uuid", session.WorkspaceUUID)
	if err != nil {
		return sessionUUIDs{}, err
	}
	sessionUUID, err := parseDBUUID("session_uuid", session.UUID)
	if err != nil {
		return sessionUUIDs{}, err
	}
	environmentUUID, err := parseDBUUID("environment_uuid", session.EnvironmentUUID)
	if err != nil {
		return sessionUUIDs{}, err
	}
	return sessionUUIDs{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		SessionUUID:      sessionUUID,
		EnvironmentUUID:  environmentUUID,
	}, nil
}
