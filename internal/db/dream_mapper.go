package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DreamMapper -sql ./dream_mapper.xml -out ./dream_mapper.sqlmap.gen.go -dialect postgres

type dreamRow struct {
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID   *string    `db:"created_by_api_key_uuid"`
	RuntimeUserUUID       *string    `db:"runtime_user_uuid"`
	Status                string     `db:"status"`
	Model                 string     `db:"model"`
	Instructions          *string    `db:"instructions"`
	Inputs                []byte     `db:"inputs"`
	Outputs               []byte     `db:"outputs"`
	Error                 []byte     `db:"error"`
	Usage                 []byte     `db:"usage"`
	OutputMemoryStoreUUID *string    `db:"output_memory_store_uuid"`
	InternalSessionUUID   *string    `db:"internal_session_uuid"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	StartedAt             *time.Time `db:"started_at"`
	EndedAt               *time.Time `db:"ended_at"`
	ArchivedAt            *time.Time `db:"archived_at"`
	ExecutionState        string     `db:"execution_state"`
	AttemptCount          int        `db:"attempt_count"`
	NextAttemptAt         *time.Time `db:"next_attempt_at"`
	ClaimedByWorkerID     *string    `db:"claimed_by_worker_id"`
	ClaimExpiresAt        *time.Time `db:"claim_expires_at"`
	LastError             *string    `db:"last_error"`
}

type insertDreamParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID *string
	RuntimeUserUUID     *string
	Status              string
	Model               string
	Instructions        *string
	Inputs              []byte
	CreatedAt           time.Time
}
type listDreamsParams struct {
	WorkspaceUUID   string
	Limit           int
	IncludeArchived bool
	HasCursor       bool
	CursorCreatedAt time.Time
	CursorUUID      string
}

type recordDreamResourcesParams struct {
	WorkspaceUUID         string
	ExternalID            string
	WorkerID              string
	OutputMemoryStoreUUID string
	OutputMemoryStoreID   string
	InternalSessionUUID   string
	InternalSessionID     string
	Now                   time.Time
}

type markDreamRunningParams struct {
	WorkspaceUUID       string
	ExternalID          string
	WorkerID            string
	OutputMemoryStoreID string
	InternalSessionID   string
	Now                 time.Time
}

type claimPendingDreamParams struct {
	WorkerID       string
	ClaimExpiresAt time.Time
}

type schedulePendingDreamRetryParams struct {
	WorkspaceUUID string
	ExternalID    string
	WorkerID      string
	NextAttemptAt time.Time
	LastError     string
	Now           time.Time
}

type renewPendingDreamClaimParams struct {
	WorkspaceUUID  string
	ExternalID     string
	WorkerID       string
	ClaimExpiresAt time.Time
	Now            time.Time
}

type markDreamTerminalParams struct {
	WorkspaceUUID string
	ExternalID    string
	Status        string
	Error         []byte
	Usage         []byte
	Now           time.Time
}

type updateRunningDreamUsageParams struct {
	WorkspaceUUID string
	ExternalID    string
	Usage         []byte
	Now           time.Time
}

type markClaimedDreamFailedParams struct {
	WorkspaceUUID string
	ExternalID    string
	WorkerID      string
	Error         []byte
	Usage         []byte
	Now           time.Time
}

type DreamMapper interface {
	Insert(ctx context.Context, params insertDreamParams) (dreamRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (dreamRow, error)
	FindByInternalSessionUUID(ctx context.Context, workspaceUUID, internalSessionUUID string) (dreamRow, error)
	ListPage(ctx context.Context, params listDreamsParams) ([]dreamRow, error)
	ListByStatus(ctx context.Context, status string, limit int) ([]dreamRow, error)
	ListStoppedWithActiveSession(ctx context.Context, limit int) ([]dreamRow, error)
	ListTerminalAwaitingRuntimeReclaim(ctx context.Context, limit int) ([]dreamRow, error)
	ClaimNextPending(ctx context.Context, params claimPendingDreamParams) (dreamRow, error)
	RenewPendingClaim(ctx context.Context, params renewPendingDreamClaimParams) (int64, error)
	SchedulePendingRetry(ctx context.Context, params schedulePendingDreamRetryParams) (dreamRow, error)
	ListAwaitingInternalSessionArchive(ctx context.Context, limit int) ([]dreamRow, error)
	RecordPendingResources(ctx context.Context, params recordDreamResourcesParams) (dreamRow, error)
	MarkRunning(ctx context.Context, params markDreamRunningParams) (dreamRow, error)
	UpdateRunningUsage(ctx context.Context, params updateRunningDreamUsageParams) (dreamRow, error)
	MarkTerminal(ctx context.Context, params markDreamTerminalParams) (dreamRow, error)
	MarkClaimedFailed(ctx context.Context, params markClaimedDreamFailedParams) (dreamRow, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, externalID string) (dreamRow, error)
}
