package telemetryexport

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
	"github.com/superduck-ai/open-managed-agents/internal/jetstreamconsume"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/telemetrydestinations"
)

type DeliveryStore interface {
	GetTelemetryDestinationByUUID(context.Context, string) (db.TelemetryDestination, error)
	RecordTelemetryDeliverySuccess(context.Context, string, time.Time) error
	RecordTelemetryDeliveryFailure(context.Context, string, string, string, time.Duration, time.Time) (bool, error)
}

type Opener interface {
	Open(context.Context, secrets.Binding, secrets.Envelope) ([]byte, error)
}

type Worker struct {
	store  DeliveryStore
	opener Opener
	queue  *Queue
	cfg    config.TelemetryExportConfig
	logger *slog.Logger
}

func NewWorker(store DeliveryStore, opener Opener, queue *Queue, cfg config.TelemetryExportConfig, logger *slog.Logger) *Worker {
	return &Worker{store: store, opener: opener, queue: queue, cfg: cfg, logger: logging.LoggerOrDefault(logger)}
}

func (w *Worker) Start(ctx context.Context) (func(), error) {
	if !w.cfg.WorkerEnabled {
		return func() {}, nil
	}
	client, transport := newDeliveryClient(ctx, w.cfg.AllowInsecure, w.cfg.Timeout, w.cfg.Concurrency)
	runtime := jetstreamconsume.New(ctx, jetstreamconsume.Options{
		Name: "telemetry_export", Consumer: w.queue.consumer, ConnClosed: func() bool { return w.queue.js.Conn().IsClosed() },
		Concurrency: w.cfg.Concurrency, MessageTimeout: 2*w.cfg.Timeout + 15*time.Second, Logger: w.logger,
		Handle: func(ctx context.Context, msg jetstream.Msg) { w.process(ctx, client, msg) },
		OnStop: transport.CloseIdleConnections,
	})
	if err := runtime.Start(); err != nil {
		return nil, err
	}
	return runtime.Stop, nil
}

func destinationHeaders(ctx context.Context, opener Opener, destination db.TelemetryDestination) (map[string]string, error) {
	headers := map[string]string{}
	if destination.Headers == nil {
		return headers, nil
	}
	plaintext, err := opener.Open(ctx, telemetrydestinations.HeaderBinding(destination.OrganizationUUID, destination.ExternalID), *destination.Headers)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(plaintext, &headers); err != nil {
		return nil, err
	}
	return headers, nil
}

func (w *Worker) process(ctx context.Context, client *http.Client, msg jetstream.Msg) {
	destinationUUID := msg.Headers().Get(destinationHeader)
	metadata, err := msg.Metadata()
	if err != nil {
		w.logger.ErrorContext(ctx, "telemetry delivery metadata unavailable", "error", err)
		return
	}
	destination, err := w.store.GetTelemetryDestinationByUUID(ctx, destinationUUID)
	if errors.Is(err, db.ErrNotFound) {
		w.finish(ctx, msg)
		return
	}
	if err != nil {
		w.logger.ErrorContext(ctx, "telemetry destination lookup failed", "destination_uuid", destinationUUID, "error", err)
		w.retry(ctx, msg, metadata.NumDelivered)
		return
	}
	if destination.State != "enabled" || !destination.TracesEnabled || msg.Headers().Get(contentPolicyHeader) != contentPolicy(destination) {
		w.finish(ctx, msg)
		return
	}
	headers, err := destinationHeaders(ctx, w.opener, destination)
	if err != nil {
		w.logger.ErrorContext(ctx, "telemetry destination headers unavailable", "destination_uuid", destinationUUID, "error", err)
		w.retry(ctx, msg, metadata.NumDelivered)
		return
	}
	result := deliver(ctx, client, destination.TracesEndpointURL, headers, msg.Data())
	if ctx.Err() != nil {
		return
	}
	disabled := w.record(ctx, destination, result)
	if !result.failed() || !result.retryable || disabled {
		w.finish(ctx, msg)
		return
	}
	w.retry(ctx, msg, metadata.NumDelivered)
}

func (w *Worker) record(ctx context.Context, destination db.TelemetryDestination, result outcome) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if !result.failed() {
		if err := w.store.RecordTelemetryDeliverySuccess(ctx, destination.UUID, now); err != nil {
			w.logger.ErrorContext(ctx, "telemetry delivery health update failed", "destination_uuid", destination.UUID, "error", err)
		}
		return false
	}
	disabled, err := w.store.RecordTelemetryDeliveryFailure(ctx, destination.UUID, result.err.Error(), result.disableReason, w.cfg.FailureDisableAfter, now)
	if err != nil {
		w.logger.ErrorContext(ctx, "telemetry delivery health update failed", "destination_uuid", destination.UUID, "error", err)
	}
	if disabled {
		w.logger.WarnContext(ctx, "telemetry destination disabled", "destination_uuid", destination.UUID, "organization_uuid", destination.OrganizationUUID)
	}
	return disabled
}

func (w *Worker) finish(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		return
	}
	if err := msg.DoubleAck(ctx); err != nil {
		w.logger.ErrorContext(ctx, "telemetry delivery acknowledgment failed", "error", err)
	}
}

func (w *Worker) retry(ctx context.Context, msg jetstream.Msg, deliveries uint64) {
	if deliveries >= uint64(w.cfg.MaxAttempts) {
		w.finish(ctx, msg)
		return
	}
	if ctx.Err() != nil {
		return
	}
	delay := time.Second<<min(deliveries, 4) + time.Duration(rand.Int64N(int64(time.Second)))
	if err := msg.NakWithDelay(delay); err != nil {
		w.logger.ErrorContext(ctx, "telemetry delivery retry scheduling failed", "error", err)
	}
}
