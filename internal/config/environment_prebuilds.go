package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// EnvironmentPrebuildConfig applies to newly configured environments and explicit retries.
type EnvironmentPrebuildConfig struct {
	Enabled  bool                `yaml:"enabled"`
	Timeout  time.Duration       `yaml:"timeout"`
	Image    ImageBuildConfig    `yaml:"image"`
	Template TemplateBuildConfig `yaml:"template"`
}

type ImageBuildConfig struct {
	BaseImage string           `yaml:"base_image"`
	Flow      AliyunFlowConfig `yaml:"flow"`
}

type AliyunFlowConfig struct {
	PipelineURL string `yaml:"pipeline_url"`
	Token       string `yaml:"token"`
}

type TemplateBuildConfig struct {
	DiskSize     string                `yaml:"disk_size"`
	CPU          uint32                `yaml:"cpu"`
	Memory       uint32                `yaml:"memory"`
	Network      TemplateNetworkConfig `yaml:"network"`
	RegistryAuth RegistryAuthConfig    `yaml:"registry_auth"`
}

type TemplateNetworkConfig struct {
	DNSServers         []string `yaml:"dns_servers"`
	AllowOutboundCIDRs []string `yaml:"allow_outbound_cidrs"`
	DenyOutboundCIDRs  []string `yaml:"deny_outbound_cidrs"`
	AllowInternet      bool     `yaml:"allow_internet"`
	InjectEgressCA     bool     `yaml:"inject_egress_ca"`
}

type RegistryAuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func normalizeEnvironmentPrebuildConfig(prebuild *EnvironmentPrebuildConfig) {
	image := &prebuild.Image
	template := &prebuild.Template
	image.BaseImage = strings.TrimSpace(image.BaseImage)
	image.Flow.PipelineURL = strings.TrimRight(strings.TrimSpace(image.Flow.PipelineURL), "/")
	image.Flow.Token = strings.TrimSpace(image.Flow.Token)
	template.DiskSize = strings.TrimSpace(template.DiskSize)
	template.RegistryAuth.Username = strings.TrimSpace(template.RegistryAuth.Username)
	for _, values := range [][]string{template.Network.DNSServers, template.Network.AllowOutboundCIDRs, template.Network.DenyOutboundCIDRs} {
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
		}
	}
}

func validateEnvironmentPrebuildConfig(prebuild EnvironmentPrebuildConfig, e2b E2BConfig) error {
	if !prebuild.Enabled {
		return nil
	}
	if prebuild.Timeout <= 0 {
		return errors.New("environment_prebuilds.timeout must be positive")
	}
	if prebuild.Template.DiskSize == "" {
		return errors.New("environment_prebuilds.template.disk_size is required")
	}
	if err := validateImageBaseImage(prebuild.Image); err != nil {
		return err
	}
	auth := prebuild.Template.RegistryAuth
	if (auth.Username == "") != (auth.Password == "") {
		return errors.New("environment_prebuilds.template.registry_auth username and password must be configured together")
	}
	if prebuild.Image.Flow.Token == "" {
		return errors.New("environment_prebuilds.image.flow.token is required")
	}
	if e2b.APIKey == "" {
		return errors.New("e2b.api_key is required when environment_prebuilds is enabled")
	}
	for _, endpoint := range []struct{ field, value string }{
		{"environment_prebuilds.image.flow.pipeline_url", prebuild.Image.Flow.PipelineURL},
		{"e2b.api_url", e2b.APIURL},
	} {
		u, err := url.Parse(endpoint.value)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" {
			return fmt.Errorf("%s must be an HTTPS URL without credentials, query or fragment", endpoint.field)
		}
	}
	return nil
}

// validateImageBaseImage requires the base image to name its registry host
// explicitly. Without one, Docker resolves the first path component as a
// namespace on the implicit default registry (docker.io), so the prebuild would
// push to and pull from a repository the operator never configured.
func validateImageBaseImage(image ImageBuildConfig) error {
	host, repository, found := strings.Cut(image.Repository(), "/")
	if !found || host == "" || repository == "" {
		return fmt.Errorf("environment_prebuilds.image.base_image must include a registry and repository, got %q", image.BaseImage)
	}
	if !isExplicitImageRegistry(host) {
		return fmt.Errorf("environment_prebuilds.image.base_image must start with an explicit registry host such as registry.example.com/team/base, got %q in %q", host, image.BaseImage)
	}
	return nil
}

// isExplicitImageRegistry reports whether Docker treats the first component of an
// image reference as the registry host instead of a namespace on the default
// registry. It mirrors distribution/reference's splitDockerDomain rule: the
// component is a host when it contains "." or ":", is "localhost", or is not all
// lowercase. A component ending in ":" is a URL scheme such as "https:", not a
// host with a port.
func isExplicitImageRegistry(host string) bool {
	if strings.HasSuffix(host, ":") {
		return false
	}
	return strings.ContainsAny(host, ".:") ||
		strings.EqualFold(host, "localhost") ||
		strings.ToLower(host) != host
}

// Repository returns the base image's registry/repository without its tag or digest.
func (c ImageBuildConfig) Repository() string {
	repository, _, _ := strings.Cut(c.BaseImage, "@")
	// Preserve the registry's port when removing an optional tag.
	if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
		repository = repository[:colon]
	}
	return repository
}
