package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"charm.land/fantasy"
)

type runState struct {
	engine            *Engine
	call              Call
	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.Mutex
	step              int
	attempt           int
	blockIDs          []string
	texts             map[string]string
	failure           error
	lastPart          fantasy.StreamPart
	responseParts     []fantasy.MessagePart
	responseContent   fantasy.ResponseContent
	finish            fantasy.StreamPart
	pendingCalls      int
	responsePersisted bool
}

func (s *runState) publish(event Event) error {
	event.SessionID = s.call.SessionID
	event.RunID = s.call.RunID
	event.Step = s.step
	event.Attempt = s.attempt
	return s.fail(s.engine.opts.Events.Publish(s.ctx, event))
}

func (s *runState) streamCallbacks() fantasy.AgentStreamCall {
	return fantasy.AgentStreamCall{
		MaxRetries: &s.engine.opts.MaxRetries,
		StopWhen: []fantasy.StopCondition{
			fantasy.StepCountIs(s.engine.opts.MaxSteps),
		},
		OnStepStart: func(step int) error {
			s.step = step
			s.attempt = 1
			s.resetBlocks()
			return s.publish(Event{Type: EventStepStart})
		},
		OnTextStart: func(id string) error {
			s.texts[id] = ""
			return s.publish(Event{Type: EventTextStart, ID: id})
		},
		OnTextDelta: func(id, text string) error {
			s.texts[id] += text
			return s.publish(Event{Type: EventTextDelta, ID: id, Text: text})
		},
		OnTextEnd: func(id string) error {
			s.blockIDs = append(s.blockIDs, id)
			s.responseParts = append(s.responseParts, fantasy.TextPart{Text: s.texts[id], ProviderOptions: fantasy.ProviderOptions(s.lastPart.ProviderMetadata)})
			s.responseContent = append(s.responseContent, fantasy.TextContent{Text: s.texts[id], ProviderMetadata: s.lastPart.ProviderMetadata})
			return s.publish(Event{Type: EventTextEnd, ID: id, Text: s.texts[id]})
		},
		OnReasoningStart: func(id string, reasoning fantasy.ReasoningContent) error {
			s.texts[id] = reasoning.Text
			return s.publish(Event{Type: EventReasoningStart, ID: id, Text: reasoning.Text, Reasoning: &reasoning})
		},
		OnReasoningDelta: func(id, text string) error {
			s.texts[id] += text
			return s.publish(Event{Type: EventReasoningDelta, ID: id, Text: text})
		},
		OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
			s.blockIDs = append(s.blockIDs, id)
			s.responseParts = append(s.responseParts, fantasy.ReasoningPart{Text: reasoning.Text, ProviderOptions: fantasy.ProviderOptions(reasoning.ProviderMetadata)})
			s.responseContent = append(s.responseContent, reasoning)
			return s.publish(Event{Type: EventReasoningEnd, ID: id, Text: reasoning.Text, Reasoning: &reasoning})
		},
		OnToolCall: s.toolCall,
		OnChunk:    s.chunk,
		OnToolResult: func(result fantasy.ToolResultContent) error {
			if err := s.err(); err != nil {
				return err
			}
			if !result.ProviderExecuted {
				message := fantasy.Message{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
					fantasy.ToolResultPart{
						ToolCallID: result.ToolCallID, Output: result.Result,
						ProviderExecuted: result.ProviderExecuted, ProviderOptions: fantasy.ProviderOptions(result.ProviderMetadata), ClientMetadata: result.ClientMetadata,
					},
				}}
				if err := s.engine.opts.History.Append(s.ctx, s.call.SessionID, []fantasy.Message{message}); err != nil {
					return fmt.Errorf("persist tool result: %w", err)
				}
			}
			return s.publish(Event{Type: EventToolResult, ID: result.ToolCallID, ToolResult: &result})
		},
		OnStepFinish: func(step fantasy.StepResult) error {
			if err := s.persistResponse(step); err != nil {
				return err
			}
			return s.publish(Event{Type: EventStepFinish, Result: &step})
		},
		OnRetry: func(_ *fantasy.ProviderError, _ time.Duration) {
			if err := s.publish(Event{Type: EventRetry}); err != nil {
				s.fail(err)
			}
			s.attempt++
			s.resetBlocks()
		},
		OnAgentFinish: func(result *fantasy.AgentResult) error {
			return s.publish(Event{Type: EventRunFinish, AgentResult: result})
		},
	}
}

func (s *runState) resetBlocks() {
	s.blockIDs = nil
	s.texts = make(map[string]string)
	s.responseParts = nil
	s.responseContent = nil
	s.finish = fantasy.StreamPart{}
	s.pendingCalls = 0
	s.responsePersisted = false
}
