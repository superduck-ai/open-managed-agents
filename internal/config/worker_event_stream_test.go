package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWorkerEventStream(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want WorkerEventStreamConfig
		err  string
	}{
		{name: "zero inactivity", yaml: "consumer_inactive_threshold: 0s", err: "consumer_inactive_threshold must be positive"},
		{name: "negative inactivity", yaml: "consumer_inactive_threshold: -1s", err: "consumer_inactive_threshold must be positive"},
		{name: "inactivity override", yaml: "consumer_inactive_threshold: 2m", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 2 * time.Minute, MaxBytes: 1 << 28, MaxMsgSize: 1 << 20, Replicas: 3}},
		{name: "zero capacity", yaml: "max_bytes: 0", err: "max_bytes must be positive"},
		{name: "negative capacity", yaml: "max_bytes: -1", err: "max_bytes must be positive"},
		{name: "zero replicas", yaml: "replicas: 0", err: "replicas must be between 1 and 5"},
		{name: "negative replicas", yaml: "replicas: -1", err: "replicas must be between 1 and 5"},
		{name: "too many replicas", yaml: "replicas: 6", err: "replicas must be between 1 and 5"},
		{name: "negative age", yaml: "max_age: -1s", err: "max_age must be zero or at least 100ms"},
		{name: "short age", yaml: "max_age: 1ms", err: "max_age must be zero or at least 100ms"},
		{name: "zero message size", yaml: "max_msg_size: 0", err: "max_msg_size must be positive"},
		{name: "negative message size", yaml: "max_msg_size: -1", err: "max_msg_size must be positive"},
		{name: "zero age", yaml: "max_age: 0s", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, MaxBytes: 1 << 28, MaxMsgSize: 1 << 20, Replicas: 3}},
		{name: "age override", yaml: "max_age: 1h", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, MaxAge: time.Hour, MaxBytes: 1 << 28, MaxMsgSize: 1 << 20, Replicas: 3}},
		{name: "message size override", yaml: "max_msg_size: 2097152", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, MaxBytes: 1 << 28, MaxMsgSize: 2 << 20, Replicas: 3}},
		{name: "defaults", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, MaxMsgSize: 1 << 20, MaxBytes: 1 << 28, Replicas: 3}},
		{name: "single node", yaml: "replicas: 1", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, MaxMsgSize: 1 << 20, MaxBytes: 1 << 28, Replicas: 1}},
		{name: "capacity override", yaml: "max_bytes: 10737418240", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, MaxMsgSize: 1 << 20, MaxBytes: 10 << 30, Replicas: 3}},
		{name: "both overrides", yaml: "max_bytes: 2097152\n    replicas: 5", want: WorkerEventStreamConfig{ConsumerInactiveThreshold: 5 * time.Minute, MaxMsgSize: 1 << 20, MaxBytes: 2 << 20, Replicas: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareLoadTest(t)
			yaml := "{}"
			if tc.yaml != "" {
				yaml = "nats:\n  worker_event_stream:\n    " + tc.yaml
			}
			cfg, err := loadConfigTestYAML(t, yaml)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Load() error = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.NATS.WorkerEventStream != tc.want {
				t.Fatalf("stream config = %+v, want %+v", cfg.NATS.WorkerEventStream, tc.want)
			}
		})
	}
}
