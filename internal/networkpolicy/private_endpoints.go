package networkpolicy

import (
	"errors"
	"net/netip"
	"strconv"
)

type PrivateEndpoint struct {
	Host         string   `yaml:"host" json:"host"`
	Port         uint16   `yaml:"port" json:"port"`
	AllowedCIDRs []string `yaml:"allowed_cidrs" json:"allowed_cidrs"`
}

type privateEndpointRule struct {
	host     string
	port     string
	prefixes []netip.Prefix
}

type PrivateNetworkPolicy struct {
	rules []privateEndpointRule
}

var privateAddressNetworks = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fc00::/7"),
}

func NewPrivateNetworkPolicy(endpoints []PrivateEndpoint) (PrivateNetworkPolicy, error) {
	policy := PrivateNetworkPolicy{}
	seen := make(map[string]bool)
	for _, endpoint := range endpoints {
		host, err := NormalizeHost(endpoint.Host)
		if err != nil || endpoint.Port != 443 || len(endpoint.AllowedCIDRs) == 0 {
			return PrivateNetworkPolicy{}, errors.New("each endpoint requires an exact host, port 443 and private CIDRs")
		}
		if seen[host] {
			return PrivateNetworkPolicy{}, errors.New("duplicate private endpoint")
		}
		seen[host] = true
		rule := privateEndpointRule{host: host, port: strconv.Itoa(int(endpoint.Port))}
		for _, raw := range endpoint.AllowedCIDRs {
			prefix, err := netip.ParsePrefix(raw)
			if err != nil || prefix != prefix.Masked() || !privatePrefix(prefix) {
				return PrivateNetworkPolicy{}, errors.New("allowed_cidrs must be canonical private-network prefixes")
			}
			rule.prefixes = append(rule.prefixes, prefix)
		}
		policy.rules = append(policy.rules, rule)
	}
	return policy, nil
}

func privatePrefix(prefix netip.Prefix) bool {
	for _, network := range privateAddressNetworks {
		if network.Contains(prefix.Addr()) && prefix.Bits() >= network.Bits() {
			return true
		}
	}
	return false
}

func (p PrivateNetworkPolicy) Allows(host, port string, address netip.Addr) bool {
	normalized, err := NormalizeHost(host)
	if err != nil {
		return false
	}
	address = address.Unmap()
	for _, rule := range p.rules {
		if rule.host != normalized || rule.port != port {
			continue
		}
		for _, prefix := range rule.prefixes {
			if prefix.Contains(address) {
				return true
			}
		}
	}
	return false
}
