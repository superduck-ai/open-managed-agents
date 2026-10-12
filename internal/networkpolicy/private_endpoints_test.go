package networkpolicy

import (
	"net/netip"
	"testing"
)

func TestPrivateNetworkPolicyRejectsInvalidHosts(t *testing.T) {
	for _, host := range []string{"*.example.com", "", "host/path"} {
		if _, err := NewPrivateNetworkPolicy([]string{host}); err == nil {
			t.Fatalf("accepted invalid registry host %q", host)
		}
	}
}

func TestPrivateNetworkPolicyMatchesHostPortAndAddress(t *testing.T) {
	policy, err := NewPrivateNetworkPolicy([]string{"registry.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		host, port, address string
		allow               bool
	}{
		{"other.example.com", "443", "10.20.30.40", false},
		{"registry.example.com", "80", "10.20.30.40", false},
		{"registry.example.com", "443", "10.20.30.41", true},
		{"registry.example.com", "443", "127.0.0.1", false},
		{"registry.example.com", "443", "169.254.169.254", false},
		{"registry.example.com", "443", "1.1.1.1", false},
		{"registry.example.com", "443", "100.100.100.200", false},
		{"registry.example.com", "443", "fd00:ec2::254", false},
		{"registry.example.com", "443", "fd20:ce::254", false},
		{"registry.example.com", "443", "fe80::1", false},
		{"registry.example.com", "443", "::1", false},
		{"registry.example.com", "443", "198.18.0.1", false},
		{"registry.example.com", "443", "172.16.4.5", true},
		{"registry.example.com", "443", "192.168.1.1", true},
		{"registry.example.com", "443", "100.64.0.1", true},
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
