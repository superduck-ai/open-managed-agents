package webtools

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"net/netip"
	"net/url"
	"slices"
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

type Policy struct {
	Environment networkpolicy.Config
	Tools       []ToolConfig
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
		return c, invalidConfig("user_location is not supported by the DeepSeek Search provider")
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

func (p Policy) Allows(target *url.URL) bool {
	port := target.Port()
	if port == "" {
		port = "443"
		if target.Scheme == "http" {
			port = "80"
		}
	}
	if p.Environment.Type == networkpolicy.TypeLimited {
		allowed, err := networkpolicy.AllowsHost(p.Environment.AllowedHostPatterns(), target.Hostname(), port)
		if err != nil || !allowed {
			return false
		}
	}
	for _, c := range p.Tools {
		if c.Enabled != nil && !*c.Enabled {
			return false
		}
		if c.AllowedDomains != nil && !matchesDomains(target, c.AllowedDomains) {
			return false
		}
		if matchesDomains(target, c.BlockedDomains) {
			return false
		}
	}
	return true
}

func matchesDomains(target *url.URL, domains []string) bool {
	for _, domain := range domains {
		host, path, _ := strings.Cut(domain, "/")
		host = strings.ToLower(host)
		if matchesDomain(target.Hostname(), host) && (path == "" || strings.HasPrefix(target.EscapedPath(), "/"+path)) {
			return true
		}
	}
	return false
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
			host, path, _ := strings.Cut(domain, "/")
			host = strings.ToLower(host)
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

func (p Policy) hasDomainIntersection() bool {
	var domains []string
	for _, tool := range p.Tools {
		if tool.AllowedDomains == nil {
			continue
		}
		if domains == nil {
			domains = slices.Clone(tool.AllowedDomains)
			continue
		}
		next := []string{}
		for _, a := range domains {
			for _, b := range tool.AllowedDomains {
				if domain, ok := intersectDomains(a, b); ok && !slices.Contains(next, domain) {
					next = append(next, domain)
				}
			}
		}
		domains = next
	}
	return domains == nil || len(domains) > 0
}

func intersectDomains(a, b string) (string, bool) {
	ah, ap, _ := strings.Cut(a, "/")
	bh, bp, _ := strings.Cut(b, "/")
	ah, bh = strings.ToLower(ah), strings.ToLower(bh)
	host := ah
	if matchesDomain(bh, ah) {
		host = bh
	} else if !matchesDomain(ah, bh) {
		return "", false
	}
	path := ap
	if strings.HasPrefix(bp, ap) {
		path = bp
	} else if !strings.HasPrefix(ap, bp) {
		return "", false
	}
	if path != "" {
		host += "/" + path
	}
	return host, true
}
