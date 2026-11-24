package db

import (
	"encoding/json"
	"fmt"

	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

type harnessMessageData struct {
	Kind   string             `json:"kind"`
	Text   string             `json:"text,omitempty"`
	Call   harness.ToolCall   `json:"call,omitzero"`
	CallID harness.ToolCallID `json:"call_id,omitempty"`
	Failed bool               `json:"failed,omitempty"`
}

func encodeHarnessMessage(message harness.Message) (string, error) {
	var data harnessMessageData
	switch value := message.(type) {
	case harness.UserMessage:
		data = harnessMessageData{Kind: "user", Text: value.Text}
	case harness.AssistantMessage:
		data = harnessMessageData{Kind: "assistant", Text: value.Text, Call: value.Call}
	case harness.ToolResult:
		data = harnessMessageData{Kind: "tool_result", CallID: value.CallID, Text: value.Text, Failed: value.Failed}
	default:
		return "", fmt.Errorf("unsupported harness conversation message %T", message)
	}
	raw, err := json.Marshal(data)
	return string(raw), err
}

func decodeHarnessMessage(raw string) (harness.Message, error) {
	var data harnessMessageData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	switch data.Kind {
	case "user":
		return harness.UserMessage{Text: data.Text}, nil
	case "assistant":
		return harness.AssistantMessage{Text: data.Text, Call: data.Call}, nil
	case "tool_result":
		return harness.ToolResult{CallID: data.CallID, Text: data.Text, Failed: data.Failed}, nil
	default:
		return nil, fmt.Errorf("unsupported stored harness message kind %q", data.Kind)
	}
}
