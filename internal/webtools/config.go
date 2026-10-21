package webtools

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"net/netip"
	"strings"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"golang.org/x/net/publicsuffix"
)

type UserLocation struct {
	Type     string `json:"type"`
	City     string `json:"city,omitempty"`
	Region   string `json:"region,omitempty"`
	Country  string `json:"country,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

type ToolConfig struct {
	Type             string `json:"type,omitempty"`
	PermissionPolicy *struct {
		Type string `json:"type"`
	} `json:"permission_policy,omitzero"`
	Name             string        `json:"name"`
	Enabled          *bool         `json:"enabled,omitempty"`
	AllowedDomains   []string      `json:"allowed_domains,omitempty"`
	BlockedDomains   []string      `json:"blocked_domains,omitempty"`
	MaxContentTokens *int          `json:"max_content_tokens,omitempty"`
	UserLocation     *UserLocation `json:"user_location,omitempty"`
}

func ParseToolConfig(raw []byte) (ToolConfig, error) {
	var c ToolConfig
	if json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil {
		return c, invalidConfig("Invalid web tool configuration")
	}
	if (c.Name != "web_search" && c.Name != "web_fetch") || (c.Type != "" && c.Type != c.Name) {
		return c, invalidConfig("Invalid web tool name")
	}
	if c.AllowedDomains != nil && c.BlockedDomains != nil {
		return c, invalidConfig("Only one of allowed_domains or blocked_domains may be set")
	}
	if err := validateDomains(c); err != nil {
		return c, err
	}

	if c.MaxContentTokens != nil && (c.Name != "web_fetch" || *c.MaxContentTokens < 1) {
		return c, invalidConfig("max_content_tokens must be a positive integer on web_fetch")
	}
	if c.UserLocation != nil {
		if c.Name != "web_search" {
			return c, invalidField("user_location", "Only web_search supports user_location")
		}
		if c.UserLocation.Type != "approximate" {
			return c, invalidField("user_location.type", "Must be approximate")
		}
	}

	return c, nil
}

func ConfigFromSnapshot(raw []byte, name string) (ToolConfig, error) {
	var snapshot struct {
		Tools []struct {
			Type    string              `json:"type"`
			Configs []jsonv1.RawMessage `json:"configs"`
		} `json:"tools"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		return ToolConfig{}, invalidConfig("Invalid web tool snapshot")
	}
	for _, toolset := range snapshot.Tools {
		if toolset.Type != "agent_toolset_20260401" {
			continue
		}
		for _, rawConfig := range toolset.Configs {
			var identity struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(rawConfig, &identity) != nil {
				return ToolConfig{}, invalidConfig("Invalid web tool snapshot")
			}
			if identity.Name == name {
				return ParseToolConfig(rawConfig)
			}
		}
	}
	return ToolConfig{Name: name}, nil
}

func ValidateSession(environment, snapshot []byte) error {
	policy, err := networkpolicy.ParseConfig(environment)
	if err != nil {
		return err
	}
	for _, name := range []string{"web_search", "web_fetch"} {
		c, err := ConfigFromSnapshot(snapshot, name)
		if err != nil {
			return err
		}
		if c.Enabled != nil && !*c.Enabled {
			continue
		}
		if policy.Type != networkpolicy.TypeLimited {
			continue
		}
		for _, domain := range c.AllowedDomains {
			host, _, _ := strings.Cut(strings.ToLower(domain), "/")
			allowedHTTPS, err := networkpolicy.AllowsHost(policy.AllowedHostPatterns(), host, "443")
			if err != nil {
				return err
			}
			allowedHTTP, err := networkpolicy.AllowsHost(policy.AllowedHostPatterns(), host, "80")
			if err != nil || (!allowedHTTPS && !allowedHTTP) {
				return invalidConfig("Web tool allowed_domains must be within Environment allowed_hosts")
			}
		}
	}
	return nil
}

func validateDomains(c ToolConfig) error {
	for _, list := range []struct {
		name    string
		domains []string
	}{{"allowed_domains", c.AllowedDomains}, {"blocked_domains", c.BlockedDomains}} {
		if list.domains == nil {
			continue
		}
		if len(list.domains) == 0 {
			return invalidField(list.name, "Empty list of domains is ambiguous. Provide at least one domain or null.")
		}
		if len(list.domains) > 64 {
			return invalidField(list.name, "Provide at most 64 domains.")
		}
		seen := map[string]bool{}
		for index, domain := range list.domains {
			host, path, err := parseDomainScope(domain)
			if err != nil {
				return invalidDomain(list.name, index, "Invalid path encoding")
			}
			if _, err := netip.ParseAddr(host); err == nil {
				return invalidDomain(list.name, index, "IP addresses are not supported; provide a plain hostname like example.com")
			}
			if len(domain) > 255 || strings.ContainsAny(domain, " \t\r\n:@*?#$ ,|^!") || strings.ContainsFunc(domain, func(r rune) bool { return r > unicode.MaxASCII }) || (c.Name == "web_fetch" && path != "") {
				return invalidDomain(list.name, index, "Invalid domain format")
			}
			_, suffixErr := publicsuffix.EffectiveTLDPlusOne(host)
			if !validDomainHost(host) || suffixErr != nil {
				return invalidDomain(list.name, index, "Provide a registrable domain or subdomain")
			}
			if _, err := publicURL("https://" + host); err != nil {
				return invalidDomain(list.name, index, "Provide a plain public hostname")
			}
			key := host + "/" + path
			if seen[key] {
				return invalidDomain(list.name, index, "Duplicate domain")
			}
			seen[key] = true
		}
	}
	return nil
}

func validDomainHost(host string) bool {
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}
