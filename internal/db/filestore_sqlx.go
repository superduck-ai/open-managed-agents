package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type filestoreFilesystemRow struct {
	UUID                uuid.UUID     `db:"uuid"`
	ExternalID          string        `db:"external_id"`
	OrganizationUUID    uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID       uuid.UUID     `db:"workspace_uuid"`
	SessionUUID         uuid.UUID     `db:"session_uuid"`
	CodeSessionUUID     uuid.NullUUID `db:"code_session_uuid"`
	CreatedByAPIKeyUUID uuid.NullUUID `db:"created_by_api_key_uuid"`
	CreatedAt           time.Time     `db:"created_at"`
	UpdatedAt           time.Time     `db:"updated_at"`
	DeletedAt           *time.Time    `db:"deleted_at"`
}

type filestoreTokenScopeRow struct {
	OrganizationUUID     uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID        uuid.UUID `db:"workspace_uuid"`
	WorkspaceExternalID  string    `db:"workspace_external_id"`
	AccountUUID          uuid.UUID `db:"account_uuid"`
	AccountExternalID    string    `db:"account_external_id"`
	FilesystemUUID       uuid.UUID `db:"filesystem_uuid"`
	FilesystemExternalID string    `db:"filesystem_external_id"`
	OrgTaintsJSON        []byte    `db:"org_taints_json"`
	WorkspaceCMEKEnabled bool      `db:"workspace_cmek_enabled"`
}

type sessionResourceFileRow struct {
	ID                    int64         `db:"id"`
	UUID                  uuid.UUID     `db:"uuid"`
	ExternalID            string        `db:"external_id"`
	OrganizationUUID      uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID         uuid.UUID     `db:"workspace_uuid"`
	SessionUUID           uuid.UUID     `db:"session_uuid"`
	Kind                  string        `db:"kind"`
	Path                  string        `db:"path"`
	ParentPath            *string       `db:"parent_path"`
	SizeBytes             *int64        `db:"size_bytes"`
	MediaType             *string       `db:"media_type"`
	DetectedMimeType      *string       `db:"detected_mime_type"`
	Metadata              []byte        `db:"metadata"`
	AuthorizationMetadata []byte        `db:"authorization_metadata"`
	TagsJSON              string        `db:"tags_json"`
	Downloadable          bool          `db:"downloadable"`
	MD5                   *string       `db:"md5"`
	SHA256                *string       `db:"sha256"`
	S3Bucket              *string       `db:"s3_bucket"`
	S3Key                 *string       `db:"s3_key"`
	S3ETag                *string       `db:"s3_etag"`
	S3VersionID           *string       `db:"s3_version_id"`
	ExpiresAt             *time.Time    `db:"expires_at"`
	SourceFileUUID        uuid.NullUUID `db:"source_file_uuid"`
	CreatedAt             time.Time     `db:"created_at"`
	UpdatedAt             time.Time     `db:"updated_at"`
	DeletedAt             *time.Time    `db:"deleted_at"`
}

func getFilestoreFilesystemByUUIDSQLX(ctx context.Context, database sqlxNamedQueryer, workspaceUUID, filesystemUUID string) (FilestoreFilesystem, error) {
	return getFilestoreFilesystemSQLX(ctx, database, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and uuid = :filesystem_uuid
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":  dbUUID(workspaceUUID),
		"filesystem_uuid": dbUUID(filesystemUUID),
	})
}

func getFilestoreFilesystemSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (FilestoreFilesystem, error) {
	var row filestoreFilesystemRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreFilesystem{}, ErrNotFound
	}
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	return row.filesystem()
}

func getFilestoreTokenScopeSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (FilestoreTokenScope, error) {
	var row filestoreTokenScopeRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreTokenScope{}, ErrNotFound
	}
	if err != nil {
		return FilestoreTokenScope{}, err
	}
	return row.scope()
}

func getActiveSessionResourceFileSQLX(ctx context.Context, database sqlxNamedQueryer, filesystem FilestoreFilesystem, entryPath string) (SessionResourceFile, error) {
	return getSessionResourceFileSQLX(ctx, database, sessionResourceFileSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and path = :entry_path
			and deleted_at is null
			and (expires_at is null or expires_at > now())
	`, map[string]any{
		"workspace_uuid": dbUUID(filesystem.WorkspaceUUID),
		"session_uuid":   dbUUID(filesystem.SessionUUID),
		"entry_path":     entryPath,
	})
}

func getSessionResourceFileSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (SessionResourceFile, error) {
	var row sessionResourceFileRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionResourceFile{}, ErrNotFound
	}
	if err != nil {
		return SessionResourceFile{}, err
	}
	return row.entry()
}

func insertFilestoreObjectCleanupJobSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	input EnqueueFilestoreObjectCleanupJobInput,
) (FilestoreObjectCleanupJob, error) {
	var job FilestoreObjectCleanupJob
	err := namedGetContext(ctx, database, &job, `
		with inserted_job as (
			insert into jobs (external_id, workspace_uuid, type, status, payload, run_after)
			values (
				concat('job_', replace(cast(gen_random_uuid() as text), '-', '')),
				:workspace_uuid, :job_type, 'pending',
				jsonb_build_object(
					'workspace_uuid', cast(:workspace_uuid as text),
					'filesystem_uuid', cast(:filesystem_uuid as text),
					'entry_external_id', cast(:entry_external_id as text),
					'bucket', cast(:bucket as text),
					'key', cast(:key as text),
					'etag', cast(:etag as text),
					'version_id', cast(:version_id as text),
					'reason', cast(:reason as text)
				),
				:run_after
			)
			returning *
		)
		select `+filestoreFilesystemCleanupJobColumns("j", "fs")+`
		from inserted_job j
		join filestore_filesystems fs
			on cast(fs.uuid as text) = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = j.workspace_uuid
	`, map[string]any{
		"workspace_uuid":    input.WorkspaceUUID,
		"job_type":          filestoreCleanupJobType,
		"filesystem_uuid":   input.FilesystemUUID,
		"entry_external_id": input.EntryExternalID,
		"bucket":            input.Bucket,
		"key":               input.Key,
		"etag":              input.ETag,
		"version_id":        input.VersionID,
		"reason":            input.Reason,
		"run_after":         input.RunAfter,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	if err != nil {
		return FilestoreObjectCleanupJob{}, err
	}
	return job, nil
}

func (row filestoreFilesystemRow) filesystem() (FilestoreFilesystem, error) {
	if row.SessionUUID == uuid.Nil {
		return FilestoreFilesystem{}, ErrNotFound
	}
	return FilestoreFilesystem{
		UUID:                row.UUID.String(),
		ExternalID:          row.ExternalID,
		OrganizationUUID:    row.OrganizationUUID.String(),
		WorkspaceUUID:       row.WorkspaceUUID.String(),
		SessionUUID:         row.SessionUUID.String(),
		CodeSessionUUID:     nullableUUIDString(row.CodeSessionUUID),
		CreatedByAPIKeyUUID: nullableUUIDString(row.CreatedByAPIKeyUUID),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		DeletedAt:           row.DeletedAt,
	}, nil
}

func (row filestoreTokenScopeRow) scope() (FilestoreTokenScope, error) {
	var orgTaints []string
	if err := json.Unmarshal(row.OrgTaintsJSON, &orgTaints); err != nil {
		return FilestoreTokenScope{}, fmt.Errorf("decode Filestore organization taints: %w", err)
	}
	if orgTaints == nil {
		orgTaints = []string{}
	}
	return FilestoreTokenScope{
		OrganizationUUID:     row.OrganizationUUID.String(),
		WorkspaceUUID:        row.WorkspaceUUID.String(),
		WorkspaceExternalID:  row.WorkspaceExternalID,
		AccountUUID:          row.AccountUUID.String(),
		AccountExternalID:    row.AccountExternalID,
		FilesystemUUID:       row.FilesystemUUID.String(),
		FilesystemExternalID: row.FilesystemExternalID,
		OrgTaints:            orgTaints,
		WorkspaceCMEKEnabled: row.WorkspaceCMEKEnabled,
	}, nil
}

func (row sessionResourceFileRow) entry() (SessionResourceFile, error) {
	var tags []string
	if err := json.Unmarshal([]byte(row.TagsJSON), &tags); err != nil {
		return SessionResourceFile{}, fmt.Errorf("decode filestore resource tags: %w", err)
	}
	if tags == nil {
		tags = []string{}
	}
	return SessionResourceFile{
		ID:                    row.ID,
		UUID:                  row.UUID.String(),
		ExternalID:            row.ExternalID,
		OrganizationUUID:      row.OrganizationUUID.String(),
		WorkspaceUUID:         row.WorkspaceUUID.String(),
		SessionUUID:           row.SessionUUID.String(),
		Kind:                  row.Kind,
		Path:                  row.Path,
		ParentPath:            row.ParentPath,
		SizeBytes:             row.SizeBytes,
		MediaType:             row.MediaType,
		DetectedMimeType:      row.DetectedMimeType,
		Metadata:              copyRaw(row.Metadata),
		AuthorizationMetadata: copyRaw(row.AuthorizationMetadata),
		Tags:                  tags,
		Downloadable:          row.Downloadable,
		MD5:                   row.MD5,
		SHA256:                row.SHA256,
		S3Bucket:              row.S3Bucket,
		S3Key:                 row.S3Key,
		S3ETag:                row.S3ETag,
		S3VersionID:           row.S3VersionID,
		ExpiresAt:             row.ExpiresAt,
		// SourceFileUUID 只标识公开 Input Resource 引用的 Source File。
		SourceFileUUID: nullableUUIDString(row.SourceFileUUID),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		DeletedAt:      row.DeletedAt,
	}, nil
}

func sessionResourceFilesFromSQLXRows(rows []sessionResourceFileRow) ([]SessionResourceFile, error) {
	entries := make([]SessionResourceFile, 0, len(rows))
	for _, row := range rows {
		entry, err := row.entry()
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
