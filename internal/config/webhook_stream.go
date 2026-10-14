package config

import "errors"

func validateWebhookStream(cfg WebhookStreamConfig) error {
	if cfg.MaxBytes <= 0 {
		return errors.New("nats.webhook_stream.max_bytes must be positive")
	}
	if cfg.MaxAge <= 0 {
		return errors.New("nats.webhook_stream.max_age must be positive")
	}
	if cfg.Replicas < 1 || cfg.Replicas > 5 {
		return errors.New("nats.webhook_stream.replicas must be between 1 and 5")
	}
	return nil
}
