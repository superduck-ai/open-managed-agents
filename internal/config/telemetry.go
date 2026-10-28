package config

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

func validateTelemetryConfig(cfg TelemetryConfig) error {
	if !cfg.Enabled {
		return nil
	}
	endpoint, err := url.Parse(cfg.OTLP.BaseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || strings.Trim(endpoint.Path, "/") != "" {
		return errors.New("telemetry.otlp.base_url must be an HTTP or HTTPS URL with a host and no path")
	}
	checks := []struct {
		valid   bool
		message string
	}{
		{cfg.ServiceName != "", "telemetry.service_name is required"},
		{cfg.OTLP.Organization != "", "telemetry.otlp.organization is required"},
		{cfg.OTLP.Timeout > 0, "telemetry.otlp.timeout must be positive"},
		{(cfg.OTLP.Username == "") == (cfg.OTLP.Password == ""), "telemetry.otlp.username and password must both be set or both be empty"},
		{!cfg.Traces.Enabled || cfg.OTLP.TracesStream != "", "telemetry.otlp.traces_stream is required when traces are enabled"},
		{!cfg.Metrics.Enabled || cfg.Metrics.ExportInterval >= time.Second, "telemetry.metrics.export_interval must be at least 1s"},
		{!cfg.Logs.Enabled || cfg.OTLP.LogsStream != "", "telemetry.otlp.logs_stream is required when logs are enabled"},
		{!cfg.Metrics.Enabled || cfg.StateMetrics.Interval >= 10*time.Second, "telemetry.state_metrics.interval must be at least 10s"},
		{!cfg.Metrics.Enabled || cfg.StateMetrics.StaleRunningAfter > 0, "telemetry.state_metrics.stale_running_after must be positive"},
		{!cfg.Metrics.Enabled || cfg.StateMetrics.StuckStartingAfter > 0, "telemetry.state_metrics.stuck_starting_after must be positive"},
	}
	for _, check := range checks {
		if !check.valid {
			return errors.New(check.message)
		}
	}
	var level slog.Level
	if cfg.Logs.Enabled && level.UnmarshalText([]byte(cfg.Logs.Level)) != nil {
		return errors.New("telemetry.logs.level must be a valid slog level")
	}
	return nil
}

func (c TelemetryLogsConfig) SlogLevel() slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Level)); err != nil {
		return slog.LevelInfo
	}
	return level
}

func (c TelemetryConfig) StateMetricsActive() bool {
	return c.Enabled && c.Metrics.Enabled
}
