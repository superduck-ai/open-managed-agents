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

func TestBrainRuntimeConfigRequiresClaimToken(t *testing.T) {
	if err := validateBrainRuntimeConfig(EnvironmentRunnerConfig{AgentMode: "brain"}, CodeSessionConfig{}); err == nil {
		t.Fatal("brain mode accepted without a claim token file")
	}
	if err := validateBrainRuntimeConfig(EnvironmentRunnerConfig{AgentMode: "sandbox"}, CodeSessionConfig{}); err != nil {
		t.Fatalf("sandbox config: %v", err)
	}
	if err := validateBrainRuntimeConfig(EnvironmentRunnerConfig{AgentMode: "brain"}, CodeSessionConfig{BrainClaimTokenFile: "/run/secrets/brain"}); err != nil {
		t.Fatalf("Brain config: %v", err)
	}
}
