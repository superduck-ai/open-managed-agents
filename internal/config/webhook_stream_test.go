package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWebhookStream(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want WebhookStreamConfig
		err  string
	}{
		{name: "zero capacity", yaml: "max_bytes: 0", err: "max_bytes must be positive"},
		{name: "negative capacity", yaml: "max_bytes: -1", err: "max_bytes must be positive"},
		{name: "zero replicas", yaml: "replicas: 0", err: "replicas must be between 1 and 5"},
		{name: "negative replicas", yaml: "replicas: -1", err: "replicas must be between 1 and 5"},
		{name: "too many replicas", yaml: "replicas: 6", err: "replicas must be between 1 and 5"},
		{name: "zero age", yaml: "max_age: 0s", err: "max_age must be positive"},
		{name: "negative age", yaml: "max_age: -1s", err: "max_age must be positive"},
		{name: "defaults", want: WebhookStreamConfig{MaxBytes: 64 << 20, MaxAge: 24 * time.Hour, Replicas: 3}},
		{name: "override", yaml: "max_bytes: 2097152\n    max_age: 1h\n    replicas: 1", want: WebhookStreamConfig{MaxBytes: 2 << 20, MaxAge: time.Hour, Replicas: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareLoadTest(t)
			yaml := "{}"
			if tc.yaml != "" {
				yaml = "nats:\n  webhook_stream:\n    " + tc.yaml
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
			if cfg.NATS.WebhookStream != tc.want {
				t.Fatalf("stream config = %+v, want %+v", cfg.NATS.WebhookStream, tc.want)
			}
		})
	}
}
