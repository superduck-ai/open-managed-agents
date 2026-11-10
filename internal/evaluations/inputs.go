package evaluations

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/messagemedia"
)

type storedInput struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Source *storedFile `json:"source,omitempty"`
}

type storedFile struct {
	Type   string `json:"type"`
	FileID string `json:"file_id"`
}

type incomingBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source struct {
		Type   string `json:"type"`
		FileID string `json:"file_id"`
	} `json:"source"`
}

func canonicalInputs(query string, raw json.RawMessage) (string, json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return textInputs(query)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil, invalidArgument(inputInvalidMessage, nil)
	}
	blocks, err := decodeInputBlocks(raw)
	if err != nil {
		return "", nil, err
	}
	stored, err := strictInputs(blocks)
	if err != nil {
		return "", nil, err
	}
	return finishInputs(stored)
}

func textInputs(query string) (string, json.RawMessage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil, invalidArgument(inputRequiredMessage, nil)
	}
	return finishInputs([]storedInput{{Type: "text", Text: query}})
}

func decodeInputBlocks(raw json.RawMessage) ([]incomingBlock, error) {
	var blocks []incomingBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, invalidArgument(inputInvalidMessage, err)
	}
	return blocks, nil
}

func strictInputs(blocks []incomingBlock) ([]storedInput, error) {
	stored := make([]storedInput, 0, len(blocks))
	for _, block := range blocks {
		item, keep, err := classifyBlock(block)
		if err != nil {
			return nil, err
		}
		if keep {
			stored = append(stored, item)
		}
	}
	return stored, nil
}

func looseInputs(blocks []incomingBlock) []storedInput {
	stored := make([]storedInput, 0, len(blocks))
	for _, block := range blocks {
		item, keep, err := classifyBlock(block)
		if err != nil || !keep {
			continue
		}
		stored = append(stored, item)
	}
	return stored
}

func classifyBlock(block incomingBlock) (storedInput, bool, error) {
	switch block.Type {
	case "text":
		text := strings.TrimSpace(block.Text)
		if text == "" {
			return storedInput{}, false, nil
		}
		return storedInput{Type: "text", Text: text}, true, nil
	case "image", "document":
		if block.Source.Type != messagemedia.SourceFile || strings.TrimSpace(block.Source.FileID) == "" {
			return storedInput{}, false, invalidArgument(inputInvalidMessage, nil)
		}
		return storedInput{Type: block.Type, Source: &storedFile{Type: messagemedia.SourceFile, FileID: strings.TrimSpace(block.Source.FileID)}}, true, nil
	default:
		return storedInput{}, false, invalidArgument(inputInvalidMessage, nil)
	}
}

func finishInputs(stored []storedInput) (string, json.RawMessage, error) {
	if len(stored) == 0 {
		return "", nil, invalidArgument(inputRequiredMessage, nil)
	}
	if len(stored) > maxInputBlocks {
		return "", nil, invalidArgument(inputTooLargeMessage, nil)
	}
	var texts []string
	for _, block := range stored {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	query := strings.Join(texts, "\n")
	if utf8.RuneCountInString(query) > maxQueryLength {
		return "", nil, invalidArgument(queryTooLongMessage, nil)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return "", nil, internalFailure(loadFailedMessage, err)
	}
	return query, encoded, nil
}

func resolveInputs(inputs json.RawMessage, lookup func(string) (string, error)) error {
	var blocks []storedInput
	if err := json.Unmarshal(inputs, &blocks); err != nil {
		return internalFailure(loadFailedMessage, err)
	}
	for _, block := range blocks {
		if block.Type != "image" && block.Type != "document" {
			continue
		}
		mime, err := lookup(block.Source.FileID)
		if err != nil {
			return err
		}
		if !messagemedia.SupportedMediaType(block.Type, mime) {
			return invalidArgument(fileMediaMessage, nil)
		}
	}
	return nil
}

const (
	userTurnEmpty = iota
	userTurnSkipped
	userTurnReady
)

func userTurnInputs(payload json.RawMessage) (string, json.RawMessage, int) {
	var body struct {
		Content json.RawMessage `json:"content"`
		Text    string          `json:"text"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", nil, userTurnEmpty
	}
	content := bytes.TrimSpace(body.Content)
	if len(content) > 0 && !bytes.Equal(content, []byte("null")) {
		if content[0] == '"' {
			var text string
			if err := json.Unmarshal(content, &text); err == nil {
				return readyTextTurn(text)
			}
		}
		if content[0] == '[' {
			blocks, err := decodeInputBlocks(content)
			if err != nil {
				return "", nil, userTurnSkipped
			}
			stored := looseInputs(blocks)
			if len(stored) == 0 {
				if len(blocks) == 0 {
					return "", nil, userTurnEmpty
				}
				return "", nil, userTurnSkipped
			}
			query, inputs, err := finishInputs(stored)
			if err != nil {
				return "", nil, userTurnSkipped
			}
			return query, inputs, userTurnReady
		}
	}
	return readyTextTurn(body.Text)
}

func readyTextTurn(text string) (string, json.RawMessage, int) {
	if strings.TrimSpace(text) == "" {
		return "", nil, userTurnEmpty
	}
	query, inputs, err := textInputs(text)
	if err != nil {
		return "", nil, userTurnSkipped
	}
	return query, inputs, userTurnReady
}
