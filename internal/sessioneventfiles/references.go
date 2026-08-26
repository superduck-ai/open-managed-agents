// Package sessioneventfiles validates Files API references in public Session
// events and expands them only at the Code Session worker boundary.
package sessioneventfiles

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const userMessageType = "user.message"

type contentBlock struct {
	Type   string            `json:"type"`
	Source jsonv1.RawMessage `json:"source"`
}

type fileSource struct {
	Type   string `json:"type"`
	FileID string `json:"file_id"`
	Path   string `json:"path"`
}

type reference struct {
	blockType string
	fileID    string
}

type parsedContentBlock struct {
	raw           jsonv1.RawMessage
	fileReference bool
}

type resolvedEvent struct {
	object     map[string]jsonv1.RawMessage
	blocks     []parsedContentBlock
	references []reference
	bindings   []sessioncontract.EventFileBinding
}

type validationError struct{ cause error }

func (e *validationError) Error() string { return e.cause.Error() }

func (e *validationError) Unwrap() error { return e.cause }

// IsValidationError 判断错误是否来自公开事件输入。
func IsValidationError(err error) bool {
	_, ok := errors.AsType[*validationError](err)
	return ok
}

func markValidationError(err error) error {
	return &validationError{cause: err}
}

// ValidateMountedReferences 校验所有文件引用都已挂载到当前 Session。
func ValidateMountedReferences(
	eventType string,
	raw jsonv1.RawMessage,
	bindings []sessioncontract.EventFileBinding,
) error {
	_, err := resolveEvent(eventType, raw, bindings)
	if err != nil {
		return markValidationError(err)
	}
	return nil
}

// WorkerPayload replaces public file-source blocks with Claude Code @ path
// mentions while preserving the original public event outside this function.
func WorkerPayload(eventType string, raw jsonv1.RawMessage, bindings []sessioncontract.EventFileBinding) (jsonv1.RawMessage, error) {
	event, err := resolveEvent(eventType, raw, bindings)
	if err != nil {
		return nil, markValidationError(err)
	}
	if len(event.references) == 0 {
		return raw, nil
	}
	workerContent := make([]jsonv1.RawMessage, 0, len(event.blocks)+1)
	for _, block := range event.blocks {
		if !block.fileReference {
			workerContent = append(workerContent, block.raw)
		}
	}
	mentions := make([]string, 0, len(event.bindings))
	seenPaths := make(map[string]struct{}, len(event.bindings))
	for _, binding := range event.bindings {
		mention, sandboxPath, err := pathMention(binding.Path)
		if err != nil {
			return nil, fmt.Errorf("file_id %s has an invalid mount path", binding.FileID)
		}
		if _, exists := seenPaths[sandboxPath]; exists {
			continue
		}
		seenPaths[sandboxPath] = struct{}{}
		mentions = append(mentions, mention)
	}
	mentionBlock, err := json.Marshal(map[string]string{
		"type": "text",
		"text": strings.Join(mentions, "\n"),
	})
	if err != nil {
		return nil, err
	}
	workerContent = append(workerContent, mentionBlock)
	contentRaw, err := json.Marshal(workerContent)
	if err != nil {
		return nil, err
	}
	event.object["content"] = contentRaw
	return json.Marshal(event.object)
}

func resolveEvent(
	eventType string,
	raw jsonv1.RawMessage,
	bindings []sessioncontract.EventFileBinding,
) (resolvedEvent, error) {
	event, err := parseEvent(eventType, raw)
	if err != nil {
		return resolvedEvent{}, err
	}
	event.bindings, err = resolveReferences(event.references, bindings)
	if err != nil {
		return resolvedEvent{}, err
	}
	return event, nil
}

func parseEvent(eventType string, raw jsonv1.RawMessage) (resolvedEvent, error) {
	var object map[string]jsonv1.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return resolvedEvent{}, errors.New("event payload is invalid")
	}
	var content []jsonv1.RawMessage
	if contentRaw, exists := object["content"]; exists {
		if err := json.Unmarshal(contentRaw, &content); err != nil {
			return resolvedEvent{}, errors.New("event payload is invalid")
		}
	}
	event := resolvedEvent{
		object:     object,
		blocks:     make([]parsedContentBlock, 0, len(content)),
		references: make([]reference, 0),
	}
	for _, item := range content {
		block, source, err := decodeFileCapableBlock(item)
		if err != nil {
			return resolvedEvent{}, err
		}
		parsedBlock := parsedContentBlock{raw: item}
		if block == nil || source == nil {
			event.blocks = append(event.blocks, parsedBlock)
			continue
		}
		if source.Path != "" || localPathSourceType(source.Type) {
			return resolvedEvent{}, errors.New("local file paths are not accepted; upload the file and attach it through the Session Resources API")
		}
		if source.Type != "file" {
			event.blocks = append(event.blocks, parsedBlock)
			continue
		}
		if eventType != userMessageType {
			return resolvedEvent{}, errors.New("file content blocks are only supported in user.message events")
		}
		fileID := source.FileID
		parsedBlock.fileReference = true
		event.blocks = append(event.blocks, parsedBlock)
		event.references = append(event.references, reference{blockType: block.Type, fileID: fileID})
	}
	return event, nil
}

func decodeFileCapableBlock(raw jsonv1.RawMessage) (*contentBlock, *fileSource, error) {
	var block contentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return nil, nil, errors.New("content items must be objects")
	}
	if block.Type != "document" && block.Type != "image" {
		return nil, nil, nil
	}
	if len(block.Source) == 0 || string(block.Source) == "null" {
		return &block, nil, nil
	}
	var source fileSource
	if err := json.Unmarshal(block.Source, &source); err != nil {
		return nil, nil, errors.New("document and image source must be an object")
	}
	return &block, &source, nil
}

func resolveReferences(
	references []reference,
	bindings []sessioncontract.EventFileBinding,
) ([]sessioncontract.EventFileBinding, error) {
	byFileID := make(map[string]sessioncontract.EventFileBinding, len(bindings))
	for _, binding := range bindings {
		if _, exists := byFileID[binding.FileID]; !exists {
			byFileID[binding.FileID] = binding
		}
	}
	resolved := make([]sessioncontract.EventFileBinding, 0, len(references))
	for _, reference := range references {
		binding, ok := byFileID[reference.fileID]
		if !ok {
			return nil, fmt.Errorf(
				"file_id %s is not mounted in this session; attach it through the Session Resources API before sending the event",
				reference.fileID,
			)
		}
		if reference.blockType == "image" && !supportedImageMimeType(binding.MimeType) {
			return nil, fmt.Errorf("file_id %s is not a supported image", reference.fileID)
		}
		resolved = append(resolved, binding)
	}
	return resolved, nil
}

func pathMention(backingPath string) (string, string, error) {
	sandboxPath, err := sandboxmount.SandboxFilePath(backingPath)
	if err != nil {
		return "", "", err
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(sandboxPath)
	return `@"` + escaped + `"`, sandboxPath, nil
}

func localPathSourceType(sourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "path", "file_path", "local_file", "local_path":
		return true
	default:
		return false
	}
}

func supportedImageMimeType(mimeType string) bool {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
