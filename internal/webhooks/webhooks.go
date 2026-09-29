package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return deliveryFailure{reason: redirectReason, immediateDisable: true}
		}
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
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
