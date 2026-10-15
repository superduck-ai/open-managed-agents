package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestTelemetryValidationRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name, field string
		change      func(*TelemetryConfig)
	}{
		{"service", "service_name", func(c *TelemetryConfig) { c.ServiceName = "" }},
		{"organization", "organization", func(c *TelemetryConfig) { c.OTLP.Organization = "" }},
		{"malformed URL", "base_url", func(c *TelemetryConfig) { c.OTLP.BaseURL = "://" }},
		{"URL scheme", "base_url", func(c *TelemetryConfig) { c.OTLP.BaseURL = "ftp://example.com" }},
		{"URL host", "base_url", func(c *TelemetryConfig) { c.OTLP.BaseURL = "http:///path" }},
		{"timeout", "timeout", func(c *TelemetryConfig) { c.OTLP.Timeout = 0 }},
		{"username only", "username", func(c *TelemetryConfig) { c.OTLP.Username = "writer" }},
		{"password only", "password", func(c *TelemetryConfig) { c.OTLP.Password = "password" }},
		{"trace stream", "traces_stream", func(c *TelemetryConfig) { c.OTLP.TracesStream = "" }},
		{"metric interval", "export_interval", func(c *TelemetryConfig) { c.Metrics.ExportInterval = time.Second - time.Nanosecond }},
		{"log stream", "logs_stream", func(c *TelemetryConfig) { c.OTLP.LogsStream = "" }},
		{"log level", "level", func(c *TelemetryConfig) { c.Logs.Level = "invalid" }},
		{"state interval", "interval", func(c *TelemetryConfig) { c.StateMetrics.Interval = 10*time.Second - time.Nanosecond }},
		{"stale threshold", "stale_running_after", func(c *TelemetryConfig) { c.StateMetrics.StaleRunningAfter = 0 }},
		{"stuck threshold", "stuck_starting_after", func(c *TelemetryConfig) { c.StateMetrics.StuckStartingAfter = -time.Second }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultConfig().Telemetry
			cfg.Enabled = true
			test.change(&cfg)
			if err := validateTelemetryConfig(cfg); err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("validation error = %v, want %s", err, test.field)
			}
		})
	}
}

func TestTelemetryConfigurationDefaults(t *testing.T) {
	cfg := defaultConfig().Telemetry
	if cfg.Enabled || cfg.ServiceName != "oma-server" || cfg.OTLP.BaseURL != "http://openobserve:5080" || cfg.OTLP.Organization != "oma" || cfg.OTLP.TracesStream != "oma_server" || cfg.OTLP.LogsStream != "oma_server" || cfg.OTLP.Timeout != 10*time.Second {
		t.Fatalf("unexpected telemetry defaults: %+v", cfg)
	}
	if !cfg.Traces.Enabled || !cfg.Metrics.Enabled || cfg.Metrics.ExportInterval != 30*time.Second || !cfg.Logs.Enabled || cfg.Logs.SlogLevel() != slog.LevelInfo || !cfg.StateMetrics.Enabled || cfg.StateMetrics.Interval != time.Minute || cfg.StateMetrics.StaleRunningAfter != 30*time.Minute || cfg.StateMetrics.StuckStartingAfter != 10*time.Minute {
		t.Fatalf("unexpected signal defaults: %+v", cfg)
	}
	if err := validateTelemetryConfig(TelemetryConfig{}); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := validateTelemetryConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.OTLP.Username, cfg.OTLP.Password = "writer", " password "
	cfg.Metrics.ExportInterval = time.Second
	cfg.StateMetrics.Interval = 10 * time.Second
	if err := validateTelemetryConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Traces.Enabled, cfg.Metrics.Enabled, cfg.Logs.Enabled, cfg.StateMetrics.Enabled = false, false, false, false
	cfg.OTLP.TracesStream, cfg.OTLP.LogsStream = "", ""
	cfg.Metrics.ExportInterval, cfg.StateMetrics.Interval = 0, 0
	cfg.Logs.Level = "invalid"
	if err := validateTelemetryConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Logs.SlogLevel() != slog.LevelInfo {
		t.Fatal("invalid level must fall back to info")
	}
}

func TestLoadTelemetryTrimsExternalValues(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, `telemetry:
  enabled: true
  service_name: " oma-server "
  otlp:
    base_url: " https://observe.example.com/ "
    organization: " oma "
    username: " writer "
    password: " password "
    traces_stream: " traces "
    logs_stream: " logs "
  logs:
    level: " DEBUG "
`)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Telemetry
	if got.ServiceName != "oma-server" || got.OTLP.BaseURL != "https://observe.example.com/" || got.OTLP.Organization != "oma" || got.OTLP.Username != "writer" || got.OTLP.Password != " password " || got.OTLP.TracesStream != "traces" || got.OTLP.LogsStream != "logs" || got.Logs.SlogLevel() != slog.LevelDebug {
		t.Fatal("telemetry config was not normalized at the input boundary")
	}
}

func TestStateMetricsActive(t *testing.T) {
	for _, tc := range []struct{ enabled, metrics, state, want bool }{
		{false, true, true, false}, {true, false, true, false}, {true, true, false, false}, {true, true, true, true},
	} {
		cfg := TelemetryConfig{Enabled: tc.enabled, Metrics: TelemetryMetricsConfig{Enabled: tc.metrics}, StateMetrics: TelemetryStateMetricsConfig{Enabled: tc.state}}
		if got := cfg.StateMetricsActive(); got != tc.want {
			t.Fatalf("state metrics active=%t for %+v", got, tc)
		}
	}
}
