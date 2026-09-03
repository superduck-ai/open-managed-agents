package config

import (
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestSandboxLifecycleConfiguration(t *testing.T) {
	for _, value := range []string{"0s", "-1h"} {
		t.Run("reject "+value, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.SandboxLifecycle.IdleTimeout = 0
			if err := yaml.Unmarshal([]byte("idle_timeout: "+value), &cfg.SandboxLifecycle); err != nil {
				t.Fatal(err)
			}
			if err := validatePositiveValues(cfg); err == nil {
				t.Fatal("accepted nonpositive idle timeout")
			}
		})
	}
	input := newYAMLConfig()
	if err := yaml.Unmarshal([]byte("sandbox_lifecycle:\n  enabled: false\n  dry_run: false\n  idle_timeout: 2h\n"), &input); err != nil {
		t.Fatal(err)
	}
	got := input.resolve().SandboxLifecycle
	if got.Enabled || got.DryRun || got.IdleTimeout != 2*time.Hour {
		t.Fatalf("explicit configuration = %+v", got)
	}
	defaults := newYAMLConfig().resolve().SandboxLifecycle
	if !defaults.Enabled || !defaults.DryRun || defaults.IdleTimeout != 24*time.Hour {
		t.Fatalf("defaults = %+v", defaults)
	}
}
