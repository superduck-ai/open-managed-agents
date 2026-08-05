package db

import (
	"context"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MessageBatchMapper -sql ./batches_mapper.xml -out ./batches_mapper.sqlmap.gen.go -dialect postgres

type messageBatchCreatedRow struct {
	UUID      string    `db:"uuid"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type messageBatchPageAnchor struct {
	UUID      string    `db:"uuid"`
	CreatedAt time.Time `db:"created_at"`
}

type messageBatchRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	APIVariant          string     `db:"api_variant"`
	AnthropicVersion    string     `db:"anthropic_version"`
	BetaHeadersJSON     []byte     `db:"beta_headers"`
	ProcessingStatus    string     `db:"processing_status"`
	RequestCount        int        `db:"request_count"`
	ProcessingCount     int        `db:"processing_count"`
	SucceededCount      int        `db:"succeeded_count"`
	ErroredCount        int        `db:"errored_count"`
	CanceledCount       int        `db:"canceled_count"`
	ExpiredCount        int        `db:"expired_count"`
	ResultsS3Bucket     *string    `db:"results_s3_bucket"`
	ResultsS3Key        *string    `db:"results_s3_key"`
	ResultsSizeBytes    *int64     `db:"results_size_bytes"`
	ResultsSHA256       *string    `db:"results_sha256"`
	CreatedAt           time.Time  `db:"created_at"`
	ExpiresAt           time.Time  `db:"expires_at"`
	EndedAt             *time.Time `db:"ended_at"`
	CancelInitiatedAt   *time.Time `db:"cancel_initiated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
	LastError           *string    `db:"last_error"`
	UpdatedAt           time.Time  `db:"updated_at"`
}

type messageBatchRequestRow struct {
	UUID              string     `db:"uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	MessageBatchUUID  string     `db:"message_batch_uuid"`
	RequestIndex      int        `db:"request_index"`
	ExternalID        string     `db:"external_id"`
	CustomID          string     `db:"custom_id"`
	ParamsJSON        []byte     `db:"params"`
	Status            string     `db:"status"`
	ResultJSON        []byte     `db:"result"`
	UpstreamRequestID *string    `db:"upstream_request_id"`
	StartedAt         *time.Time `db:"started_at"`
	CompletedAt       *time.Time `db:"completed_at"`
	InFlightWorkerID  *string    `db:"in_flight_worker_id"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
}

type messageBatchJobRow struct {
	UUID                   string `db:"uuid"`
	ExternalID             string `db:"external_id"`
	WorkspaceUUID          string `db:"workspace_uuid"`
	MessageBatchUUID       string `db:"message_batch_uuid"`
	MessageBatchExternalID string `db:"message_batch_external_id"`
	Attempts               int    `db:"attempts"`
}

type insertMessageBatchParams struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	APIVariant          string
	AnthropicVersion    string
	BetaHeaders         json.RawMessage
	RequestCount        int
	CreatedAt           time.Time
	ExpiresAt           time.Time
}

type insertMessageBatchRequestParams struct {
	ExternalID       string
	WorkspaceUUID    string
	MessageBatchUUID string
	RequestIndex     int
	CustomID         string
	Params           json.RawMessage
}

type finalizeMessageBatchParams struct {
	BatchUUID     string
	Processing    int
	Succeeded     int
	Errored       int
	Canceled      int
	Expired       int
	ResultsBucket string
	ResultsKey    string
	ResultsSize   int64
	ResultsSHA256 string
	EndedAt       time.Time
}

type messageBatchRequestCountsRow struct {
	Processing int `db:"processing"`
	Succeeded  int `db:"succeeded"`
	Errored    int `db:"errored"`
	Canceled   int `db:"canceled"`
	Expired    int `db:"expired"`
}

type completeMessageBatchRequestParams struct {
	RequestUUID       string
	Status            string
	Result            json.RawMessage
	UpstreamRequestID string
	CompletedAt       time.Time
}

type failMessageBatchJobParams struct {
	JobUUID  string
	Status   string
	RunAfter time.Time
	Reason   string
	Attempts int
}

type MessageBatchMapper interface {
	Insert(ctx context.Context, params insertMessageBatchParams) (messageBatchCreatedRow, error)
	InsertRequest(ctx context.Context, params insertMessageBatchRequestParams) error
	InsertJob(ctx context.Context, workspaceUUID string, payload []byte) error
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (messageBatchRow, error)
	FindByUUID(ctx context.Context, batchUUID string) (messageBatchRow, error)
	FindPageAnchorByExternalID(ctx context.Context, workspaceUUID, externalID string) (messageBatchPageAnchor, bool, error)
	ListPage(ctx context.Context, workspaceUUID string, anchor *messageBatchPageAnchor, before bool, limit int) ([]messageBatchRow, error)
	MarkCancelingByExternalID(ctx context.Context, workspaceUUID, externalID string) (int64, error)
	ExistsByExternalID(ctx context.Context, workspaceUUID, externalID string) (bool, error)
	SoftDeleteEndedByExternalID(ctx context.Context, workspaceUUID, externalID string) (int64, error)
	FindProcessingStatusByExternalID(ctx context.Context, workspaceUUID, externalID string) (string, error)
	Finalize(ctx context.Context, params finalizeMessageBatchParams) (int64, error)
	FinalizePendingRequests(ctx context.Context, batchUUID, finalStatus string, result json.RawMessage) error
	MarkStaleInFlightRequestsErrored(ctx context.Context, batchUUID string, before time.Time, result json.RawMessage) (int64, error)
	CountRequestsByStatus(ctx context.Context, batchUUID string) (messageBatchRequestCountsRow, error)
	ListExpired(ctx context.Context, now time.Time, limit int) ([]messageBatchRow, error)
	FindRequestByIndex(ctx context.Context, batchUUID string, requestIndex int) (messageBatchRequestRow, error)
	ListRequestsOrdered(ctx context.Context, batchUUID string) ([]messageBatchRequestRow, error)
	ClaimRequest(ctx context.Context, requestUUID, workerID string, startedAt time.Time) (int64, error)
	CompleteRequest(ctx context.Context, params completeMessageBatchRequestParams) (int64, error)
	LeaseJobs(ctx context.Context, workerID string, limit int, leaseMicroseconds int64) ([]messageBatchJobRow, error)
	ExtendJobLease(ctx context.Context, jobUUID, workerID string, leaseMicroseconds int64) (int64, error)
	CompleteJob(ctx context.Context, jobUUID string) error
	FailJob(ctx context.Context, params failMessageBatchJobParams) error
}
