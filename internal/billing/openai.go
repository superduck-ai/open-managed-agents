package billing

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

type ResponseUsage struct {
	Input        *int64 `json:"input_tokens"`
	Output       *int64 `json:"output_tokens"`
	InputDetails *struct {
		Cached *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

type ResponsePayload struct {
	ID     string           `json:"id"`
	Status string           `json:"status"`
	Usage  *ResponseUsage   `json:"usage"`
	Output []ResponseOutput `json:"output"`
}

type ResponseOutput struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Summary   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func OpenAIUsage(input, output, cached *int64) (Usage, bool) {
	usage := Usage{InputTokens: input, OutputTokens: output}
	if (input != nil && *input < 0) || (output != nil && *output < 0) {
		return Usage{}, false
	}
	if cached != nil {
		if *cached < 0 || input == nil || *cached > *input {
			return Usage{}, false
		}
		usage.InputTokens = new(*input - *cached)
		usage.CacheReadTokens = cached
	}
	return usage, true
}

func (u *ResponseUsage) BillingUsage() (Usage, bool) {
	if u == nil {
		return Usage{}, true
	}
	var cached *int64
	if u.InputDetails != nil {
		cached = u.InputDetails.Cached
	}
	return OpenAIUsage(u.Input, u.Output, cached)
}

func ParseResponse(data []byte, streaming bool) (ResponsePayload, []byte, bool, error) {
	raw := data
	if streaming {
		var event struct {
			Type     string         `json:"type"`
			Response jsontext.Value `json:"response"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return ResponsePayload{}, nil, false, err
		}
		switch event.Type {
		case "response.completed", "response.incomplete", "response.failed":
			raw = event.Response
		case "error":
			return ResponsePayload{Status: "failed"}, data, true, nil
		default:
			return ResponsePayload{}, nil, false, nil
		}
	}
	var response ResponsePayload
	if err := json.Unmarshal(raw, &response); err != nil {
		return response, nil, false, err
	}
	switch response.Status {
	case "completed", "incomplete", "failed":
	default:
		return response, nil, false, errResponseNotTerminal
	}
	if response.ID == "" {
		return response, nil, false, errResponseMissingID
	}
	return response, raw, true, nil
}
