package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type MessageBatch struct {
	ID                int64
	UUID              string
	ExternalID        string
	WorkspaceID       int64
	WorkspaceUUID     string
	CreatedByAPIKeyID int64
	APIVariant        string
	AnthropicVersion  string
	BetaHeaders       []string
	ProcessingStatus  string
	RequestCount      int
	ProcessingCount   int
	SucceededCount    int
	ErroredCount      int
	CanceledCount     int
	ExpiredCount      int
	ResultsS3Bucket   *string
	ResultsS3Key      *string
	ResultsSizeBytes  *int64
	ResultsSHA256     *string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	EndedAt           *time.Time
	CancelInitiatedAt *time.Time
	ArchivedAt        *time.Time
	DeletedAt         *time.Time
	LastError         *string
	UpdatedAt         time.Time
}

type NewBatchRequest struct {
	ExternalID   string
	WorkspaceID  int64
	RequestIndex int
	CustomID     string
	Params       json.RawMessage
}

type ListMessageBatchesPageParams struct {
	WorkspaceID int64
	AfterID     string
	BeforeID    string
	Limit       int
}

type MessageBatchRequest struct {
	ID                int64
	WorkspaceID       int64
	MessageBatchID    int64
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
	ID                     int64
	ExternalID             string
	WorkspaceID            int64
	MessageBatchID         int64
	MessageBatchExternalID string
	Attempts               int
}

func (d *DB) CreateMessageBatch(ctx context.Context, b MessageBatch, reqs []NewBatchRequest) (MessageBatch, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return MessageBatch{}, err
	}
	defer tx.Rollback(ctx)

	betaHeaders, err := json.Marshal(b.BetaHeaders)
	if err != nil {
		return MessageBatch{}, err
	}
	err = tx.QueryRow(ctx, `
		insert into message_batches (
			uuid, external_id, workspace_id, created_by_api_key_id, api_variant,
			anthropic_version, beta_headers, request_count, processing_count,
			created_at, expires_at
		)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $8, $9, $10)
		returning id, uuid::text, created_at, updated_at
	`, b.UUID, b.ExternalID, b.WorkspaceID, b.CreatedByAPIKeyID, b.APIVariant,
		b.AnthropicVersion, betaHeaders, len(reqs), b.CreatedAt, b.ExpiresAt).Scan(&b.ID, &b.UUID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return MessageBatch{}, err
	}
	b.RequestCount = len(reqs)
	b.ProcessingCount = len(reqs)
	b.ProcessingStatus = "in_progress"

	batch := &pgx.Batch{}
	for _, req := range reqs {
		batch.Queue(`
			insert into message_batch_requests (
				external_id, workspace_id, message_batch_id, request_index, custom_id, params
			)
			values ($1, $2, $3, $4, $5, $6::jsonb)
		`, req.ExternalID, req.WorkspaceID, b.ID, req.RequestIndex, req.CustomID, []byte(req.Params))
	}
	br := tx.SendBatch(ctx, batch)
	for range reqs {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return MessageBatch{}, err
		}
	}
	if err := br.Close(); err != nil {
		return MessageBatch{}, err
	}

	if _, err := tx.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1,
			'message_batch_process',
			'pending',
			jsonb_build_object(
				'message_batch_id', $2::bigint,
				'message_batch_external_id', $3::text
			)
		)
	`, b.WorkspaceID, b.ID, b.ExternalID); err != nil {
		return MessageBatch{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return MessageBatch{}, err
	}
	return b, nil
}

func (d *DB) GetMessageBatch(ctx context.Context, workspaceID int64, externalID string) (MessageBatch, error) {
	row := d.Pool.QueryRow(ctx, messageBatchSelectSQL()+`
		where mb.workspace_id = $1 and mb.external_id = $2 and mb.deleted_at is null
	`, workspaceID, externalID)
	return scanMessageBatch(row)
}

func (d *DB) GetMessageBatchByID(ctx context.Context, id int64) (MessageBatch, error) {
	row := d.Pool.QueryRow(ctx, messageBatchSelectSQL()+`
		where mb.id = $1
	`, id)
	return scanMessageBatch(row)
}

func (d *DB) ListMessageBatchesPage(ctx context.Context, params ListMessageBatchesPageParams) ([]MessageBatch, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.AfterID != "" {
		params.BeforeID = ""
	}

	var cursorID int64
	var cursorCreatedAt time.Time
	if params.AfterID != "" || params.BeforeID != "" {
		cursorExternalID := params.AfterID
		if cursorExternalID == "" {
			cursorExternalID = params.BeforeID
		}
		err := d.Pool.QueryRow(ctx, `
			select id, created_at
			from message_batches
			where workspace_id = $1 and external_id = $2 and deleted_at is null
		`, params.WorkspaceID, cursorExternalID).Scan(&cursorID, &cursorCreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}

	query := messageBatchSelectSQL() + `
		where mb.workspace_id = $1 and mb.deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if params.AfterID != "" {
		query += fmt.Sprintf(" and (mb.created_at < $%d or (mb.created_at = $%d and mb.id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, cursorCreatedAt, cursorID)
		nextArg += 2
	} else if params.BeforeID != "" {
		query += fmt.Sprintf(" and (mb.created_at > $%d or (mb.created_at = $%d and mb.id > $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, cursorCreatedAt, cursorID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by mb.created_at desc, mb.id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var batches []MessageBatch
	for rows.Next() {
		b, err := scanMessageBatch(rows)
		if err != nil {
			return nil, false, err
		}
		batches = append(batches, b)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(batches) > params.Limit
	if hasMore {
		batches = batches[:params.Limit]
	}
	return batches, hasMore, nil
}

func (d *DB) CancelMessageBatch(ctx context.Context, workspaceID int64, externalID string) (MessageBatch, error) {
	tag, err := d.Pool.Exec(ctx, `
		update message_batches
		set processing_status = 'canceling',
			cancel_initiated_at = now(),
			updated_at = now()
		where workspace_id = $1
			and external_id = $2
			and deleted_at is null
			and processing_status = 'in_progress'
	`, workspaceID, externalID)
	if err != nil {
		return MessageBatch{}, err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := d.Pool.QueryRow(ctx, `
			select exists(
				select 1 from message_batches
				where workspace_id = $1 and external_id = $2 and deleted_at is null
			)
		`, workspaceID, externalID).Scan(&exists); err != nil {
			return MessageBatch{}, err
		}
		if !exists {
			return MessageBatch{}, ErrNotFound
		}
	}
	return d.GetMessageBatch(ctx, workspaceID, externalID)
}

func (d *DB) SoftDeleteMessageBatch(ctx context.Context, workspaceID int64, externalID string) error {
	tag, err := d.Pool.Exec(ctx, `
		update message_batches
		set deleted_at = now(), updated_at = now()
		where workspace_id = $1
			and external_id = $2
			and deleted_at is null
			and processing_status = 'ended'
	`, workspaceID, externalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	var status string
	err = d.Pool.QueryRow(ctx, `
		select processing_status
		from message_batches
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrInvalidState
}

func (d *DB) FinalizeMessageBatch(ctx context.Context, id int64, processing, succeeded, errored, canceled, expired int, resultsBucket, resultsKey string, resultsSize int64, resultsSHA string, endedAt time.Time) error {
	tag, err := d.Pool.Exec(ctx, `
		update message_batches
		set processing_status = 'ended',
			ended_at = $2,
			processing_count = $3,
			succeeded_count = $4,
			errored_count = $5,
			canceled_count = $6,
			expired_count = $7,
			results_s3_bucket = $8,
			results_s3_key = $9,
			results_size_bytes = $10,
			results_sha256 = $11,
			updated_at = now()
		where id = $1 and processing_status in ('in_progress', 'canceling')
	`, id, endedAt, processing, succeeded, errored, canceled, expired, resultsBucket, resultsKey, resultsSize, resultsSHA)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) FinalizePendingRequests(ctx context.Context, batchID int64, finalStatus string, result json.RawMessage) error {
	_, err := d.Pool.Exec(ctx, `
		update message_batch_requests
		set status = $2,
			result = $3::jsonb,
			completed_at = now(),
			updated_at = now()
		where message_batch_id = $1 and status = 'queued'
	`, batchID, finalStatus, []byte(result))
	return err
}

func (d *DB) MarkStaleInFlightRequestsErrored(ctx context.Context, batchID int64, before time.Time, result json.RawMessage) (int64, error) {
	tag, err := d.Pool.Exec(ctx, `
		update message_batch_requests
		set status = 'errored',
			result = $3::jsonb,
			completed_at = now(),
			updated_at = now()
		where message_batch_id = $1
			and status = 'in_flight'
			and started_at < $2
	`, batchID, before, []byte(result))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (d *DB) CountRequestsByStatus(ctx context.Context, batchID int64) (processing, succeeded, errored, canceled, expired int, err error) {
	err = d.Pool.QueryRow(ctx, `
		select
			count(*) filter (where status in ('queued', 'in_flight'))::int,
			count(*) filter (where status = 'succeeded')::int,
			count(*) filter (where status = 'errored')::int,
			count(*) filter (where status = 'canceled')::int,
			count(*) filter (where status = 'expired')::int
		from message_batch_requests
		where message_batch_id = $1
	`, batchID).Scan(&processing, &succeeded, &errored, &canceled, &expired)
	return
}

func (d *DB) ListExpiredBatches(ctx context.Context, now time.Time, limit int) ([]MessageBatch, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.Pool.Query(ctx, messageBatchSelectSQL()+`
		where mb.deleted_at is null
			and mb.processing_status in ('in_progress', 'canceling')
			and mb.expires_at <= $1
		order by mb.expires_at, mb.id
		limit $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var batches []MessageBatch
	for rows.Next() {
		b, err := scanMessageBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, b)
	}
	return batches, rows.Err()
}

func (d *DB) GetMessageBatchRequestByIndex(ctx context.Context, batchID int64, index int) (MessageBatchRequest, error) {
	row := d.Pool.QueryRow(ctx, messageBatchRequestSelectSQL()+`
		where message_batch_id = $1 and request_index = $2
	`, batchID, index)
	return scanMessageBatchRequest(row)
}

func (d *DB) ListMessageBatchRequestsOrdered(ctx context.Context, batchID int64) ([]MessageBatchRequest, error) {
	rows, err := d.Pool.Query(ctx, messageBatchRequestSelectSQL()+`
		where message_batch_id = $1
		order by request_index
	`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []MessageBatchRequest
	for rows.Next() {
		req, err := scanMessageBatchRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

func (d *DB) ClaimMessageBatchRequest(ctx context.Context, id int64, workerID string, startedAt time.Time) (bool, error) {
	tag, err := d.Pool.Exec(ctx, `
		update message_batch_requests
		set status = 'in_flight',
			started_at = $2,
			in_flight_worker_id = $3,
			updated_at = now()
		where id = $1 and status = 'queued'
	`, id, startedAt, workerID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (d *DB) CompleteMessageBatchRequest(ctx context.Context, id int64, status string, result json.RawMessage, upstreamRequestID string, completedAt time.Time) (bool, error) {
	tag, err := d.Pool.Exec(ctx, `
		update message_batch_requests
		set status = $2,
			result = $3::jsonb,
			upstream_request_id = nullif($4, ''),
			completed_at = $5,
			updated_at = now()
		where id = $1 and status = 'in_flight'
	`, id, status, []byte(result), upstreamRequestID, completedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (d *DB) EnqueueMessageBatchJob(ctx context.Context, workspaceID, batchID int64, batchExternalID string) error {
	_, err := d.Pool.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1,
			'message_batch_process',
			'pending',
			jsonb_build_object(
				'message_batch_id', $2::bigint,
				'message_batch_external_id', $3::text
			)
		)
	`, workspaceID, batchID, batchExternalID)
	return err
}

func (d *DB) LeaseMessageBatchJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]MessageBatchJob, error) {
	if limit <= 0 {
		limit = 1
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	rows, err := d.Pool.Query(ctx, `
		with next_jobs as (
			select id
			from jobs
			where type = 'message_batch_process'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit $1
			for update skip locked
		)
		update jobs j
		set status = 'running',
			locked_by = $2,
			locked_until = now() + $3::interval,
			updated_at = now()
		from next_jobs
		where j.id = next_jobs.id
		returning j.id, j.external_id, j.workspace_id,
			(j.payload->>'message_batch_id')::bigint,
			coalesce(j.payload->>'message_batch_external_id', ''),
			j.attempts
	`, limit, workerID, leaseDuration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []MessageBatchJob
	for rows.Next() {
		var job MessageBatchJob
		if err := rows.Scan(&job.ID, &job.ExternalID, &job.WorkspaceID, &job.MessageBatchID, &job.MessageBatchExternalID, &job.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) ExtendMessageBatchJobLease(ctx context.Context, jobID int64, workerID string, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	tag, err := d.Pool.Exec(ctx, `
		update jobs
		set locked_until = now() + $3::interval,
			updated_at = now()
		where id = $1
			and type = 'message_batch_process'
			and status = 'running'
			and locked_by = $2
	`, jobID, workerID, leaseDuration)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CompleteMessageBatchJob(ctx context.Context, jobID int64) error {
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where id = $1 and type = 'message_batch_process'
	`, jobID)
	return err
}

func (d *DB) FailMessageBatchJob(ctx context.Context, jobID int64, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = $2,
			locked_by = null,
			locked_until = null,
			run_after = $3,
			updated_at = now(),
			attempts = $5,
			payload = payload || jsonb_build_object('last_error', $4::text)
		where id = $1 and type = 'message_batch_process'
	`, jobID, status, runAfter, reason, nextAttempts)
	return err
}

func messageBatchSelectSQL() string {
	return `
		select mb.id, mb.uuid::text, mb.external_id, mb.workspace_id, w.uuid::text,
			mb.created_by_api_key_id, mb.api_variant, mb.anthropic_version, mb.beta_headers,
			mb.processing_status, mb.request_count, mb.processing_count, mb.succeeded_count,
			mb.errored_count, mb.canceled_count, mb.expired_count, mb.results_s3_bucket,
			mb.results_s3_key, mb.results_size_bytes, mb.results_sha256, mb.created_at,
			mb.expires_at, mb.ended_at, mb.cancel_initiated_at, mb.archived_at,
			mb.deleted_at, mb.last_error, mb.updated_at
		from message_batches mb
		join workspaces w on w.id = mb.workspace_id
	`
}

type messageBatchScanner interface {
	Scan(dest ...any) error
}

func scanMessageBatch(row messageBatchScanner) (MessageBatch, error) {
	var b MessageBatch
	var betaHeaders []byte
	err := row.Scan(&b.ID, &b.UUID, &b.ExternalID, &b.WorkspaceID, &b.WorkspaceUUID,
		&b.CreatedByAPIKeyID, &b.APIVariant, &b.AnthropicVersion, &betaHeaders,
		&b.ProcessingStatus, &b.RequestCount, &b.ProcessingCount, &b.SucceededCount,
		&b.ErroredCount, &b.CanceledCount, &b.ExpiredCount, &b.ResultsS3Bucket,
		&b.ResultsS3Key, &b.ResultsSizeBytes, &b.ResultsSHA256, &b.CreatedAt,
		&b.ExpiresAt, &b.EndedAt, &b.CancelInitiatedAt, &b.ArchivedAt,
		&b.DeletedAt, &b.LastError, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MessageBatch{}, ErrNotFound
	}
	if err != nil {
		return MessageBatch{}, err
	}
	if len(betaHeaders) > 0 {
		if err := json.Unmarshal(betaHeaders, &b.BetaHeaders); err != nil {
			return MessageBatch{}, err
		}
	}
	return b, nil
}

func messageBatchRequestSelectSQL() string {
	return `
		select id, workspace_id, message_batch_id, request_index, external_id, custom_id,
			params, status, result, upstream_request_id, started_at, completed_at,
			in_flight_worker_id, created_at, updated_at
		from message_batch_requests
	`
}

func scanMessageBatchRequest(row messageBatchScanner) (MessageBatchRequest, error) {
	var req MessageBatchRequest
	var params, result []byte
	err := row.Scan(&req.ID, &req.WorkspaceID, &req.MessageBatchID, &req.RequestIndex,
		&req.ExternalID, &req.CustomID, &params, &req.Status, &result,
		&req.UpstreamRequestID, &req.StartedAt, &req.CompletedAt,
		&req.InFlightWorkerID, &req.CreatedAt, &req.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MessageBatchRequest{}, ErrNotFound
	}
	if err != nil {
		return MessageBatchRequest{}, err
	}
	req.Params = append(json.RawMessage(nil), params...)
	if len(result) > 0 {
		req.Result = append(json.RawMessage(nil), result...)
	}
	return req, nil
}
