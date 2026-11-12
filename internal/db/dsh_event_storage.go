package db

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

type DSHStoredEvent struct {
	Event         SessionEvent
	PayloadDigest string
	SHA256        string
	Size          int64
}

func (d *DB) ProjectDSHStorageEvents(ctx context.Context, scope DSHScope, item DSHPublicEvent) ([]SessionEvent, error) {
	var events []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, primary, err := lockDSHSession(ctx, executor, scope)
		if err != nil {
			return err
		}
		if err = prepareDSHCoordinationEvents(ctx, executor, scope, session, primary, &item); err != nil {
			return err
		}
		events = item.Events
		return nil
	})
	return events, err
}

func dshStoredEventFor(events []DSHStoredEvent, id string) (DSHStoredEvent, bool, error) {
	var result DSHStoredEvent
	found := false
	for _, event := range events {
		if event.Event.ExternalID == id {
			if found {
				return result, false, ErrDSHPublicConflict
			}
			result, found = event, true
		}
	}
	return result, found, nil
}

func validateDSHStoredEvent(ctx context.Context, executor yourbatis.Executor, scope DSHScope, original SessionEvent, stored DSHStoredEvent) error {
	digest, err := jsoncanonical.Digest(original.Payload)
	if err != nil {
		return err
	}
	if digest != stored.PayloadDigest || stored.Event.PayloadBlobUUID == nil || original.EventType != stored.Event.EventType || !sameDSHParent(original.ThreadExternalID, stored.Event.ThreadExternalID) {
		return ErrDSHPublicConflict
	}
	blob, err := NewEventPayloadBlobMapper(executor).FindForPublication(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, *stored.Event.PayloadBlobUUID)
	if err != nil {
		return mapNoRows(err)
	}
	if blob.SHA256 != stored.SHA256 || blob.Size != stored.Size {
		return ErrDSHPublicConflict
	}
	var summary struct {
		Type    string `json:"type"`
		Size    int64  `json:"size"`
		BlobRef struct {
			Version int    `json:"version"`
			ID      string `json:"id"`
		} `json:"blob_ref"`
	}
	if err = json.Unmarshal(stored.Event.Payload, &summary); err != nil {
		return err
	}
	if summary.Type != original.EventType || summary.Size != stored.Size || summary.BlobRef.Version != 1 || summary.BlobRef.ID != blob.ExternalID {
		return ErrDSHPublicConflict
	}
	return nil
}

func dshEventsForStorage(ctx context.Context, executor yourbatis.Executor, scope DSHScope, originals []SessionEvent, representations []DSHStoredEvent) ([]SessionEvent, error) {
	events := append([]SessionEvent(nil), originals...)
	for i, original := range events {
		stored, found, err := dshStoredEventFor(representations, original.ExternalID)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if err = validateDSHStoredEvent(ctx, executor, scope, original, stored); err != nil {
			return nil, err
		}
		events[i].Payload, events[i].PayloadBlobUUID, events[i].ToolUseID = stored.Event.Payload, stored.Event.PayloadBlobUUID, stored.Event.ToolUseID
	}
	return events, nil
}

func equalDSHStoredPayload(ctx context.Context, executor yourbatis.Executor, session Session, original SessionEvent, actual sessionEventRow, representations []DSHStoredEvent) (bool, error) {
	if actual.PayloadBlobUUID == nil {
		return maevents.EqualPublicationPayload(original.EventType, actual.Payload, original.Payload)
	}
	stored, found, err := dshStoredEventFor(representations, original.ExternalID)
	if err != nil || !found {
		return false, err
	}
	scope := DSHScope{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID}
	if err = validateDSHStoredEvent(ctx, executor, scope, original, stored); err != nil {
		return false, err
	}
	blob, err := NewEventPayloadBlobMapper(executor).Find(ctx, session.WorkspaceUUID, *actual.PayloadBlobUUID)
	if err != nil {
		return false, mapNoRows(err)
	}
	if blob.OrganizationUUID != session.OrganizationUUID || blob.SHA256 != stored.SHA256 || blob.Size != stored.Size || !sameDSHParent(actual.ToolUseID, stored.Event.ToolUseID) {
		return false, nil
	}
	var expected, existing map[string]json.RawMessage
	if err = json.Unmarshal(stored.Event.Payload, &expected); err != nil {
		return false, err
	}
	if err = json.Unmarshal(actual.Payload, &existing); err != nil {
		return false, err
	}
	var ref struct {
		Version int    `json:"version"`
		ID      string `json:"id"`
	}
	if err = json.Unmarshal(existing["blob_ref"], &ref); err != nil {
		return false, err
	}
	if ref.Version != 1 || ref.ID != blob.ExternalID {
		return false, nil
	}
	expected["blob_ref"] = existing["blob_ref"]
	encoded, err := json.Marshal(expected)
	if err != nil {
		return false, err
	}
	return jsoncanonical.Equal(encoded, actual.Payload)
}
