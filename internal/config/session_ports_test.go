package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestValidateSessionPortsRejectsInvalidSettings(t *testing.T) {
	for name, mutate := range map[string]func(*SessionPortsConfig){
		"uppercase domain":         func(c *SessionPortsConfig) { c.DomainSuffix = "Preview.Example.com" },
		"empty label":              func(c *SessionPortsConfig) { c.DomainSuffix = "preview..example.com" },
		"scheme":                   func(c *SessionPortsConfig) { c.URLScheme = "ftp" },
		"url port":                 func(c *SessionPortsConfig) { c.URLPort = 70000 },
		"blocked port":             func(c *SessionPortsConfig) { c.BlockedPorts = []int{0} },
		"wake timeout":             func(c *SessionPortsConfig) { c.WakeTimeout = 0 },
		"keepalive":                func(c *SessionPortsConfig) { c.KeepaliveInterval = -time.Second },
		"max body":                 func(c *SessionPortsConfig) { c.MaxRequestBodyBytes = 0 },
		"max websockets":           func(c *SessionPortsConfig) { c.MaxWebSockets = 0 },
		"concurrency":              func(c *SessionPortsConfig) { c.MaxConcurrentRequests = 0 },
		"response idle":            func(c *SessionPortsConfig) { c.ResponseIdleTimeout = 0 },
		"prod without labelkey":    func(c *SessionPortsConfig) { c.DomainSuffix = "preview.example.com"; c.LabelKeyFile = "" },
		"listen without advertise": func(c *SessionPortsConfig) { c.InternalListenAddr = ":38090" },
		"advertise without listen": func(c *SessionPortsConfig) { c.InternalAdvertiseAddr = "10.0.0.1:38090" },
		"advertise without host": func(c *SessionPortsConfig) {
			c.InternalListenAddr = ":38090"
			c.InternalAdvertiseAddr = ":38090"
		},
		"listen without port": func(c *SessionPortsConfig) {
			c.InternalListenAddr = "0.0.0.0"
			c.InternalAdvertiseAddr = "10.0.0.1:38090"
		},
		"forwarding without label key": func(c *SessionPortsConfig) {
			c.DomainSuffix = "preview.example.com"
			c.LabelKeyFile = ""
			c.InternalListenAddr = ":38090"
			c.InternalAdvertiseAddr = "10.0.0.1:38090"
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := defaultSessionPortsConfig()
			cfg.LabelKeyFile = "/run/secrets/label"
			mutate(&cfg)
			if err := validateSessionPorts(EnvironmentProd, cfg); err == nil {
				t.Fatalf("accepted %+v", cfg)
			}
		})
	}
}

func TestSessionPortsDefaultsAndYAMLOverrides(t *testing.T) {
	defaults := newYAMLConfig().resolve().SessionPorts
	if defaults.Enabled() || defaults.URLScheme != "https" || defaults.WakeTimeout != 30*time.Second ||
		defaults.KeepaliveInterval != 20*time.Second || defaults.ResponseIdleTimeout != 5*time.Minute ||
		defaults.MaxRequestBodyBytes != 16<<20 || defaults.MaxConcurrentRequests != 64 || defaults.MaxWebSockets != 32 ||
		len(defaults.StripCookieNames) != 2 {
		t.Fatalf("defaults = %+v", defaults)
	}
	if err := validateSessionPorts(EnvironmentProd, defaults); err != nil {
		t.Fatalf("disabled defaults rejected: %v", err)
	}
	input := newYAMLConfig()
	if err := yaml.Unmarshal([]byte("session_ports:\n  domain_suffix: preview.localhost\n  url_scheme: http\n  url_port: 38080\n  blocked_ports: [9229]\n"), &input); err != nil {
		t.Fatal(err)
	}
	got := input.resolve().SessionPorts
	if !got.Enabled() || got.URLScheme != "http" || got.URLPort != 38080 || got.BlockedPorts[0] != 9229 || got.WakeTimeout != 30*time.Second {
		t.Fatalf("overrides = %+v", got)
	}
	if err := validateSessionPorts(EnvironmentDev, got); err != nil {
		t.Fatalf("dev config without label key rejected: %v", err)
	}
}

func TestLabelKeyFileResolvesLikeOtherFileSettings(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "run")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string]string{
		"secrets/label.key":   filepath.Join(configDir, "secrets", "label.key"),
		"./secrets/label.key": filepath.Join(configDir, "secrets", "label.key"),
		"/etc/oma/label.key":  "/etc/oma/label.key",
		"~/label.key":         filepath.Join(home, "label.key"),
	} {
		cfg := Config{SessionPorts: SessionPortsConfig{LabelKeyFile: input}}
		if err := resolveConfigPaths(&cfg, configDir); err != nil {
			t.Fatal(err)
		}
		if cfg.SessionPorts.LabelKeyFile != want {
			t.Fatalf("label_key_file %q resolved to %q, want %q", input, cfg.SessionPorts.LabelKeyFile, want)
		}
	}
}

func TestSessionPortForwardingDevRequiresLabelKey(t *testing.T) {
	cfg := defaultSessionPortsConfig()
	cfg.DomainSuffix = "preview.localhost"
	cfg.InternalListenAddr = ":38090"
	cfg.InternalAdvertiseAddr = "10.0.0.1:38090"
	if err := validateSessionPorts(EnvironmentDev, cfg); err == nil {
		t.Fatal("dev forwarding without a shared label key accepted")
	}
	cfg.LabelKeyFile = "/run/secrets/label"
	if err := validateSessionPorts(EnvironmentDev, cfg); err != nil || !cfg.ForwardingEnabled() {
		t.Fatalf("forwarding config rejected: %v", err)
	}
	if (SessionPortsConfig{InternalListenAddr: ":38090"}).ForwardingEnabled() {
		t.Fatal("forwarding enabled without domain_suffix")
	}
}

func TestSessionPortAdvertiseAddressExpandsEnvironment(t *testing.T) {
	cfg := Config{SessionPorts: SessionPortsConfig{InternalAdvertiseAddr: " ${OMA_TEST_POD_IP}:38090 "}}
	if err := resolveSessionPortsAddresses(&cfg); err == nil {
		t.Fatal("missing environment variable accepted")
	}
	t.Setenv("OMA_TEST_POD_IP", "10.1.2.3")
	if err := resolveSessionPortsAddresses(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SessionPorts.InternalAdvertiseAddr != "10.1.2.3:38090" {
		t.Fatalf("advertise addr = %q", cfg.SessionPorts.InternalAdvertiseAddr)
	}
	empty := Config{}
	if err := resolveSessionPortsAddresses(&empty); err != nil || empty.SessionPorts.InternalAdvertiseAddr != "" {
		t.Fatalf("empty advertise addr changed: %q %v", empty.SessionPorts.InternalAdvertiseAddr, err)
	}
}
