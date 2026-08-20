package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper FileMapper -sql ./file_mapper.xml -out ./file_mapper.sqlmap.gen.go -dialect postgres

type FileMapper interface {
	InsertFile(ctx context.Context, params fileMapperRecordParams) error
	GetFile(ctx context.Context, workspaceUUID, fileExternalID string) (fileRecordRow, error)
	GetFileByUUID(ctx context.Context, workspaceUUID, fileUUID string) (fileRecordRow, error)
	GetFileByUUIDInOrganization(ctx context.Context, organizationUUID, fileUUID string) (fileRecordRow, error)
	ListFilesByUUIDs(ctx context.Context, params fileMapperFileUUIDsParams) ([]fileRecordRow, error)
	ListFiles(ctx context.Context, params fileMapperListParams) ([]fileRecordRow, error)
	ListSessionFiles(ctx context.Context, params fileMapperListParams) ([]fileRecordRow, error)
	FindPageCursor(ctx context.Context, params fileMapperListParams) (filePageCursorRow, bool, error)
	FindSessionPageCursor(ctx context.Context, params fileMapperListParams) (filePageCursorRow, bool, error)
	ListFilesPage(ctx context.Context, params fileMapperListParams) ([]fileRecordRow, error)
	ListSessionFilesPage(ctx context.Context, params fileMapperListParams) ([]fileRecordRow, error)
	GetFileForDelete(ctx context.Context, workspaceUUID, fileUUID string) (fileRecordRow, error)
	GetFileForShare(ctx context.Context, workspaceUUID, fileExternalID string) (fileRecordRow, error)
	HasActiveReference(ctx context.Context, workspaceUUID, fileUUID string) (bool, error)
	SoftDeleteFile(ctx context.Context, workspaceUUID, fileUUID string) error
	UpdateOwnedFile(ctx context.Context, params sessionResourceFileWriteParams) error
	RetireOwnedFile(ctx context.Context, params sessionResourceRetireParams) error
	RetireOwnedFilesInSubtree(ctx context.Context, params sessionResourceSubtreeParams) error
	RetireSkillArchiveFiles(ctx context.Context, params sessionSkillArchiveRetireParams) error
	InsertSkillArchiveFile(ctx context.Context, params sessionSkillArchiveInsertParams) error

	EnqueueObjectCleanupJob(ctx context.Context, workspaceUUID string, payload []byte) error
	LeaseObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]objectCleanupJobRow, error)
	CompleteObjectCleanupJob(ctx context.Context, jobUUID string) error
	FailObjectCleanupJob(ctx context.Context, params objectCleanupJobFailureParams) error
}

type fileMapperRecordParams struct {
	FileUUID            string
	FileExternalID      string
	WorkspaceUUID       string
	Filename            string
	MimeType            string
	SizeBytes           int64
	SHA256              string
	S3Bucket            string
	S3Key               string
	Downloadable        bool
	ScopeType           *string
	ScopeID             *string
	CreatedByAPIKeyUUID string
	CreatedAt           time.Time
}

type fileMapperListParams struct {
	WorkspaceUUID    string
	ScopeID          string
	CursorExternalID string
	CursorUUID       string
	CursorCreatedAt  time.Time
	Limit            int
	SessionScope     bool
	HasScope         bool
	HasCursor        bool
	Before           bool
}

type fileMapperFileUUIDsParams struct {
	WorkspaceUUID string
	FileUUIDs     []string
}

type objectCleanupJobFailureParams struct {
	JobUUID  string
	Status   string
	RunAfter time.Time
	Attempts int
	Reason   string
}

type fileRecordRow struct {
	UUID                string    `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	WorkspaceUUID       string    `db:"workspace_uuid"`
	Filename            string    `db:"filename"`
	MimeType            string    `db:"mime_type"`
	SizeBytes           int64     `db:"size_bytes"`
	SHA256              string    `db:"sha256"`
	S3Bucket            string    `db:"s3_bucket"`
	S3Key               string    `db:"s3_key"`
	Downloadable        bool      `db:"downloadable"`
	ScopeType           *string   `db:"scope_type"`
	ScopeID             *string   `db:"scope_id"`
	CreatedByAPIKeyUUID string    `db:"created_by_api_key_uuid"`
	CreatedAt           time.Time `db:"created_at"`
}

type filePageCursorRow struct {
	UUID      string    `db:"uuid"`
	CreatedAt time.Time `db:"created_at"`
}

type objectCleanupJobRow struct {
	UUID           string `db:"uuid"`
	ExternalID     string `db:"external_id"`
	WorkspaceUUID  string `db:"workspace_uuid"`
	Bucket         string `db:"bucket"`
	Key            string `db:"object_key"`
	FileExternalID string `db:"file_external_id"`
	Attempts       int    `db:"attempts"`
}

func fileMapperRecordParameters(file FileRecord) fileMapperRecordParams {
	return fileMapperRecordParams{
		FileUUID:            file.UUID,
		FileExternalID:      file.ExternalID,
		WorkspaceUUID:       file.WorkspaceUUID,
		Filename:            file.Filename,
		MimeType:            file.MimeType,
		SizeBytes:           file.SizeBytes,
		SHA256:              file.SHA256,
		S3Bucket:            file.S3Bucket,
		S3Key:               file.S3Key,
		Downloadable:        file.Downloadable,
		ScopeType:           file.ScopeType,
		ScopeID:             file.ScopeID,
		CreatedByAPIKeyUUID: file.CreatedByAPIKeyUUID,
		CreatedAt:           file.CreatedAt,
	}
}

func newFileMapperListParams(workspaceUUID string, scopeID string) fileMapperListParams {
	return fileMapperListParams{
		WorkspaceUUID: workspaceUUID,
		ScopeID:       scopeID,
		SessionScope:  isSessionFilesScope(scopeID),
		HasScope:      scopeID != "",
	}
}

func fileRecordFromMapperRow(row fileRecordRow, err error) (FileRecord, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return FileRecord{}, ErrNotFound
	}
	if err != nil {
		return FileRecord{}, err
	}
	return row.record(), nil
}

func fileRecordsFromMapperRows(rows []fileRecordRow, err error) ([]FileRecord, error) {
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, nil
	}
	files := make([]FileRecord, 0, len(rows))
	for _, row := range rows {
		files = append(files, row.record())
	}
	return files, nil
}

func (r fileRecordRow) record() FileRecord {
	return FileRecord{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		WorkspaceUUID:       r.WorkspaceUUID,
		Filename:            r.Filename,
		MimeType:            r.MimeType,
		SizeBytes:           r.SizeBytes,
		SHA256:              r.SHA256,
		S3Bucket:            r.S3Bucket,
		S3Key:               r.S3Key,
		Downloadable:        r.Downloadable,
		ScopeType:           r.ScopeType,
		ScopeID:             r.ScopeID,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		CreatedAt:           r.CreatedAt,
	}
}

func (r objectCleanupJobRow) job() ObjectCleanupJob {
	return ObjectCleanupJob{
		UUID:           r.UUID,
		ExternalID:     r.ExternalID,
		WorkspaceUUID:  r.WorkspaceUUID,
		Bucket:         r.Bucket,
		Key:            r.Key,
		FileExternalID: r.FileExternalID,
		Attempts:       r.Attempts,
	}
}
