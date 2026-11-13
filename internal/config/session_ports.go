package config

import (
	"errors"
	"fmt"
	"net"
	"time"
)

type SessionPortsConfig struct {
	DomainSuffix          string        `yaml:"domain_suffix"`
	SlugKeyFile           string        `yaml:"slug_key_file"`
	URLScheme             string        `yaml:"url_scheme"`
	URLPort               int           `yaml:"url_port"`
	InternalListenAddr    string        `yaml:"internal_listen_addr"`
	InternalAdvertiseAddr string        `yaml:"internal_advertise_addr"`
	WakeTimeout           time.Duration `yaml:"wake_timeout"`
	KeepaliveInterval     time.Duration `yaml:"keepalive_interval"`
	ResponseIdleTimeout   time.Duration `yaml:"response_idle_timeout"`
	MaxRequestBodyBytes   int64         `yaml:"max_request_body_bytes"`
	MaxConcurrentRequests int           `yaml:"max_concurrent_requests"`
	MaxWebSockets         int           `yaml:"max_websockets"`
	BlockedPorts          []int         `yaml:"blocked_ports"`
	StripCookieNames      []string      `yaml:"strip_cookie_names"`
}

func (c SessionPortsConfig) Enabled() bool {
	return c.DomainSuffix != ""
}

func (c SessionPortsConfig) ForwardingEnabled() bool {
	return c.Enabled() && c.InternalListenAddr != ""
}

func defaultSessionPortsConfig() SessionPortsConfig {
	return SessionPortsConfig{
		URLScheme:             "https",
		WakeTimeout:           30 * time.Second,
		KeepaliveInterval:     20 * time.Second,
		ResponseIdleTimeout:   5 * time.Minute,
		MaxRequestBodyBytes:   16 << 20,
		MaxConcurrentRequests: 64,
		MaxWebSockets:         32,
		StripCookieNames:      []string{"sessionKey", "lastActiveOrg"},
	}
}

func validateSessionPorts(environment string, cfg SessionPortsConfig) error {
	if cfg.DomainSuffix != "" {
		if err := validateLowercaseDNSName("session_ports.domain_suffix", cfg.DomainSuffix); err != nil {
			return err
		}
		if environment == EnvironmentProd && cfg.SlugKeyFile == "" {
			return errors.New("session_ports.slug_key_file is required in production when domain_suffix is set")
		}
	}
	if cfg.URLScheme != "http" && cfg.URLScheme != "https" {
		return errors.New("session_ports.url_scheme must be http or https")
	}
	if cfg.URLPort < 0 || cfg.URLPort > 65535 {
		return errors.New("session_ports.url_port must be between 0 and 65535")
	}
	if err := validateSessionPortForwarding(cfg); err != nil {
		return err
	}
	for _, port := range cfg.BlockedPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("session_ports.blocked_ports contains invalid port %d", port)
		}
	}
	checks := []struct {
		name  string
		valid bool
	}{
		{"session_ports.wake_timeout", cfg.WakeTimeout > 0},
		{"session_ports.keepalive_interval", cfg.KeepaliveInterval > 0},
		{"session_ports.response_idle_timeout", cfg.ResponseIdleTimeout > 0},
		{"session_ports.max_request_body_bytes", cfg.MaxRequestBodyBytes > 0},
		{"session_ports.max_concurrent_requests", cfg.MaxConcurrentRequests > 0},
		{"session_ports.max_websockets", cfg.MaxWebSockets > 0},
	}
	for _, check := range checks {
		if !check.valid {
			return fmt.Errorf("%s must be greater than zero", check.name)
		}
	}
	return nil
}

func validateSessionPortForwarding(cfg SessionPortsConfig) error {
	if (cfg.InternalListenAddr == "") != (cfg.InternalAdvertiseAddr == "") {
		return errors.New("session_ports.internal_listen_addr and session_ports.internal_advertise_addr must be set together")
	}
	if cfg.InternalListenAddr == "" {
		return nil
	}
	if _, _, err := net.SplitHostPort(cfg.InternalListenAddr); err != nil {
		return fmt.Errorf("session_ports.internal_listen_addr must be host:port: %w", err)
	}
	host, port, err := net.SplitHostPort(cfg.InternalAdvertiseAddr)
	if err != nil || host == "" || port == "" {
		return errors.New("session_ports.internal_advertise_addr must be a reachable host:port")
	}
	if cfg.Enabled() && cfg.SlugKeyFile == "" {
		return errors.New("session_ports.slug_key_file is required when cross-instance forwarding is enabled")
	}
	return nil
}
