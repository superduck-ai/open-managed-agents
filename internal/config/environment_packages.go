package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

type EnvironmentPackageConfig struct {
	NPM     NPMRegistryConfig    `yaml:"npm" json:"npm"`
	Network PackageNetworkConfig `yaml:"network" json:"network"`
}

type NPMRegistryConfig struct {
	Registry         string            `yaml:"registry" json:"registry,omitempty"`
	ScopedRegistries map[string]string `yaml:"scoped_registries" json:"scoped_registries,omitempty"`
}

type PackageNetworkConfig struct {
	PrivateEndpoints []networkpolicy.PrivateEndpoint `yaml:"private_endpoints" json:"private_endpoints,omitempty"`
}

var npmScopePattern = regexp.MustCompile(`^@[a-z0-9][a-z0-9._-]*$`)

func normalizeEnvironmentPackageConfig(packages *EnvironmentPackageConfig) error {
	packages.NPM.Registry = strings.TrimSpace(packages.NPM.Registry)
	registries := make(map[string]string, len(packages.NPM.ScopedRegistries))
	for scope, registry := range packages.NPM.ScopedRegistries {
		key := strings.TrimSpace(scope)
		if _, exists := registries[key]; exists {
			return errors.New("environment_packages.npm.scoped_registries contains duplicate scopes")
		}
		registries[key] = strings.TrimSpace(registry)
	}
	packages.NPM.ScopedRegistries = registries
	for i := range packages.Network.PrivateEndpoints {
		endpoint := &packages.Network.PrivateEndpoints[i]
		host, err := networkpolicy.NormalizeHost(endpoint.Host)
		if err != nil {
			return errors.New("environment_packages.network.private_endpoints requires exact valid hosts")
		}
		endpoint.Host = host
		for j := range endpoint.AllowedCIDRs {
			endpoint.AllowedCIDRs[j] = strings.TrimSpace(endpoint.AllowedCIDRs[j])
		}
	}
	return nil
}

func validateEnvironmentPackageConfig(packages EnvironmentPackageConfig) error {
	if packages.NPM.Registry != "" {
		if err := validateNPMRegistry(packages.NPM.Registry); err != nil {
			return fmt.Errorf("environment_packages.npm.registry: %w", err)
		}
	}
	for scope, registry := range packages.NPM.ScopedRegistries {
		if !npmScopePattern.MatchString(scope) {
			return errors.New("environment_packages.npm.scoped_registries keys must be npm scopes such as @example")
		}
		if err := validateNPMRegistry(registry); err != nil {
			return fmt.Errorf("environment_packages.npm.scoped_registries: %w", err)
		}
	}
	if _, err := networkpolicy.NewPrivateNetworkPolicy(packages.Network.PrivateEndpoints); err != nil {
		return fmt.Errorf("environment_packages.network.private_endpoints: %w", err)
	}
	return nil
}

func validateNPMRegistry(registry string) error {
	u, err := url.Parse(registry)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("must be an HTTPS registry on port 443 without credentials, query or fragment")
	}
	if strings.ContainsAny(registry, "$\\") || strings.ContainsFunc(registry, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return errors.New("registry must not contain whitespace, control characters or variable interpolation")
	}
	if _, err := networkpolicy.NormalizeHost(u.Hostname()); err != nil {
		return errors.New("registry must have a valid host")
	}
	return nil
}

func (c NPMRegistryConfig) Options() []string {
	var options []string
	if c.Registry != "" {
		options = append(options, "--registry="+c.Registry)
	}
	scopes := make([]string, 0, len(c.ScopedRegistries))
	for scope := range c.ScopedRegistries {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	for _, scope := range scopes {
		options = append(options, "--"+scope+":registry="+c.ScopedRegistries[scope])
	}
	return options
}

func (c EnvironmentPackageConfig) RegistryHosts() []string {
	var hosts []string
	for _, option := range c.NPM.Options() {
		_, registry, _ := strings.Cut(option, "=")
		u, err := url.Parse(registry)
		if err != nil {
			continue
		}
		host, err := networkpolicy.NormalizeHost(u.Hostname())
		if err == nil {
			hosts = append(hosts, host)
		}
	}
	slices.Sort(hosts)
	return slices.Compact(hosts)
}
