package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/messagemedia"
)

type SessionAttachmentStore interface {
	PutSessionAttachment(context.Context, filestore.SessionAttachmentTarget, string, io.Reader) error
	CopyFileToSessionAttachment(context.Context, filestore.SessionAttachmentTarget, db.FileRecord) error
}

func (s *Service) WithSessionAttachments(store SessionAttachmentStore) *Service {
	if s != nil {
		s.attachments = store
	}
	return s
}

func (s *Service) materializeMessageAttachments(ctx context.Context, event db.SessionEvent) (db.SessionEvent, error) {
	if event.EventType != "user.message" {
		return event, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &fields); err != nil {
		return event, nil
	}
	var content []json.RawMessage
	if err := json.Unmarshal(fields["content"], &content); err != nil {
		return event, nil
	}
	changed := false
	for index, raw := range content {
		var header struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &header) != nil || !messagemedia.IsMediaBlock(header.Type) {
			continue
		}
		block, err := messagemedia.ParseBlock(raw)
		if err != nil || !block.Stored() {
			continue
		}
		replacement, err := s.attachmentReference(ctx, event, index, block)
		if err != nil {
			return db.SessionEvent{}, fmt.Errorf("store message attachment %d of event %s: %w", index, event.ExternalID, err)
		}
		content[index] = replacement
		changed = true
	}
	if !changed {
		return event, nil
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return db.SessionEvent{}, err
	}
	fields["content"] = encoded
	payload, err := json.Marshal(fields)
	if err != nil {
		return db.SessionEvent{}, err
	}
	event.Payload = payload
	return event, nil
}

func (s *Service) attachmentReference(ctx context.Context, event db.SessionEvent, index int, block messagemedia.Block) (json.RawMessage, error) {
	var source *db.FileRecord
	var data []byte
	mediaType := block.InlineMediaType()
	if block.Source.Type == messagemedia.SourceFile {
		stored, found, err := s.storedAttachment(ctx, event, index, block)
		if err != nil || found {
			return stored, err
		}
		file, err := s.db.GetFile(ctx, event.WorkspaceUUID, block.Source.FileID)
		if errors.Is(err, db.ErrNotFound) {
			return unavailableAttachment(block, "the referenced file was not found")
		}
		if err != nil {
			return nil, err
		}
		source = &file
		mediaType = file.MimeType
	} else {
		inline, err := block.InlineContent()
		if err != nil {
			return unavailableAttachment(block, err.Error())
		}
		data = inline
	}
	if !messagemedia.SupportedMediaType(block.Type, mediaType) {
		return unavailableAttachment(block, fmt.Sprintf("media type %q is not supported", mediaType))
	}
	target := filestore.SessionAttachmentTarget{
		WorkspaceUUID:     event.WorkspaceUUID,
		SessionExternalID: event.SessionExternalID,
		Path:              messagemedia.AttachmentPath(event.ExternalID, index, block.Type, mediaType),
	}
	stored, err := s.db.GetSessionAttachmentFileByPath(ctx, event.WorkspaceUUID, event.SessionUUID, target.Path)
	if errors.Is(err, db.ErrNotFound) {
		if s.attachments == nil {
			return unavailableAttachment(block, "attachment storage is not configured")
		}
		if source != nil {
			err = s.attachments.CopyFileToSessionAttachment(ctx, target, *source)
		} else {
			err = s.attachments.PutSessionAttachment(ctx, target, mediaType, bytes.NewReader(data))
		}
		if errors.Is(err, filestore.ErrSessionAttachmentRejected) {
			return unavailableAttachment(block, "the attachment content was rejected")
		}
		if err != nil {
			return nil, err
		}
		stored, err = s.db.GetSessionAttachmentFileByPath(ctx, event.WorkspaceUUID, event.SessionUUID, target.Path)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(block.WithFileReference(stored.ExternalID))
}

func (s *Service) storedAttachment(ctx context.Context, event db.SessionEvent, index int, block messagemedia.Block) (json.RawMessage, bool, error) {
	for _, mediaType := range messagemedia.CandidateMediaTypes(block.Type) {
		path := messagemedia.AttachmentPath(event.ExternalID, index, block.Type, mediaType)
		stored, err := s.db.GetSessionAttachmentFileByPath(ctx, event.WorkspaceUUID, event.SessionUUID, path)
		if errors.Is(err, db.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		reference, err := json.Marshal(block.WithFileReference(stored.ExternalID))
		return reference, true, err
	}
	return nil, false, nil
}

func unavailableAttachment(block messagemedia.Block, reason string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{
		"type": "text",
		"text": fmt.Sprintf("[The user attached a %s that is unavailable: %s.]", block.Type, reason),
	})
}
