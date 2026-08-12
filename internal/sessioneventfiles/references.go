// Package sessioneventfiles validates Files API references in public Session
// events and expands them only at the Code Session worker boundary.
package sessioneventfiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const userMessageType = "user.message"

type eventEnvelope struct {
	Content []json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type   string          `json:"type"`
	Source json.RawMessage `json:"source"`
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

type validationError struct{ cause error }

func (e *validationError) Error() string { return e.cause.Error() }

func (e *validationError) Unwrap() error { return e.cause }

// IsValidationError 判断错误是否来自公开事件输入。
func IsValidationError(err error) bool {
	var validationErr *validationError
	return errors.As(err, &validationErr)
}

func markValidationError(err error) error {
	return &validationError{cause: err}
}

// ValidateMountedReferences 校验所有文件引用都已挂载到当前 Session。
func ValidateMountedReferences(
	eventType string,
	raw json.RawMessage,
	bindings []sessioncontract.EventFileBinding,
) error {
	references, err := referencesFromEvent(eventType, raw)
	if err != nil {
		return markValidationError(err)
	}
	_, err = resolveReferences(references, bindings)
	if err != nil {
		return markValidationError(err)
	}
	return nil
}

// WorkerPayload replaces public file-source blocks with Claude Code @ path
// mentions while preserving the original public event outside this function.
func WorkerPayload(eventType string, raw json.RawMessage, bindings []sessioncontract.EventFileBinding) (json.RawMessage, error) {
	references, err := referencesFromEvent(eventType, raw)
	if err != nil {
		return nil, markValidationError(err)
	}
	if len(references) == 0 {
		return raw, nil
	}
	resolved, err := resolveReferences(references, bindings)
	if err != nil {
		return nil, markValidationError(err)
	}

	var envelope eventEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, errors.New("event payload is invalid")
	}
	workerContent := make([]json.RawMessage, 0, len(envelope.Content)+1)
	for _, item := range envelope.Content {
		fileBlock, err := isFileSourceBlock(item)
		if err != nil {
			return nil, err
		}
		if !fileBlock {
			workerContent = append(workerContent, item)
		}
	}
	mentions := make([]string, 0, len(resolved))
	seenPaths := make(map[string]struct{}, len(resolved))
	for _, binding := range resolved {
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
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, errors.New("event payload is invalid")
	}
	object["content"] = contentRaw
	return json.Marshal(object)
}

func referencesFromEvent(eventType string, raw json.RawMessage) ([]reference, error) {
	var envelope eventEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, errors.New("event payload is invalid")
	}
	references := make([]reference, 0)
	for _, item := range envelope.Content {
		block, source, err := decodeFileCapableBlock(item)
		if err != nil {
			return nil, err
		}
		if block == nil || source == nil {
			continue
		}
		if source.Path != "" || localPathSourceType(source.Type) {
			return nil, errors.New("local file paths are not accepted; upload the file and attach it through the Session Resources API")
		}
		if source.Type != "file" {
			continue
		}
		if eventType != userMessageType {
			return nil, errors.New("file content blocks are only supported in user.message events")
		}
		fileID := source.FileID
		if strings.TrimSpace(fileID) == "" {
			return nil, errors.New("file content block source.file_id is required")
		}
		references = append(references, reference{blockType: block.Type, fileID: fileID})
	}
	return references, nil
}

func decodeFileCapableBlock(raw json.RawMessage) (*contentBlock, *fileSource, error) {
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

func isFileSourceBlock(raw json.RawMessage) (bool, error) {
	block, source, err := decodeFileCapableBlock(raw)
	return err == nil && block != nil && source != nil && source.Type == "file", err
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
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
