package sessioncreation

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
)

type contentBlockRequest struct {
	Type    jsonv1.RawMessage `json:"type"`
	Text    jsonv1.RawMessage `json:"text"`
	Source  jsonv1.RawMessage `json:"source"`
	Context jsonv1.RawMessage `json:"context"`
	Title   jsonv1.RawMessage `json:"title"`
}

type contentSourceRequest struct {
	Type      jsonv1.RawMessage `json:"type"`
	Data      jsonv1.RawMessage `json:"data"`
	MediaType jsonv1.RawMessage `json:"media_type"`
	URL       jsonv1.RawMessage `json:"url"`
	FileID    jsonv1.RawMessage `json:"file_id"`
}

type outcomeRubricRequest struct {
	Type    jsonv1.RawMessage `json:"type"`
	FileID  jsonv1.RawMessage `json:"file_id"`
	Content jsonv1.RawMessage `json:"content"`
}

type initialEventRequest struct {
	Type          string            `json:"type"`
	Content       jsonv1.RawMessage `json:"content,omitempty"`
	Description   string            `json:"description,omitempty"`
	Rubric        jsonv1.RawMessage `json:"rubric,omitempty"`
	MaxIterations *int              `json:"max_iterations,omitempty"`
}

// InitialEvent 保存规范初始事件及审计所需的原始输入。
type InitialEvent struct {
	Raw           jsonv1.RawMessage `json:"-"`
	Type          string            `json:"type"`
	Content       []ContentBlock    `json:"content,omitempty"`
	Description   string            `json:"description,omitempty"`
	Rubric        *OutcomeRubric    `json:"rubric,omitempty"`
	MaxIterations *int              `json:"max_iterations,omitempty"`
}

type ContentBlock struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Source  *ContentSource `json:"source,omitempty"`
	Context string         `json:"context,omitempty"`
	Title   string         `json:"title,omitempty"`
}

type ContentSource struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

type OutcomeRubric struct {
	Type    string `json:"type"`
	FileID  string `json:"file_id,omitempty"`
	Content string `json:"content,omitempty"`
}

// ParseInitialEvents 共享初始事件内容与顺序校验，保留原文供 Session 写入。
func ParseInitialEvents(raw jsonv1.RawMessage) ([]InitialEvent, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, errors.New("initial_events is required")
	}
	var inputs []jsonv1.RawMessage
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, errors.New("initial_events must be an array")
	}
	if len(inputs) == 0 || len(inputs) > 50 {
		return nil, errors.New("initial_events must contain between 1 and 50 events")
	}
	events := make([]InitialEvent, 0, len(inputs))
	for index, rawInput := range inputs {
		var request initialEventRequest
		if err := json.Unmarshal(rawInput, &request); err != nil {
			return nil, errors.New("initial_events items must be objects")
		}
		event := InitialEvent{
			Raw:  rawInput,
			Type: request.Type, Description: request.Description, MaxIterations: request.MaxIterations,
		}
		switch request.Type {
		case "user.message":
			content, err := normalizeMessageContent(request.Content, false)
			if err != nil {
				return nil, err
			}
			event.Content = content
		case "system.message":
			if index != len(inputs)-1 {
				return nil, errors.New("system.message must be the final initial event")
			}
			if index == 0 || events[index-1].Type != "user.message" {
				return nil, errors.New("system.message must immediately follow user.message")
			}
			content, err := normalizeMessageContent(request.Content, true)
			if err != nil {
				return nil, err
			}
			event.Content = content
		case "user.define_outcome":
			if strings.TrimSpace(request.Description) == "" {
				return nil, errors.New("description must be non-empty")
			}
			rubric, err := normalizeOutcomeRubric(request.Rubric)
			if err != nil {
				return nil, err
			}
			event.Rubric = rubric
			if request.MaxIterations != nil {
				if *request.MaxIterations < 1 {
					return nil, errors.New("max_iterations must be positive")
				}
				if *request.MaxIterations > 20 {
					return nil, errors.New("max_iterations must be at most 20")
				}
			}
		default:
			return nil, errors.New("initial_events type must be user.message, user.define_outcome, or system.message")
		}
		events = append(events, event)
	}
	return events, nil
}

func normalizeMessageContent(raw jsonv1.RawMessage, textOnly bool) ([]ContentBlock, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, errors.New("initial_events content is required")
	}
	var requests []contentBlockRequest
	if err := json.Unmarshal(raw, &requests); err != nil {
		return nil, errors.New("initial_events content must be an array")
	}
	if len(requests) == 0 {
		return nil, errors.New("initial_events content must contain at least one block")
	}
	blocks := make([]ContentBlock, 0, len(requests))
	for _, request := range requests {
		blockType, err := parseRequiredRawString(request.Type, "content.type")
		if err != nil {
			return nil, err
		}
		if textOnly && blockType != "text" {
			return nil, errors.New("system.message content must contain only text blocks")
		}
		block := ContentBlock{Type: blockType}
		switch blockType {
		case "text":
			block.Text, err = parseRequiredRawString(request.Text, "content.text")
			if err != nil {
				return nil, err
			}
		case "image":
			block.Source, err = normalizeContentSource(request.Source, false)
			if err != nil {
				return nil, err
			}
		case "document":
			block.Source, err = normalizeContentSource(request.Source, true)
			if err != nil {
				return nil, err
			}
			for _, field := range []struct {
				name  string
				raw   jsonv1.RawMessage
				value *string
			}{
				{name: "context", raw: request.Context, value: &block.Context},
				{name: "title", raw: request.Title, value: &block.Title},
			} {
				if len(field.raw) > 0 && !jsonx.IsNull(field.raw) {
					*field.value, err = parseRequiredRawString(field.raw, field.name)
					if err != nil {
						return nil, err
					}
				}
			}
		default:
			return nil, errors.New("user.message content type must be text, image, or document")
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func normalizeContentSource(raw jsonv1.RawMessage, document bool) (*ContentSource, error) {
	var request contentSourceRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, errors.New("content source must be an object")
	}
	sourceType, err := parseRequiredRawString(request.Type, "source.type")
	if err != nil {
		return nil, err
	}
	source := &ContentSource{Type: sourceType}
	switch sourceType {
	case "base64":
		source.Data, err = parseRequiredRawString(request.Data, "source.data")
		if err != nil {
			return nil, err
		}
		source.MediaType, err = parseRequiredRawString(request.MediaType, "source.media_type")
	case "url":
		source.URL, err = parseRequiredRawString(request.URL, "source.url")
	case "file":
		source.FileID, err = parseRequiredRawString(request.FileID, "source.file_id")
	case "text":
		if !document {
			return nil, errors.New("image source type must be base64, url, or file")
		}
		source.Data, err = parseRequiredRawString(request.Data, "source.data")
		if err != nil {
			return nil, err
		}
		source.MediaType, err = parseRequiredRawString(request.MediaType, "source.media_type")
		if err != nil {
			return nil, err
		}
		if source.MediaType != "text/plain" {
			return nil, errors.New("text document media_type must be text/plain")
		}
	default:
		if document {
			return nil, errors.New("document source type must be base64, text, url, or file")
		}
		return nil, errors.New("image source type must be base64, url, or file")
	}
	if err != nil {
		return nil, err
	}
	return source, nil
}

func normalizeOutcomeRubric(raw jsonv1.RawMessage) (*OutcomeRubric, error) {
	var request outcomeRubricRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, errors.New("user.define_outcome rubric must be an object")
	}
	rubricType, err := parseRequiredRawString(request.Type, "rubric.type")
	if err != nil {
		return nil, err
	}
	rubric := &OutcomeRubric{Type: rubricType}
	switch rubricType {
	case "file":
		rubric.FileID, err = parseRequiredRawString(request.FileID, "rubric.file_id")
	case "text":
		rubric.Content, err = parseRequiredRawString(request.Content, "rubric.content")
		if err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(rubric.Content) > 262144 {
			return nil, errors.New("user.define_outcome text rubric must be at most 262144 characters")
		}
	default:
		return nil, errors.New("user.define_outcome rubric type must be file or text")
	}
	if err != nil {
		return nil, err
	}
	return rubric, nil
}

func parseRequiredRawString(raw jsonv1.RawMessage, name string) (string, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return "", fmt.Errorf("%s is required", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be non-empty", name)
	}
	return value, nil
}
