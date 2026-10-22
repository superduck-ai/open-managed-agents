package evaluations

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/google/uuid"
)

const caseFileFallbackName = "file"

var (
	errCaseFileIDMissing = errors.New("file id is missing")
	errCaseFileNotFound  = errors.New("file was not found")
	errCaseFileTooLarge  = errors.New("file exceeds maximum size")
	errCaseFileBucket    = errors.New("file is not in the workspace store")
	errCaseFileStorage   = errors.New("workspace storage limit exceeded")
	errCaseFileCopy      = errors.New("could not copy file")
)

type CaseFileCopier interface {
	CopyCaseFile(ctx context.Context, workspaceUUID string, source sessionCaseFileRef) (sessionCaseFileRef, error)
}

type caseFileRecords interface {
	GetFile(ctx context.Context, workspaceUUID, fileExternalID string) (db.FileRecord, error)
	CreateFileIfWithinLimit(ctx context.Context, file db.FileRecord, workspaceStorageLimitBytes int64) error
}

type caseFileObjects interface {
	Name() string
	Copy(ctx context.Context, sourceKey, destinationKey string) (storage.CopyResult, error)
	Delete(ctx context.Context, key string, opts storage.DeleteOptions) error
}

type WorkspaceFileCopier struct {
	records             caseFileRecords
	objects             caseFileObjects
	maxFileBytes        int64
	workspaceLimitBytes int64
}

func NewWorkspaceFileCopier(records *db.DB, objects storage.ObjectStore, maxFileBytes, workspaceLimitBytes int64) *WorkspaceFileCopier {
	if records == nil || objects == nil {
		return nil
	}
	return &WorkspaceFileCopier{
		records:             records,
		objects:             objects,
		maxFileBytes:        maxFileBytes,
		workspaceLimitBytes: workspaceLimitBytes,
	}
}

func (c *WorkspaceFileCopier) CopyCaseFile(ctx context.Context, workspaceUUID string, source sessionCaseFileRef) (sessionCaseFileRef, error) {
	record, err := c.sourceFile(ctx, workspaceUUID, source.FileID)
	if err != nil {
		return sessionCaseFileRef{}, err
	}
	filename := caseFileObjectName(source.Name, record.Filename)
	fileUUID := uuid.NewString()
	externalID, err := ids.New("file_")
	if err != nil {
		return sessionCaseFileRef{}, err
	}
	key := fmt.Sprintf("workspaces/%s/files/%s/%s", workspaceUUID, fileUUID, filename)
	if _, err = c.objects.Copy(ctx, record.S3Key, key); err != nil {
		return sessionCaseFileRef{}, fmt.Errorf("%w: %w", errCaseFileCopy, err)
	}
	copied := db.FileRecord{
		UUID:          fileUUID,
		ExternalID:    externalID,
		WorkspaceUUID: workspaceUUID,
		Filename:      filename,
		MimeType:      caseFileMimeType(record.MimeType),
		SizeBytes:     record.SizeBytes,
		SHA256:        record.SHA256,
		S3Bucket:      c.objects.Name(),
		S3Key:         key,
		Downloadable:  record.Downloadable,
		CreatedAt:     time.Now().UTC(),
	}
	if err = c.records.CreateFileIfWithinLimit(ctx, copied, c.workspaceLimitBytes); err != nil {
		_ = c.objects.Delete(ctx, key, storage.DeleteOptions{})
		if errors.Is(err, db.ErrStorageLimitExceeded) {
			return sessionCaseFileRef{}, errCaseFileStorage
		}
		return sessionCaseFileRef{}, fmt.Errorf("%w: %w", errCaseFileCopy, err)
	}
	name := source.Name
	if name == "" {
		name = filename
	}
	return sessionCaseFileRef{Name: name, FileID: externalID}, nil
}

func (c *WorkspaceFileCopier) sourceFile(ctx context.Context, workspaceUUID, fileID string) (db.FileRecord, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return db.FileRecord{}, errCaseFileIDMissing
	}
	record, err := c.records.GetFile(ctx, workspaceUUID, fileID)
	if errors.Is(err, db.ErrNotFound) {
		return db.FileRecord{}, errCaseFileNotFound
	}
	if err != nil {
		return db.FileRecord{}, fmt.Errorf("%w: %w", errCaseFileCopy, err)
	}
	if record.SizeBytes < 0 || (c.maxFileBytes > 0 && record.SizeBytes > c.maxFileBytes) {
		return db.FileRecord{}, errCaseFileTooLarge
	}
	if record.S3Bucket == "" || record.S3Key == "" || record.S3Bucket != c.objects.Name() {
		return db.FileRecord{}, errCaseFileBucket
	}
	return record, nil
}

func caseFileObjectName(displayName, fallback string) string {
	base := path.Base(strings.TrimSpace(displayName))
	if base == "." || base == "/" || base == "" {
		base = path.Base(strings.TrimSpace(fallback))
	}
	sanitized := sanitizeCaseFileName(base)
	if sanitized == "" {
		return caseFileFallbackName
	}
	return sanitized
}

func sanitizeCaseFileName(filename string) string {
	var builder strings.Builder
	for _, r := range filename {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func caseFileMimeType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "application/octet-stream"
	}
	return value
}

func caseFileSkipReason(err error) string {
	switch {
	case errors.Is(err, errCaseFileIDMissing):
		return errCaseFileIDMissing.Error()
	case errors.Is(err, errCaseFileNotFound):
		return errCaseFileNotFound.Error()
	case errors.Is(err, errCaseFileTooLarge):
		return errCaseFileTooLarge.Error()
	case errors.Is(err, errCaseFileBucket):
		return errCaseFileBucket.Error()
	case errors.Is(err, errCaseFileStorage):
		return errCaseFileStorage.Error()
	default:
		return errCaseFileCopy.Error()
	}
}
