package messages

import (
	"encoding/base64"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

type messageFileBlock struct {
	Type   string `json:"type"`
	Source struct {
		Type   string `json:"type"`
		FileID string `json:"file_id"`
	} `json:"source"`
	Content json.RawMessage `json:"content"`
}

type messageFileText struct {
	Type    string `json:"type"`
	Title   string `json:"title,omitempty"`
	Context string `json:"context,omitempty"`
	Content string `json:"content"`
}

type messageFileDocument struct {
	Title     string `json:"title"`
	Context   string `json:"context"`
	Citations struct {
		Enabled bool `json:"enabled"`
	} `json:"citations"`
}

type messageInlineFileSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func resolveMessageFileReferences(body []byte, load messageFileLoader) ([]byte, *httpapi.Error) {
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil {
		return nil, fileReferenceInvalidRequestError()
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(request["messages"], &messages) != nil {
		return body, nil
	}
	changed := false
	for _, message := range messages {
		content, replaced, err := resolveMessageFileContent(message["content"], load)
		if err != nil {
			return nil, err
		}
		if replaced {
			message["content"] = content
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		return nil, fileReferenceInvalidRequestError()
	}
	request["messages"] = encoded
	encoded, err = json.Marshal(request)
	if err != nil {
		return nil, fileReferenceInvalidRequestError()
	}
	if int64(len(encoded)) > llmproviders.MaxMessagesRequestBodyBytes {
		return nil, fileReferenceTooLargeError()
	}
	return encoded, nil
}

func resolveMessageFileContent(raw json.RawMessage, load messageFileLoader) (json.RawMessage, bool, *httpapi.Error) {
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return raw, false, nil
	}
	changed := false
	for i, raw := range blocks {
		var block messageFileBlock
		if json.Unmarshal(raw, &block) != nil {
			if (block.Type == "document" || block.Type == "image") && block.Source.Type == "file" {
				return nil, false, fileReferenceInvalidRequestError()
			}
			continue
		}
		var replacement json.RawMessage
		var replaced bool
		var err *httpapi.Error
		switch {
		case (block.Type == "document" || block.Type == "image") && block.Source.Type == "file":
			if block.Source.FileID == "" {
				return nil, false, fileReferenceInvalidRequestError()
			}
			file, loadErr := load(block.Source.FileID)
			if loadErr != nil {
				return nil, false, loadErr
			}
			replacement, err = resolvedMessageFileBlock(raw, block, file)
			replaced = true
		case block.Type == "tool_result":
			content, updated, childErr := resolveMessageFileContent(block.Content, load)
			if childErr != nil {
				return nil, false, childErr
			}
			if updated {
				var envelope map[string]json.RawMessage
				if json.Unmarshal(raw, &envelope) != nil {
					return nil, false, fileReferenceInvalidRequestError()
				}
				envelope["content"] = content
				replacement, _ = json.Marshal(envelope)
				replaced = true
			}
		}
		if err != nil {
			return nil, false, err
		}
		if replaced {
			blocks[i] = replacement
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return nil, false, fileReferenceInvalidRequestError()
	}
	return encoded, true, nil
}

func resolvedMessageFileBlock(raw json.RawMessage, block messageFileBlock, file messageFileContent) (json.RawMessage, *httpapi.Error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, fileReferenceInvalidRequestError()
	}
	var source messageInlineFileSource
	switch {
	case block.Type == "document" && file.mediaType == "text/plain":
		var attributes messageFileDocument
		if json.Unmarshal(raw, &attributes) != nil {
			return nil, fileReferenceInvalidRequestError()
		}
		if !attributes.Citations.Enabled {
			document, _ := json.Marshal(messageFileText{Type: "document", Title: attributes.Title, Context: attributes.Context, Content: string(file.data)})
			encoded, _ := json.Marshal(struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text", Text: string(document)})
			return encoded, nil
		}
		source = messageInlineFileSource{Type: "text", MediaType: "text/plain", Data: string(file.data)}
	case block.Type == "document" && file.mediaType == "application/pdf":
		source = encodedMessageFileSource(file)
	case block.Type == "image" && (file.mediaType == "image/png" || file.mediaType == "image/jpeg" || file.mediaType == "image/gif" || file.mediaType == "image/webp"):
		source = encodedMessageFileSource(file)
	default:
		return nil, fileReferenceInvalidTypeError()
	}
	envelope["source"], _ = json.Marshal(source)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fileReferenceInvalidRequestError()
	}
	return encoded, nil
}

func encodedMessageFileSource(file messageFileContent) messageInlineFileSource {
	return messageInlineFileSource{Type: "base64", MediaType: file.mediaType, Data: base64.StdEncoding.EncodeToString(file.data)}
}
