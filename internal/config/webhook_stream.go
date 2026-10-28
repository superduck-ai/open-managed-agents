package config

import "fmt"

func validateStreamLimits(name string, cfg WebhookStreamConfig) error {
	if cfg.MaxBytes <= 0 {
		return fmt.Errorf("%s.max_bytes must be positive", name)
	}
	if cfg.MaxAge <= 0 {
		return fmt.Errorf("%s.max_age must be positive", name)
	}
	if cfg.Replicas < 1 || cfg.Replicas > 5 {
		return fmt.Errorf("%s.replicas must be between 1 and 5", name)
	}
	return nil
}
