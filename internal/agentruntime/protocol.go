package agentruntime

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"charm.land/fantasy"
)

type launchConfig struct {
	Mode               Mode   `json:"agent_runtime_mode"`
	Model              string `json:"model"`
	SystemPrompt       string `json:"system_prompt"`
	AppendSystemPrompt string `json:"append_system_prompt"`
	MCPConfig          struct {
		Servers map[string]remoteTarget `json:"mcpServers"`
	} `json:"mcp_config"`
}

type remoteTarget struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type inputPayload struct {
	Type            string `json:"type"`
	ID              string `json:"id"`
	UUID            string `json:"uuid"`
	SessionThreadID string `json:"session_thread_id"`
	Message         struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Request struct {
		Subtype string `json:"subtype"`
	} `json:"request"`
	Response struct {
		Subtype   string             `json:"subtype"`
		RequestID string             `json:"request_id"`
		Response  permissionResponse `json:"response"`
	} `json:"response"`
}

type permissionResponse struct {
	Behavior     string          `json:"behavior"`
	ToolUseID    string          `json:"toolUseID"`
	UpdatedInput json.RawMessage `json:"updatedInput"`
	Message      string          `json:"message"`
}

type inputContent struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source struct {
		Type      string `json:"type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
		MediaType string `json:"media_type"`
	} `json:"source"`
}

func decodePrompt(content json.RawMessage) (string, []fantasy.FilePart, error) {
	var plain string
	if json.Unmarshal(content, &plain) == nil {
		if plain == "" {
			return "", nil, ErrInvalidInput
		}
		return plain, nil, nil
	}
	var blocks []inputContent
	if err := json.Unmarshal(content, &blocks); err != nil || len(blocks) == 0 {
		return "", nil, ErrInvalidInput
	}
	var texts []string
	var files []fantasy.FilePart
	for _, block := range blocks {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "image", "document":
			var data []byte
			switch block.Source.Type {
			case "base64":
				var err error
				data, err = base64.StdEncoding.DecodeString(block.Source.Data)
				if err != nil {
					return "", nil, ErrInvalidInput
				}
			default:
				return "", nil, ErrInvalidInput
			}
			if len(data) == 0 {
				return "", nil, ErrInvalidInput
			}
			files = append(files, fantasy.FilePart{Data: data, MediaType: block.Source.MediaType})
		default:
			return "", nil, ErrInvalidInput
		}
	}
	prompt := strings.Join(texts, "\n")
	if prompt == "" && len(files) == 0 {
		return "", nil, ErrInvalidInput
	}
	return prompt, files, nil
}
