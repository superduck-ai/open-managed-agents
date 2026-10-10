package config

import (
	"errors"
	"time"
)

func validateWorkerEventStream(cfg WorkerEventStreamConfig) error {
	if cfg.ConsumerInactiveThreshold <= 0 {
		return errors.New("nats.worker_event_stream.consumer_inactive_threshold must be positive")
	}
	if cfg.MaxBytes <= 0 {
		return errors.New("nats.worker_event_stream.max_bytes must be positive")
	}
	if cfg.MaxAge != 0 && cfg.MaxAge < 100*time.Millisecond {
		return errors.New("nats.worker_event_stream.max_age must be zero or at least 100ms")
	}
	if cfg.Replicas < 1 || cfg.Replicas > 5 {
		return errors.New("nats.worker_event_stream.replicas must be between 1 and 5")
	}
	if cfg.MaxMsgSize <= 0 {
		return errors.New("nats.worker_event_stream.max_msg_size must be positive")
	}
	return nil
}
