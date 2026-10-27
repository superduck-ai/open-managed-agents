package config

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

type EnvironmentPackageConfig struct {
	NPM   NPMRegistryConfig   `yaml:"npm" json:"npm"`
	PIP   PIPRegistryConfig   `yaml:"pip" json:"pip"`
	Go    GoRegistryConfig    `yaml:"go" json:"go"`
	Cargo CargoRegistryConfig `yaml:"cargo" json:"cargo"`
	Gem   GemRegistryConfig   `yaml:"gem" json:"gem"`
}

type NPMRegistryConfig struct {
	Registry         string            `yaml:"registry" json:"registry,omitempty"`
	ScopedRegistries map[string]string `yaml:"scoped_registries" json:"scoped_registries,omitempty"`
}

type PIPRegistryConfig struct {
	IndexURL string `yaml:"index_url" json:"index_url,omitempty"`
}

type GoRegistryConfig struct {
	Proxy string `yaml:"proxy" json:"proxy,omitempty"`
}

type CargoRegistryConfig struct {
	Registries map[string]string `yaml:"registries" json:"registries,omitempty"`
}

type GemRegistryConfig struct {
	Sources []string `yaml:"sources" json:"sources,omitempty"`
}

var npmScopePattern = regexp.MustCompile(`^@[a-z0-9][a-z0-9._-]*$`)
var cargoRegistryPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func validateEnvironmentPackageConfig(packages EnvironmentPackageConfig) error {
	if strings.ContainsAny(packages.Go.Proxy, ",|") {
		return errors.New("environment_packages.go.proxy must be a single HTTPS URL without proxy list separators")
	}
	for scope := range packages.NPM.ScopedRegistries {
		if !npmScopePattern.MatchString(scope) {
			return errors.New("environment_packages.npm.scoped_registries keys must be npm scopes such as @example")
		}
	}
	for name := range packages.Cargo.Registries {
		if !cargoRegistryPattern.MatchString(name) {
			return errors.New("environment_packages.cargo.registries keys must start with a lowercase letter and contain only lowercase letters, digits, hyphens or underscores")
		}
	}
	for _, registry := range packages.registryURLs() {
		if err := validatePackageRegistry(registry); err != nil {
			return fmt.Errorf("environment_packages: %w", err)
		}
	}
	return nil
}

func validatePackageRegistry(registry string) error {
	u, err := url.Parse(registry)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(registry, "#") || u.Opaque != "" || strings.HasSuffix(u.Host, ":") || (u.Port() != "" && u.Port() != "443") {
		return errors.New("must be an HTTPS registry on port 443 without credentials, query or fragment")
	}
	for _, value := range []string{registry, u.Path} {
		if strings.ContainsAny(value, "$\\") || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return errors.New("registry must not contain whitespace, control characters or variable interpolation")
		}
	}
	if _, err := networkpolicy.NormalizeHost(u.Hostname()); err != nil {
		return errors.New("registry must have a valid host")
	}
	return nil
}

func (c EnvironmentPackageConfig) registryURLs() []string {
	var registries []string
	for _, registry := range []string{c.NPM.Registry, c.PIP.IndexURL, c.Go.Proxy} {
		if registry != "" {
			registries = append(registries, registry)
		}
	}
	registries = slices.AppendSeq(registries, maps.Values(c.NPM.ScopedRegistries))
	registries = slices.AppendSeq(registries, maps.Values(c.Cargo.Registries))
	return append(registries, c.Gem.Sources...)
}

func (c EnvironmentPackageConfig) Empty() bool {
	return len(c.registryURLs()) == 0
}

func (c NPMRegistryConfig) Options() []string {
	var options []string
	if c.Registry != "" {
		options = append(options, "--registry="+c.Registry)
	}
	for _, scope := range slices.Sorted(maps.Keys(c.ScopedRegistries)) {
		options = append(options, "--"+scope+":registry="+c.ScopedRegistries[scope])
	}
	return options
}

func (c EnvironmentPackageConfig) RegistryHosts() []string {
	var hosts []string
	for _, registry := range c.registryURLs() {
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
