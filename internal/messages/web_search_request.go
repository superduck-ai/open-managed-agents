package messages

import (
	"encoding/json"
	"errors"
	"fmt"
)

// webSearchGatewayRequest is the request projection boundary. It retains only
// the structured fields needed to decide whether and how Web Search is managed.
type webSearchGatewayRequest struct {
	fields   map[string]json.RawMessage
	messages []json.RawMessage
	stream   bool
}

type webSearchPolicy struct {
	MaxUses        int
	AllowedDomains []string
	BlockedDomains []string
}

type webSearchUserLocation struct {
	Type     string `json:"type"`
	City     string `json:"city,omitempty"`
	Region   string `json:"region,omitempty"`
	Country  string `json:"country,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

func parseWebSearchRequest(body []byte) (webSearchGatewayRequest, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return webSearchGatewayRequest{}, fmt.Errorf("invalid JSON request body: %w", err)
	}
	var messages []json.RawMessage
	if raw := fields["messages"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return webSearchGatewayRequest{}, fmt.Errorf("messages must be an array: %w", err)
		}
	}
	var stream bool
	if raw := fields["stream"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return webSearchGatewayRequest{}, fmt.Errorf("stream must be a boolean: %w", err)
		}
	}
	return webSearchGatewayRequest{fields: fields, messages: messages, stream: stream}, nil
}

func hasWebSearchTool(raw json.RawMessage) bool {
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return false
	}
	for _, rawTool := range tools {
		var tool struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			continue
		}
		if isServerWebSearchToolType(tool.Type) {
			return true
		}
	}
	return false
}

func projectWebSearchFields(fields map[string]json.RawMessage) (map[string]json.RawMessage, webSearchPolicy, error) {
	projected := cloneRawMap(fields)
	rawTools, ok := fields["tools"]
	if !ok {
		return projected, webSearchPolicy{}, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return nil, webSearchPolicy{}, fmt.Errorf("tools must be an array: %w", err)
	}
	projectedTools := make([]json.RawMessage, 0, len(tools))
	var searchPolicy webSearchPolicy
	foundSearchTool := false
	for _, rawTool := range tools {
		var tool webSearchTool
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			return nil, webSearchPolicy{}, fmt.Errorf("decode tool: %w", err)
		}
		if isServerWebSearchToolType(tool.Type) {
			if foundSearchTool {
				return nil, webSearchPolicy{}, errors.New("multiple web search tools are unsupported")
			}
			var err error
			searchPolicy, err = tool.searchPolicy()
			if err != nil {
				return nil, webSearchPolicy{}, err
			}
			foundSearchTool = true
			projectedTools = append(projectedTools, searchToolDefinition())
			continue
		}
		projectedTools = append(projectedTools, rawTool)
	}
	encodedTools, err := json.Marshal(projectedTools)
	if err != nil {
		return nil, webSearchPolicy{}, fmt.Errorf("encode tools: %w", err)
	}
	projected["tools"] = encodedTools
	if foundSearchTool {
		if err := projectWebSearchToolChoice(projected); err != nil {
			return nil, webSearchPolicy{}, err
		}
	}
	return projected, searchPolicy, nil
}

func projectWebSearchToolChoice(fields map[string]json.RawMessage) error {
	rawChoice, ok := fields["tool_choice"]
	if !ok {
		return nil
	}
	var choice map[string]json.RawMessage
	if err := json.Unmarshal(rawChoice, &choice); err != nil {
		return nil
	}
	var choiceType, name string
	if json.Unmarshal(choice["type"], &choiceType) != nil || json.Unmarshal(choice["name"], &name) != nil {
		return nil
	}
	if choiceType != "tool" || name != searchToolName {
		return nil
	}
	encodedName, err := json.Marshal(upstreamSearchToolName)
	if err != nil {
		return fmt.Errorf("encode projected tool choice name: %w", err)
	}
	choice["name"] = encodedName
	encodedChoice, err := json.Marshal(choice)
	if err != nil {
		return fmt.Errorf("encode projected tool choice: %w", err)
	}
	fields["tool_choice"] = encodedChoice
	return nil
}

type webSearchTool struct {
	Type              string                 `json:"type"`
	MaxUses           *int                   `json:"max_uses,omitempty"`
	AllowedDomains    []string               `json:"allowed_domains,omitempty"`
	BlockedDomains    []string               `json:"blocked_domains,omitempty"`
	AllowedCallers    []string               `json:"allowed_callers,omitempty"`
	ResponseInclusion string                 `json:"response_inclusion,omitempty"`
	UserLocation      *webSearchUserLocation `json:"user_location,omitempty"`
}

func (t webSearchTool) searchPolicy() (webSearchPolicy, error) {
	if err := t.validateDirectCaller(); err != nil {
		return webSearchPolicy{}, err
	}
	if err := t.validateResponseInclusion(); err != nil {
		return webSearchPolicy{}, err
	}
	if t.MaxUses != nil && *t.MaxUses <= 0 {
		return webSearchPolicy{}, errors.New("web search max_uses must be positive")
	}
	if len(t.AllowedDomains) > 0 && len(t.BlockedDomains) > 0 {
		return webSearchPolicy{}, errors.New("web search cannot include both allowed_domains and blocked_domains")
	}
	if t.UserLocation != nil {
		return webSearchPolicy{}, errors.New("web search user_location is unsupported by the configured provider")
	}
	policy := webSearchPolicy{AllowedDomains: append([]string(nil), t.AllowedDomains...), BlockedDomains: append([]string(nil), t.BlockedDomains...)}
	if t.MaxUses != nil {
		policy.MaxUses = *t.MaxUses
	}
	return policy, nil
}

func (t webSearchTool) validateDirectCaller() error {
	if t.AllowedCallers == nil {
		if t.Type == "web_search_20250305" {
			return nil
		}
		return errors.New(`web search allowed_callers must include "direct" for the configured BYOK model`)
	}
	direct := false
	for _, caller := range t.AllowedCallers {
		switch caller {
		case "direct":
			direct = true
		case "code_execution_20260120":
		default:
			return fmt.Errorf("unsupported web search allowed caller %q", caller)
		}
	}
	if !direct {
		return errors.New(`web search allowed_callers must include "direct" for the configured BYOK model`)
	}
	return nil
}

func (t webSearchTool) validateResponseInclusion() error {
	if t.ResponseInclusion == "" {
		return nil
	}
	if t.Type != "web_search_20260318" {
		return errors.New("web search response_inclusion requires web_search_20260318")
	}
	if t.ResponseInclusion != "full" && t.ResponseInclusion != "excluded" {
		return fmt.Errorf("unsupported web search response_inclusion %q", t.ResponseInclusion)
	}
	return nil
}

func isServerWebSearchToolType(value string) bool {
	switch value {
	case "web_search_20250305", "web_search_20260209", "web_search_20260318":
		return true
	default:
		return false
	}
}

func searchToolDefinition() json.RawMessage {
	return json.RawMessage(`{"name":"` + upstreamSearchToolName + `","description":"Search the public web and return relevant results.","input_schema":{"type":"object","properties":{"query":{"type":"string","description":"The web search query"}},"required":["query"]}}`)
}
