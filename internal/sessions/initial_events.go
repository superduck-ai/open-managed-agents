package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type sessionInitialEvent struct {
	Type            string                       `json:"type"`
	SessionThreadID *string                      `json:"session_thread_id"`
	Content         []sessionInitialContentBlock `json:"content"`
}

type sessionInitialContentBlock struct {
	Type   string `json:"type"`
	Source struct {
		Type string `json:"type"`
	} `json:"source"`
}

func normalizeSessionInitialEvents(session *db.Session, raw json.RawMessage, now time.Time) ([]db.SessionEvent, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var inputs []json.RawMessage
	if httpapi.IsJSONNull(raw) || json.Unmarshal(raw, &inputs) != nil {
		return nil, errors.New("initial_events must be an array")
	}
	if len(inputs) > 50 {
		return nil, errors.New("initial_events may contain at most 50 events")
	}
	if len(inputs) == 0 {
		return nil, nil
	}
	events := make([]db.SessionEvent, 0, len(inputs)+1)
	outcomes, documents := 0, 0
	for i, input := range inputs {
		var boundary sessionInitialEvent
		if err := json.Unmarshal(input, &boundary); err != nil {
			return nil, fmt.Errorf("initial_events[%d] must be an object", i)
		}
		if boundary.Type != "user.message" && boundary.Type != "user.define_outcome" {
			return nil, errors.New("initial_events only accepts user.message and user.define_outcome")
		}
		if boundary.SessionThreadID != nil {
			return nil, errors.New("initial_events cannot target an existing session thread")
		}
		if boundary.Type == "user.define_outcome" {
			outcomes++
			if outcomes > 1 {
				return nil, errors.New("initial_events may contain at most one user.define_outcome")
			}
		}
		for _, block := range boundary.Content {
			if block.Type == "document" && block.Source.Type == "file" {
				documents++
			}
		}
		if documents > 100 {
			return nil, errors.New("initial_events may contain at most 100 file-sourced document blocks")
		}
		event, evaluations, changed, err := normalizeInputEvent(*session, input, now)
		if err != nil {
			return nil, fmt.Errorf("initial_events[%d]: %w", i, err)
		}
		if changed {
			session.OutcomeEvaluations = evaluations
		}
		events = append(events, event)
	}
	status, err := initialSessionRunningEvent(events[0])
	if err != nil {
		return nil, err
	}
	return append([]db.SessionEvent{status}, events...), nil
}

func initialSessionRunningEvent(first db.SessionEvent) (db.SessionEvent, error) {
	status := first
	id, err := ids.New("sevt_")
	if err != nil {
		return db.SessionEvent{}, err
	}
	status.ExternalID = id
	status.EventType = "session.status_running"
	status.ProcessedAt = eventTime(first.CreatedAt)
	status.Payload = nil
	status.UUID = uuid.NewV4().String()
	return status, nil
}
