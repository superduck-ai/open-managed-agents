package e2bruntime

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestConnectionOptsFromConfigMapsAllFields(t *testing.T) {
	cfg := config.E2BConfig{
		APIKey:         "api-key",
		AccessToken:    "access-token",
		Domain:         "e2b.example.test",
		APIURL:         "https://api.e2b.example.test",
		SandboxURL:     "https://sandbox.e2b.example.test",
		Debug:          true,
		RequestTimeout: 23 * time.Second,
	}

	got := ConnectionOptsFromConfig(cfg)
	if got.ApiKey != cfg.APIKey || got.AccessToken != cfg.AccessToken || got.Domain != cfg.Domain || got.ApiUrl != cfg.APIURL || got.SandboxUrl != cfg.SandboxURL {
		t.Fatalf("ConnectionOptsFromConfig() = %#v, want all connection fields from E2BConfig", got)
	}
	if got.Debug == nil || !*got.Debug {
		t.Fatalf("ConnectionOptsFromConfig().Debug = %v, want true", got.Debug)
	}
	wantTimeoutMs := int(cfg.RequestTimeout / time.Millisecond)
	if got.RequestTimeoutMs == nil || *got.RequestTimeoutMs != wantTimeoutMs {
		t.Fatalf("ConnectionOptsFromConfig().RequestTimeoutMs = %v, want %d", got.RequestTimeoutMs, wantTimeoutMs)
	}
}

func TestSandboxVolumeMountsOnlyIncludeUserData(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.E2BConfig
	}{
		{name: "hosted", cfg: config.E2BConfig{Domain: "e2b.example.test"}},
		{name: "local endpoint", cfg: config.E2BConfig{APIURL: "http://127.0.0.1:3000"}},
		{name: "debug", cfg: config.E2BConfig{Debug: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider(tt.cfg)
			mounts := provider.sandboxVolumeMounts(nil)
			if got := mounts[sandboxUserDataMountPath]; got != sandboxUserDataVolumeName {
				t.Fatalf("mount %s = %v, want %s", sandboxUserDataMountPath, got, sandboxUserDataVolumeName)
			}
			if len(mounts) != 1 {
				t.Fatalf("mounts = %#v, want only user-data", mounts)
			}
		})
	}
}

func TestResolveLimitedNetworkFailsClosedOnInvalidAllowedHost(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	_, err := provider.Resolve(db.Environment{
		ExternalID:       "env_invalid_network",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["bad/path","api.example.com"]}}`),
		ResolvedTemplate: "template_test",
	}, nil)
	if err == nil {
		t.Fatal("invalid allowed_hosts policy must fail closed")
	}
}

func TestResolveLimitedNetworkFailsClosedOnMalformedMCPMetadata(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	_, err := provider.Resolve(db.Environment{
		ExternalID:       "env_invalid_mcp_metadata",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}}`),
		ResolvedTemplate: "template_test",
	}, &db.EnvironmentWork{
		ExternalID: "work_invalid_mcp_metadata",
		Metadata:   json.RawMessage(`{"mcp_allowed_hosts":["mcp.example.com",42]}`),
	})
	if err == nil {
		t.Fatal("malformed mcp_allowed_hosts metadata must fail closed")
	}
}

func TestResolveLimitedNetworkCanonicalizesExplicitAllowedHosts(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	resolution, err := provider.Resolve(db.Environment{
		ExternalID:  "env_canonical_network",
		WorkspaceID: 42,
		Config: json.RawMessage(`{
			"type":"cloud",
			"networking":{
				"type":"limited",
				"allowed_hosts":["例子.测试","API.Example.COM.","::ffff:192.0.2.1","*.例子.测试","[2606:4700:4700::1111]:443","Example.com:8443"]
			}
		}`),
	}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := []string{
		"xn--fsqu00a.xn--0zwm56d",
		"api.example.com",
		"192.0.2.1",
		"*.xn--fsqu00a.xn--0zwm56d",
		"[2606:4700:4700::1111]:443",
		"example.com:8443",
	}
	if resolution.Network == nil || !reflect.DeepEqual(resolution.Network.AllowOut, want) {
		t.Fatalf("AllowOut = %#v, want %#v", resolution.Network, want)
	}
}

func TestResolveLimitedNetworkIncludesMCPHostsWhenAllowed(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	env := db.Environment{
		ExternalID:       "env_test",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true}}`),
		ResolvedTemplate: "template_test",
	}
	work := &db.EnvironmentWork{
		ExternalID: "work_test",
		Metadata:   json.RawMessage(`{"mcp_allowed_hosts":["mcp.notion.com","api.githubcopilot.com","mcp.notion.com"]}`),
	}

	resolution, err := provider.Resolve(env, work)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.AllowInternetAccess {
		t.Fatalf("limited network should disable unrestricted internet")
	}
	if resolution.Network == nil {
		t.Fatalf("expected network options")
	}
	want := []string{"api.example.com", "mcp.notion.com", "api.githubcopilot.com"}
	if !reflect.DeepEqual(resolution.Network.AllowOut, want) {
		t.Fatalf("AllowOut = %#v, want %#v", resolution.Network.AllowOut, want)
	}
}
