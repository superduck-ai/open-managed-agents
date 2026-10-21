package db

import (
	"encoding/json"
	"uuid"
)

func sessionThreadStatusCopies(event SessionEvent, primary, target SessionThread) ([]SessionEvent, error) {
	if target.ExternalID == primary.ExternalID {
		return []SessionEvent{event}, nil
	}
	destination, label := target, "thread"
	if event.ThreadExternalID != nil && *event.ThreadExternalID == target.ExternalID {
		destination, label = primary, "primary"
	}
	copy, err := sessionThreadStatusCopy(event, destination, label)
	if err != nil {
		return nil, err
	}
	return []SessionEvent{event, copy}, nil
}

func sessionThreadStatusCopy(event SessionEvent, destination SessionThread, label string) (SessionEvent, error) {
	var payload struct {
		sessionStatusPayload
		OriginalEventID string `json:"original_event_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload.sessionStatusPayload); err != nil {
		return SessionEvent{}, err
	}
	copy := event
	copy.threadStatusMirror = true
	copy.UUID = uuid.NewV4().String()
	copy.ExternalID = derivedStatusEventID(event.ExternalID, label)
	copy.ThreadExternalID = &destination.ExternalID
	copy.ThreadUUID = &destination.UUID
	payload.ID = copy.ExternalID
	payload.OriginalEventID = event.ExternalID
	raw, err := json.Marshal(payload)
	if err != nil {
		return SessionEvent{}, err
	}
	copy.Payload = raw
	return copy, nil
}
