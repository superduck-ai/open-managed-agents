package networkpolicy

import (
	"net/netip"
	"testing"
)

func TestPrivateNetworkPolicyRejectsInvalidGrants(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint PrivateEndpoint
	}{
		{"wildcard", PrivateEndpoint{Host: "*.example.com", Port: 443, AllowedCIDRs: []string{"10.0.0.0/8"}}},
		{"missing port", PrivateEndpoint{Host: "registry.example.com", AllowedCIDRs: []string{"10.0.0.0/8"}}},
		{"non HTTPS port", PrivateEndpoint{Host: "registry.example.com", Port: 80, AllowedCIDRs: []string{"10.0.0.0/8"}}},
		{"empty prefixes", PrivateEndpoint{Host: "registry.example.com", Port: 443}},
		{"all addresses", PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"0.0.0.0/0"}}},
		{"loopback", PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"127.0.0.0/8"}}},
		{"metadata", PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"169.254.169.254/32"}}},
		{"public address", PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"1.1.1.1/32"}}},
		{"unmasked prefix", PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"10.0.0.1/24"}}},
		{"mapped prefix", PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"::ffff:10.0.0.0/104"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPrivateNetworkPolicy([]PrivateEndpoint{test.endpoint}); err == nil {
				t.Fatal("invalid grant was accepted")
			}
		})
	}
	endpoint := PrivateEndpoint{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"10.0.0.0/8"}}
	if _, err := NewPrivateNetworkPolicy([]PrivateEndpoint{endpoint, endpoint}); err == nil {
		t.Fatal("duplicate endpoints were accepted")
	}
}

func TestPrivateNetworkPolicyMatchesHostPortAndAddress(t *testing.T) {
	policy, err := NewPrivateNetworkPolicy([]PrivateEndpoint{{Host: "registry.example.com", Port: 443, AllowedCIDRs: []string{"10.20.30.40/32", "fd12::/64"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		host, port, address string
		allow               bool
	}{
		{"other.example.com", "443", "10.20.30.40", false},
		{"registry.example.com", "80", "10.20.30.40", false},
		{"registry.example.com", "443", "10.20.30.41", false},
		{"registry.example.com", "443", "127.0.0.1", false},
		{"registry.example.com", "443", "169.254.169.254", false},
		{"registry.example.com", "443", "1.1.1.1", false},
		{"registry.example.com", "443", "10.20.30.40", true},
		{"REGISTRY.EXAMPLE.COM.", "443", "::ffff:10.20.30.40", true},
		{"registry.example.com", "443", "fd12::1", true},
	} {
		t.Run(test.host+test.port+test.address, func(t *testing.T) {
			if got := policy.Allows(test.host, test.port, netip.MustParseAddr(test.address)); got != test.allow {
				t.Fatalf("Allows() = %v, want %v", got, test.allow)
			}
		})
	}
	if (PrivateNetworkPolicy{}).Allows("registry.example.com", "443", netip.MustParseAddr("10.20.30.40")) {
		t.Fatal("empty policy allowed private access")
	}
}

func TestConfiguredRegistryRequiresEnvironmentAuthorization(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		raw := []byte(`{"type":"cloud","networking":{"type":"limited","allow_package_managers":false}}`)
		if enabled {
			raw = []byte(`{"type":"cloud","networking":{"type":"limited","allow_package_managers":true}}`)
		}
		policy, err := ParsePolicy(raw, nil, "registry.example.com")
		if err != nil {
			t.Fatal(err)
		}
		if got := policy.AuthorizeHTTPS("registry.example.com:443").Allow; got != enabled {
			t.Fatalf("custom registry allowed = %v, package managers enabled = %v", got, enabled)
		}
		if policy.AuthorizeHTTPS("other.example.com:443").Allow {
			t.Fatal("custom registry granted an unrelated host")
		}
	}
}
