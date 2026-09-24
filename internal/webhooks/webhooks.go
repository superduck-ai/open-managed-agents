package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
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
	timeout := webhookTimeout(w.cfg)
	ctx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	lease := max(defaultLeaseDuration, timeout+30*time.Second)
	jobs, err := w.database.LeaseWebhookDeliveryJobs(ctx, workerID, defaultBatchSize, lease)
	if err != nil {
		return err
	}
	transport := newDeliveryTransport(ctx, w.cfg.AllowInsecure, timeout)
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	errs := make([]error, len(jobs))
	var pending sync.WaitGroup
	for i, job := range jobs {
		pending.Go(func() {
			errs[i] = w.processJob(ctx, client, job)
		})
	}
	pending.Wait()
	return errors.Join(errs...)
}

func (w *Worker) processJob(ctx context.Context, client *http.Client, job db.WebhookDeliveryJob) error {
	target, skip, deliveryErr := targetForJob(w.cfg, job)
	var applied bool
	var err error
	switch {
	case skip:
		applied, err = w.database.CompleteWebhookDeliveryJob(ctx, job, false)
	case job.Attempts >= webhookMaxAttempts(w.cfg):
		applied, err = w.database.ExhaustWebhookDeliveryJob(ctx, job)
	default:
		if deliveryErr == nil {
			deliveryErr = deliver(ctx, client, target, job.Event)
		}
		if deliveryErr == nil {
			applied, err = w.database.CompleteWebhookDeliveryJob(ctx, job, true)
		} else {
			result := db.WebhookDeliveryFailure{Reason: deliveryErr.Error(), MaxAttempts: webhookMaxAttempts(w.cfg), DisableAfter: webhookFailureDisableAfter(w.cfg)}
			var failure deliveryFailure
			if errors.As(deliveryErr, &failure) && failure.immediateDisable {
				result.Terminal = true
			}
			if !result.Terminal && job.Attempts+1 < result.MaxAttempts {
				result.RetryDelay = retryDelay(job.Attempts+1, rand.Int64N)
			}
			applied, err = w.database.FailWebhookDeliveryJob(ctx, job, result)
		}
	}
	if err != nil {
		return fmt.Errorf("record webhook job %s result: %w", job.ExternalID, err)
	}
	if !applied {
		w.logger.DebugContext(ctx, "webhook claim no longer current", "job_id", job.ExternalID)
	}
	return nil
}

func targetForJob(cfg config.WebhookConfig, job db.WebhookDeliveryJob) (deliveryTarget, bool, error) {
	if job.WebhookEndpointUUID != nil {
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
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return deliveryFailure{reason: redirectReason, immediateDisable: true}
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
	if err := validateWebhookURL(target.URL, target.AllowInsecure); err != nil {
		if errors.Is(err, errWebhookURLPrivate) {
			return deliveryFailure{reason: invalidAddressReason, immediateDisable: true}
		}
		return deliveryFailure{reason: err.Error(), immediateDisable: true}
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

// retryDelay randomizes the next eligibility time; the worker never sleeps here.
func retryDelay(attempts int, randomN func(int64) int64) time.Duration {
	attempts = min(max(attempts, 1), 5)
	upper := min(120*time.Second, 5*time.Second<<attempts)
	return 5*time.Second + time.Duration(randomN(int64(upper-5*time.Second)))
}

func webhookTimeout(cfg config.WebhookConfig) time.Duration {
	if cfg.Timeout <= 0 {
		return 10 * time.Second
	}
	return cfg.Timeout
}

func webhookMaxAttempts(cfg config.WebhookConfig) int {
	if cfg.MaxAttempts <= 0 {
		return 3
	}
	return cfg.MaxAttempts
}

func webhookFailureDisableAfter(cfg config.WebhookConfig) time.Duration {
	if cfg.FailureDisableAfter <= 0 {
		return 24 * time.Hour
	}
	return cfg.FailureDisableAfter
}
