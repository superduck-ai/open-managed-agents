package codesessions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) workerResultWasInterrupted(ctx context.Context, codeSessionID string, result preparedFailedResultAction) (bool, error) {
	codeSession, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, db.ErrNotFound
	}
	params := db.ListSessionEventsPageParams{
		WorkspaceUUID: codeSession.WorkspaceUUID, SessionExternalID: codeSession.SessionExternalID,
		ThreadExternalID: result.threadID, PrimaryOnly: result.threadID == "", Order: "desc", Limit: 100,
		CreatedAtLTE: &result.at,
		Types:        []string{"session.thread_status_running", "session.thread_status_rescheduled", "user.interrupt", "session.error", "span.model_request_start", "span.model_request_end"},
	}
	var cancellation interruptedModelResult
	for {
		events, more, err := s.eventPayloads.ListSessionEventsPage(ctx, params)
		if err != nil {
			return false, err
		}
		for _, event := range events {
			done, err := cancellation.observe(event)
			if err != nil || done {
				return cancellation.matched, err
			}
		}
		if !more || len(events) == 0 {
			return false, nil
		}
		params.Cursor = &db.SessionEventPageCursor{ExternalID: events[len(events)-1].ExternalID}
	}
}

type interruptedModelResult struct {
	end         *modelRequestEvent
	interrupted bool
	matched     bool
}

func (r *interruptedModelResult) observe(event db.SessionEvent) (bool, error) {
	switch event.EventType {
	case "session.thread_status_running", "session.thread_status_rescheduled":
		return true, nil
	case "session.error":
		r.matched = false
		return true, nil
	case "span.model_request_end":
		if r.end == nil {
			var end modelRequestEvent
			if err := json.Unmarshal(event.Payload, &end); err != nil {
				return false, fmt.Errorf("decode model request end: %w", err)
			}
			if end.Error == nil || end.Error.Type != "cancelled" || end.StartID == "" {
				return true, nil
			}
			r.end = &end
		}
	case "user.interrupt":
		if r.end != nil && !r.matched {
			r.interrupted = true
		}
	case "span.model_request_start":
		if r.end == nil {
			return true, nil
		}
		if event.ExternalID == r.end.StartID {
			r.matched = r.interrupted
		}
	}
	return false, nil
}
