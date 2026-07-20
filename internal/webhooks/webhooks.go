package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

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

func Enqueue(ctx context.Context, database *db.DB, cfg config.WebhookConfig, workspaceID int64, organizationExternalID, workspaceExternalID, eventType, resourceID string, sessionThreadID *string) {
	EnqueueWithOptions(ctx, database, cfg, workspaceID, organizationExternalID, workspaceExternalID, eventType, resourceID, EventOptions{SessionThreadID: sessionThreadID})
}

func EnqueueWithOptions(ctx context.Context, database *db.DB, cfg config.WebhookConfig, workspaceID int64, organizationExternalID, workspaceExternalID, eventType, resourceID string, options EventOptions) {
	if database == nil {
		return
	}
	eventID, err := ids.New("wevt_")
	if err != nil {
		log.Printf("webhook event id: %v", err)
		return
	}
	event := Event{
		ID:        eventID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Data: EventData{
			ID:              resourceID,
			OrganizationID:  organizationExternalID,
			Type:            eventType,
			WorkspaceID:     workspaceExternalID,
			SessionThreadID: options.SessionThreadID,
			VaultID:         options.VaultID,
		},
		Type: "event",
	}
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("marshal webhook event type=%s id=%s: %v", eventType, resourceID, err)
		return
	}

	hasEndpoints, err := database.HasWebhookEndpoints(ctx, workspaceID)
	if err != nil {
		log.Printf("load webhook endpoint configuration workspace_id=%d: %v", workspaceID, err)
		return
	}
	if hasEndpoints {
		endpoints, err := database.ListActiveWebhookEndpointsForEvent(ctx, workspaceID, eventType)
		if err != nil {
			log.Printf("list webhook endpoints event type=%s workspace_id=%d: %v", eventType, workspaceID, err)
			return
		}
		for _, endpoint := range endpoints {
			if err := database.EnqueueWebhookDeliveryJobForEndpoint(ctx, workspaceID, eventType, payload, endpoint.ID); err != nil {
				log.Printf("enqueue webhook endpoint=%s event type=%s id=%s: %v", endpoint.ExternalID, eventType, resourceID, err)
			}
		}
		return
	}

	if !enabled(cfg) || !subscribed(cfg, eventType) {
		return
	}
	if err := database.EnqueueWebhookDeliveryJob(ctx, workspaceID, eventType, payload); err != nil {
		log.Printf("enqueue webhook event type=%s id=%s: %v", eventType, resourceID, err)
	}
}

func StartWorker(ctx context.Context, database *db.DB, cfg config.WebhookConfig) {
	if !cfg.WorkerEnabled {
		return
	}
	workerID := fmt.Sprintf("webhook-delivery-%d", os.Getpid())
	go func() {
		ticker := time.NewTicker(defaultWorkerInterval)
		defer ticker.Stop()
		for {
			if err := RunOnce(ctx, database, cfg, workerID); err != nil {
				log.Printf("webhook delivery worker: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func RunOnce(ctx context.Context, database *db.DB, cfg config.WebhookConfig, workerID string) error {
	jobs, err := database.LeaseWebhookDeliveryJobs(ctx, workerID, defaultBatchSize, defaultLeaseDuration)
	if err != nil {
		return err
	}
	var errs []error
	client := &http.Client{
		Timeout: webhookTimeout(cfg),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, job := range jobs {
		target, skip, err := targetForJob(cfg, job)
		if skip {
			if err := database.CompleteWebhookDeliveryJob(ctx, job.ID); err != nil {
				errs = append(errs, fmt.Errorf("complete skipped webhook job %s: %w", job.ExternalID, err))
			}
			continue
		}
		if err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := database.FailWebhookDeliveryJob(ctx, job.ID, job.Attempts, err.Error(), delay, webhookMaxAttempts(cfg)); markErr != nil {
				errs = append(errs, fmt.Errorf("mark invalid webhook job %s retry: %w", job.ExternalID, markErr))
			}
			recordEndpointFailure(ctx, database, job, err)
			continue
		}
		if err := deliver(ctx, client, target, job.Event); err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := database.FailWebhookDeliveryJob(ctx, job.ID, job.Attempts, err.Error(), delay, webhookMaxAttempts(cfg)); markErr != nil {
				errs = append(errs, fmt.Errorf("mark webhook job %s retry: %w", job.ExternalID, markErr))
			}
			recordEndpointFailure(ctx, database, job, err)
			continue
		}
		if err := database.CompleteWebhookDeliveryJob(ctx, job.ID); err != nil {
			errs = append(errs, fmt.Errorf("complete webhook job %s: %w", job.ExternalID, err))
		}
		if job.WebhookEndpointID != nil {
			if err := database.RecordWebhookEndpointDeliverySuccess(ctx, *job.WebhookEndpointID); err != nil {
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

func recordEndpointFailure(ctx context.Context, database *db.DB, job db.WebhookDeliveryJob, err error) {
	if job.WebhookEndpointID == nil {
		return
	}
	disableAfter := autoDisableFailures
	var failure deliveryFailure
	if errors.As(err, &failure) && failure.immediateDisable {
		disableAfter = 1
	}
	if recordErr := database.RecordWebhookEndpointDeliveryFailure(ctx, *job.WebhookEndpointID, err.Error(), disableAfter); recordErr != nil {
		log.Printf("record webhook endpoint %s failure: %v", job.WebhookEndpointExternalID, recordErr)
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
