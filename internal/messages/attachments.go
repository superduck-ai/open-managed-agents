package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/messagemedia"
)

type AttachmentOpener interface {
	OpenSessionAttachment(context.Context, db.FileRecord) (io.ReadCloser, error)
}

var errAttachmentsTooLarge = errors.New("message attachments exceed the request size limit")

type attachmentContent struct {
	mediaType string
	data      []byte
	found     bool
}

type attachmentLookup func(ctx context.Context, fileID string, limit int64) (attachmentContent, error)

func (h *Handler) WithAttachments(opener AttachmentOpener) *Handler {
	if h != nil {
		h.attachments = opener
	}
	return h
}

func (h *Handler) expandSessionAttachments(ctx context.Context, principal auth.Principal, body []byte) ([]byte, error) {
	if h.attachments == nil || principal.CredentialType != auth.CredentialTypeCodeSessionOAuth || principal.PublicSessionUUID == "" {
		return body, nil
	}
	lookup := func(ctx context.Context, fileID string, limit int64) (attachmentContent, error) {
		file, err := h.database.GetSessionAttachmentFile(ctx, principal.WorkspaceUUID, principal.PublicSessionUUID, fileID)
		if errors.Is(err, db.ErrNotFound) {
			return attachmentContent{}, nil
		}
		if err != nil {
			return attachmentContent{}, err
		}
		if file.SizeBytes > limit {
			return attachmentContent{}, errAttachmentsTooLarge
		}
		reader, err := h.attachments.OpenSessionAttachment(ctx, file)
		if err != nil {
			return attachmentContent{}, err
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return attachmentContent{}, fmt.Errorf("read attachment %s: %w", fileID, err)
		}
		if int64(len(data)) > limit {
			return attachmentContent{}, errAttachmentsTooLarge
		}
		return attachmentContent{mediaType: file.MimeType, data: data, found: true}, nil
	}
	return expandAttachmentSources(ctx, body, lookup, llmproviders.MaxMessagesRequestBodyBytes)
}

func expandAttachmentSources(ctx context.Context, body []byte, lookup attachmentLookup, limit int64) ([]byte, error) {
	if !bytes.Contains(body, []byte(`"file_id"`)) {
		return body, nil
	}
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return body, nil
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(request["messages"], &messages); err != nil {
		return body, nil
	}
	expander := attachmentExpander{lookup: lookup, remaining: limit - int64(len(body))}
	changed := false
	for _, message := range messages {
		content, contentChanged, err := expander.expandContent(ctx, message["content"])
		if err != nil {
			return nil, err
		}
		if contentChanged {
			message["content"] = content
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	request["messages"] = encoded
	expanded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if int64(len(expanded)) > limit {
		return nil, errAttachmentsTooLarge
	}
	return expanded, nil
}

type attachmentExpander struct {
	lookup    attachmentLookup
	remaining int64
}

func (e *attachmentExpander) expandContent(ctx context.Context, raw json.RawMessage) (json.RawMessage, bool, error) {
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return raw, false, nil
	}
	changed := false
	for index, block := range blocks {
		var blockType string
		_ = json.Unmarshal(block["type"], &blockType)
		if blockType == "tool_result" {
			nested, nestedChanged, err := e.expandContent(ctx, block["content"])
			if err != nil {
				return nil, false, err
			}
			if nestedChanged {
				block["content"] = nested
				changed = true
			}
			continue
		}
		replacement, err := e.expandBlock(ctx, blockType, block)
		if err != nil {
			return nil, false, err
		}
		if replacement != nil {
			blocks[index] = replacement
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(blocks)
	return encoded, true, err
}

func (e *attachmentExpander) expandBlock(ctx context.Context, blockType string, block map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if !messagemedia.IsMediaBlock(blockType) {
		return nil, nil
	}
	var source messagemedia.Source
	if json.Unmarshal(block["source"], &source) != nil || source.Type != messagemedia.SourceFile || source.FileID == "" {
		return nil, nil
	}
	if e.remaining <= 0 {
		return nil, errAttachmentsTooLarge
	}
	content, err := e.lookup(ctx, source.FileID, e.remaining)
	if err != nil {
		return nil, err
	}
	if !content.found {
		return attachmentPlaceholder(blockType, "is no longer available")
	}
	expanded, err := messagemedia.Block{Type: blockType}.WithContent(content.mediaType, content.data)
	if err != nil {
		return attachmentPlaceholder(blockType, "has an unsupported format")
	}
	encoded, err := json.Marshal(expanded.Source)
	if err != nil {
		return nil, err
	}
	e.remaining -= int64(len(encoded))
	block["source"] = encoded
	return block, nil
}

func attachmentPlaceholder(blockType, reason string) (map[string]json.RawMessage, error) {
	text, err := json.Marshal(fmt.Sprintf("[The attached %s %s.]", blockType, reason))
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{"type": json.RawMessage(`"text"`), "text": text}, nil
}
