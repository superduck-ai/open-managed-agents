package runtime

import (
	"fmt"

	"charm.land/fantasy"
)

func (s *runState) chunk(part fantasy.StreamPart) error {
	s.lastPart = part
	if part.Type == fantasy.StreamPartTypeToolCall {
		s.pendingCalls++
		s.responseParts = append(s.responseParts, fantasy.ToolCallPart{ToolCallID: part.ID, ToolName: part.ToolCallName, Input: part.ToolCallInput, ProviderExecuted: part.ProviderExecuted, ProviderOptions: fantasy.ProviderOptions(part.ProviderMetadata)})
		s.responseContent = append(s.responseContent, fantasy.ToolCallContent{ToolCallID: part.ID, ToolName: part.ToolCallName, Input: part.ToolCallInput, ProviderExecuted: part.ProviderExecuted, ProviderMetadata: part.ProviderMetadata})
	}
	if part.Type == fantasy.StreamPartTypeFinish {
		s.finish = part
	}
	return nil
}

func (s *runState) toolCall(call fantasy.ToolCallContent) error {
	for index, part := range s.responseParts {
		if prior, ok := part.(fantasy.ToolCallPart); ok && prior.ToolCallID == call.ToolCallID {
			s.responseParts[index] = fantasy.ToolCallPart{ToolCallID: call.ToolCallID, ToolName: call.ToolName, Input: call.Input, ProviderExecuted: call.ProviderExecuted, ProviderOptions: fantasy.ProviderOptions(call.ProviderMetadata)}
			s.responseContent[index] = call
			break
		}
	}
	if err := s.publish(Event{Type: EventToolCall, ID: call.ToolCallID, ToolCall: &call}); err != nil {
		return err
	}
	s.pendingCalls--
	if s.pendingCalls == 0 {
		step := fantasy.StepResult{Response: fantasy.Response{Content: s.responseContent, FinishReason: s.finish.FinishReason, Usage: s.finish.Usage, ProviderMetadata: s.finish.ProviderMetadata}, Messages: []fantasy.Message{{Role: fantasy.MessageRoleAssistant, Content: s.responseParts}}}
		return s.persistResponse(step)
	}
	return nil
}

func (s *runState) persistResponse(step fantasy.StepResult) error {
	if s.responsePersisted {
		return nil
	}
	if len(step.Messages) > 0 {
		if err := s.engine.opts.History.Append(s.ctx, s.call.SessionID, step.Messages); err != nil {
			return fmt.Errorf("persist model response: %w", err)
		}
	}
	s.responsePersisted = true
	return s.publish(Event{Type: EventStepResponse, Result: &step, BlockIDs: append([]string(nil), s.blockIDs...)})
}
