package sessions

import (
	"encoding/json"
	"time"
)

func nativeStreamPreview(batch codeSessionStreamFanout) (sessionStreamEvent, bool) {
	var payload struct {
		Type     string `json:"type"`
		EventID  string `json:"event_id"`
		ThreadID string `json:"session_thread_id"`
		Event    struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"event"`
		Delta struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Content struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"delta"`
	}
	if json.Unmarshal(batch.Payload, &payload) != nil {
		return sessionStreamEvent{}, false
	}
	id := payload.EventID
	switch payload.Type {
	case previewEventStart:
		if payload.Event.Type != "agent.message" && payload.Event.Type != "agent.thinking" {
			return sessionStreamEvent{}, false
		}
		id = payload.Event.ID
	case previewEventDelta:
		if payload.Delta.Type != "content_delta" || payload.Delta.Index != 0 || payload.Delta.Content.Type != "text" && payload.Delta.Content.Type != "thinking" {
			return sessionStreamEvent{}, false
		}
	default:
		return sessionStreamEvent{}, false
	}
	if id == "" || batch.WorkspaceUUID == "" || batch.SessionExternalID == "" || batch.WorkerEpoch <= 0 {
		return sessionStreamEvent{}, false
	}
	event := sessionStreamEvent{ExternalID: id, WorkspaceUUID: batch.WorkspaceUUID, SessionExternalID: batch.SessionExternalID, PrimaryThread: payload.ThreadID == "", EventType: payload.Type, Payload: batch.Payload, ProcessedAt: time.Now().UTC()}
	if payload.ThreadID != "" {
		event.ThreadExternalID = &payload.ThreadID
	}
	return event, true
}
