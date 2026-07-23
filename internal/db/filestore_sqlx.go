package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type filestoreFilesystemRow struct {
	ID                  int64      `db:"id"`
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	SessionUUID         string     `db:"session_uuid"`
	CodeSessionUUID     *string    `db:"code_session_uuid"`
	CreatedByAPIKeyUUID *string    `db:"created_by_api_key_uuid"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type filestoreTokenScopeRow struct {
	OrganizationID         int64  `db:"organization_id"`
	OrganizationUUID       string `db:"organization_uuid"`
	OrganizationExternalID string `db:"organization_external_id"`
	WorkspaceID            int64  `db:"workspace_id"`
	WorkspaceUUID          string `db:"workspace_uuid"`
	WorkspaceExternalID    string `db:"workspace_external_id"`
	AccountID              int64  `db:"account_id"`
	AccountUUID            string `db:"account_uuid"`
	AccountExternalID      string `db:"account_external_id"`
	FilesystemID           int64  `db:"filesystem_id"`
	FilesystemUUID         string `db:"filesystem_uuid"`
	FilesystemExternalID   string `db:"filesystem_external_id"`
	OrgTaintsJSON          []byte `db:"org_taints_json"`
	WorkspaceCMEKEnabled   bool   `db:"workspace_cmek_enabled"`
}

type filestoreEntryRow struct {
	ID                       int64      `db:"id"`
	UUID                     string     `db:"uuid"`
	ExternalID               string     `db:"external_id"`
	OrganizationUUID         string     `db:"organization_uuid"`
	WorkspaceUUID            string     `db:"workspace_uuid"`
	FilesystemUUID           string     `db:"filesystem_uuid"`
	Kind                     string     `db:"kind"`
	Path                     string     `db:"path"`
	ParentPath               *string    `db:"parent_path"`
	SizeBytes                *int64     `db:"size_bytes"`
	MediaType                *string    `db:"media_type"`
	DetectedMimeType         *string    `db:"detected_mime_type"`
	Metadata                 []byte     `db:"metadata"`
	AuthorizationMetadata    []byte     `db:"authorization_metadata"`
	TagsJSON                 string     `db:"tags_json"`
	Downloadable             bool       `db:"downloadable"`
	MD5                      *string    `db:"md5"`
	SHA256                   *string    `db:"sha256"`
	S3Bucket                 *string    `db:"s3_bucket"`
	S3Key                    *string    `db:"s3_key"`
	S3ETag                   *string    `db:"s3_etag"`
	S3VersionID              *string    `db:"s3_version_id"`
	ExpiresAt                *time.Time `db:"expires_at"`
	CreatedByAPIKeyUUID      *string    `db:"created_by_api_key_uuid"`
	CreatedBySessionUUID     *string    `db:"created_by_session_uuid"`
	CreatedByCodeSessionUUID *string    `db:"created_by_code_session_uuid"`
	CreatedAt                time.Time  `db:"created_at"`
	UpdatedAt                time.Time  `db:"updated_at"`
	DeletedAt                *time.Time `db:"deleted_at"`
}

func filestoreFilesystemSelectSQLX() string {
	return `select id, cast(uuid as text) as uuid, external_id,
		cast(organization_uuid as text) as organization_uuid,
		cast(workspace_uuid as text) as workspace_uuid,
		cast(session_uuid as text) as session_uuid,
		cast(code_session_uuid as text) as code_session_uuid,
		cast(created_by_api_key_uuid as text) as created_by_api_key_uuid,
		created_at, updated_at, deleted_at
		from filestore_filesystems`
}

func filestoreEntrySelectSQLX() string {
	return `select id, cast(uuid as text) as uuid, external_id,
		cast(organization_uuid as text) as organization_uuid,
		cast(workspace_uuid as text) as workspace_uuid,
		cast(filesystem_uuid as text) as filesystem_uuid,
		kind, path, parent_path, size_bytes, media_type, detected_mime_type,
		metadata, authorization_metadata,
		cast(coalesce(to_jsonb(tags), cast('[]' as jsonb)) as text) as tags_json,
		downloadable, md5, sha256, s3_bucket, s3_key, s3_etag, s3_version_id,
		expires_at, cast(created_by_api_key_uuid as text) as created_by_api_key_uuid,
		cast(created_by_session_uuid as text) as created_by_session_uuid,
		cast(created_by_code_session_uuid as text) as created_by_code_session_uuid,
		created_at, updated_at, deleted_at
		from filestore_entries`
}

func getFilestoreFilesystemByIDSQLX(ctx context.Context, database *sqlx.DB, workspaceID, filesystemID int64) (FilestoreFilesystem, error) {
	return getFilestoreFilesystemSQLX(ctx, database, filestoreFilesystemSelectSQLX()+`
		where workspace_uuid = (select uuid from workspaces where id = :workspace_id)
			and id = :filesystem_id and deleted_at is null
	`, map[string]any{
		"workspace_id":  workspaceID,
		"filesystem_id": filesystemID,
	})
}

func getFilestoreFilesystemSQLX(ctx context.Context, database *sqlx.DB, query string, arguments map[string]any) (FilestoreFilesystem, error) {
	var row filestoreFilesystemRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreFilesystem{}, ErrNotFound
	}
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	return row.filesystem(), nil
}

func getFilestoreTokenScopeSQLX(ctx context.Context, database *sqlx.DB, query string, arguments map[string]any) (FilestoreTokenScope, error) {
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

func getActiveFilestoreEntrySQLX(ctx context.Context, database *sqlx.DB, filesystem FilestoreFilesystem, entryPath string) (FilestoreEntry, error) {
	var row filestoreEntryRow
	err := namedGetContext(ctx, database, &row, filestoreEntrySelectSQLX()+`
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and path = :entry_path
			and deleted_at is null
			and (expires_at is null or expires_at > now())
	`, map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"entry_path":      entryPath,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreEntry{}, ErrNotFound
	}
	if err != nil {
		return FilestoreEntry{}, err
	}
	return row.entry()
}

func insertFilestoreObjectCleanupJobSQLX(
	ctx context.Context,
	database *sqlx.DB,
	input EnqueueFilestoreObjectCleanupJobInput,
) (FilestoreObjectCleanupJob, error) {
	var job FilestoreObjectCleanupJob
	err := namedGetContext(ctx, database, &job, `
		insert into jobs (external_id, workspace_id, type, status, payload, run_after)
		values (
			concat('job_', replace(cast(gen_random_uuid() as text), '-', '')),
			:workspace_id, :job_type, 'pending',
			jsonb_build_object(
				'filesystem_id', cast(:filesystem_id as bigint),
				'filesystem_external_id', cast(:filesystem_external_id as text),
				'entry_external_id', cast(:entry_external_id as text),
				'bucket', cast(:bucket as text),
				'key', cast(:key as text),
				'etag', cast(:etag as text),
				'version_id', cast(:version_id as text),
				'reason', cast(:reason as text)
			),
			:run_after
		)
		returning `+filestoreCleanupJobColumns("jobs"), map[string]any{
		"workspace_id":           input.WorkspaceID,
		"job_type":               filestoreCleanupJobType,
		"filesystem_id":          input.FilesystemID,
		"filesystem_external_id": input.FilesystemExternalID,
		"entry_external_id":      input.EntryExternalID,
		"bucket":                 input.Bucket,
		"key":                    input.Key,
		"etag":                   input.ETag,
		"version_id":             input.VersionID,
		"reason":                 input.Reason,
		"run_after":              input.RunAfter,
	})
	if err != nil {
		return FilestoreObjectCleanupJob{}, err
	}
	return job, nil
}

func (row filestoreFilesystemRow) filesystem() FilestoreFilesystem {
	return FilestoreFilesystem{
		ID:                  row.ID,
		UUID:                row.UUID,
		ExternalID:          row.ExternalID,
		OrganizationUUID:    row.OrganizationUUID,
		WorkspaceUUID:       row.WorkspaceUUID,
		SessionUUID:         row.SessionUUID,
		CodeSessionUUID:     row.CodeSessionUUID,
		CreatedByAPIKeyUUID: row.CreatedByAPIKeyUUID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		DeletedAt:           row.DeletedAt,
	}
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
		OrganizationID:         row.OrganizationID,
		OrganizationUUID:       row.OrganizationUUID,
		OrganizationExternalID: row.OrganizationExternalID,
		WorkspaceID:            row.WorkspaceID,
		WorkspaceUUID:          row.WorkspaceUUID,
		WorkspaceExternalID:    row.WorkspaceExternalID,
		AccountID:              row.AccountID,
		AccountUUID:            row.AccountUUID,
		AccountExternalID:      row.AccountExternalID,
		FilesystemID:           row.FilesystemID,
		FilesystemUUID:         row.FilesystemUUID,
		FilesystemExternalID:   row.FilesystemExternalID,
		OrgTaints:              orgTaints,
		WorkspaceCMEKEnabled:   row.WorkspaceCMEKEnabled,
	}, nil
}

func (row filestoreEntryRow) entry() (FilestoreEntry, error) {
	var tags []string
	if err := json.Unmarshal([]byte(row.TagsJSON), &tags); err != nil {
		return FilestoreEntry{}, fmt.Errorf("decode filestore entry tags: %w", err)
	}
	if tags == nil {
		tags = []string{}
	}
	return FilestoreEntry{
		ID:                       row.ID,
		UUID:                     row.UUID,
		ExternalID:               row.ExternalID,
		OrganizationUUID:         row.OrganizationUUID,
		WorkspaceUUID:            row.WorkspaceUUID,
		FilesystemUUID:           row.FilesystemUUID,
		Kind:                     row.Kind,
		Path:                     row.Path,
		ParentPath:               row.ParentPath,
		SizeBytes:                row.SizeBytes,
		MediaType:                row.MediaType,
		DetectedMimeType:         row.DetectedMimeType,
		Metadata:                 copyRaw(row.Metadata),
		AuthorizationMetadata:    copyRaw(row.AuthorizationMetadata),
		Tags:                     tags,
		Downloadable:             row.Downloadable,
		MD5:                      row.MD5,
		SHA256:                   row.SHA256,
		S3Bucket:                 row.S3Bucket,
		S3Key:                    row.S3Key,
		S3ETag:                   row.S3ETag,
		S3VersionID:              row.S3VersionID,
		ExpiresAt:                row.ExpiresAt,
		CreatedByAPIKeyUUID:      row.CreatedByAPIKeyUUID,
		CreatedBySessionUUID:     row.CreatedBySessionUUID,
		CreatedByCodeSessionUUID: row.CreatedByCodeSessionUUID,
		CreatedAt:                row.CreatedAt,
		UpdatedAt:                row.UpdatedAt,
		DeletedAt:                row.DeletedAt,
	}, nil
}

func filestoreEntriesFromSQLXRows(rows []filestoreEntryRow) ([]FilestoreEntry, error) {
	entries := make([]FilestoreEntry, 0, len(rows))
	for _, row := range rows {
		entry, err := row.entry()
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
