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
	"log"
	"os"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const (
	defaultWorkerPollInterval = 5 * time.Second
	batchJobMaxAttempts       = 10
)

func StartBatchWorker(ctx context.Context, database *db.DB, store storage.ObjectStore, cfg config.Config) {
	workerID := fmt.Sprintf("message-batch-%d", os.Getpid())
	upstream := NewHTTPUpstreamClient(cfg)
	go func() {
		ticker := time.NewTicker(defaultWorkerPollInterval)
		defer ticker.Stop()
		for {
			if err := RunBatchOnce(ctx, database, store, cfg, upstream, workerID); err != nil {
				log.Printf("message batch worker: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func StartBatchExpirySweep(ctx context.Context, database *db.DB, cfg config.Config) {
	interval := cfg.Batch.ExpirySweepInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := RunBatchExpirySweepOnce(ctx, database, time.Now().UTC()); err != nil {
				log.Printf("message batch expiry sweep: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func RunBatchExpirySweepOnce(ctx context.Context, database *db.DB, now time.Time) error {
	batches, err := database.ListExpiredBatches(ctx, now, 100)
	if err != nil {
		return err
	}
	var errs []error
	for _, batch := range batches {
		if err := database.EnqueueMessageBatchJob(ctx, batch.WorkspaceID, batch.ID, batch.ExternalID); err != nil {
			errs = append(errs, fmt.Errorf("enqueue expired batch %s: %w", batch.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

func RunBatchOnce(ctx context.Context, database *db.DB, store storage.ObjectStore, cfg config.Config, upstream UpstreamClient, workerID string) error {
	jobs, err := database.LeaseMessageBatchJobs(ctx, workerID, cfg.Batch.WorkerConcurrency, cfg.Batch.JobLeaseDuration)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if err := processBatchJob(ctx, database, store, cfg, upstream, workerID, job); err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := database.FailMessageBatchJob(ctx, job.ID, job.Attempts, err.Error(), delay, batchJobMaxAttempts); markErr != nil {
				errs = append(errs, fmt.Errorf("mark batch job %s retry: %w", job.ExternalID, markErr))
			}
			errs = append(errs, fmt.Errorf("process batch job %s: %w", job.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

func processBatchJob(ctx context.Context, database *db.DB, store storage.ObjectStore, cfg config.Config, upstream UpstreamClient, workerID string, job db.MessageBatchJob) error {
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	heartbeatErr := startHeartbeat(heartbeatCtx, database, job.ID, workerID, cfg)

	batch, err := database.GetMessageBatchByID(ctx, job.MessageBatchID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return database.CompleteMessageBatchJob(ctx, job.ID)
		}
		return err
	}
	if batch.DeletedAt != nil || batch.ProcessingStatus == "ended" {
		return database.CompleteMessageBatchJob(ctx, job.ID)
	}

	staleBefore := time.Now().UTC().Add(-cfg.Batch.UpstreamTimeout - time.Minute)
	if cfg.Batch.UpstreamTimeout <= 0 {
		staleBefore = time.Now().UTC().Add(-11 * time.Minute)
	}
	if _, err := database.MarkStaleInFlightRequestsErrored(ctx, batch.ID, staleBefore, unknownStatusResult()); err != nil {
		return err
	}

	for i := 0; i < batch.RequestCount; i++ {
		if err := pollHeartbeat(heartbeatErr); err != nil {
			return err
		}
		current, err := database.GetMessageBatchByID(ctx, batch.ID)
		if err != nil {
			return err
		}
		if current.ProcessingStatus == "canceling" || time.Now().UTC().After(current.ExpiresAt) {
			break
		}
		req, err := database.GetMessageBatchRequestByIndex(ctx, batch.ID, i)
		if err != nil {
			return err
		}
		if req.Status != "queued" {
			continue
		}
		ok, err := database.ClaimMessageBatchRequest(ctx, req.ID, workerID, time.Now().UTC())
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		result, err := upstream.Send(ctx, batch, req)
		if err != nil {
			return err
		}
		if _, err := database.CompleteMessageBatchRequest(ctx, req.ID, result.Status, result.Result, result.UpstreamRequestID, time.Now().UTC()); err != nil {
			return err
		}
	}

	current, err := database.GetMessageBatchByID(ctx, batch.ID)
	if err != nil {
		return err
	}
	switch {
	case current.CancelInitiatedAt != nil || current.ProcessingStatus == "canceling":
		if err := database.FinalizePendingRequests(ctx, batch.ID, "canceled", json.RawMessage(`{"type":"canceled"}`)); err != nil {
			return err
		}
	case time.Now().UTC().After(current.ExpiresAt):
		if err := database.FinalizePendingRequests(ctx, batch.ID, "expired", json.RawMessage(`{"type":"expired"}`)); err != nil {
			return err
		}
	}

	processing, succeeded, errored, canceled, expired, err := database.CountRequestsByStatus(ctx, batch.ID)
	if err != nil {
		return err
	}
	if processing > 0 {
		return fmt.Errorf("batch %s still has %d processing requests", batch.ExternalID, processing)
	}

	bucket, key, size, shaHex, err := uploadResults(ctx, store, database, batch)
	if err != nil {
		return err
	}
	if err := database.FinalizeMessageBatch(ctx, batch.ID, 0, succeeded, errored, canceled, expired, bucket, key, size, shaHex, time.Now().UTC()); err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			return database.CompleteMessageBatchJob(ctx, job.ID)
		}
		return err
	}
	return database.CompleteMessageBatchJob(ctx, job.ID)
}

func startHeartbeat(ctx context.Context, database *db.DB, jobID int64, workerID string, cfg config.Config) <-chan error {
	errCh := make(chan error, 1)
	interval := cfg.Batch.JobLeaseHeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	lease := cfg.Batch.JobLeaseDuration
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
				if err := database.ExtendMessageBatchJobLease(ctx, jobID, workerID, lease); err != nil {
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

func uploadResults(ctx context.Context, store storage.ObjectStore, database *db.DB, batch db.MessageBatch) (bucket, key string, size int64, shaHex string, err error) {
	key = fmt.Sprintf("workspaces/%s/message_batches/%s/results.jsonl", batch.WorkspaceUUID, batch.UUID)
	// producer 和对象存储通过无缓冲 pipe 传输结果。uploadResults 持有读取端，
	// 因此必须保证所有返回路径都会关闭读取端。
	pr, pw := io.Pipe()
	defer pr.Close()

	// 使用子 context，使 Put 提前返回时可以中断数据库读取；如果 producer
	// 已经阻塞在 Write，后续关闭 pipe 会负责解除阻塞。
	producerCtx, cancelProducer := context.WithCancel(ctx)
	defer cancelProducer()
	// 使用带缓冲的结果通道，避免 Put 返回期间 producer 因上报结果而再次阻塞。
	producerDone := make(chan error, 1)
	go func() {
		producerErr := writeResultsJSONL(producerCtx, database, batch.ID, pw)
		_ = pw.CloseWithError(producerErr)
		producerDone <- producerErr
	}()
	reader := newCountingHashReader(pr)
	putErr := store.Put(ctx, key, reader, -1, "application/x-jsonl")
	// Put 可能在未消费完 body 时失败。等待 producer 前必须先关闭读取端，
	// 让阻塞在 PipeWriter.Write 的 producer 能够退出。
	cancelProducer()
	if putErr != nil {
		_ = pr.CloseWithError(putErr)
	} else {
		_ = pr.Close()
	}
	producerErr := <-producerDone
	if putErr != nil {
		// 优先保留真实的存储错误；producer 错误通常只是上述取消操作或
		// CloseWithError 派生出的后续错误。
		return "", "", 0, "", putErr
	}
	if producerErr != nil {
		return "", "", 0, "", producerErr
	}
	return store.Bucket(), key, reader.Size(), reader.SHA256Hex(), nil
}

func writeResultsJSONL(ctx context.Context, database *db.DB, batchID int64, w io.Writer) error {
	requests, err := database.ListMessageBatchRequestsOrdered(ctx, batchID)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
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
