package batches

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const (
	defaultWorkerPollInterval = 5 * time.Second
	batchJobMaxAttempts       = 10
)

// Worker owns the message batch processing and expiry loops.
type Worker struct {
	database *db.DB
	store    storage.ObjectStore
	cfg      config.BatchConfig
	upstream UpstreamClient
	logger   *slog.Logger
}

// NewWorker constructs a message batch worker.
func NewWorker(
	database *db.DB,
	store storage.ObjectStore,
	cfg config.BatchConfig,
	upstream UpstreamClient,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		database: database,
		store:    store,
		cfg:      cfg,
		upstream: upstream,
		logger:   logging.LoggerOrDefault(logger),
	}
}

// Start launches batch processing and expiry loops when batch workers are enabled.
func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.database == nil || w.store == nil || w.upstream == nil || !w.cfg.WorkerEnabled {
		return
	}
	w.startProcessing(ctx)
	w.startExpirySweep(ctx)
}

func (w *Worker) startProcessing(ctx context.Context) {
	workerID := fmt.Sprintf("message-batch-%d", os.Getpid())
	go func() {
		ticker := time.NewTicker(defaultWorkerPollInterval)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx, workerID); err != nil {
				w.logger.ErrorContext(ctx, "message batch worker", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *Worker) startExpirySweep(ctx context.Context) {
	interval := w.cfg.ExpirySweepInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := w.runExpirySweepOnce(ctx, time.Now().UTC()); err != nil {
				w.logger.ErrorContext(ctx, "message batch expiry sweep", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *Worker) runExpirySweepOnce(ctx context.Context, now time.Time) error {
	batches, err := w.database.ListExpiredBatches(ctx, now, 100)
	if err != nil {
		return err
	}
	var errs []error
	for _, batch := range batches {
		if err := w.database.EnqueueMessageBatchJob(ctx, batch.WorkspaceUUID, batch.UUID, batch.ExternalID); err != nil {
			errs = append(errs, fmt.Errorf("enqueue expired batch %s: %w", batch.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

// RunOnce leases and processes one batch of message batch jobs.
func (w *Worker) RunOnce(ctx context.Context, workerID string) error {
	jobs, err := w.database.LeaseMessageBatchJobs(ctx, workerID, w.cfg.WorkerConcurrency, w.cfg.JobLeaseDuration)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if err := w.processJob(ctx, workerID, job); err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := w.database.FailMessageBatchJob(ctx, job.UUID, job.Attempts, err.Error(), delay, batchJobMaxAttempts); markErr != nil {
				errs = append(errs, fmt.Errorf("mark batch job %s retry: %w", job.ExternalID, markErr))
			}
			errs = append(errs, fmt.Errorf("process batch job %s: %w", job.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

func (w *Worker) processJob(ctx context.Context, workerID string, job db.MessageBatchJob) error {
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	heartbeatErr := w.startHeartbeat(heartbeatCtx, job.UUID, workerID)

	batch, err := w.database.GetMessageBatchByUUID(ctx, job.MessageBatchUUID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return w.database.CompleteMessageBatchJob(ctx, job.UUID)
		}
		return err
	}
	if batch.DeletedAt != nil || batch.ProcessingStatus == "ended" {
		return w.database.CompleteMessageBatchJob(ctx, job.UUID)
	}

	staleBefore := time.Now().UTC().Add(-w.cfg.UpstreamTimeout - time.Minute)
	if w.cfg.UpstreamTimeout <= 0 {
		staleBefore = time.Now().UTC().Add(-11 * time.Minute)
	}
	if _, err := w.database.MarkStaleInFlightRequestsErrored(ctx, batch.UUID, staleBefore, unknownStatusResult()); err != nil {
		return err
	}

	for i := 0; i < batch.RequestCount; i++ {
		if err := pollHeartbeat(heartbeatErr); err != nil {
			return err
		}
		current, err := w.database.GetMessageBatchByUUID(ctx, batch.UUID)
		if err != nil {
			return err
		}
		if current.ProcessingStatus == "canceling" || time.Now().UTC().After(current.ExpiresAt) {
			break
		}
		req, err := w.database.GetMessageBatchRequestByIndex(ctx, batch.UUID, i)
		if err != nil {
			return err
		}
		if req.Status != "queued" {
			continue
		}
		ok, err := w.database.ClaimMessageBatchRequest(ctx, req.UUID, workerID, time.Now().UTC())
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		result, err := w.upstream.Send(ctx, batch, req)
		if err != nil {
			return err
		}
		if _, err := w.database.CompleteMessageBatchRequest(ctx, req.UUID, result.Status, result.Result, result.UpstreamRequestID, time.Now().UTC()); err != nil {
			return err
		}
	}

	current, err := w.database.GetMessageBatchByUUID(ctx, batch.UUID)
	if err != nil {
		return err
	}
	switch {
	case current.CancelInitiatedAt != nil || current.ProcessingStatus == "canceling":
		if err := w.database.FinalizePendingRequests(ctx, batch.UUID, "canceled", json.RawMessage(`{"type":"canceled"}`)); err != nil {
			return err
		}
	case time.Now().UTC().After(current.ExpiresAt):
		if err := w.database.FinalizePendingRequests(ctx, batch.UUID, "expired", json.RawMessage(`{"type":"expired"}`)); err != nil {
			return err
		}
	}

	processing, succeeded, errored, canceled, expired, err := w.database.CountRequestsByStatus(ctx, batch.UUID)
	if err != nil {
		return err
	}
	if processing > 0 {
		return fmt.Errorf("batch %s still has %d processing requests", batch.ExternalID, processing)
	}

	bucket, key, size, shaHex, err := w.uploadResults(ctx, batch)
	if err != nil {
		return err
	}
	if err := w.database.FinalizeMessageBatch(ctx, batch.UUID, 0, succeeded, errored, canceled, expired, bucket, key, size, shaHex, time.Now().UTC()); err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return w.database.CompleteMessageBatchJob(ctx, job.UUID)
		}
		return err
	}
	return w.database.CompleteMessageBatchJob(ctx, job.UUID)
}

func (w *Worker) startHeartbeat(ctx context.Context, jobUUID, workerID string) <-chan error {
	errCh := make(chan error, 1)
	interval := w.cfg.JobLeaseHeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	lease := w.cfg.JobLeaseDuration
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.database.ExtendMessageBatchJobLease(ctx, jobUUID, workerID, lease); err != nil {
					select {
					case errCh <- fmt.Errorf("extend batch job lease: %w", err):
					default:
					}
					return
				}
			}
		}
	}()
	return errCh
}

func pollHeartbeat(errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (w *Worker) uploadResults(ctx context.Context, batch db.MessageBatch) (bucket, key string, size int64, shaHex string, err error) {
	key = fmt.Sprintf("workspaces/%s/message_batches/%s/results.jsonl", batch.WorkspaceUUID, batch.UUID)
	// producer 和对象存储通过无缓冲 pipe 传输结果。uploadResults 持有读取端，
	// 因此必须保证所有返回路径都会关闭读取端。
	pr, pw := io.Pipe()
	defer pr.Close()

	// 使用子 context，使 Upload 提前返回时可以中断数据库读取；如果 producer
	// 已经阻塞在 Write，后续关闭 pipe 会负责解除阻塞。
	producerCtx, cancelProducer := context.WithCancel(ctx)
	defer cancelProducer()
	// 使用带缓冲的结果通道，避免 Upload 返回期间 producer 因上报结果而再次阻塞。
	producerDone := make(chan error, 1)
	go func() {
		producerErr := w.writeResultsJSONL(producerCtx, batch.UUID, pw)
		_ = pw.CloseWithError(producerErr)
		producerDone <- producerErr
	}()
	reader := newCountingHashReader(pr)
	_, uploadErr := w.store.Upload(ctx, key, reader, storage.UploadOptions{Size: -1, ContentType: "application/x-jsonl"})
	// Upload 可能在未消费完 body 时失败。等待 producer 前必须先关闭读取端，
	// 让阻塞在 PipeWriter.Write 的 producer 能够退出。
	cancelProducer()
	if uploadErr != nil {
		_ = pr.CloseWithError(uploadErr)
	} else {
		_ = pr.Close()
	}
	producerErr := <-producerDone
	if uploadErr != nil {
		// 优先保留真实的存储错误；producer 错误通常只是上述取消操作或
		// CloseWithError 派生出的后续错误。
		return "", "", 0, "", uploadErr
	}
	if producerErr != nil {
		return "", "", 0, "", producerErr
	}
	return w.store.Name(), key, reader.Size(), reader.SHA256Hex(), nil
}

func (w *Worker) writeResultsJSONL(ctx context.Context, batchUUID string, output io.Writer) error {
	requests, err := w.database.ListMessageBatchRequestsOrdered(ctx, batchUUID)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	for _, req := range requests {
		if len(req.Result) == 0 {
			return fmt.Errorf("request %s has no terminal result", req.CustomID)
		}
		line := map[string]json.RawMessage{
			"custom_id": mustMarshalString(req.CustomID),
			"result":    req.Result,
		}
		if err := encoder.Encode(line); err != nil {
			return err
		}
	}
	return nil
}

func mustMarshalString(value string) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func unknownStatusResult() json.RawMessage {
	errorResponse := errorResponse("api_error", "upstream request status is unknown after worker recovery; request was not retried", "")
	result, _ := json.Marshal(map[string]json.RawMessage{
		"type":  json.RawMessage(`"errored"`),
		"error": errorResponse,
	})
	return result
}

type countingHashReader struct {
	r    io.Reader
	n    int64
	hash hash.Hash
}

func newCountingHashReader(r io.Reader) *countingHashReader {
	return &countingHashReader{r: r, hash: sha256.New()}
}

func (r *countingHashReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.n += int64(n)
		_, _ = r.hash.Write(p[:n])
	}
	return n, err
}

func (r *countingHashReader) Size() int64 {
	return r.n
}

func (r *countingHashReader) SHA256Hex() string {
	return hex.EncodeToString(r.hash.Sum(nil))
}

func retryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(attempts*attempts) * time.Minute
}
