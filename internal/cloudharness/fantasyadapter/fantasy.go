package fantasyadapter

import (
	"context"
	"errors"
	"fmt"

	"charm.land/fantasy"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

type FantasyModel struct {
	model           fantasy.LanguageModel
	tools           []fantasy.Tool
	systemPrompt    string
	runtimeReminder string
	maxOutputTokens *int64
	providerOptions fantasy.ProviderOptions
	callbacks       StreamCallbacks
}

type StreamCallbacks struct {
	OnTextDelta      fantasy.OnTextDeltaFunc
	OnReasoningStart fantasy.OnReasoningStartFunc
	OnReasoningEnd   fantasy.OnReasoningEndFunc
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

func (m *FantasyModel) WithMaxOutputTokens(maxTokens *int64) *FantasyModel {
	model := *m
	model.maxOutputTokens = maxTokens
	return &model
}

func (m *FantasyModel) WithProviderOptions(options fantasy.ProviderOptions) *FantasyModel {
	model := *m
	model.providerOptions = options
	return &model
}

func (m *FantasyModel) WithStreamCallbacks(callbacks StreamCallbacks) *FantasyModel {
	model := *m
	model.callbacks = callbacks
	return &model
}

func (m *FantasyModel) Complete(ctx context.Context, history []harness.Message) (harness.ModelCompleted, error) {
	prompt := make(fantasy.Prompt, 0, len(history)+2)
	if m.systemPrompt != "" {
		prompt = append(prompt, fantasy.NewSystemMessage(m.systemPrompt))
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
	if len(history) == 0 && m.runtimeReminder == "" {
		return harness.ModelCompleted{}, &fantasy.Error{Title: "invalid argument", Message: "prompt can't be empty when there are no messages"}
	}
	switch prompt[len(prompt)-1].Role {
	case fantasy.MessageRoleUser, fantasy.MessageRoleTool:
	default:
		return harness.ModelCompleted{}, &fantasy.Error{Title: "invalid argument", Message: "prompt can't be empty when the last message is not a user or tool message"}
	}
	stream, err := m.model.Stream(ctx, fantasy.Call{
		Prompt:          prompt,
		MaxOutputTokens: m.maxOutputTokens,
		Tools:           m.tools,
		ToolChoice:      new(fantasy.ToolChoiceAuto),
		ProviderOptions: m.providerOptions,
	})
	if err != nil {
		return harness.ModelCompleted{}, err
	}
	return readModelStream(stream, m.callbacks)
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
