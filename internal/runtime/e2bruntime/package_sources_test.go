package e2bruntime

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCustomRegistrySandboxNetworkingRequiresPackageManagerPermission(t *testing.T) {
	provider := NewProvider(config.E2BConfig{}, "registry.example.com")
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
		if got := slices.Contains(hosts, "registry.example.com"); got != test.allow {
			t.Fatalf("registry allowed = %v, want %v", got, test.allow)
		}
	}
}
