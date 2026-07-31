package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type MessageBatch struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	APIVariant          string
	AnthropicVersion    string
	BetaHeaders         []string
	ProcessingStatus    string
	RequestCount        int
	ProcessingCount     int
	SucceededCount      int
	ErroredCount        int
	CanceledCount       int
	ExpiredCount        int
	ResultsS3Bucket     *string
	ResultsS3Key        *string
	ResultsSizeBytes    *int64
	ResultsSHA256       *string
	CreatedAt           time.Time
	ExpiresAt           time.Time
	EndedAt             *time.Time
	CancelInitiatedAt   *time.Time
	ArchivedAt          *time.Time
	DeletedAt           *time.Time
	LastError           *string
	UpdatedAt           time.Time
}

type NewBatchRequest struct {
	ExternalID    string
	WorkspaceUUID string
	RequestIndex  int
	CustomID      string
	Params        json.RawMessage
}

type ListMessageBatchesPageParams struct {
	WorkspaceUUID string
	AfterID       string
	BeforeID      string
	Limit         int
}

type MessageBatchRequest struct {
	UUID              string
	WorkspaceUUID     string
	MessageBatchUUID  string
	RequestIndex      int
	ExternalID        string
	CustomID          string
	Params            json.RawMessage
	Status            string
	Result            json.RawMessage
	UpstreamRequestID *string
	StartedAt         *time.Time
	CompletedAt       *time.Time
	InFlightWorkerID  *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type MessageBatchJob struct {
	UUID                   string
	ExternalID             string
	WorkspaceUUID          string
	MessageBatchUUID       string
	MessageBatchExternalID string
	Attempts               int
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

func (d *DB) CreateMessageBatch(ctx context.Context, b MessageBatch, reqs []NewBatchRequest) (MessageBatch, error) {
	betaHeaders, err := json.Marshal(b.BetaHeaders)
	if err != nil {
		return MessageBatch{}, err
	}

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return MessageBatch{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var created struct {
		UUID      string    `db:"uuid"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	err = namedGetContext(ctx, tx, &created, `
		insert into message_batches (
			uuid, external_id, workspace_uuid, created_by_api_key_uuid, api_variant,
			anthropic_version, beta_headers, request_count, processing_count,
			created_at, expires_at
		)
		values (
			:uuid, :external_id, :workspace_uuid, :created_by_api_key_uuid, :api_variant,
			:anthropic_version, CAST(:beta_headers AS jsonb), :request_count,
			:request_count, :created_at, :expires_at
		)
		returning CAST(uuid AS text) AS uuid, created_at, updated_at
	`, map[string]any{
		"uuid":                    b.UUID,
		"external_id":             b.ExternalID,
		"workspace_uuid":          b.WorkspaceUUID,
		"created_by_api_key_uuid": b.CreatedByAPIKeyUUID,
		"api_variant":             b.APIVariant,
		"anthropic_version":       b.AnthropicVersion,
		"beta_headers":            string(betaHeaders),
		"request_count":           len(reqs),
		"created_at":              b.CreatedAt,
		"expires_at":              b.ExpiresAt,
	})
	if err != nil {
		return MessageBatch{}, err
	}
	b.UUID = created.UUID
	b.CreatedAt = created.CreatedAt
	b.UpdatedAt = created.UpdatedAt
	b.RequestCount = len(reqs)
	b.ProcessingCount = len(reqs)
	b.ProcessingStatus = "in_progress"

	requestStatement, err := tx.PrepareNamedContext(ctx, insertMessageBatchRequestSQL)
	if err != nil {
		return MessageBatch{}, err
	}
	defer requestStatement.Close()
	for _, req := range reqs {
		if _, err := requestStatement.ExecContext(ctx, map[string]any{
			"external_id":        req.ExternalID,
			"workspace_uuid":     req.WorkspaceUUID,
			"message_batch_uuid": b.UUID,
			"request_index":      req.RequestIndex,
			"custom_id":          req.CustomID,
			"params":             string(req.Params),
		}); err != nil {
			return MessageBatch{}, err
		}
	}

	if _, err := namedExecContext(ctx, tx, `
		insert into jobs (external_id, workspace_uuid, type, status, payload)
		values (
			concat('job_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:workspace_uuid,
			'message_batch_process',
			'pending',
			jsonb_build_object(
				'message_batch_uuid', CAST(:message_batch_uuid AS text),
				'message_batch_external_id', CAST(:message_batch_external_id AS text)
			)
		)
	`, map[string]any{
		"workspace_uuid":            b.WorkspaceUUID,
		"message_batch_uuid":        b.UUID,
		"message_batch_external_id": b.ExternalID,
	}); err != nil {
		return MessageBatch{}, err
	}

	if err := tx.Commit(); err != nil {
		return MessageBatch{}, err
	}
	return b, nil
}

func (d *DB) GetMessageBatch(ctx context.Context, workspaceUUID, externalID string) (MessageBatch, error) {
	return getMessageBatchSQLX(ctx, d.sql, messageBatchSelectSQL()+`
		where mb.workspace_uuid = :workspace_uuid
			and mb.external_id = :external_id
			and mb.deleted_at is null
	`, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	})
}

func (d *DB) GetMessageBatchByUUID(ctx context.Context, batchUUID string) (MessageBatch, error) {
	return getMessageBatchSQLX(ctx, d.sql, messageBatchSelectSQL()+`
		where mb.uuid = :batch_uuid
	`, map[string]any{"batch_uuid": batchUUID})
}

func (d *DB) ListMessageBatchesPage(ctx context.Context, params ListMessageBatchesPageParams) ([]MessageBatch, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.AfterID != "" {
		params.BeforeID = ""
	}

	var cursor struct {
		UUID      string    `db:"uuid"`
		CreatedAt time.Time `db:"created_at"`
	}
	if params.AfterID != "" || params.BeforeID != "" {
		cursorExternalID := params.AfterID
		if cursorExternalID == "" {
			cursorExternalID = params.BeforeID
		}
		err := namedGetContext(ctx, d.sql, &cursor, `
			select CAST(uuid AS text) AS uuid, created_at
			from message_batches
			where workspace_uuid = :workspace_uuid
				and external_id = :external_id
				and deleted_at is null
		`, map[string]any{
			"workspace_uuid": params.WorkspaceUUID,
			"external_id":    cursorExternalID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}

	query := messageBatchSelectSQL() + `
		where mb.workspace_uuid = :workspace_uuid and mb.deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": params.WorkspaceUUID,
		"limit":          params.Limit + 1,
	}
	if params.AfterID != "" {
		query += `
			and (mb.created_at < :cursor_created_at
				or (mb.created_at = :cursor_created_at and mb.uuid < :cursor_uuid))
		`
		arguments["cursor_created_at"] = cursor.CreatedAt
		arguments["cursor_uuid"] = cursor.UUID
	} else if params.BeforeID != "" {
		query += `
			and (mb.created_at > :cursor_created_at
				or (mb.created_at = :cursor_created_at and mb.uuid > :cursor_uuid))
		`
		arguments["cursor_created_at"] = cursor.CreatedAt
		arguments["cursor_uuid"] = cursor.UUID
	}
	query += ` order by mb.created_at desc, mb.uuid desc limit :limit`

	var rows []messageBatchRow
	if err := namedSelectContext(ctx, d.sql, &rows, query, arguments); err != nil {
		return nil, false, err
	}
	batches, err := messageBatchesFromRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(batches) > params.Limit
	if hasMore {
		batches = batches[:params.Limit]
	}
	return batches, hasMore, nil
}

func (d *DB) CancelMessageBatch(ctx context.Context, workspaceUUID, externalID string) (MessageBatch, error) {
	arguments := map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update message_batches
		set processing_status = 'canceling',
			cancel_initiated_at = now(),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
			and processing_status = 'in_progress'
	`, arguments)
	if err != nil {
		return MessageBatch{}, err
	}
	if rowsAffected == 0 {
		var exists bool
		if err := namedGetContext(ctx, d.sql, &exists, `
			select exists(
				select 1 from message_batches
				where workspace_uuid = :workspace_uuid
					and external_id = :external_id
					and deleted_at is null
			)
		`, arguments); err != nil {
			return MessageBatch{}, err
		}
		if !exists {
			return MessageBatch{}, ErrNotFound
		}
	}
	return d.GetMessageBatch(ctx, workspaceUUID, externalID)
}

func (d *DB) SoftDeleteMessageBatch(ctx context.Context, workspaceUUID, externalID string) error {
	arguments := map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update message_batches
		set deleted_at = now(), updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
			and processing_status = 'ended'
	`, arguments)
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}

	var status string
	err = namedGetContext(ctx, d.sql, &status, `
		select processing_status
		from message_batches
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
	`, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrInvalidState
}

func (d *DB) FinalizeMessageBatch(ctx context.Context, batchUUID string, processing, succeeded, errored, canceled, expired int, resultsBucket, resultsKey string, resultsSize int64, resultsSHA string, endedAt time.Time) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update message_batches
		set processing_status = 'ended',
			ended_at = :ended_at,
			processing_count = :processing_count,
			succeeded_count = :succeeded_count,
			errored_count = :errored_count,
			canceled_count = :canceled_count,
			expired_count = :expired_count,
			results_s3_bucket = :results_s3_bucket,
			results_s3_key = :results_s3_key,
			results_size_bytes = :results_size_bytes,
			results_sha256 = :results_sha256,
			updated_at = now()
		where uuid = :batch_uuid and processing_status in ('in_progress', 'canceling')
	`, map[string]any{
		"batch_uuid":         batchUUID,
		"ended_at":           endedAt,
		"processing_count":   processing,
		"succeeded_count":    succeeded,
		"errored_count":      errored,
		"canceled_count":     canceled,
		"expired_count":      expired,
		"results_s3_bucket":  resultsBucket,
		"results_s3_key":     resultsKey,
		"results_size_bytes": resultsSize,
		"results_sha256":     resultsSHA,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) FinalizePendingRequests(ctx context.Context, batchUUID, finalStatus string, result json.RawMessage) error {
	_, err := namedExecContext(ctx, d.sql, `
		update message_batch_requests
		set status = :final_status,
			result = CAST(:result AS jsonb),
			completed_at = now(),
			updated_at = now()
		where message_batch_uuid = :message_batch_uuid and status = 'queued'
	`, map[string]any{
		"message_batch_uuid": batchUUID,
		"final_status":       finalStatus,
		"result":             string(result),
	})
	return err
}

func (d *DB) MarkStaleInFlightRequestsErrored(ctx context.Context, batchUUID string, before time.Time, result json.RawMessage) (int64, error) {
	return namedExecRowsAffected(ctx, d.sql, `
		update message_batch_requests
		set status = 'errored',
			result = CAST(:result AS jsonb),
			completed_at = now(),
			updated_at = now()
		where message_batch_uuid = :message_batch_uuid
			and status = 'in_flight'
			and started_at < :before
	`, map[string]any{
		"message_batch_uuid": batchUUID,
		"before":             before,
		"result":             string(result),
	})
}

func (d *DB) CountRequestsByStatus(ctx context.Context, batchUUID string) (processing, succeeded, errored, canceled, expired int, err error) {
	var counts struct {
		Processing int `db:"processing"`
		Succeeded  int `db:"succeeded"`
		Errored    int `db:"errored"`
		Canceled   int `db:"canceled"`
		Expired    int `db:"expired"`
	}
	err = namedGetContext(ctx, d.sql, &counts, `
		select
			CAST(count(*) filter (where status in ('queued', 'in_flight')) AS int) AS processing,
			CAST(count(*) filter (where status = 'succeeded') AS int) AS succeeded,
			CAST(count(*) filter (where status = 'errored') AS int) AS errored,
			CAST(count(*) filter (where status = 'canceled') AS int) AS canceled,
			CAST(count(*) filter (where status = 'expired') AS int) AS expired
		from message_batch_requests
		where message_batch_uuid = :message_batch_uuid
	`, map[string]any{"message_batch_uuid": batchUUID})
	processing = counts.Processing
	succeeded = counts.Succeeded
	errored = counts.Errored
	canceled = counts.Canceled
	expired = counts.Expired
	return
}

func (d *DB) ListExpiredBatches(ctx context.Context, now time.Time, limit int) ([]MessageBatch, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []messageBatchRow
	err := namedSelectContext(ctx, d.sql, &rows, messageBatchSelectSQL()+`
		where mb.deleted_at is null
			and mb.processing_status in ('in_progress', 'canceling')
			and mb.expires_at <= :now
		order by mb.expires_at, mb.uuid
		limit :limit
	`, map[string]any{
		"now":   now,
		"limit": limit,
	})
	if err != nil {
		return nil, err
	}
	return messageBatchesFromRows(rows)
}

func (d *DB) GetMessageBatchRequestByIndex(ctx context.Context, batchUUID string, index int) (MessageBatchRequest, error) {
	return getMessageBatchRequestSQLX(ctx, d.sql, messageBatchRequestSelectSQL()+`
		where message_batch_uuid = :message_batch_uuid and request_index = :request_index
	`, map[string]any{
		"message_batch_uuid": batchUUID,
		"request_index":      index,
	})
}

func (d *DB) ListMessageBatchRequestsOrdered(ctx context.Context, batchUUID string) ([]MessageBatchRequest, error) {
	var rows []messageBatchRequestRow
	err := namedSelectContext(ctx, d.sql, &rows, messageBatchRequestSelectSQL()+`
		where message_batch_uuid = :message_batch_uuid
		order by request_index
	`, map[string]any{"message_batch_uuid": batchUUID})
	if err != nil {
		return nil, err
	}
	var requests []MessageBatchRequest
	for _, row := range rows {
		requests = append(requests, row.request())
	}
	return requests, nil
}

func (d *DB) ClaimMessageBatchRequest(ctx context.Context, requestUUID, workerID string, startedAt time.Time) (bool, error) {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update message_batch_requests
		set status = 'in_flight',
			started_at = :started_at,
			in_flight_worker_id = :worker_id,
			updated_at = now()
		where uuid = :request_uuid and status = 'queued'
	`, map[string]any{
		"request_uuid": requestUUID,
		"worker_id":    workerID,
		"started_at":   startedAt,
	})
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (d *DB) CompleteMessageBatchRequest(ctx context.Context, requestUUID, status string, result json.RawMessage, upstreamRequestID string, completedAt time.Time) (bool, error) {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update message_batch_requests
		set status = :status,
			result = CAST(:result AS jsonb),
			upstream_request_id = nullif(:upstream_request_id, ''),
			completed_at = :completed_at,
			updated_at = now()
		where uuid = :request_uuid and status = 'in_flight'
	`, map[string]any{
		"request_uuid":        requestUUID,
		"status":              status,
		"result":              string(result),
		"upstream_request_id": upstreamRequestID,
		"completed_at":        completedAt,
	})
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (d *DB) EnqueueMessageBatchJob(ctx context.Context, workspaceUUID, batchUUID, batchExternalID string) error {
	_, err := namedExecContext(ctx, d.sql, `
		insert into jobs (external_id, workspace_uuid, type, status, payload)
		values (
			concat('job_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:workspace_uuid,
			'message_batch_process',
			'pending',
			jsonb_build_object(
				'message_batch_uuid', CAST(:message_batch_uuid AS text),
				'message_batch_external_id', CAST(:message_batch_external_id AS text)
			)
		)
	`, map[string]any{
		"workspace_uuid":            workspaceUUID,
		"message_batch_uuid":        batchUUID,
		"message_batch_external_id": batchExternalID,
	})
	return err
}

func (d *DB) LeaseMessageBatchJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]MessageBatchJob, error) {
	if limit <= 0 {
		limit = 1
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	var rows []messageBatchJobRow
	err := namedSelectContext(ctx, d.sql, &rows, leaseMessageBatchJobsSQL, map[string]any{
		"limit":              limit,
		"worker_id":          workerID,
		"lease_microseconds": leaseDuration.Microseconds(),
	})
	if err != nil {
		return nil, err
	}
	var jobs []MessageBatchJob
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs, nil
}

const leaseMessageBatchJobsSQL = `
		with next_jobs as (
			select uuid
			from jobs
			where type = 'message_batch_process'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit :limit
			for update skip locked
		)
		update jobs j
		set status = 'running',
			locked_by = :worker_id,
			locked_until = now() + :lease_microseconds * interval '1 microsecond',
			updated_at = now()
		from next_jobs
		where j.uuid = next_jobs.uuid
		returning CAST(j.uuid AS text) AS uuid, j.external_id,
			CAST(j.workspace_uuid AS text) AS workspace_uuid,
			j.payload->>'message_batch_uuid' AS message_batch_uuid,
			coalesce(j.payload->>'message_batch_external_id', '') AS message_batch_external_id,
			j.attempts
	`

func (d *DB) ExtendMessageBatchJobLease(ctx context.Context, jobUUID, workerID string, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set locked_until = now() + :lease_microseconds * interval '1 microsecond',
			updated_at = now()
		where uuid = :job_uuid
			and type = 'message_batch_process'
			and status = 'running'
			and locked_by = :worker_id
	`, map[string]any{
		"job_uuid":           jobUUID,
		"worker_id":          workerID,
		"lease_microseconds": leaseDuration.Microseconds(),
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CompleteMessageBatchJob(ctx context.Context, jobUUID string) error {
	_, err := namedExecContext(ctx, d.sql, `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where uuid = :job_uuid and type = 'message_batch_process'
	`, map[string]any{"job_uuid": jobUUID})
	return err
}

func (d *DB) FailMessageBatchJob(ctx context.Context, jobUUID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	_, err := namedExecContext(ctx, d.sql, `
		update jobs
		set status = :status,
			locked_by = null,
			locked_until = null,
			run_after = :run_after,
			updated_at = now(),
			attempts = :attempts,
			payload = payload || jsonb_build_object('last_error', CAST(:reason AS text))
		where uuid = :job_uuid and type = 'message_batch_process'
	`, map[string]any{
		"job_uuid":  jobUUID,
		"status":    status,
		"run_after": runAfter,
		"reason":    reason,
		"attempts":  nextAttempts,
	})
	return err
}

func messageBatchSelectSQL() string {
	return `
		select CAST(mb.uuid AS text) AS uuid, mb.external_id,
			CAST(mb.workspace_uuid AS text) AS workspace_uuid,
			CAST(mb.created_by_api_key_uuid AS text) AS created_by_api_key_uuid,
			mb.api_variant, mb.anthropic_version, mb.beta_headers, mb.processing_status,
			mb.request_count, mb.processing_count, mb.succeeded_count, mb.errored_count,
			mb.canceled_count, mb.expired_count, mb.results_s3_bucket, mb.results_s3_key,
			mb.results_size_bytes, mb.results_sha256, mb.created_at, mb.expires_at,
			mb.ended_at, mb.cancel_initiated_at, mb.archived_at, mb.deleted_at,
			mb.last_error, mb.updated_at
		from message_batches mb
	`
}

func getMessageBatchSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (MessageBatch, error) {
	var row messageBatchRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageBatch{}, ErrNotFound
	}
	if err != nil {
		return MessageBatch{}, err
	}
	return row.batch()
}

func messageBatchesFromRows(rows []messageBatchRow) ([]MessageBatch, error) {
	var batches []MessageBatch
	for _, row := range rows {
		batch, err := row.batch()
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

func (row messageBatchRow) batch() (MessageBatch, error) {
	var betaHeaders []string
	if len(row.BetaHeadersJSON) > 0 {
		if err := json.Unmarshal(row.BetaHeadersJSON, &betaHeaders); err != nil {
			return MessageBatch{}, err
		}
	}
	return MessageBatch{
		UUID:                row.UUID,
		ExternalID:          row.ExternalID,
		WorkspaceUUID:       row.WorkspaceUUID,
		CreatedByAPIKeyUUID: row.CreatedByAPIKeyUUID,
		APIVariant:          row.APIVariant,
		AnthropicVersion:    row.AnthropicVersion,
		BetaHeaders:         betaHeaders,
		ProcessingStatus:    row.ProcessingStatus,
		RequestCount:        row.RequestCount,
		ProcessingCount:     row.ProcessingCount,
		SucceededCount:      row.SucceededCount,
		ErroredCount:        row.ErroredCount,
		CanceledCount:       row.CanceledCount,
		ExpiredCount:        row.ExpiredCount,
		ResultsS3Bucket:     row.ResultsS3Bucket,
		ResultsS3Key:        row.ResultsS3Key,
		ResultsSizeBytes:    row.ResultsSizeBytes,
		ResultsSHA256:       row.ResultsSHA256,
		CreatedAt:           row.CreatedAt,
		ExpiresAt:           row.ExpiresAt,
		EndedAt:             row.EndedAt,
		CancelInitiatedAt:   row.CancelInitiatedAt,
		ArchivedAt:          row.ArchivedAt,
		DeletedAt:           row.DeletedAt,
		LastError:           row.LastError,
		UpdatedAt:           row.UpdatedAt,
	}, nil
}

const insertMessageBatchRequestSQL = `
	insert into message_batch_requests (
		external_id, workspace_uuid, message_batch_uuid, request_index, custom_id, params
	)
	values (
		:external_id, :workspace_uuid, :message_batch_uuid, :request_index, :custom_id,
		CAST(:params AS jsonb)
	)
`

func messageBatchRequestSelectSQL() string {
	return `
		select CAST(uuid AS text) AS uuid,
			CAST(workspace_uuid AS text) AS workspace_uuid,
			CAST(message_batch_uuid AS text) AS message_batch_uuid,
			request_index, external_id, custom_id,
			params, status, result, upstream_request_id, started_at, completed_at,
			in_flight_worker_id, created_at, updated_at
		from message_batch_requests
	`
}

func getMessageBatchRequestSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (MessageBatchRequest, error) {
	var row messageBatchRequestRow
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageBatchRequest{}, ErrNotFound
	}
	if err != nil {
		return MessageBatchRequest{}, err
	}
	return row.request(), nil
}

func (row messageBatchRequestRow) request() MessageBatchRequest {
	return MessageBatchRequest{
		UUID:              row.UUID,
		WorkspaceUUID:     row.WorkspaceUUID,
		MessageBatchUUID:  row.MessageBatchUUID,
		RequestIndex:      row.RequestIndex,
		ExternalID:        row.ExternalID,
		CustomID:          row.CustomID,
		Params:            append(json.RawMessage(nil), row.ParamsJSON...),
		Status:            row.Status,
		Result:            append(json.RawMessage(nil), row.ResultJSON...),
		UpstreamRequestID: row.UpstreamRequestID,
		StartedAt:         row.StartedAt,
		CompletedAt:       row.CompletedAt,
		InFlightWorkerID:  row.InFlightWorkerID,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func (row messageBatchJobRow) job() MessageBatchJob {
	return MessageBatchJob{
		UUID:                   row.UUID,
		ExternalID:             row.ExternalID,
		WorkspaceUUID:          row.WorkspaceUUID,
		MessageBatchUUID:       row.MessageBatchUUID,
		MessageBatchExternalID: row.MessageBatchExternalID,
		Attempts:               row.Attempts,
	}
}
