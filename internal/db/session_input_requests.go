package db

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/superduck-ai/yourbatis"
)

var ErrSessionInputRequestConflict = errors.New("session input request key already used with different events")

type SessionInputRequest struct {
	Key         string
	Fingerprint string
}

func (d *DB) FindSessionInputRequest(ctx context.Context, session Session, request SessionInputRequest) (SessionEventChanges, bool, error) {
	return findSessionInputRequest(ctx, d.mapperDB, session, request)
}

func (d *DB) CompleteSessionInputRequestHandoff(ctx context.Context, session Session, request SessionInputRequest) error {
	rows, err := NewSessionInputRequestMapper(d.mapperDB).CompleteHandoff(ctx, session, request)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func findSessionInputRequest(ctx context.Context, executor yourbatis.Executor, session Session, request SessionInputRequest) (SessionEventChanges, bool, error) {
	row, found, err := NewSessionInputRequestMapper(executor).Find(ctx, session, request)
	if err != nil || !found {
		return SessionEventChanges{}, false, err
	}
	if row.Fingerprint != request.Fingerprint {
		return SessionEventChanges{}, false, ErrSessionInputRequestConflict
	}
	var eventIDs []string
	var changes SessionEventChanges
	if err := json.Unmarshal([]byte(row.EventIDs), &eventIDs); err != nil {
		return SessionEventChanges{}, false, err
	}
	if err := json.Unmarshal([]byte(row.SettledInputEventIDs), &changes.SettledInputEventIDs); err != nil {
		return SessionEventChanges{}, false, err
	}
	mapper := NewSessionEventMapper(executor)
	for _, id := range eventIDs {
		event, err := mapper.FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, id)
		if err != nil {
			return SessionEventChanges{}, false, err
		}
		changes.Events = append(changes.Events, event.event())
	}
	changes.Replayed = true
	changes.InputHandoffCompleted = row.HandoffCompleted
	return changes, true, nil
}

func saveSessionInputRequest(ctx context.Context, executor yourbatis.Executor, session Session, request SessionInputRequest, changes SessionEventChanges) error {
	ids := make([]string, 0, len(changes.Events))
	for _, event := range changes.Events {
		ids = append(ids, event.ExternalID)
	}
	events, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	settled, err := json.Marshal(changes.SettledInputEventIDs)
	if err != nil {
		return err
	}
	return NewSessionInputRequestMapper(executor).Insert(ctx, session, request, sessionInputRequestRow{EventIDs: string(events), SettledInputEventIDs: string(settled)})
}
