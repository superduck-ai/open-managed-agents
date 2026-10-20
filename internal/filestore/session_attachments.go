package filestore

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type SessionAttachmentTarget struct {
	WorkspaceUUID     string
	SessionExternalID string
	Path              string
}

func (s *Service) PutSessionAttachment(ctx context.Context, target SessionAttachmentTarget, mediaType string, body io.Reader) error {
	filesystem, err := s.db.GetFilestoreFilesystemBySession(ctx, target.WorkspaceUUID, target.SessionExternalID)
	if err != nil {
		return fmt.Errorf("load session filesystem for attachment: %w", err)
	}
	now := s.now().UTC()
	if _, err := s.db.MakeFilestoreDirectory(ctx, db.MakeFilestoreDirectoryInput{
		WorkspaceUUID:  target.WorkspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           filestorepath.Parent(target.Path),
		MakeParents:    true,
		Now:            now,
	}); err != nil {
		return fmt.Errorf("create attachment directory: %w", err)
	}
	md5Hash := md5.New()
	sha256Hash := sha256.New()
	hashedReader := io.TeeReader(limitedUploadReader(body, s.cfg.Storage.MaxFileBytes), io.MultiWriter(md5Hash, sha256Hash))
	mediaType = normalizeMediaType(mediaType)
	principal := Principal{WorkspaceUUID: target.WorkspaceUUID}
	var upload storage.UploadResult
	staged, apiErr := s.stageFilestoreObject(ctx, principal, filesystem, now, func(objectKey string) (objectWriteResult, *apiError) {
		var uploadErr error
		upload, uploadErr = s.store.Upload(ctx, objectKey, hashedReader, storage.UploadOptions{Size: -1, ContentType: mediaType})
		result := objectWriteResult{ETag: upload.ETag, VersionID: upload.VersionID}
		if uploadErr != nil {
			return result, mapBlobstoreError("upload attachment", uploadErr)
		}
		return result, nil
	})
	if apiErr != nil {
		return apiErr
	}
	_, err = s.db.PutFilestoreFile(ctx, db.PutFilestoreFileInput{
		WorkspaceUUID:  target.WorkspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           target.Path,
		Blob: db.FilestoreFileBlob{
			SizeBytes:        upload.Size,
			MediaType:        mediaType,
			DetectedMimeType: mediaType,
			MD5:              hex.EncodeToString(md5Hash.Sum(nil)),
			SHA256:           hex.EncodeToString(sha256Hash.Sum(nil)),
			S3Bucket:         s.store.Name(),
			S3Key:            staged.Key,
			S3ETag:           upload.ETag,
			S3VersionID:      upload.VersionID,
		},
		OrphanCleanupJobExternalID: staged.CleanupJob.ExternalID,
		Now:                        now,
	})
	if errors.Is(err, db.ErrFilestorePathExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("record attachment: %w", err)
	}
	return nil
}

func (s *Service) CopyFileToSessionAttachment(ctx context.Context, target SessionAttachmentTarget, file db.FileRecord) error {
	object, err := s.store.Open(ctx, file.S3Key, nil)
	if err != nil {
		return fmt.Errorf("open attachment source file: %w", err)
	}
	defer object.Body.Close()
	return s.PutSessionAttachment(ctx, target, file.MimeType, object.Body)
}
