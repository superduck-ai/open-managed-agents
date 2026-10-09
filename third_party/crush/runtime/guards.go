package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"maps"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
)

type SchemaTool interface {
	InputSchema() json.RawMessage
}

type guardedModel struct {
	fantasy.LanguageModel
	state *runState
	tools []fantasy.AgentTool
}

type guardedTool struct {
	fantasy.AgentTool
	state *runState
}

type modelTool struct {
	Name         string                  `json:"name"`
	Description  string                  `json:"description,omitempty"`
	InputSchema  json.RawMessage         `json:"input_schema"`
	CacheControl *anthropic.CacheControl `json:"cache_control,omitempty"`
}

func (m *guardedModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	if err := m.state.err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var tools []modelTool
	for _, definition := range call.Tools {
		function, ok := definition.(fantasy.FunctionTool)
		if !ok {
			continue
		}
		schema, err := json.Marshal(function.InputSchema)
		if err != nil {
			return nil, err
		}
		for _, tool := range m.tools {
			if tool.Info().Name == function.Name {
				if full, ok := tool.(SchemaTool); ok && len(full.InputSchema()) > 0 {
					schema = full.InputSchema()
				}
			}
		}
		definition := modelTool{Name: function.Name, Description: function.Description, InputSchema: schema}
		if cache, ok := function.ProviderOptions[anthropic.Name].(*anthropic.ProviderCacheControlOptions); ok {
			definition.CacheControl = &cache.CacheControl
		}
		tools = append(tools, definition)
	}
	if len(tools) > 0 {
		options := anthropic.ProviderOptions{}
		if prior, ok := call.ProviderOptions[anthropic.Name].(*anthropic.ProviderOptions); ok {
			options = *prior
		}
		options.ExtraBody = maps.Clone(options.ExtraBody)
		if options.ExtraBody == nil {
			options.ExtraBody = make(map[string]any)
		}
		options.ExtraBody["tools"] = tools
		call.ProviderOptions = maps.Clone(call.ProviderOptions)
		if call.ProviderOptions == nil {
			call.ProviderOptions = make(fantasy.ProviderOptions)
		}
		call.ProviderOptions[anthropic.Name] = &options
	}
	return m.LanguageModel.Stream(ctx, call)
}

func (s *runState) wrapTools(tools []fantasy.AgentTool) []fantasy.AgentTool {
	result := make([]fantasy.AgentTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, &guardedTool{AgentTool: tool, state: s})
	}
	return result
}

func (t *guardedTool) Info() fantasy.ToolInfo {
	info := t.AgentTool.Info()
	info.Parallel = false
	return info
}

func (t *guardedTool) InputSchema() json.RawMessage {
	if source, ok := t.AgentTool.(SchemaTool); ok {
		return source.InputSchema()
	}
	return nil
}

func (t *guardedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if err := t.state.err(); err != nil {
		return fantasy.ToolResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return fantasy.ToolResponse{}, err
	}
	response, err := t.AgentTool.Run(ctx, call)
	var fatal *ExecutionError
	if errors.As(err, &fatal) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return response, t.state.fail(err)
	}
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return response, nil
}

func (s *runState) fail(err error) error {
	if err == nil {
		return nil
	}
	s.mu.Lock()
	if s.failure == nil {
		s.failure = err
	}
	s.mu.Unlock()
	s.cancel()
	return err
}

func (s *runState) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failure
}
