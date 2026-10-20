package webtools

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"net/url"
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
	for _, domains := range [][]string{c.AllowedDomains, c.BlockedDomains} {
		if domains == nil {
			continue
		}
		if len(domains) == 0 || len(domains) > 64 {
			return invalidConfig("Domain lists must contain 1–64 entries")
		}
		seen := map[string]bool{}
		for _, domain := range domains {
			host, path, _ := strings.Cut(domain, "/")
			host = strings.ToLower(host)
			if len(domain) > 255 || strings.ContainsAny(domain, " \t\r\n:@*?#$ ,|^!") || strings.ContainsFunc(domain, func(r rune) bool { return r > unicode.MaxASCII }) || (c.Name == "web_fetch" && path != "") {
				return invalidConfig("Invalid web tool domain")
			}
			normalized, err := networkpolicy.NormalizeHost(host)
			_, suffixErr := publicsuffix.EffectiveTLDPlusOne(host)
			if err != nil || normalized != host || suffixErr != nil || seen[strings.ToLower(domain)] {
				return invalidConfig("Invalid or duplicate web tool domain")
			}
			if _, err = publicURL("https://" + host); err != nil {
				return invalidConfig("Invalid web tool domain")
			}
			seen[strings.ToLower(domain)] = true
		}
	}
	return nil
}
