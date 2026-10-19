package config

import (
	"errors"
)

func validateBrainRuntimeConfig(cfg EnvironmentRunnerConfig, codeSession CodeSessionConfig) error {
	if cfg.AgentMode != "" && cfg.AgentMode != "sandbox" && cfg.AgentMode != "brain" {
		return errors.New("environment_runner.agent_mode must be sandbox or brain")
	}
	if cfg.AgentMode == "brain" && codeSession.BrainClaimTokenFile == "" {
		return errors.New("code_session.brain_claim_token_file is required when environment_runner.agent_mode is brain")
	}
	if cfg.SandboxMCPPort < 0 || cfg.SandboxMCPPort > 65535 {
		return errors.New("environment_runner.sandbox_mcp_port must be a valid TCP port")
	}
	if cfg.SandboxMCPPath != "" && cfg.SandboxMCPPath[0] != '/' {
		return errors.New("environment_runner.sandbox_mcp_path must begin with a slash")
	}
	return nil
}
