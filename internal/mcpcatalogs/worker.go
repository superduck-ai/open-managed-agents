package mcpcatalogs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	discoveryMinimumLeaseDuration = 30 * time.Second
	discoveryLeaseCleanupMargin   = 30 * time.Second
	discoveryFreshTTL             = 10 * time.Minute
	discoveryPollInterval         = time.Second
	discoveryMaxAttempts          = 4
	defaultDiscoveryProbeTimeout  = 10 * time.Second
)

type WorkerStore interface {
	LeaseMCPToolDiscoveryJobs(context.Context, string, int, time.Duration) ([]db.MCPToolDiscoveryJob, error)
	CompleteMCPToolDiscovery(context.Context, db.CompleteMCPToolDiscoveryInput) error
	FailMCPToolDiscovery(context.Context, db.FailMCPToolDiscoveryInput) error
	RunMCPToolCatalogRetention(context.Context, time.Time) error
}

type Worker struct {
	store    WorkerStore
	prober   Prober
	cfg      config.Config
	workerID string
}

func NewWorker(store WorkerStore, cfg config.Config) *Worker {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "local"
	}
	return &Worker{
		store:    store,
		prober:   Prober{},
		cfg:      cfg,
		workerID: fmt.Sprintf("mcp-catalog-%s-%d", hostname, os.Getpid()),
	}
}

func StartWorker(ctx context.Context, store WorkerStore, cfg config.Config) {
	if !cfg.MCPDiscoveryEnabled {
		return
	}
	worker := NewWorker(store, cfg)
	// 任务消费与缓存保留策略使用独立循环：探测按秒轮询，清理启动时执行一次后按天运行。
	go worker.run(ctx)
	go worker.runRetention(ctx)
}

func (w *Worker) run(ctx context.Context) {
	ticker := time.NewTicker(discoveryPollInterval)
	defer ticker.Stop()
	for {
		if err := w.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("mcp catalog worker: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) error {
	limit := w.cfg.MCPDiscoveryWorkerConcurrency
	if limit <= 0 {
		limit = 3
	}
	jobs, err := w.store.LeaseMCPToolDiscoveryJobs(ctx, w.workerID, limit, discoveryLeaseDuration(w.cfg.MCPDiscoveryProbeTimeout))
	if err != nil {
		return err
	}
	done := make(chan struct{}, limit)
	for _, job := range jobs {
		job := job
		go func() {
			defer func() { done <- struct{}{} }()
			w.process(ctx, job)
		}()
	}
	for range jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
	return nil
}

func (w *Worker) process(ctx context.Context, job db.MCPToolDiscoveryJob) {
	// 每次外部探测都有独立超时；job lease 会额外预留落库时间，因此正常情况下不会在结算前过期。
	// 数据库仍以 locked_by 和 generation 作为最终围栏；结算返回 ErrNotFound 表示当前 worker 已失去所有权，可直接忽略。
	timeout := discoveryProbeTimeout(w.cfg.MCPDiscoveryProbeTimeout)
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	result, err := w.prober.Probe(probeCtx, job.EndpointURL)
	cancel()
	now := time.Now().UTC()
	if err != nil {
		code, message, retryable := probeErrorDetails(err)
		delay := retryDelay(job.Attempts)
		if !retryable {
			delay = terminalRetryDelay(code)
		}
		if failErr := w.store.FailMCPToolDiscovery(ctx, db.FailMCPToolDiscoveryInput{
			JobID:             job.ID,
			WorkerID:          w.workerID,
			CatalogExternalID: job.CatalogExternalID,
			Generation:        job.Generation,
			Attempts:          job.Attempts,
			MaxAttempts:       discoveryMaxAttempts,
			Retryable:         retryable,
			RetryDelay:        delay,
			ErrorCode:         code,
			ErrorMessage:      message,
			Now:               now,
		}); failErr != nil && !errors.Is(failErr, db.ErrNotFound) {
			log.Printf("mcp catalog settle failure catalog=%s generation=%d code=%s: %v", job.CatalogExternalID, job.Generation, code, failErr)
		}
		return
	}
	tools, marshalErr := json.Marshal(result.Tools)
	if marshalErr != nil {
		log.Printf("mcp catalog marshal catalog=%s generation=%d: %v", job.CatalogExternalID, job.Generation, marshalErr)
		return
	}
	hash := sha256.Sum256(tools)
	if completeErr := w.store.CompleteMCPToolDiscovery(ctx, db.CompleteMCPToolDiscoveryInput{
		JobID:             job.ID,
		WorkerID:          w.workerID,
		CatalogExternalID: job.CatalogExternalID,
		Generation:        job.Generation,
		Tools:             tools,
		ProtocolVersion:   result.ProtocolVersion,
		ServerInfo:        result.ServerInfo,
		CatalogHash:       hex.EncodeToString(hash[:]),
		DiscoveredAt:      now,
		ExpiresAt:         now.Add(discoveryFreshTTL),
	}); completeErr != nil && !errors.Is(completeErr, db.ErrNotFound) {
		log.Printf("mcp catalog settle success catalog=%s generation=%d: %v", job.CatalogExternalID, job.Generation, completeErr)
	}
}

func discoveryProbeTimeout(configured time.Duration) time.Duration {
	if configured <= 0 {
		return defaultDiscoveryProbeTimeout
	}
	return configured
}

func discoveryLeaseDuration(configuredProbeTimeout time.Duration) time.Duration {
	// lease 必须覆盖完整探测时间并预留数据库落盘余量；否则结果提交前任务可能被
	// 另一实例重新领取，导致同一 generation 被多个 worker 并发执行。
	probeTimeout := discoveryProbeTimeout(configuredProbeTimeout)
	if probeTimeout > time.Duration(1<<63-1)-discoveryLeaseCleanupMargin {
		return time.Duration(1<<63 - 1)
	}
	lease := probeTimeout + discoveryLeaseCleanupMargin
	if lease < discoveryMinimumLeaseDuration {
		return discoveryMinimumLeaseDuration
	}
	return lease
}

func (w *Worker) runRetention(ctx context.Context) {
	if err := w.store.RunMCPToolCatalogRetention(ctx, time.Now().UTC()); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("mcp catalog retention: %v", err)
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := w.store.RunMCPToolCatalogRetention(ctx, now.UTC()); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("mcp catalog retention: %v", err)
			}
		}
	}
}

func retryDelay(attempts int) time.Duration {
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	if attempts < 0 {
		attempts = 0
	}
	if attempts >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempts]
}

func terminalRetryDelay(code string) time.Duration {
	switch code {
	case "auth_required":
		return 30 * time.Minute
	default:
		return time.Hour
	}
}
