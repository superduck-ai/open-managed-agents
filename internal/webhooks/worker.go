package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type deliveryStore interface {
	FindWebhookDeliveryTarget(context.Context, string, string) (db.WebhookDeliveryTarget, bool, error)
	RecordWebhookDeliverySuccess(context.Context, string, string) error
	RecordWebhookDeliveryFailure(context.Context, string, string, string, bool, time.Duration) (bool, error)
}

// Worker processes bounded batches. Each instance owns at most one active batch.
type Worker struct {
	database deliveryStore
	queue    *Queue
	cfg      config.WebhookConfig
	logger   *slog.Logger
}

func NewWorker(database *db.DB, queue *Queue, cfg config.WebhookConfig, logger *slog.Logger) *Worker {
	return &Worker{database: database, queue: queue, cfg: cfg, logger: logging.LoggerOrDefault(logger)}
}

// Start returns an idempotent stop function which cancels and waits before the
// shared NATS connection or database may be closed, including startup failures.
func (w *Worker) Start(ctx context.Context) func() {
	if !w.cfg.WorkerEnabled {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "webhook consumption failed", "error", err)
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (w *Worker) RunOnce(ctx context.Context) error {
	timeout := webhookTimeout(w.cfg)
	ctx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	fetchCtx, stopFetch := context.WithTimeout(ctx, time.Second)
	defer stopFetch()
	batch, err := w.queue.consumer.Fetch(defaultBatchSize, jetstream.FetchContext(fetchCtx))
	if err != nil {
		return err
	}
	transport := newDeliveryTransport(ctx, w.cfg.AllowInsecure, timeout)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var pending sync.WaitGroup
	for msg := range batch.Messages() {
		pending.Go(func() { w.processMessage(ctx, client, msg) })
	}
	pending.Wait()
	if err := batch.Error(); err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, jetstream.ErrNoMessages) {
		return err
	}
	return ctx.Err()
}

func (w *Worker) processMessage(ctx context.Context, client *http.Client, msg jetstream.Msg) {
	var envelope Envelope
	if len(msg.Data()) > maxMessageBytes || json.Unmarshal(msg.Data(), &envelope) != nil {
		w.logger.WarnContext(ctx, "invalid webhook envelope")
		w.finish(ctx, msg)
		return
	}
	event, err := envelope.event()
	if err != nil {
		w.logger.WarnContext(ctx, "invalid webhook envelope", "error", err)
		w.finish(ctx, msg)
		return
	}
	metadata, err := msg.Metadata()
	if err != nil {
		w.logger.ErrorContext(ctx, "webhook message metadata unavailable", "error", err)
		return
	}
	target, found, err := w.database.FindWebhookDeliveryTarget(ctx, envelope.WorkspaceUUID, envelope.EndpointUUID)
	if err != nil {
		w.logger.ErrorContext(ctx, "webhook target lookup failed", "event_id", event.ID, "endpoint_uuid", envelope.EndpointUUID, "error", err)
		w.retry(ctx, msg, metadata.NumDelivered)
		return
	}
	if !found || target.Status != "enabled" || target.URL == "" || target.SigningSecret == "" {
		w.finish(ctx, msg)
		return
	}
	destination := deliveryTarget{URL: target.URL, SigningKey: target.SigningSecret, AllowInsecure: w.cfg.AllowInsecure}
	err = validateDeliveryTarget(destination, "webhook endpoint")
	if err == nil {
		err = deliver(ctx, client, destination, envelope.Event)
	}
	terminal := w.recordResult(ctx, envelope, err)
	if err == nil || terminal {
		w.finish(ctx, msg)
	} else {
		w.retry(ctx, msg, metadata.NumDelivered)
	}
}

func (w *Worker) recordResult(ctx context.Context, envelope Envelope, deliveryErr error) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var err error
	var disabled bool
	var failure deliveryFailure
	terminal := errors.As(deliveryErr, &failure) && failure.immediateDisable
	if deliveryErr == nil {
		err = w.database.RecordWebhookDeliverySuccess(ctx, envelope.WorkspaceUUID, envelope.EndpointUUID)
	} else {
		disabled, err = w.database.RecordWebhookDeliveryFailure(ctx, envelope.WorkspaceUUID, envelope.EndpointUUID, deliveryErr.Error(), terminal, webhookFailureDisableAfter(w.cfg))
	}
	if err != nil {
		w.logger.ErrorContext(ctx, "webhook statistics update failed", "endpoint_uuid", envelope.EndpointUUID, "error", err)
	}
	return terminal || disabled
}

func (w *Worker) finish(ctx context.Context, msg jetstream.Msg) {
	if err := msg.DoubleAck(ctx); err != nil {
		w.logger.ErrorContext(ctx, "webhook acknowledgment failed", "error", err)
	}
}
func (w *Worker) retry(ctx context.Context, msg jetstream.Msg, deliveries uint64) {
	if deliveries >= uint64(webhookMaxAttempts(w.cfg)) {
		w.finish(ctx, msg)
		return
	}
	if ctx.Err() != nil {
		return
	}
	if err := msg.NakWithDelay(retryDelay(int(min(deliveries, 5)), rand.Int64N)); err != nil {
		w.logger.ErrorContext(ctx, "webhook retry scheduling failed", "error", err)
	}
}
