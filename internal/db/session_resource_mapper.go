package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionResourceMapper -sql ./session_resource_mapper.xml -out ./session_resource_mapper.sqlmap.gen.go -dialect postgres

type SessionResourceMapper interface {
	Insert(ctx context.Context, params sessionResourceWriteParams) (sessionResourceRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, sessionExternalID, resourceExternalID string) (sessionResourceRow, error)
	List(ctx context.Context, workspaceUUID, sessionExternalID string, maxOutputResources int) ([]sessionResourceRow, error)
	Update(ctx context.Context, params sessionResourceUpdateParams) (sessionResourceRow, error)
	SoftDeleteBySession(ctx context.Context, workspaceUUID, sessionExternalID string) (int64, error)
	CountSessionFileResources(ctx context.Context, workspaceUUID, sessionExternalID, resourceType string) (int, error)
	FindMountConflict(ctx context.Context, params sessionResourcePathParams) (string, bool, error)
	BindSessionFileResource(ctx context.Context, params sessionFileResourceBindingParams) (sessionResourceRow, error)
	GetSessionResourceForMutation(ctx context.Context, workspaceUUID, sessionExternalID, resourceExternalID string) (sessionResourceRow, error)
	SoftDeleteSessionResource(ctx context.Context, workspaceUUID, sessionExternalID, resourceExternalID string) (int64, error)
	ListEventFileBindings(ctx context.Context, workspaceUUID, sessionExternalID string) ([]sessionEventFileBindingRow, error)

	InsertDirectory(ctx context.Context, params sessionResourceDirectoryInsertParams) (int64, error)
	UpdateResourceFile(ctx context.Context, params sessionResourceFileWriteParams) error
	InsertOwnedFileAndResource(ctx context.Context, params sessionResourceFileWriteParams) (int64, error)
	RetireResource(ctx context.Context, params sessionResourceRetireParams) error
	MoveResourceFile(ctx context.Context, params sessionResourceMoveParams) error
	MaxMovedPathBytes(ctx context.Context, params sessionResourceMoveParams) (int, error)
	SubtreeContainsInput(ctx context.Context, params sessionResourceMoveParams) (bool, error)
	FindMoveConflict(ctx context.Context, params sessionResourceMoveParams) (int64, bool, error)
	MoveResourceSubtree(ctx context.Context, params sessionResourceMoveParams) error
	CountDirectoryChildren(ctx context.Context, params sessionResourceSubtreeParams) (int, error)
	SubtreeContainsMountedInput(ctx context.Context, params sessionResourceSubtreeParams) (bool, error)
	RetireResourceSubtree(ctx context.Context, params sessionResourceSubtreeParams) error

	RetireSkillArchiveResources(ctx context.Context, params sessionSkillArchiveRetireParams) error
	InsertSkillArchiveResource(ctx context.Context, params sessionSkillArchiveInsertParams) error
}

type sessionEventFileBindingRow struct {
	FileExternalID string `db:"file_external_id"`
	Path           string `db:"path"`
	MimeType       string `db:"mime_type"`
}

type sessionResourceRow struct {
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	SessionUUID       string     `db:"session_uuid"`
	SessionExternalID string     `db:"session_external_id"`
	ResourceType      string     `db:"resource_type"`
	Payload           []byte     `db:"payload"`
	SecretPayload     []byte     `db:"secret_payload"`
	Path              *string    `db:"path"`
	FileExternalID    *string    `db:"file_external_id"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	DeletedAt         *time.Time `db:"deleted_at"`
}

type sessionResourceWriteParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionExternalID string
	ResourceType      string
	Payload           []byte
	SecretPayload     []byte
	CreatedAt         time.Time
}

type sessionResourceUpdateParams struct {
	WorkspaceUUID      string
	SessionExternalID  string
	ResourceExternalID string
	Payload            []byte
	SecretPayload      []byte
}

type sessionResourcePathParams struct {
	WorkspaceUUID string
	SessionUUID   string
	EntryPath     string
}

type sessionFileResourceBindingParams struct {
	EntryPath     string
	ParentPath    string
	FileUUID      string
	UpdatedAt     time.Time
	ResourceUUID  string
	WorkspaceUUID string
	SessionUUID   string
}

type sessionResourceDirectoryInsertParams struct {
	ResourceUUID       string
	ResourceExternalID string
	OrganizationUUID   string
	WorkspaceUUID      string
	SessionUUID        string
	EntryPath          string
	ParentPath         string
	Now                time.Time
}

type sessionResourceFileWriteParams struct {
	FileUUID              string
	FileExternalID        string
	ResourceUUID          string
	ResourceExternalID    string
	OrganizationUUID      string
	WorkspaceUUID         string
	SessionUUID           string
	EntryPath             string
	ParentPath            string
	Filename              string
	SizeBytes             int64
	MediaType             string
	DetectedMimeType      any
	Metadata              string
	AuthorizationMetadata string
	Tags                  []string
	Downloadable          bool
	MD5                   string
	SHA256                string
	S3Bucket              string
	S3Key                 string
	S3ETag                any
	S3VersionID           any
	ExpiresAt             *time.Time
	CreatedByAPIKeyUUID   *string
	Now                   time.Time
}

type sessionResourceRetireParams struct {
	ResourceUUID  string
	WorkspaceUUID string
	RetiredAt     time.Time
}

type sessionResourceMoveParams struct {
	WorkspaceUUID         string
	SessionUUID           string
	ResourceUUID          string
	SourcePath            string
	DestinationPath       string
	DestinationParentPath string
	Now                   time.Time
}

type sessionResourceSubtreeParams struct {
	WorkspaceUUID string
	SessionUUID   string
	EntryPath     string
	Now           time.Time
}

type sessionSkillArchiveRetireParams struct {
	WorkspaceUUID string
	SessionUUID   string
	Now           time.Time
}

type sessionSkillArchiveInsertParams struct {
	FileUUID           string
	FileExternalID     string
	ResourceUUID       string
	ResourceExternalID string
	WorkspaceUUID      string
	SessionUUID        string
	EntryPath          string
	Filename           string
	Source             string
	SizeBytes          int64
	SHA256             string
	S3Bucket           string
	S3Key              string
	Now                time.Time
}
