package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sessionSystemContext struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type systemContextPayload struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type systemContextPageReader func(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error)

func loadSystemContext(ctx context.Context, params db.ListSessionEventsPageParams, read systemContextPageReader) ([]sessionSystemContext, error) {
	contexts := make([]sessionSystemContext, 0)
	seen := make(map[string]bool)
	for {
		events, more, err := read(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if event.EventType != "system.message" || event.ExternalID == "" || seen[event.ExternalID] {
				return nil, errors.New("invalid system context event identity")
			}
			seen[event.ExternalID] = true
			var payload systemContextPayload
			if err = json.Unmarshal(event.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode system context: %w", err)
			}
			if len(payload.Content) == 0 {
				return nil, errors.New("system context has no text blocks")
			}
			texts := make([]string, 0, len(payload.Content))
			for _, block := range payload.Content {
				if block.Type != "text" {
					return nil, errors.New("system context contains a non-text block")
				}
				texts = append(texts, block.Text)
			}
			contexts = append(contexts, sessionSystemContext{ID: event.ExternalID, Text: strings.Join(texts, "\n")})
		}
		if !more {
			return contexts, nil
		}
		if len(events) == 0 {
			return nil, errors.New("empty system context page")
		}
		params.Cursor = &db.SessionEventPageCursor{ExternalID: events[len(events)-1].ExternalID}
	}
}

func workerPayloadWithSystemContext(payload json.RawMessage, contexts []sessionSystemContext) (json.RawMessage, error) {
	fields, err := decodeRawJSONObject(payload)
	if err != nil {
		return nil, err
	}
	raw, err := marshalRaw(contexts)
	if err != nil {
		return nil, err
	}
	fields["oma_system_context"] = raw
	fields["oma_refresh_tools"] = json.RawMessage(`true`)
	return marshalRaw(fields)
}

func (s *Service) publicWorkerPayload(ctx context.Context, codeSession db.CodeSession, event db.SessionEvent) (json.RawMessage, error) {
	event, err := s.materializeMessageAttachments(ctx, event)
	if err != nil {
		return nil, err
	}
	payload, err := workerPayloadForPublicEvent(codeSession.ExternalID, event)
	if err == nil && event.EventType == "user.interrupt" {
		return s.publicWorkerInterruptPayload(ctx, codeSession, payload)
	}
	if err != nil || event.EventType != "user.message" || codeSession.SessionExternalID == "" {
		return payload, err
	}
	if event.ThreadExternalID != nil {
		primary, found, lookupErr := s.db.GetPrimarySessionThreadExternalID(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !found {
			return nil, db.ErrNotFound
		}
		if *event.ThreadExternalID != primary {
			return payload, nil
		}
	}
	contexts, err := loadSystemContext(ctx, db.ListSessionEventsPageParams{
		WorkspaceUUID: codeSession.WorkspaceUUID, SessionExternalID: codeSession.SessionExternalID,
		PrimaryOnly: true, Limit: 500, Order: "asc", Types: []string{"system.message"}, CreatedAtLTE: &event.CreatedAt,
	}, s.eventPayloads.ListSessionEventsPage)
	if err != nil {
		return nil, err
	}
	return workerPayloadWithSystemContext(payload, contexts)
}

func (s *Service) publicWorkerInterruptPayload(ctx context.Context, codeSession db.CodeSession, payload json.RawMessage) (json.RawMessage, error) {
	var input workerOutputCommonPayload
	if err := json.Unmarshal(payload, &input); err != nil {
		return nil, err
	}
	if input.SessionThreadID == "" || codeSession.SessionExternalID == "" {
		return payload, nil
	}
	primary, found, err := s.db.GetPrimarySessionThreadExternalID(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	return workerInterruptTargetPayload(payload, primary, codeSession.ExternalID)
}

func workerInterruptTargetPayload(payload json.RawMessage, primaryThreadID, codeSessionID string) (json.RawMessage, error) {
	fields, err := decodeRawJSONObject(payload)
	if err != nil {
		return nil, err
	}
	var target string
	if value, ok := fields["session_thread_id"]; ok {
		if err := json.Unmarshal(value, &target); err != nil {
			return nil, err
		}
	}
	if target != "" && target == primaryThreadID {
		setRawJSONField(fields, "session_thread_id", codeSessionID)
	}
	return marshalRaw(fields)
}
