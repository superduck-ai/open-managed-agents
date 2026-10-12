package codesessions

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

func TestPrivateRegistryResolutionRejectsOtherTargetsAndPinsDNS(t *testing.T) {
	policy, err := networkpolicy.NewPrivateNetworkPolicy([]string{"registry.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, target, address string
		want                  string
	}{
		{"unlisted host", "other.example.com:443", "10.20.30.40", ""},
		{"another private address", "registry.example.com:443", "10.20.30.41", "10.20.30.41:443"},
		{"wrong port", "registry.example.com:80", "10.20.30.40", ""},
		{"metadata", "registry.example.com:443", "169.254.169.254", ""},
		{"loopback", "registry.example.com:443", "127.0.0.1", ""},
		{"IP does not inherit hostname grant", "10.20.30.40:443", "10.20.30.40", ""},
		{"granted address", "registry.example.com:443", "10.20.30.40", "10.20.30.40:443"},
		{"public destinations retain behavior", "other.example.com:443", "1.1.1.1", "1.1.1.1:443"},
	} {
		t.Run(test.name, func(t *testing.T) {
			lookups := 0
			lookup := func(context.Context, string) ([]net.IPAddr, error) {
				lookups++
				return []net.IPAddr{{IP: net.ParseIP(test.address)}}, nil
			}
			resolved, err := resolveProxyTargetWithPolicy(context.Background(), test.target, false, true, policy, lookup)
			if test.want == "" {
				var targetError *upstreamProxyTargetError
				if !errors.As(err, &targetError) || targetError.status != 403 {
					t.Fatalf("rejected target error = %v, want 403", err)
				}
				return
			}
			if err != nil || resolved != test.want || lookups != 1 {
				t.Fatalf("resolved = %q, error = %v, lookups = %d", resolved, err, lookups)
			}
		})
	}
}

func TestAllDeploymentRegistriesRequireEnvironmentAuthorization(t *testing.T) {
	for _, sources := range []config.EnvironmentPackageConfig{
		{NPM: config.NPMRegistryConfig{ScopedRegistries: map[string]string{"@example": "https://10.20.30.40/npm/"}}},
		{PIP: config.PIPRegistryConfig{IndexURL: "https://10.20.30.40/simple/"}},
		{Go: config.GoRegistryConfig{Proxy: "https://10.20.30.40/proxy/"}},
		{Cargo: config.CargoRegistryConfig{Registries: map[string]string{"example": "https://10.20.30.40/index/"}}},
		{Gem: config.GemRegistryConfig{Sources: []string{"https://10.20.30.40/gems/"}}},
	} {
		for _, enabled := range []bool{false, true} {
			raw := []byte(`{"type":"cloud","networking":{"type":"limited","allow_package_managers":false}}`)
			if enabled {
				raw = []byte(`{"type":"cloud","networking":{"type":"limited","allow_package_managers":true}}`)
			}
			policy, err := networkpolicy.ParsePolicy(raw, nil, sources.RegistryHosts()...)
			if err != nil {
				t.Fatal(err)
			}
			handler := policyTestHandler(t, policy, nil)
			handler.cfg.EnvironmentPackages = sources
			status, dialed := connectViaTunnel(t, handler, "10.20.30.40:443")
			want := "HTTP/1.1 403 Forbidden\r\n\r\n"
			if enabled {
				want = "HTTP/1.1 200 Connection Established\r\n\r\n"
			}
			if status != want || dialed != enabled {
				t.Fatalf("CONNECT status=%q dialed=%t enabled=%t", status, dialed, enabled)
			}
		}
	}
}

func TestPrivateRegistryGrantDoesNotBypassEnvironmentPolicy(t *testing.T) {
	handler := policyTestHandler(t, mustParsePolicy(t, []byte(`{"type":"cloud","networking":{"type":"limited"}}`)), nil)
	handler.cfg.EnvironmentPackages.NPM.Registry = "https://10.20.30.40/"
	status, dialed := connectViaTunnel(t, handler, "10.20.30.40:443")
	if status != "HTTP/1.1 403 Forbidden\r\n\r\n" || dialed {
		t.Fatalf("environment-denied target status = %q, dialed = %v", status, dialed)
	}
	allowed := policyTestHandler(t, mustParsePolicy(t, []byte(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["10.20.30.40"]}}`)), nil)
	allowed.cfg.EnvironmentPackages.NPM.Registry = "https://10.20.30.40/"
	status, dialed = connectViaTunnel(t, allowed, "10.20.30.40:443")
	if status != "HTTP/1.1 200 Connection Established\r\n\r\n" || !dialed {
		t.Fatalf("granted target status = %q, dialed = %v", status, dialed)
	}
}
