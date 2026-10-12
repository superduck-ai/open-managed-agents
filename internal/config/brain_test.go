package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBrainSandboxPrewarm(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			body := requiredConfigTestYAML + fmt.Sprintf("\nenvironment_runner:\n  brain_sandbox_prewarm: %t\n", enabled)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(configFileEnv, path)
			cfg, err := Load()
			if err != nil || cfg.EnvironmentRunner.BrainSandboxPrewarm != enabled {
				t.Fatalf("prewarm config=%t err=%v", cfg.EnvironmentRunner.BrainSandboxPrewarm, err)
			}
		})
	}
}

func TestBrainRuntimeConfigUsesEmbeddedMCP(t *testing.T) {
	if err := validateBrainRuntimeConfig(EnvironmentRunnerConfig{AgentMode: "brain"}); err != nil {
		t.Fatalf("Brain config: %v", err)
	}
}
