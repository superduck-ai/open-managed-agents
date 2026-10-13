package e2bruntime

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCustomRegistrySandboxNetworkingRequiresPackageManagerPermission(t *testing.T) {
	sources := config.EnvironmentPackageConfig{
		NPM:   config.NPMRegistryConfig{ScopedRegistries: map[string]string{"@example": "https://npm.example.internal/"}},
		PIP:   config.PIPRegistryConfig{IndexURL: "https://pip.example.internal/simple/"},
		Go:    config.GoRegistryConfig{Proxy: "https://go.example.internal/"},
		Cargo: config.CargoRegistryConfig{Registries: map[string]string{"example": "https://cargo.example.internal/index/"}},
		Gem:   config.GemRegistryConfig{Sources: []string{"https://gem.example.internal/"}},
	}
	provider := NewProvider(config.E2BConfig{}, sources.RegistryHosts()...)
	for _, test := range []struct {
		raw   string
		allow bool
	}{
		{`{"type":"cloud","networking":{"type":"limited","allow_package_managers":false}}`, false},
		{`{"type":"cloud","networking":{"type":"limited","allow_package_managers":true}}`, true},
	} {
		resolved, err := provider.Resolve(db.Environment{Config: json.RawMessage(test.raw)}, nil)
		if err != nil || resolved.Network == nil || resolved.AllowInternetAccess {
			t.Fatalf("resolution = %+v, error = %v", resolved, err)
		}
		hosts, ok := resolved.Network.AllowOut.([]string)
		if !ok {
			t.Fatalf("unexpected network selector: %T", resolved.Network.AllowOut)
		}
		for _, host := range sources.RegistryHosts() {
			if got := slices.Contains(hosts, host); got != test.allow {
				t.Fatalf("%s registry allowed = %v, want %v", host, got, test.allow)
			}
		}
	}
}
