package environments

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestSandboxMCPCustomCommandPreservesInputAndWorkingDirectory(t *testing.T) {
	cfg := config.Config{EnvironmentRunner: config.EnvironmentRunnerConfig{
		SandboxMCPPort: 3001, SandboxMCPPath: "/mcp",
		SandboxMCPCommand: `printf '%s\n%s\n%s\n' "$OMA_SANDBOX_WORK_DIR" "$OMA_SANDBOX_MCP_PORT" "$OMA_SANDBOX_MCP_PATH"; cat`,
	}}
	workDir := "/workspace/project '$(false)'"
	payload := []byte(`{"auth":[{"token":"private-test-token"}]}`)
	command := buildSandboxMCPManagerCommand("cse_test", workDir, cfg, payload)
	if strings.Contains(command.ShellCommand, "private-test-token") || strings.Contains(command.ShellCommand, "environment-manager") {
		t.Fatal("custom startup exposed credentials or used manager")
	}
	process := exec.CommandContext(t.Context(), "sh", "-c", command.ShellCommand)
	process.Stdin = bytes.NewReader(command.Payload)
	output, err := process.Output()
	if err != nil || string(output) != workDir+"\n3001\n/mcp\n"+string(payload) {
		t.Fatalf("custom startup output=%q error=%v", output, err)
	}
}

func TestRuntimeModeRejectsInvalidSnapshotAndPersistedMode(t *testing.T) {
	if _, err := agentruntime.ResolveMode(json.RawMessage(`{"metadata":{"agent_runtime_mode":"bun"}}`), "sandbox"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, err := agentruntime.PersistedMode(json.RawMessage(`{"config":{"agent_runtime_mode":"bun"}}`)); err == nil {
		t.Fatal("invalid stored mode accepted")
	}
}

func TestRuntimeModeDefaultAndSnapshotOverride(t *testing.T) {
	for _, tc := range []struct {
		snapshot, fallback string
		want               agentruntime.Mode
	}{
		{`{}`, "", agentruntime.Sandbox}, {`{}`, "host", agentruntime.Host},
		{`{"metadata":{"agent_runtime_mode":"sandbox"}}`, "host", agentruntime.Sandbox},
		{`{"metadata":{"agent_runtime_mode":"host"}}`, "sandbox", agentruntime.Host},
	} {
		mode, err := agentruntime.ResolveMode(json.RawMessage(tc.snapshot), tc.fallback)
		if err != nil || mode != tc.want {
			t.Fatalf("mode=%s error=%v", mode, err)
		}
	}
	mode, err := agentruntime.PersistedMode(json.RawMessage(`{"config":{"agent_runtime_mode":"host"}}`))
	if err != nil || mode != agentruntime.Host {
		t.Fatalf("stored mode=%s error=%v", mode, err)
	}
}

func TestSandboxMCPCommandUsesStdinAndSeparateEntrypoint(t *testing.T) {
	payload := []byte(`{"auth":[{"type":"sandbox_mcp","token":"private-test-token"}]}`)
	command := buildSandboxMCPManagerCommand("cse_test", "/workspace", config.Config{}, payload)
	if !strings.Contains(command.ShellCommand, " sandbox-mcp --session 'cse_test'") || strings.Contains(command.ShellCommand, "private-test-token") || strings.Contains(command.ShellCommand, "claude") {
		t.Fatalf("unexpected command: %s", command.ShellCommand)
	}
	payload[0] = 'x'
	if command.Payload[0] != '{' {
		t.Fatal("launch payload shares mutable caller storage")
	}
	defaults := sandboxMCPConfig(config.EnvironmentRunnerConfig{})
	if defaults.Port != 8090 || defaults.Path != "/mcp" || defaults.Transport != "streamable_http" {
		t.Fatalf("MCP defaults=%#v", defaults)
	}
}
