package codesessions

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

func TestPrivateRegistryResolutionRejectsOtherTargetsAndPinsDNS(t *testing.T) {
	endpoint := networkpolicy.PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"10.20.30.40/32"}}
	policy, err := networkpolicy.NewPrivateNetworkPolicy([]networkpolicy.PrivateEndpoint{endpoint})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, target, address string
		want                  string
	}{
		{"unlisted host", "other.example.com:443", "10.20.30.40", ""},
		{"unlisted address", "registry.example.com:443", "10.20.30.41", ""},
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

func TestPrivateRegistryGrantDoesNotBypassEnvironmentPolicy(t *testing.T) {
	endpoint := networkpolicy.PrivateEndpoint{Host: "10.20.30.40", Port: 443, AllowedCIDRs: []string{"10.20.30.40/32"}}
	handler := policyTestHandler(t, mustParsePolicy(t, []byte(`{"type":"cloud","networking":{"type":"limited"}}`)), nil)
	handler.cfg.EnvironmentPackages.Network.PrivateEndpoints = []networkpolicy.PrivateEndpoint{endpoint}
	status, dialed := connectViaTunnel(t, handler, "10.20.30.40:443")
	if status != "HTTP/1.1 403 Forbidden\r\n\r\n" || dialed {
		t.Fatalf("environment-denied target status = %q, dialed = %v", status, dialed)
	}
	allowed := policyTestHandler(t, mustParsePolicy(t, []byte(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["10.20.30.40"]}}`)), nil)
	allowed.cfg.EnvironmentPackages.Network.PrivateEndpoints = []networkpolicy.PrivateEndpoint{endpoint}
	status, dialed = connectViaTunnel(t, allowed, "10.20.30.40:443")
	if status != "HTTP/1.1 200 Connection Established\r\n\r\n" || !dialed {
		t.Fatalf("granted target status = %q, dialed = %v", status, dialed)
	}
}
