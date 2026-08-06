package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionResourceFileMapper -sql ./session_resource_file_mapper.xml -out ./session_resource_file_mapper.sqlmap.gen.go -dialect postgres

// SessionResourceFileMapper reads the joined Session Resource/File projection.
// Mutations remain owned by the mapper for their primary table.
type SessionResourceFileMapper interface {
	FindResourceFile(ctx context.Context, params sessionResourcePathParams) (sessionResourceFileRow, bool, error)
	FindActiveResourceFile(ctx context.Context, params sessionResourcePathParams) (sessionResourceFileRow, bool, error)
	GetResourceFileByUUID(ctx context.Context, params sessionResourceIdentityParams) (sessionResourceFileRow, error)
	GetResourceFileForMoveResult(ctx context.Context, params sessionResourceIdentityParams) (sessionResourceFileRow, error)
	GetMovedDirectory(ctx context.Context, params sessionResourceMoveParams) (sessionResourceFileRow, error)
	ListResourceFilesPage(ctx context.Context, params sessionResourceFilePageMapperParams) ([]sessionResourceFileRow, error)
	ListSkillArchiveResources(ctx context.Context, workspaceUUID, filesystemUUID string) ([]sessionResourceFileRow, error)
}

type sessionResourceIdentityParams struct {
	WorkspaceUUID string
	SessionUUID   string
	ResourceUUID  string
}

type sessionResourceFilePageMapperParams struct {
	WorkspaceUUID   string
	SessionUUID     string
	DirectoryPath   string
	DirectoryPrefix string
	CursorUUID      string
	CursorPath      string
	FetchLimit      int
	Recursive       bool
	HasCursor       bool
}

type sessionResourceFileRow struct {
	ID                    int64      `db:"id"`
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	SessionUUID           string     `db:"session_uuid"`
	Kind                  string     `db:"kind"`
	Path                  string     `db:"path"`
	ParentPath            *string    `db:"parent_path"`
	SizeBytes             *int64     `db:"size_bytes"`
	MediaType             *string    `db:"media_type"`
	DetectedMimeType      *string    `db:"detected_mime_type"`
	Metadata              []byte     `db:"metadata"`
	AuthorizationMetadata []byte     `db:"authorization_metadata"`
	TagsJSON              string     `db:"tags_json"`
	Downloadable          bool       `db:"downloadable"`
	MD5                   *string    `db:"md5"`
	SHA256                *string    `db:"sha256"`
	S3Bucket              *string    `db:"s3_bucket"`
	S3Key                 *string    `db:"s3_key"`
	S3ETag                *string    `db:"s3_etag"`
	S3VersionID           *string    `db:"s3_version_id"`
	ExpiresAt             *time.Time `db:"expires_at"`
	FileUUID              *string    `db:"file_uuid"`
	FileOwnership         *string    `db:"file_ownership"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

func (row sessionResourceFileRow) entry() (SessionResourceFile, error) {
	var tags []string
	if err := json.Unmarshal([]byte(row.TagsJSON), &tags); err != nil {
		return SessionResourceFile{}, fmt.Errorf("decode filestore resource tags: %w", err)
	}
	if tags == nil {
		tags = []string{}
	}
	fileOwnership, err := sessionResourceFileOwnershipFromRow(row.FileOwnership)
	if err != nil {
		return SessionResourceFile{}, err
	}
	return SessionResourceFile{
		ID:                    row.ID,
		UUID:                  row.UUID,
		ExternalID:            row.ExternalID,
		OrganizationUUID:      row.OrganizationUUID,
		WorkspaceUUID:         row.WorkspaceUUID,
		SessionUUID:           row.SessionUUID,
		Kind:                  row.Kind,
		Path:                  row.Path,
		ParentPath:            row.ParentPath,
		FileUUID:              row.FileUUID,
		FileOwnership:         fileOwnership,
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
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		DeletedAt:             row.DeletedAt,
	}, nil
}

func sessionResourceFileOwnershipFromRow(value *string) (SessionResourceFileOwnership, error) {
	if value == nil {
		return "", nil
	}
	ownership := SessionResourceFileOwnership(*value)
	switch ownership {
	case SessionResourceFileOwnershipReferenced, SessionResourceFileOwnershipOwned:
		return ownership, nil
	default:
		return "", fmt.Errorf("decode Session Resource File ownership %q", *value)
	}
}

func sessionResourceFilesFromMapperRows(rows []sessionResourceFileRow) ([]SessionResourceFile, error) {
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
