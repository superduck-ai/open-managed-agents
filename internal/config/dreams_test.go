package config

import (
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestDreamsConfiguration(t *testing.T) {
	for _, value := range []string{"0s", "-1h"} {
		t.Run("reject "+value, func(t *testing.T) {
			cfg := defaultConfig()
			if err := yaml.Unmarshal([]byte("run_timeout: "+value), &cfg.Dreams); err != nil {
				t.Fatal(err)
			}
			if err := validatePositiveValues(cfg); err == nil {
				t.Fatal("accepted nonpositive Dream run timeout")
			}
		})
	}
	input := newYAMLConfig()
	if err := yaml.Unmarshal([]byte("dreams:\n  run_timeout: 45m\n"), &input); err != nil {
		t.Fatal(err)
	}
	if got := input.resolve().Dreams.RunTimeout; got != 45*time.Minute {
		t.Fatalf("explicit run_timeout = %s, want 45m", got)
	}
	if got := newYAMLConfig().resolve().Dreams.RunTimeout; got != 6*time.Hour {
		t.Fatalf("default run_timeout = %s, want 6h", got)
	}
	if !newYAMLConfig().resolve().Dreams.KeepRuntime {
		t.Fatal("default keep_runtime must be true")
	}
	keep := newYAMLConfig()
	if err := yaml.Unmarshal([]byte("dreams:\n  keep_runtime: false\n"), &keep); err != nil {
		t.Fatal(err)
	}
	resolved := keep.resolve()
	if resolved.Dreams.KeepRuntime {
		t.Fatal("explicit keep_runtime = true, want false")
	}
	if resolved.Dreams.RunTimeout != 6*time.Hour {
		t.Fatalf("keep_runtime must not reset run_timeout, got %s", resolved.Dreams.RunTimeout)
	}
}
