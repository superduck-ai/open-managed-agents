package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

var environmentManagerArtifactSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func normalizeEnvironmentManagerArtifact(artifact *EnvironmentManagerArtifactConfig) {
	artifact.Path = strings.TrimSpace(artifact.Path)
	artifact.URL = strings.TrimSpace(artifact.URL)
	artifact.SHA256 = strings.ToLower(strings.TrimSpace(artifact.SHA256))
	artifact.Version = strings.TrimSpace(artifact.Version)
}

func validateEnvironmentManagerArtifact(artifact EnvironmentManagerArtifactConfig) error {
	const prefix = "environment_runner.manager_artifact"
	if artifact.Path != "" && artifact.URL != "" {
		return fmt.Errorf("%s.path and %s.url are mutually exclusive", prefix, prefix)
	}
	if artifact.SHA256 != "" && !environmentManagerArtifactSHA256Pattern.MatchString(artifact.SHA256) {
		return fmt.Errorf("%s.sha256 must be 64 lowercase hexadecimal characters", prefix)
	}
	if artifact.Version != "" {
		canonical := semver.Canonical("v" + artifact.Version)
		version, _, _ := strings.Cut(artifact.Version, "+")
		if canonical == "" || canonical != "v"+version {
			return fmt.Errorf("%s.version must be a semantic version such as 0.1.0", prefix)
		}
		if !artifact.Enabled() {
			return fmt.Errorf("%s.version requires %s.path or %s.url", prefix, prefix, prefix)
		}
	}
	if !artifact.Enabled() {
		if artifact.SHA256 != "" {
			return fmt.Errorf("%s.sha256 requires %s.path or %s.url", prefix, prefix, prefix)
		}
		return nil
	}
	if artifact.URL == "" {
		return nil
	}
	if artifact.SHA256 == "" {
		return fmt.Errorf("%s.sha256 is required when %s.url is set", prefix, prefix)
	}
	parsed, err := url.Parse(artifact.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errors.New(prefix + ".url must be an absolute http or https URL")
	}
	if parsed.User != nil {
		return errors.New(prefix + ".url must not embed credentials")
	}
	return nil
}
