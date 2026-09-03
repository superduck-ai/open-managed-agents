package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type MessageBatch struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
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

type messageBatchJobPayload struct {
	MessageBatchUUID       string `json:"message_batch_uuid"`
	MessageBatchExternalID string `json:"message_batch_external_id"`
}

func (d *DB) CreateMessageBatch(ctx context.Context, batch MessageBatch, requests []NewBatchRequest) (MessageBatch, error) {
	betaHeaders, err := json.Marshal(batch.BetaHeaders)
	if err != nil {
		return MessageBatch{}, err
	}

	var created messageBatchCreatedRow
	err = d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewMessageBatchMapper(executor)
		created, err = mapper.Insert(ctx, insertMessageBatchParams{
			UUID:                batch.UUID,
			ExternalID:          batch.ExternalID,
			OrganizationUUID:    batch.OrganizationUUID,
			WorkspaceUUID:       batch.WorkspaceUUID,
			CreatedByAPIKeyUUID: nullableString(batch.CreatedByAPIKeyUUID),
			APIVariant:          batch.APIVariant,
			AnthropicVersion:    batch.AnthropicVersion,
			BetaHeaders:         betaHeaders,
			RequestCount:        len(requests),
			CreatedAt:           batch.CreatedAt,
			ExpiresAt:           batch.ExpiresAt,
		})
		if err != nil {
			return err
		}
		for _, request := range requests {
			if err = mapper.InsertRequest(ctx, insertMessageBatchRequestParams{
				ExternalID:       request.ExternalID,
				WorkspaceUUID:    request.WorkspaceUUID,
				MessageBatchUUID: created.UUID,
				RequestIndex:     request.RequestIndex,
				CustomID:         request.CustomID,
				Params:           request.Params,
			}); err != nil {
				return err
			}
		}
		payload, marshalErr := marshalMessageBatchJobPayload(created.UUID, batch.ExternalID)
		if marshalErr != nil {
			return marshalErr
		}
		return mapper.InsertJob(ctx, batch.WorkspaceUUID, payload)
	})
	if err != nil {
		return MessageBatch{}, err
	}

	batch.UUID = created.UUID
	batch.CreatedAt = created.CreatedAt
	batch.UpdatedAt = created.UpdatedAt
	batch.RequestCount = len(requests)
	batch.ProcessingCount = len(requests)
	batch.ProcessingStatus = "in_progress"
	return batch, nil
}

func (d *DB) GetMessageBatch(ctx context.Context, workspaceUUID, externalID string) (MessageBatch, error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	return messageBatchFromRow(row, err)
}

func (d *DB) GetMessageBatchByUUID(ctx context.Context, batchUUID string) (MessageBatch, error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	row, err := mapper.FindByUUID(ctx, batchUUID)
	return messageBatchFromRow(row, err)
}

func (d *DB) ListMessageBatchesPage(ctx context.Context, params ListMessageBatchesPageParams) ([]MessageBatch, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.AfterID != "" {
		params.BeforeID = ""
	}

	mapper := NewMessageBatchMapper(d.mapperDB)
	var anchor *messageBatchPageAnchor
	cursorID := params.AfterID
	if cursorID == "" {
		cursorID = params.BeforeID
	}
	if cursorID != "" {
		position, found, err := mapper.FindPageAnchorByExternalID(ctx, params.WorkspaceUUID, cursorID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil
		}
		anchor = &position
	}
	before := params.AfterID == "" && params.BeforeID != ""
	rows, err := mapper.ListPage(ctx, params.WorkspaceUUID, anchor, before, params.Limit+1)
	if err != nil {
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
	mapper := NewMessageBatchMapper(d.mapperDB)
	rowsAffected, err := mapper.MarkCancelingByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return MessageBatch{}, err
	}
	if rowsAffected == 0 {
		exists, existsErr := mapper.ExistsByExternalID(ctx, workspaceUUID, externalID)
		if existsErr != nil {
			return MessageBatch{}, existsErr
		}
		if !exists {
			return MessageBatch{}, ErrNotFound
		}
	}
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	return messageBatchFromRow(row, err)
}

func (d *DB) SoftDeleteMessageBatch(ctx context.Context, workspaceUUID, externalID string) error {
	mapper := NewMessageBatchMapper(d.mapperDB)
	rowsAffected, err := mapper.SoftDeleteEndedByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}
	_, err = mapper.FindProcessingStatusByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return mapNoRows(err)
	}
	return ErrInvalidState
}

func (d *DB) FinalizeMessageBatch(ctx context.Context, batchUUID string, processing, succeeded, errored, canceled, expired int, resultsBucket, resultsKey string, resultsSize int64, resultsSHA string, endedAt time.Time) error {
	mapper := NewMessageBatchMapper(d.mapperDB)
	rowsAffected, err := mapper.Finalize(ctx, finalizeMessageBatchParams{
		BatchUUID:     batchUUID,
		Processing:    processing,
		Succeeded:     succeeded,
		Errored:       errored,
		Canceled:      canceled,
		Expired:       expired,
		ResultsBucket: resultsBucket,
		ResultsKey:    resultsKey,
		ResultsSize:   resultsSize,
		ResultsSHA256: resultsSHA,
		EndedAt:       endedAt,
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
	mapper := NewMessageBatchMapper(d.mapperDB)
	return mapper.FinalizePendingRequests(ctx, batchUUID, finalStatus, result)
}

func (d *DB) MarkStaleInFlightRequestsErrored(ctx context.Context, batchUUID string, before time.Time, result json.RawMessage) (int64, error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	return mapper.MarkStaleInFlightRequestsErrored(ctx, batchUUID, before, result)
}

func (d *DB) CountRequestsByStatus(ctx context.Context, batchUUID string) (processing, succeeded, errored, canceled, expired int, err error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	counts, err := mapper.CountRequestsByStatus(ctx, batchUUID)
	return counts.Processing, counts.Succeeded, counts.Errored, counts.Canceled, counts.Expired, err
}

func (d *DB) ListExpiredBatches(ctx context.Context, now time.Time, limit int) ([]MessageBatch, error) {
	if limit <= 0 {
		limit = 100
	}
	mapper := NewMessageBatchMapper(d.mapperDB)
	rows, err := mapper.ListExpired(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	return messageBatchesFromRows(rows)
}

func (d *DB) GetMessageBatchRequestByIndex(ctx context.Context, batchUUID string, index int) (MessageBatchRequest, error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	row, err := mapper.FindRequestByIndex(ctx, batchUUID, index)
	if err != nil {
		return MessageBatchRequest{}, mapNoRows(err)
	}
	return row.request(), nil
}

func (d *DB) ListMessageBatchRequestsOrdered(ctx context.Context, batchUUID string) ([]MessageBatchRequest, error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	rows, err := mapper.ListRequestsOrdered(ctx, batchUUID)
	if err != nil {
		return nil, err
	}
	requests := make([]MessageBatchRequest, 0, len(rows))
	for _, row := range rows {
		requests = append(requests, row.request())
	}
	return requests, nil
}

func (d *DB) ClaimMessageBatchRequest(ctx context.Context, requestUUID, workerID string, startedAt time.Time) (bool, error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	rowsAffected, err := mapper.ClaimRequest(ctx, requestUUID, workerID, startedAt)
	return rowsAffected > 0, err
}

func (d *DB) CompleteMessageBatchRequest(ctx context.Context, requestUUID, status string, result json.RawMessage, upstreamRequestID string, completedAt time.Time) (bool, error) {
	mapper := NewMessageBatchMapper(d.mapperDB)
	rowsAffected, err := mapper.CompleteRequest(ctx, completeMessageBatchRequestParams{
		RequestUUID:       requestUUID,
		Status:            status,
		Result:            result,
		UpstreamRequestID: upstreamRequestID,
		CompletedAt:       completedAt,
	})
	return rowsAffected > 0, err
}

func (d *DB) EnqueueMessageBatchJob(ctx context.Context, workspaceUUID, batchUUID, batchExternalID string) error {
	payload, err := marshalMessageBatchJobPayload(batchUUID, batchExternalID)
	if err != nil {
		return err
	}
	mapper := NewMessageBatchMapper(d.mapperDB)
	return mapper.InsertJob(ctx, workspaceUUID, payload)
}

func (d *DB) LeaseMessageBatchJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]MessageBatchJob, error) {
	if limit <= 0 {
		limit = 1
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	mapper := NewMessageBatchMapper(d.mapperDB)
	rows, err := mapper.LeaseJobs(ctx, workerID, limit, leaseDuration.Microseconds())
	if err != nil {
		return nil, err
	}
	jobs := make([]MessageBatchJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs, nil
}

func (d *DB) ExtendMessageBatchJobLease(ctx context.Context, jobUUID, workerID string, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	mapper := NewMessageBatchMapper(d.mapperDB)
	rowsAffected, err := mapper.ExtendJobLease(ctx, jobUUID, workerID, leaseDuration.Microseconds())
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CompleteMessageBatchJob(ctx context.Context, jobUUID string) error {
	mapper := NewMessageBatchMapper(d.mapperDB)
	return mapper.CompleteJob(ctx, jobUUID)
}

func (d *DB) FailMessageBatchJob(ctx context.Context, jobUUID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	mapper := NewMessageBatchMapper(d.mapperDB)
	return mapper.FailJob(ctx, failMessageBatchJobParams{
		JobUUID:  jobUUID,
		Status:   status,
		RunAfter: time.Now().UTC().Add(retryDelay),
		Reason:   reason,
		Attempts: nextAttempts,
	})
}

func marshalMessageBatchJobPayload(batchUUID, batchExternalID string) ([]byte, error) {
	return json.Marshal(messageBatchJobPayload{
		MessageBatchUUID:       batchUUID,
		MessageBatchExternalID: batchExternalID,
	})
}

func messageBatchFromRow(row messageBatchRow, err error) (MessageBatch, error) {
	if err != nil {
		return MessageBatch{}, mapNoRows(err)
	}
	return row.batch()
}

func messageBatchesFromRows(rows []messageBatchRow) ([]MessageBatch, error) {
	batches := make([]MessageBatch, 0, len(rows))
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
		OrganizationUUID:    row.OrganizationUUID,
		WorkspaceUUID:       row.WorkspaceUUID,
		CreatedByAPIKeyUUID: stringFromNullable(row.CreatedByAPIKeyUUID),
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
