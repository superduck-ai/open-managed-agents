package db

import (
	"context"
	"github.com/superduck-ai/yourbatis"
)

// RestoreTranscriptEventsBatch restores original identities without changing the append watermark.
func (d *DB) RestoreTranscriptEventsBatch(ctx context.Context, archive TranscriptArchive, events []CodeSessionInternalEvent) error {
	if len(events) == 0 || len(events) > 500 {
		return ErrLimitExceeded
	}
	return d.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		mapper := NewTranscriptArchiveMapper(tx)
		_, found, err := mapper.LockAttached(ctx, archive.Scope(), archive.UUID)
		if err != nil {
			return err
		}
		if !found {
			return ErrInvalidState
		}
		for _, event := range events {
			if event.OrganizationUUID != archive.OrganizationUUID || event.WorkspaceUUID != archive.WorkspaceUUID || event.CodeSessionUUID != archive.CodeSessionUUID {
				return ErrInvalidState
			}
			covered, err := mapper.HasAttachedCovering(ctx, archive.Scope(), archive.UUID, event.SequenceNum)
			if err != nil {
				return err
			}
			if !covered {
				return ErrInvalidState
			}
			count, err := NewCodeSessionInternalEventMapper(tx).RestoreArchived(ctx, event)
			if err != nil {
				return err
			}
			if count == 1 {
				if err := attachEventPayloadBlob(ctx, tx, event.WorkspaceUUID, event.PayloadBlobUUID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
