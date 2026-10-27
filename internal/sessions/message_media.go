package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/messagemedia"
)

func messageMediaBlocks(payload json.RawMessage) ([]messagemedia.Block, error) {
	var message struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(payload, &message); err != nil {
		return nil, errors.New("content must be an array")
	}
	blocks := make([]messagemedia.Block, 0, len(message.Content))
	for _, raw := range message.Content {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &header); err != nil || !messagemedia.IsMediaBlock(header.Type) {
			continue
		}
		block, err := messagemedia.ParseBlock(raw)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func validateMessageMedia(payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	blocks, err := messageMediaBlocks(raw)
	if err != nil {
		return err
	}
	for _, block := range blocks {
		if err := messagemedia.ValidateInline(block); err != nil {
			return fmt.Errorf("content %s %w", block.Type, err)
		}
	}
	return nil
}

func (h *Handler) validateMessageFileSources(ctx context.Context, session db.Session, events []db.SessionEvent) error {
	for _, event := range events {
		if event.EventType != "user.message" {
			continue
		}
		blocks, err := messageMediaBlocks(event.Payload)
		if err != nil {
			return invalidRequest(err)
		}
		for _, block := range blocks {
			if block.Source.Type != messagemedia.SourceFile {
				continue
			}
			file, err := h.db.GetFile(ctx, session.WorkspaceUUID, block.Source.FileID)
			if errors.Is(err, db.ErrNotFound) {
				return invalidRequest(fmt.Errorf("content %s file_id %q was not found", block.Type, block.Source.FileID))
			}
			if err != nil {
				return internalError("Could not load message file", fmt.Errorf("load message file %q: %w", block.Source.FileID, err))
			}
			if !messagemedia.SupportedMediaType(block.Type, file.MimeType) {
				return invalidRequest(fmt.Errorf("content %s file_id %q has unsupported media type %q", block.Type, block.Source.FileID, file.MimeType))
			}
		}
	}
	return nil
}
