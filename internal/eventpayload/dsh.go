package eventpayload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
)

func (s *Store) ValidateDSHPreviewWorker(ctx context.Context, scope db.DSHScope) error {
	return s.database.ValidateDSHPreviewWorker(ctx, scope)
}

func (s *Store) PublishDSHEvents(ctx context.Context, scope db.DSHScope, batch []db.DSHPublicEvent) (db.DSHPublishResult, error) {
	if err := s.ValidateDSHPreviewWorker(ctx, scope); err != nil {
		return db.DSHPublishResult{}, err
	}
	prepared := append([]db.DSHPublicEvent(nil), batch...)
	for i, item := range prepared {
		needs := false
		for _, event := range item.Events {
			needs = needs || needsBlobStorage(event.Payload)
		}
		if !needs {
			continue
		}
		events, err := s.database.ProjectDSHStorageEvents(ctx, scope, item)
		if err != nil {
			return db.DSHPublishResult{}, err
		}
		for _, event := range events {
			if !needsBlobStorage(event.Payload) {
				continue
			}
			stored, err := s.prepareDSHStoredEvent(ctx, scope, event)
			if err != nil {
				return db.DSHPublishResult{}, err
			}
			prepared[i].StoredEvents = append(prepared[i].StoredEvents, stored)
		}
	}
	return s.database.PublishDSHEvents(ctx, scope, prepared)
}

func (s *Store) prepareDSHStoredEvent(ctx context.Context, scope db.DSHScope, event db.SessionEvent) (db.DSHStoredEvent, error) {
	original := event.Payload
	existing, err := s.database.GetSessionEvent(ctx, scope.WorkspaceUUID, scope.SessionID, event.ExternalID)
	if err == nil && existing.PayloadBlobUUID != nil {
		full, err := s.restore(ctx, scope.WorkspaceUUID, existing.Payload, existing.PayloadBlobUUID)
		if err != nil {
			return db.DSHStoredEvent{}, err
		}
		equal, err := jsoncanonical.Equal(full, original)
		if err != nil {
			return db.DSHStoredEvent{}, err
		}
		matches, err := s.dshStorageThreadMatches(ctx, scope, existing.ThreadExternalID, event.ThreadExternalID)
		if err != nil {
			return db.DSHStoredEvent{}, err
		}
		if !equal || existing.EventType != event.EventType || !matches {
			return db.DSHStoredEvent{}, db.ErrDSHPublicConflict
		}
		thread := event.ThreadExternalID
		event = existing
		event.ThreadExternalID = thread
		summary, toolID, err := Summarize(full, event.EventType)
		if err != nil {
			return db.DSHStoredEvent{}, err
		}
		blob, err := s.database.GetEventPayloadBlob(ctx, scope.WorkspaceUUID, *event.PayloadBlobUUID)
		if err != nil {
			return db.DSHStoredEvent{}, err
		}
		summary.BlobRef = &Reference{Version: 1, ID: blob.ExternalID}
		event.Payload, err = json.Marshal(summary)
		if err != nil {
			return db.DSHStoredEvent{}, err
		}
		event.ToolUseID = toolID
		if err = preparePublicEventMetadata(full, &event); err != nil {
			return db.DSHStoredEvent{}, err
		}
		original = full
	} else {
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return db.DSHStoredEvent{}, err
		}
		prepared, err := s.PreparePublic(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, []db.SessionEvent{event})
		if err != nil {
			return db.DSHStoredEvent{}, err
		}
		event = prepared[0]
	}
	digest, err := jsoncanonical.Digest(original)
	if err != nil {
		return db.DSHStoredEvent{}, err
	}
	sum := sha256.Sum256(original)
	return db.DSHStoredEvent{Event: event, PayloadDigest: digest, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(original))}, nil
}

func sameThread(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func (s *Store) dshStorageThreadMatches(ctx context.Context, scope db.DSHScope, actual, expected *string) (bool, error) {
	if expected != nil {
		return sameThread(actual, expected), nil
	}
	primary, found, err := s.database.GetPrimarySessionThread(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil || !found {
		return false, err
	}
	return actual != nil && *actual == primary.ExternalID, nil
}
