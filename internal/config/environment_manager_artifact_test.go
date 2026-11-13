package config

import (
	"strings"
	"testing"
)

func TestValidateEnvironmentManagerArtifactFailures(t *testing.T) {
	sha := strings.Repeat("a", 64)
	tests := []struct {
		name     string
		artifact EnvironmentManagerArtifactConfig
		want     string
	}{
		{name: "path and url", artifact: EnvironmentManagerArtifactConfig{Path: "/srv/manager", URL: "https://example.com/manager", SHA256: sha}, want: "mutually exclusive"},
		{name: "malformed sha", artifact: EnvironmentManagerArtifactConfig{Path: "/srv/manager", SHA256: "abc"}, want: "64 lowercase"},
		{name: "sha without location", artifact: EnvironmentManagerArtifactConfig{SHA256: sha}, want: "requires"},
		{name: "url without sha", artifact: EnvironmentManagerArtifactConfig{URL: "https://example.com/manager"}, want: "sha256 is required"},
		{name: "relative url", artifact: EnvironmentManagerArtifactConfig{URL: "/manager", SHA256: sha}, want: "absolute http"},
		{name: "unsupported scheme", artifact: EnvironmentManagerArtifactConfig{URL: "file:///srv/manager", SHA256: sha}, want: "absolute http"},
		{name: "embedded credentials", artifact: EnvironmentManagerArtifactConfig{URL: "https://user:pass@example.com/manager", SHA256: sha}, want: "credentials"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateEnvironmentManagerArtifact(test.artifact)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateEnvironmentManagerArtifact() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateEnvironmentManagerArtifactAccepts(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, artifact := range []EnvironmentManagerArtifactConfig{
		{},
		{Path: "/srv/manager"},
		{Path: "/srv/manager", SHA256: sha},
		{URL: "https://example.com/manager", SHA256: sha},
	} {
		if err := validateEnvironmentManagerArtifact(artifact); err != nil {
			t.Fatalf("validateEnvironmentManagerArtifact(%+v) error = %v", artifact, err)
		}
	}
}

func TestNormalizeEnvironmentManagerArtifact(t *testing.T) {
	artifact := EnvironmentManagerArtifactConfig{Path: " /srv/manager ", URL: " ", SHA256: " " + strings.Repeat("A", 64) + " "}
	normalizeEnvironmentManagerArtifact(&artifact)
	if artifact.Path != "/srv/manager" || artifact.URL != "" || artifact.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("normalized artifact = %+v", artifact)
	}
}
