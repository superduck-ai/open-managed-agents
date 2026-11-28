package fantasyadapter

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/fantasy"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

func readModelStream(stream fantasy.StreamResponse, callbacks StreamCallbacks) (harness.ModelCompleted, error) {
	var text strings.Builder
	var tool toolInput
	var finish *fantasy.StreamPart
	reasoning := make(map[string]fantasy.ReasoningContent)
	for part := range stream {
		var err error
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			text.WriteString(part.Delta)
			if callbacks.OnTextDelta != nil {
				err = callbacks.OnTextDelta(part.ID, part.Delta)
			}
		case fantasy.StreamPartTypeReasoningStart:
			content := fantasy.ReasoningContent{Text: part.Delta, ProviderMetadata: part.ProviderMetadata}
			reasoning[part.ID] = content
			if callbacks.OnReasoningStart != nil {
				err = callbacks.OnReasoningStart(part.ID, content)
			}
		case fantasy.StreamPartTypeReasoningDelta:
			if content, ok := reasoning[part.ID]; ok {
				content.Text += part.Delta
				if part.ProviderMetadata != nil {
					content.ProviderMetadata = part.ProviderMetadata
				}
				reasoning[part.ID] = content
			}
		case fantasy.StreamPartTypeReasoningEnd:
			if content, ok := reasoning[part.ID]; ok {
				delete(reasoning, part.ID)
				if part.ProviderMetadata != nil {
					content.ProviderMetadata = part.ProviderMetadata
				}
				if callbacks.OnReasoningEnd != nil {
					err = callbacks.OnReasoningEnd(part.ID, content)
				}
			}
		case fantasy.StreamPartTypeToolInputStart:
			err = tool.start(part.ID, part.ToolCallName)
		case fantasy.StreamPartTypeToolInputDelta:
			err = tool.delta(part.ID, part.Delta)
		case fantasy.StreamPartTypeToolInputEnd:
			err = tool.end(part.ID)
		case fantasy.StreamPartTypeToolCall:
			if part.ProviderExecuted {
				return harness.ModelCompleted{}, errors.New("harness supports one client tool call per completion")
			}
			err = tool.confirm(harness.ToolCall{
				ID:        harness.ToolCallID(part.ID),
				Name:      part.ToolCallName,
				Arguments: part.ToolCallInput,
			})
		case fantasy.StreamPartTypeFinish:
			finish = &part
		case fantasy.StreamPartTypeError:
			if part.Error != nil {
				return harness.ModelCompleted{}, part.Error
			}
			return harness.ModelCompleted{}, errors.New("model stream returned an error without details")
		}
		if err != nil {
			return harness.ModelCompleted{}, err
		}
	}
	if finish == nil {
		return harness.ModelCompleted{}, errors.New("model stream ended without a finish reason")
	}
	if err := validateFinish(finish.FinishReason, &tool); err != nil {
		return harness.ModelCompleted{}, err
	}
	return harness.ModelCompleted{
		Message: harness.AssistantMessage{Text: text.String(), Call: tool.call},
		Usage:   fantasyUsage(finish.Usage),
	}, nil
}

func validateFinish(reason fantasy.FinishReason, tool *toolInput) error {
	switch reason {
	case fantasy.FinishReasonStop:
		if tool.call.ID != "" {
			return errors.New("model stopped with a tool call")
		}
	case fantasy.FinishReasonToolCalls:
		if !tool.confirmed {
			return errors.New("incomplete tool call in model stream")
		}
	default:
		return fmt.Errorf("unexpected model stream finish reason %q", reason)
	}
	return nil
}
