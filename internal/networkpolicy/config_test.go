package networkpolicy

import (
	"encoding/json"
	"errors"
	"testing"
)

// ---- 失败场景 ----

func TestParseConfigClassifiesInvalidAllowedHostAsMalformed(t *testing.T) {
	_, err := ParseConfig(limitedConfig(t, `{"type":"limited","allowed_hosts":["bad/path"]}`))
	if !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("expected ErrMalformedConfig, got %v", err)
	}
}

func TestParseConfigMalformedJSON(t *testing.T) {
	if _, err := ParseConfig(json.RawMessage(`{"type":`)); !errors.Is(err, ErrMalformedConfig) {
		t.Fatalf("expected ErrMalformedConfig, got %v", err)
	}
}

func TestParseConfigUnknownNetworkingType(t *testing.T) {
	if _, err := ParseConfig(limitedConfig(t, `{"type":"bogus"}`)); !errors.Is(err, ErrUnknownNetworkingType) {
		t.Fatalf("expected ErrUnknownNetworkingType, got %v", err)
	}
}

func TestParseConfigEmptyNetworkingTypeFailsClosed(t *testing.T) {
	if _, err := ParseConfig(limitedConfig(t, `{"type":"","allowed_hosts":["api.example.com"]}`)); !errors.Is(err, ErrUnknownNetworkingType) {
		t.Fatalf("expected ErrUnknownNetworkingType, got %v", err)
	}
}

// ---- 成功场景 ----

func TestParseConfigLimitedFields(t *testing.T) {
	config, err := ParseConfig(limitedConfig(t, `{"type":"limited","allowed_hosts":["a.com","b.com"],"allow_mcp_servers":true,"allow_package_managers":true}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Type != TypeLimited || !config.AllowMCPServers || !config.AllowPackageManagers {
		t.Fatalf("unexpected config: %+v", config)
	}
	patterns := config.AllowedHostPatterns()
	if len(patterns) != 2 || patterns[0] != "a.com" {
		t.Fatalf("unexpected hosts: %v", patterns)
	}
}
