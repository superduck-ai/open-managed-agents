package runtime

import (
	"context"
	"time"

	"charm.land/fantasy"
)

func guardedChain[T any](state *runState, first, second func(T) error) func(T) error {
	return func(value T) error {
		if err := state.err(); err != nil {
			return err
		}
		if first != nil {
			if err := first(value); err != nil {
				return state.fail(err)
			}
		}
		return state.fail(second(value))
	}
}

func guardedPair[T any](state *runState, first, second func(string, T) error) func(string, T) error {
	return func(id string, value T) error {
		if err := state.err(); err != nil {
			return err
		}
		if first != nil {
			if err := first(id, value); err != nil {
				return state.fail(err)
			}
		}
		return state.fail(second(id, value))
	}
}

func (s *runState) decorate(_ context.Context, call fantasy.AgentStreamCall) (fantasy.AgentStreamCall, error) {
	if call.Prompt == "" && len(call.Files) > 0 {
		call.Messages = append(call.Messages, fantasy.NewUserMessage("", call.Files...))
		call.Files = nil
	}
	callbacks := s.streamCallbacks()
	call.MaxRetries = callbacks.MaxRetries
	call.StopWhen = append(call.StopWhen, callbacks.StopWhen...)
	call.OnStepStart = guardedChain(s, call.OnStepStart, callbacks.OnStepStart)
	call.OnTextStart = guardedChain(s, call.OnTextStart, callbacks.OnTextStart)
	call.OnTextDelta = guardedPair(s, call.OnTextDelta, callbacks.OnTextDelta)
	call.OnTextEnd = guardedChain(s, call.OnTextEnd, callbacks.OnTextEnd)
	call.OnReasoningStart = guardedPair(s, call.OnReasoningStart, callbacks.OnReasoningStart)
	call.OnReasoningDelta = guardedPair(s, call.OnReasoningDelta, callbacks.OnReasoningDelta)
	call.OnReasoningEnd = guardedPair(s, call.OnReasoningEnd, callbacks.OnReasoningEnd)
	toolCall := guardedChain(s, call.OnToolCall, callbacks.OnToolCall)
	call.OnToolCall = func(value fantasy.ToolCallContent) error {
		_ = toolCall(value)
		return nil
	}
	call.OnToolResult = guardedChain(s, call.OnToolResult, callbacks.OnToolResult)
	call.OnChunk = guardedChain(s, call.OnChunk, callbacks.OnChunk)
	call.OnStepFinish = guardedChain(s, call.OnStepFinish, callbacks.OnStepFinish)
	call.OnAgentFinish = guardedChain(s, call.OnAgentFinish, callbacks.OnAgentFinish)
	originalRetry := call.OnRetry
	call.OnRetry = func(err *fantasy.ProviderError, delay time.Duration) {
		if originalRetry != nil {
			originalRetry(err, delay)
		}
		callbacks.OnRetry(err, delay)
	}
	return call, nil
}
