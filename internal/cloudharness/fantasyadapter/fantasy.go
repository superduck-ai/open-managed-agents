package fantasyadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"charm.land/fantasy"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

type FantasyModel struct {
	model           fantasy.LanguageModel
	tools           []fantasy.Tool
	systemPrompt    string
	runtimeReminder string
	providerOptions fantasy.ProviderOptions
}

func NewModel(model fantasy.LanguageModel, tools []fantasy.Tool) *FantasyModel {
	return &FantasyModel{model: model, tools: tools}
}

func (m *FantasyModel) WithSystemPrompt(prompt string) *FantasyModel {
	model := *m
	model.systemPrompt = prompt
	return &model
}

func (m *FantasyModel) WithRuntimeReminder(reminder string) *FantasyModel {
	model := *m
	model.runtimeReminder = reminder
	return &model
}

func (m *FantasyModel) WithProviderOptions(options fantasy.ProviderOptions) *FantasyModel {
	model := *m
	model.providerOptions = options
	return &model
}

func (m *FantasyModel) Complete(ctx context.Context, history []harness.Message) (harness.ModelCompleted, error) {
	prompt := make(fantasy.Prompt, 0, len(history)+2)
	if m.systemPrompt != "" {
		prompt = append(prompt, fantasy.Message{Role: fantasy.MessageRoleSystem, Content: []fantasy.MessagePart{fantasy.TextPart{Text: m.systemPrompt}}})
	}
	for _, message := range history {
		converted, err := fantasyMessage(message)
		if err != nil {
			return harness.ModelCompleted{}, err
		}
		prompt = append(prompt, converted)
	}
	if m.runtimeReminder != "" {
		prompt = append(prompt, fantasy.NewUserMessage(m.runtimeReminder))
	}
	stream, err := m.model.Stream(ctx, fantasy.Call{Prompt: prompt, Tools: m.tools, MaxOutputTokens: new(int64(512)), ProviderOptions: m.providerOptions})
	if err != nil {
		return harness.ModelCompleted{}, err
	}
	return collectFantasyStream(stream)
}

func fantasyMessage(message harness.Message) (fantasy.Message, error) {
	switch value := message.(type) {
	case harness.UserMessage:
		return fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: value.Text}},
		}, nil
	case harness.AssistantMessage:
		var parts []fantasy.MessagePart
		if value.Text != "" {
			parts = append(parts, fantasy.TextPart{Text: value.Text})
		}
		if value.Call.ID != "" {
			parts = append(parts, fantasy.ToolCallPart{
				ToolCallID: string(value.Call.ID), ToolName: value.Call.Name, Input: value.Call.Arguments,
			})
		}
		return fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}, nil
	case harness.ToolResult:
		var output fantasy.ToolResultOutputContent = fantasy.ToolResultOutputContentText{Text: value.Text}
		if value.Failed {
			output = fantasy.ToolResultOutputContentError{Error: errors.New(value.Text)}
		}
		return fantasy.Message{
			Role:    fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: string(value.CallID), Output: output}},
		}, nil
	default:
		return fantasy.Message{}, fmt.Errorf("unsupported conversation message %T", message)
	}
}

func collectFantasyStream(stream fantasy.StreamResponse) (harness.ModelCompleted, error) {
	var text strings.Builder
	var call harness.ToolCall
	var finish fantasy.FinishReason
	var usage *harness.Usage
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			text.WriteString(part.Delta)
		case fantasy.StreamPartTypeToolCall:
			if call.ID != "" || part.ProviderExecuted {
				return harness.ModelCompleted{}, errors.New("harness supports one client tool call per completion")
			}
			if part.ID == "" || part.ToolCallName == "" || !json.Valid([]byte(part.ToolCallInput)) {
				return harness.ModelCompleted{}, errors.New("incomplete tool call in model stream")
			}
			call = harness.ToolCall{ID: harness.ToolCallID(part.ID), Name: part.ToolCallName, Arguments: part.ToolCallInput}
		case fantasy.StreamPartTypeFinish:
			finish = part.FinishReason
			usage = fantasyUsage(part.Usage)
		case fantasy.StreamPartTypeError:
			return harness.ModelCompleted{}, fmt.Errorf("model stream failed: %w", part.Error)
		}
	}
	if err := validateFantasyFinish(finish, call.ID != ""); err != nil {
		return harness.ModelCompleted{}, err
	}
	return harness.ModelCompleted{Message: harness.AssistantMessage{Text: text.String(), Call: call}, Usage: usage}, nil
}

func fantasyUsage(value fantasy.Usage) *harness.Usage {
	if value == (fantasy.Usage{}) {
		return nil
	}
	return &harness.Usage{
		InputTokens:       value.InputTokens,
		OutputTokens:      value.OutputTokens,
		TotalTokens:       value.TotalTokens,
		CachedInputTokens: value.CacheReadTokens,
	}
}

func validateFantasyFinish(reason fantasy.FinishReason, hasCall bool) error {
	if reason == fantasy.FinishReasonStop && !hasCall {
		return nil
	}
	if reason == fantasy.FinishReasonToolCalls && hasCall {
		return nil
	}
	return fmt.Errorf("unexpected model stream finish reason %q", reason)
}
