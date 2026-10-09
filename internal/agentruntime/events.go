package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"
	crushruntime "github.com/charmbracelet/crush/runtime"
)

type publicContent struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

type publicEvent struct {
	Type            string          `json:"type"`
	ID              string          `json:"id,omitempty"`
	SessionThreadID string          `json:"session_thread_id,omitempty"`
	Content         []publicContent `json:"content,omitempty"`
	ToolUseID       string          `json:"tool_use_id,omitempty"`
	IsError         bool            `json:"is_error,omitempty"`
	Event           *publicEvent    `json:"event,omitempty"`
	EventID         string          `json:"event_id,omitempty"`
	EventIDs        []string        `json:"event_ids,omitempty"`
	Delta           *contentDelta   `json:"delta,omitempty"`
}

type contentDelta struct {
	Type    string        `json:"type"`
	Index   int           `json:"index"`
	Content publicContent `json:"content"`
}

type eventSink struct {
	turn *turn
}

func (s *eventSink) Publish(ctx context.Context, event crushruntime.Event) error {
	switch event.Type {
	case crushruntime.EventTextStart, crushruntime.EventReasoningStart:
		s.turn.previewIDs = append(s.turn.previewIDs, s.eventID(event, event.ID))
		kind := "agent.message"
		if event.Type == crushruntime.EventReasoningStart {
			kind = "agent.thinking"
		}
		return s.turn.publish(ctx, publicEvent{Type: "event_start", Event: &publicEvent{Type: kind, ID: s.eventID(event, event.ID)}})
	case crushruntime.EventTextDelta, crushruntime.EventReasoningDelta:
		content := publicContent{Type: "text", Text: event.Text}
		if event.Type == crushruntime.EventReasoningDelta {
			content = publicContent{Type: "thinking", Thinking: event.Text}
		}
		return s.turn.publish(ctx, publicEvent{Type: "event_delta", EventID: s.eventID(event, event.ID), Delta: &contentDelta{Type: "content_delta", Content: content}})
	case crushruntime.EventStepResponse:
		return s.response(ctx, event)
	case crushruntime.EventToolResult:
		return s.toolResult(ctx, event)
	default:
		return nil
	}
}

func (s *eventSink) eventID(event crushruntime.Event, blockID string) string {
	return s.turn.history.id(fmt.Sprintf("block:%d:%d:%s", event.Step, event.Attempt, blockID))
}

func (s *eventSink) response(ctx context.Context, event crushruntime.Event) error {
	if event.Result == nil {
		return ErrExecutionFailed
	}
	block := 0
	for _, content := range event.Result.Content {
		var output publicContent
		kind := "agent.message"
		if text, ok := fantasy.AsContentType[fantasy.TextContent](content); ok {
			output = publicContent{Type: "text", Text: text.Text}
		} else if reasoning, ok := fantasy.AsContentType[fantasy.ReasoningContent](content); ok {
			kind = "agent.thinking"
			output = publicContent{Type: "thinking", Thinking: reasoning.Text}
		} else {
			continue
		}
		id := fmt.Sprintf("fallback:%d", block)
		if block < len(event.BlockIDs) {
			id = event.BlockIDs[block]
		}
		block++
		if err := s.turn.publish(ctx, publicEvent{Type: kind, ID: s.eventID(event, id), Content: []publicContent{output}}); err != nil {
			return err
		}
	}
	return nil
}

func (s *eventSink) toolResult(ctx context.Context, event crushruntime.Event) error {
	association, found := s.turn.tools[event.ID]
	if !found || event.ToolResult == nil {
		return ErrExecutionFailed
	}
	output, isError := toolOutput(event.ToolResult.Result)
	return s.turn.publish(ctx, publicEvent{Type: association.resultType, ID: s.turn.history.id("tool-result:" + event.ID), ToolUseID: association.publicID, Content: []publicContent{output}, IsError: isError})
}

func toolOutput(result fantasy.ToolResultOutputContent) (publicContent, bool) {
	if text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result); ok {
		return publicContent{Type: "text", Text: text.Text}, false
	}
	if failure, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result); ok {
		return publicContent{Type: "text", Text: failure.Error.Error()}, true
	}
	if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result); ok {
		return publicContent{Type: "image", Data: media.Data, MediaType: media.MediaType}, false
	}
	return publicContent{Type: "text", Text: "Tool returned unsupported content."}, true
}

func (t *turn) publish(ctx context.Context, event publicEvent) error {
	event.SessionThreadID = t.threadID
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return t.execution.worker.Publish(ctx, payload)
}
