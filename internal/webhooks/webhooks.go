package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
)

const (
	defaultWorkerInterval = 5 * time.Second
	defaultLeaseDuration  = time.Minute
	defaultBatchSize      = 10
	autoDisableFailures   = 20
)

type EventData struct {
	ID              string  `json:"id"`
	OrganizationID  string  `json:"organization_id"`
	Type            string  `json:"type"`
	WorkspaceID     string  `json:"workspace_id"`
	SessionThreadID *string `json:"session_thread_id,omitempty"`
	VaultID         *string `json:"vault_id,omitempty"`
}

type Event struct {
	ID        string    `json:"id"`
	CreatedAt string    `json:"created_at"`
	Data      EventData `json:"data"`
	Type      string    `json:"type"`
}

type EventOptions struct {
	SessionThreadID *string
	VaultID         *string
}

type deliveryTarget struct {
	URL           string
	SigningKey    string
	AllowInsecure bool
}

type deliveryFailure struct {
	reason           string
	immediateDisable bool
}

func (e deliveryFailure) Error() string {
	return e.reason
}

// Worker owns the webhook delivery loop and its stable dependencies.
type Worker struct {
	database *db.DB
	cfg      config.WebhookConfig
	logger   *slog.Logger
}

// NewWorker constructs a webhook delivery worker.
func NewWorker(database *db.DB, cfg config.WebhookConfig, logger *slog.Logger) *Worker {
	return &Worker{
		database: database,
		cfg:      cfg,
		logger:   logging.LoggerOrDefault(logger),
	}
}

// Start launches the webhook delivery loop when it is enabled.
func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.database == nil || !w.cfg.WorkerEnabled {
		return
	}
	workerID := fmt.Sprintf("webhook-delivery-%d", os.Getpid())
	go func() {
		ticker := time.NewTicker(defaultWorkerInterval)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx, workerID); err != nil {
				w.logger.ErrorContext(ctx, "webhook delivery worker", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// RunOnce leases and processes one batch of webhook delivery jobs.
func (w *Worker) RunOnce(ctx context.Context, workerID string) error {
	jobs, err := w.database.LeaseWebhookDeliveryJobs(ctx, workerID, defaultBatchSize, defaultLeaseDuration)
	if err != nil {
		return err
	}
	var errs []error
	client := &http.Client{
		Timeout: webhookTimeout(w.cfg),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, job := range jobs {
		target, skip, err := targetForJob(w.cfg, job)
		if skip {
			if err := w.database.CompleteWebhookDeliveryJob(ctx, job.ID); err != nil {
				errs = append(errs, fmt.Errorf("complete skipped webhook job %s: %w", job.ExternalID, err))
			}
			continue
		}
		if err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := w.database.FailWebhookDeliveryJob(ctx, job.ID, job.Attempts, err.Error(), delay, webhookMaxAttempts(w.cfg)); markErr != nil {
				errs = append(errs, fmt.Errorf("mark invalid webhook job %s retry: %w", job.ExternalID, markErr))
			}
			w.recordEndpointFailure(ctx, job, err)
			continue
		}
		if err := deliver(ctx, client, target, job.Event); err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := w.database.FailWebhookDeliveryJob(ctx, job.ID, job.Attempts, err.Error(), delay, webhookMaxAttempts(w.cfg)); markErr != nil {
				errs = append(errs, fmt.Errorf("mark webhook job %s retry: %w", job.ExternalID, markErr))
			}
			w.recordEndpointFailure(ctx, job, err)
			continue
		}
		if err := w.database.CompleteWebhookDeliveryJob(ctx, job.ID); err != nil {
			errs = append(errs, fmt.Errorf("complete webhook job %s: %w", job.ExternalID, err))
		}
		if job.WebhookEndpointID != nil {
			if err := w.database.RecordWebhookEndpointDeliverySuccess(ctx, *job.WebhookEndpointID); err != nil {
				errs = append(errs, fmt.Errorf("record webhook endpoint %s success: %w", job.WebhookEndpointExternalID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func targetForJob(cfg config.WebhookConfig, job db.WebhookDeliveryJob) (deliveryTarget, bool, error) {
	if job.WebhookEndpointID != nil {
		if job.WebhookEndpointStatus != "enabled" || job.WebhookEndpointURL == "" || job.WebhookEndpointSecret == "" {
			return deliveryTarget{}, true, nil
		}
		target := deliveryTarget{
			URL:           job.WebhookEndpointURL,
			SigningKey:    job.WebhookEndpointSecret,
			AllowInsecure: cfg.AllowInsecure,
		}
		return target, false, validateDeliveryTarget(target, "webhook endpoint")
	}
	if !enabled(cfg) || !subscribed(cfg, job.EventType) {
		return deliveryTarget{}, true, nil
	}
	target := deliveryTarget{
		URL:           cfg.EndpointURL,
		SigningKey:    cfg.SigningKey,
		AllowInsecure: cfg.AllowInsecure,
	}
	return target, false, validateDeliveryTarget(target, "webhook.endpoint_url")
}

func (w *Worker) recordEndpointFailure(ctx context.Context, job db.WebhookDeliveryJob, err error) {
	if job.WebhookEndpointID == nil {
		return
	}
	disableAfter := autoDisableFailures
	var failure deliveryFailure
	if errors.As(err, &failure) && failure.immediateDisable {
		disableAfter = 1
	}
	if recordErr := w.database.RecordWebhookEndpointDeliveryFailure(ctx, *job.WebhookEndpointID, err.Error(), disableAfter); recordErr != nil {
		w.logger.ErrorContext(ctx, "record webhook endpoint failure", "endpoint_id", job.WebhookEndpointExternalID, "error", recordErr)
	}
}

func deliver(ctx context.Context, client *http.Client, target deliveryTarget, payload []byte) error {
	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("invalid webhook payload: %w", err)
	}
	messageID := event.ID
	if messageID == "" {
		messageID = "wevt_unknown"
	}
	timestamp := time.Now().UTC()
	wh, err := standardwebhooks.NewWebhook(target.SigningKey)
	if err != nil {
		return fmt.Errorf("create webhook signer: %w", err)
	}
	signature, err := wh.Sign(messageID, timestamp, payload)
	if err != nil {
		return fmt.Errorf("sign webhook: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	timestampHeader := strconv.FormatInt(timestamp.Unix(), 10)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("webhook-id", messageID)
	req.Header.Set("webhook-timestamp", timestampHeader)
	req.Header.Set("webhook-signature", signature)
	req.Header.Set("X-Webhook-Id", messageID)
	req.Header.Set("X-Webhook-Timestamp", timestampHeader)
	req.Header.Set("X-Webhook-Signature", signature)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return deliveryFailure{reason: fmt.Sprintf("webhook status %d", resp.StatusCode), immediateDisable: true}
		}
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

func enabled(cfg config.WebhookConfig) bool {
	return cfg.WorkerEnabled && cfg.EndpointURL != "" && cfg.SigningKey != ""
}

func validateDeliveryTarget(target deliveryTarget, name string) error {
	if target.URL == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if target.SigningKey == "" {
		return errors.New("webhook signing key is empty")
	}
	parsed, err := url.Parse(target.URL)
	if err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed.Scheme != "https" && !target.AllowInsecure {
		return fmt.Errorf("%s must be https unless webhook.allow_insecure is true", name)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s must include a host", name)
	}
	if isPrivateWebhookHost(parsed.Hostname()) && !target.AllowInsecure {
		return deliveryFailure{reason: fmt.Sprintf("%s host must be publicly routable unless webhook.allow_insecure is true", name), immediateDisable: true}
	}
	return nil
}

func subscribed(cfg config.WebhookConfig, eventType string) bool {
	if len(cfg.EventTypes) == 0 {
		return true
	}
	for _, subscribed := range cfg.EventTypes {
		if subscribed == eventType {
			return true
		}
	}
	return false
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

func webhookTimeout(cfg config.WebhookConfig) time.Duration {
	if cfg.Timeout <= 0 {
		return 10 * time.Second
	}
	return cfg.Timeout
}

func webhookMaxAttempts(cfg config.WebhookConfig) int {
	if cfg.MaxAttempts <= 0 {
		return 10
	}
	return cfg.MaxAttempts
}
