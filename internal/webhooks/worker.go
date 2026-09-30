package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
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

type Worker struct {
	database deliveryStore
	queue    *Queue
	cfg      config.WebhookConfig
	logger   *slog.Logger
}

func NewWorker(database *db.DB, queue *Queue, cfg config.WebhookConfig, logger *slog.Logger) *Worker {
	return &Worker{database: database, queue: queue, cfg: cfg, logger: logging.LoggerOrDefault(logger)}
}

func (w *Worker) Start(ctx context.Context) (func(), error) {
	if !w.cfg.WorkerEnabled {
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime := newWorkerRuntime(ctx, w)
	for index := range runtime.sessions {
		session, err := runtime.consume(index)
		if err != nil {
			runtime.stop()
			return nil, err
		}
		runtime.sessions[index] = session
	}
	for index, session := range runtime.sessions {
		runtime.pending.Go(func() { runtime.runSession(index, session) })
	}
	go func() {
		select {
		case <-runtime.ctx.Done():
			runtime.stop()
		case <-runtime.done:
		}
	}()
	return runtime.stop, nil
}

func (w *Worker) processMessage(ctx context.Context, client *http.Client, msg jetstream.Msg) {
	if ctx.Err() != nil {
		return
	}
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
	if ctx.Err() != nil {
		return
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
	if ctx.Err() != nil {
		return
	}
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
