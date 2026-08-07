package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EnvironmentWorkMapper -sql ./environment_work_mapper.xml -out ./environment_work_mapper.sqlmap.gen.go -dialect postgres

type environmentWorkMapperRow struct {
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	EnvironmentUUID       string     `db:"environment_uuid"`
	EnvironmentExternalID string     `db:"environment_external_id"`
	Data                  []byte     `db:"data"`
	Metadata              []byte     `db:"metadata"`
	Secret                *string    `db:"secret"`
	State                 string     `db:"state"`
	ClaimedByWorkerID     *string    `db:"claimed_by_worker_id"`
	ClaimExpiresAt        *time.Time `db:"claim_expires_at"`
	AcknowledgedAt        *time.Time `db:"acknowledged_at"`
	StartedAt             *time.Time `db:"started_at"`
	LatestHeartbeatAt     *time.Time `db:"latest_heartbeat_at"`
	HeartbeatTTLSeconds   *int       `db:"heartbeat_ttl_seconds"`
	StopRequestedAt       *time.Time `db:"stop_requested_at"`
	StoppedAt             *time.Time `db:"stopped_at"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

type environmentWorkWriteParams struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	EnvironmentUUID       string
	EnvironmentExternalID string
	Data                  []byte
	Metadata              []byte
	Secret                *string
	State                 string
	CreatedAt             time.Time
}

type environmentWorkPageMapperParams struct {
	WorkspaceUUID         string
	EnvironmentExternalID string
	FetchLimit            int
	Cursor                *EnvironmentWorkPageCursor
}

type environmentWorkMetadataParams struct {
	WorkspaceUUID         string
	EnvironmentExternalID string
	WorkExternalID        string
	Metadata              []byte
}

type environmentWorkMetadataPatchParams struct {
	OrganizationUUID      string
	WorkspaceUUID         string
	EnvironmentUUID       string
	EnvironmentExternalID string
	WorkExternalID        string
	MetadataPatch         []byte
}

type environmentWorkHeartbeatParams struct {
	WorkspaceUUID         string
	EnvironmentExternalID string
	WorkUUID              string
	State                 string
	TTLSeconds            int
}

type environmentWorkStopParams struct {
	WorkspaceUUID         string
	EnvironmentExternalID string
	WorkExternalID        string
	State                 string
}

type environmentWorkStatsMapperRow struct {
	Depth          int        `db:"depth"`
	Pending        int        `db:"pending"`
	OldestQueuedAt *time.Time `db:"oldest_queued_at"`
	WorkersPolling int        `db:"workers_polling"`
}

type EnvironmentWorkMapper interface {
	Insert(ctx context.Context, params environmentWorkWriteParams) (environmentWorkMapperRow, error)
	CountActive(ctx context.Context, workspaceUUID, environmentUUID string) (int, error)
	FindByExternalID(ctx context.Context, workspaceUUID, environmentExternalID, workExternalID string) (environmentWorkMapperRow, error)
	FindLatestByData(ctx context.Context, workspaceUUID, environmentExternalID, dataType, dataID string) (environmentWorkMapperRow, error)
	ListPage(ctx context.Context, params environmentWorkPageMapperParams) ([]environmentWorkMapperRow, error)
	ClaimForEnvironment(ctx context.Context, workspaceUUID, environmentExternalID string, workerID *string, claimExpiresAt time.Time) (environmentWorkMapperRow, error)
	ClaimNext(ctx context.Context, workerID *string, claimExpiresAt time.Time, includeSessionWork bool) (environmentWorkMapperRow, error)
	LockByExternalID(ctx context.Context, workspaceUUID, environmentExternalID, workExternalID string) (environmentWorkMapperRow, error)
	AckByExternalID(ctx context.Context, workspaceUUID, environmentExternalID, workExternalID string) (environmentWorkMapperRow, error)
	UpdateMetadata(ctx context.Context, params environmentWorkMetadataParams) (environmentWorkMapperRow, error)
	MergeMetadata(ctx context.Context, params environmentWorkMetadataPatchParams) (int64, error)
	Heartbeat(ctx context.Context, params environmentWorkHeartbeatParams) (environmentWorkMapperRow, error)
	Stop(ctx context.Context, params environmentWorkStopParams) (environmentWorkMapperRow, error)
	StopForDeletedSession(ctx context.Context, workspaceUUID, environmentExternalID, sessionExternalID string) (int64, error)
	Stats(ctx context.Context, workspaceUUID, environmentExternalID string) (environmentWorkStatsMapperRow, error)
}
