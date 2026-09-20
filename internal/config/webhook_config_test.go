package config

import "testing"

func TestWebhookWorkerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, webhook string
		want          bool
	}{
		{"explicit false", "webhook:\n  worker_enabled: false\n", false},
		{"default without global endpoint", "", true},
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
			if cfg.Webhook.EndpointURL != "" || cfg.Webhook.SigningKey != "" {
				t.Fatal("test must not depend on a global endpoint")
			}
		})
	}
}
