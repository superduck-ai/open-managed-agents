package db

import (
	"context"
	"database/sql"
	"encoding/json/v2"
)

func (d *DB) GetSessionEventByUUID(ctx context.Context, workspaceUUID, sessionUUID, eventUUID string) (SessionEvent, error) {
	row, err := NewSessionEventMapper(d.mapperDB).FindByUUID(ctx, workspaceUUID, sessionUUID, eventUUID)
	return row.event(), mapNoRows(err)
}

type OAuthContinuation struct {
	InputUUID   string `json:"input_uuid,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
	SessionUUID string `json:"session_uuid"`
	EventUUID   string `json:"event_uuid"`
	ChannelUUID string `json:"channel_uuid"`
	BrowserHash string `json:"browser_hash"`
	Identity    bool   `json:"identity,omitzero"`
}

func oauthContinuationJSON(value *OAuthContinuation) (*string, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return new(string(data)), nil
}

func decodeOAuthContinuation(value sql.NullString) (*OAuthContinuation, error) {
	if !value.Valid {
		return nil, nil
	}
	var result OAuthContinuation
	if err := json.Unmarshal([]byte(value.String), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *DB) HasAcceptedSessionToolControl(ctx context.Context, workspaceUUID, sessionID, eventID string) (bool, error) {
	return NewSessionEventMapper(d.mapperDB).HasAcceptedToolControl(ctx, workspaceUUID, sessionID, eventID, eventID)
}
