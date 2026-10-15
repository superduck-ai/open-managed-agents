package networkpolicy

import "net/netip"

type PrivateNetworkPolicy struct {
	hosts map[string]struct{}
}

var privateAddressNetworks = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fc00::/7"),
}

func NewPrivateNetworkPolicy(hosts []string) (PrivateNetworkPolicy, error) {
	policy := PrivateNetworkPolicy{hosts: make(map[string]struct{}, len(hosts))}
	for _, raw := range hosts {
		host, err := NormalizeHost(raw)
		if err != nil {
			return PrivateNetworkPolicy{}, err
		}
		policy.hosts[host] = struct{}{}
	}
	return policy, nil
}

func (p PrivateNetworkPolicy) Allows(host, port string, address netip.Addr) bool {
	normalized, err := NormalizeHost(host)
	if err != nil || port != "443" {
		return false
	}
	if _, ok := p.hosts[normalized]; !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.Zone() != "" || address == netip.MustParseAddr("100.100.100.200") || address == netip.MustParseAddr("fd00:ec2::254") || address == netip.MustParseAddr("fd20:ce::254") {
		return false
	}
	for _, network := range privateAddressNetworks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}
