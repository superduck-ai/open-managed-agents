package codesessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

// ModelRequest owns one proxy attempt. Each worker retry gets a new span.
type ModelRequest struct {
	StartID       string
	CodeSessionID string
	ThreadID      string
	Model         string
	codeSession   db.CodeSession
}

// ModelRequestUsage is cumulative within one provider response, never a turn total.
type ModelRequestUsage struct {
	CacheCreation            *ModelRequestCacheUsage `json:"cache_creation,omitzero"`
	InputTokens              *int64                  `json:"input_tokens,omitzero"`
	OutputTokens             *int64                  `json:"output_tokens,omitzero"`
	CacheCreationInputTokens *int64                  `json:"cache_creation_input_tokens,omitzero"`
	CacheReadInputTokens     *int64                  `json:"cache_read_input_tokens,omitzero"`
}

type ModelRequestCacheUsage struct {
	Ephemeral5mInputTokens *int64 `json:"ephemeral_5m_input_tokens,omitzero"`
	Ephemeral1hInputTokens *int64 `json:"ephemeral_1h_input_tokens,omitzero"`
}

type ModelRequestResult struct {
	Messages          []ModelRequestMessage
	ToolUseIDs        []string
	EndedAt           time.Time
	Usage             ModelRequestUsage
	EventIDs          []string
	UpstreamRequestID string
	ErrorType         string
}

// ModelRequestMessage is a completed public message observed at the proxy.
// Publishing it with the end avoids depending on the worker's later echo.
type ModelRequestMessage struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Content []json.RawMessage `json:"content,omitempty"`
}

type modelRequestEvent struct {
	IsError           *bool              `json:"is_error,omitzero"`
	ToolUseIDs        []string           `json:"tool_use_ids,omitempty"`
	ID                string             `json:"id"`
	Type              string             `json:"type"`
	OwnerThreadID     string             `json:"_owner_session_thread_id,omitempty"`
	Model             string             `json:"model"`
	RequestID         string             `json:"request_id"`
	CreatedAt         time.Time          `json:"created_at"`
	ProcessedAt       time.Time          `json:"processed_at"`
	StartID           string             `json:"model_request_start_id,omitempty"`
	Usage             *ModelRequestUsage `json:"model_usage,omitzero"`
	EventIDs          []string           `json:"event_ids,omitempty"`
	UpstreamRequestID string             `json:"upstream_request_id,omitempty"`
	Error             *modelRequestError `json:"error,omitzero"`
}

type modelRequestError struct {
	Type string `json:"type"`
}

func (s *Service) BeginModelRequest(ctx context.Context, workspaceID, sessionID, codeSessionID, agentID, model string) (*ModelRequest, error) {
	codeSession, err := s.db.GetCodeSessionBySessionExternalID(ctx, workspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	if codeSession.ExternalID != codeSessionID || codeSession.Status != "active" || s.sink == nil {
		return nil, db.ErrInvalidState
	}
	threadID, err := s.modelRequestThread(ctx, codeSession, agentID)
	if err != nil {
		return nil, err
	}
	startID, err := ids.New("sevt_")
	if err != nil {
		return nil, err
	}
	request := &ModelRequest{StartID: startID, CodeSessionID: codeSessionID, ThreadID: threadID, Model: model, codeSession: codeSession}
	now := time.Now().UTC().Truncate(time.Microsecond)
	err = s.publishModelRequestEvent(ctx, request, modelRequestEvent{ID: startID, Type: "span.model_request_start", CreatedAt: now, ProcessedAt: now}, nil)
	if err != nil {
		return nil, err
	}
	return request, nil
}

func (s *Service) EndModelRequest(ctx context.Context, request *ModelRequest, result ModelRequestResult) error {
	event := modelRequestEvent{ID: request.StartID + "_end", Type: "span.model_request_end", StartID: request.StartID,
		CreatedAt: result.EndedAt, ProcessedAt: result.EndedAt, IsError: new(result.ErrorType != ""), Usage: &result.Usage, EventIDs: result.EventIDs, ToolUseIDs: result.ToolUseIDs, UpstreamRequestID: result.UpstreamRequestID}
	if result.ErrorType != "" {
		event.Error = &modelRequestError{Type: result.ErrorType}
	}
	payloads := make([]json.RawMessage, 0, len(result.Messages)+1)
	if result.ErrorType == "" {
		for _, message := range result.Messages {
			payload, err := jsonv2.Marshal(struct {
				ModelRequestMessage
				OwnerThreadID string    `json:"_owner_session_thread_id"`
				StartID       string    `json:"model_request_start_id"`
				ProcessedAt   time.Time `json:"processed_at"`
			}{ModelRequestMessage: message, OwnerThreadID: request.ThreadID, StartID: request.StartID, ProcessedAt: result.EndedAt})
			if err != nil {
				return err
			}
			payloads = append(payloads, payload)
		}
	}
	return s.publishModelRequestEvent(ctx, request, event, payloads)
}

func (s *Service) publishModelRequestEvent(ctx context.Context, request *ModelRequest, event modelRequestEvent, preceding []json.RawMessage) error {
	event.OwnerThreadID, event.Model = request.ThreadID, request.Model
	event.RequestID = request.StartID
	payload, err := jsonv2.Marshal(event)
	if err != nil {
		return err
	}
	return s.sink.PublishCodeSessionEvents(ctx, request.codeSession, append(preceding, payload))
}

func (s *Service) modelRequestThread(ctx context.Context, codeSession db.CodeSession, agentID string) (string, error) {
	if agentID == "" {
		primary, found, err := s.db.GetPrimarySessionThread(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
		if err != nil {
			return "", err
		}
		if !found {
			return "", db.ErrNotFound
		}
		return primary.ExternalID, nil
	}
	// CCR task_started and HTTP requests travel independently. Wait before dispatch,
	// so a delayed task mapping never sends a child request into the primary lane.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		mappings, err := s.subagentThreadMappings(ctx, codeSession)
		if err != nil {
			return "", err
		}
		if threadID := mappings[agentID]; threadID != "" {
			return threadID, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("resolve model request thread: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// The task registry is already persisted as thread_created events. Page it rather
// than maintaining a second registry or assuming at most 500 tasks per session.
func (s *Service) subagentThreadMappings(ctx context.Context, codeSession db.CodeSession) (map[string]string, error) {
	params := db.ListSessionEventsPageParams{WorkspaceUUID: codeSession.WorkspaceUUID, SessionExternalID: codeSession.SessionExternalID,
		PrimaryOnly: true, Limit: 500, Order: "asc", Types: []string{"session.thread_created"}}
	mappings := make(map[string]string)
	for {
		events, more, err := s.eventPayloads.ListSessionEventsPage(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			var task struct {
				ThreadID   string `json:"session_thread_id"`
				TaskID     string `json:"task_id"`
				ToolUseID  string `json:"tool_use_id"`
				AgentID    string `json:"agent_id"`
				AgentIDAlt string `json:"agentId"`
			}
			if err := json.Unmarshal(event.Payload, &task); err != nil {
				return nil, fmt.Errorf("decode thread mapping: %w", err)
			}
			if task.ThreadID == "" || (task.TaskID != "" && task.ThreadID != claudeTaskThreadIDFromFields(codeSession.ExternalID, task.ToolUseID, task.TaskID)) {
				continue
			}
			for _, id := range []string{task.TaskID, task.AgentID, task.AgentIDAlt} {
				if id = strings.TrimSpace(id); id != "" {
					mappings[id] = task.ThreadID
				}
			}
		}
		if !more {
			return mappings, nil
		}
		if len(events) == 0 {
			return nil, errors.New("empty thread mapping page")
		}
		params.Cursor = &db.SessionEventPageCursor{ExternalID: events[len(events)-1].ExternalID}
	}
}
