package config

import (
	"strings"
	"testing"
	"time"
)

func TestWebhookWorkerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, webhook string
		want          bool
	}{
		{"explicit false", "webhook:\n  worker_enabled: false\n", false},
		{"default", "", true},
		{"explicit true", "webhook:\n  worker_enabled: true\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareLoadTest(t)
			cfg, err := loadConfigTestYAML(t, tc.webhook)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Webhook.WorkerEnabled != tc.want {
				t.Fatalf("enabled=%v want %v", cfg.Webhook.WorkerEnabled, tc.want)
			}
		})
	}
}

func TestWebhookAttemptConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		want       int
		invalid    bool
	}{
		{"zero", "webhook:\n  max_attempts: 0\n", 0, true},
		{"negative", "webhook:\n  max_attempts: -1\n", 0, true},
		{"default", "", 3, false},
		{"one", "webhook:\n  max_attempts: 1\n", 1, false},
		{"custom", "webhook:\n  max_attempts: 10\n", 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareLoadTest(t)
			cfg, err := loadConfigTestYAML(t, tc.yaml)
			if tc.invalid {
				if err == nil {
					t.Fatal("accepted invalid attempt limit")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Webhook.MaxAttempts != tc.want {
				t.Fatalf("max attempts=%d want=%d", cfg.Webhook.MaxAttempts, tc.want)
			}
		})
	}
}

func TestWebhookFailureWindowConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		want       time.Duration
		invalid    bool
	}{
		{"zero", "webhook:\n  failure_disable_after: 0s\n", 0, true},
		{"negative", "webhook:\n  failure_disable_after: -1h\n", 0, true},
		{"invalid", "webhook:\n  failure_disable_after: someday\n", 0, true},
		{"default", "", 24 * time.Hour, false},
		{"custom", "webhook:\n  failure_disable_after: 120h\n", 120 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareLoadTest(t)
			cfg, err := loadConfigTestYAML(t, tc.yaml)
			if tc.invalid {
				if err == nil {
					t.Fatal("accepted invalid failure window")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Webhook.FailureDisableAfter != tc.want {
				t.Fatalf("duration=%s want=%s", cfg.Webhook.FailureDisableAfter, tc.want)
			}
		})
	}
}

func TestWebhookRetiredConfigurationRejected(t *testing.T) {
	for _, field := range []string{"endpoint_url", "signing_key", "event_types"} {
		for _, value := range []string{"", `""`, "[]", "legacy-value"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				prepareLoadTest(t)
				if _, err := loadConfigTestYAML(t, "webhook:\n  "+field+": "+value+"\n"); err == nil || !strings.Contains(err.Error(), "field "+field+" not found") {
					t.Fatalf("retired configuration error = %v", err)
				}
			})
		}
	}
}
