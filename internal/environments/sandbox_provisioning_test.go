package environments

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestBrainSandboxManagerStartsEmbeddedMCP(t *testing.T) {
	cfg := config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://sandbox.example"}}
	cfg.EnvironmentRunner.AgentMode = "brain"
	launch, err := brainSandboxManagerCommand("cse_test", "/workspace/project", cfg, nil, "ingress-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(launch.ShellCommand, "export ENVRUNNER_SKIP_CLAUDE_CODE=true") ||
		!strings.Contains(launch.ShellCommand, "export MCP_ALLOWED_HOSTS=") ||
		!strings.Contains(launch.ShellCommand, "'/usr/local/bin/environment-manager' task-run") ||
		strings.Contains(launch.ShellCommand, "mcp-server") {
		t.Fatalf("unexpected launch command: %q", launch.ShellCommand)
	}
	var payload struct {
		Auth           []AuthConfig `json:"auth"`
		StartupContext struct {
			APIBaseURL           string            `json:"api_base_url"`
			UseCodeSessions      bool              `json:"use_code_sessions"`
			SessionID            string            `json:"session_id"`
			EnvironmentVariables map[string]string `json:"environment_variables"`
		} `json:"startup_context"`
		Environment struct {
			Type string `json:"environment_type"`
			CWD  string `json:"cwd"`
		} `json:"environment"`
	}
	if err := json.Unmarshal(launch.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.StartupContext.UseCodeSessions || payload.StartupContext.SessionID != "cse_test" || payload.StartupContext.EnvironmentVariables["CCR_UPSTREAM_PROXY_ENABLED"] != "1" || payload.StartupContext.EnvironmentVariables["CLAUDE_CODE_REMOTE"] != "true" || len(payload.Auth) != 1 || payload.Auth[0].Token != "ingress-test" {
		t.Fatal("relay startup configuration missing")
	}
	if payload.StartupContext.APIBaseURL != "http://sandbox.example" || payload.Environment.Type != "anthropic" || payload.Environment.CWD != "/workspace/project" {
		t.Fatalf("unexpected manager payload: %+v", payload)
	}
}

func TestBrainSandboxManagerReceivesRepositorySources(t *testing.T) {
	resource := db.SessionResource{ResourceType: "github_repository", GitHubRepository: &db.SessionResourceGitHubRepository{URL: "https://github.com/octocat/Hello-World.git", MountPath: "/workspace/probe", Checkout: &db.SessionResourceGitHubCheckout{Type: "commit", SHA: "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"}}, SecretPayload: json.RawMessage(`{"must_not_forward":"private-test-material"}`)}
	resolved, err := resolveManagedAgentRuntimeResources([]db.SessionResource{resource})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := brainSandboxManagerCommand("cse_owned", resolved.workDir, config.Config{}, nil, "ingress-test", resolved.sources)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Startup struct {
			Sources []gitRepositoryRuntimeSource `json:"sources"`
		} `json:"startup_context"`
	}
	if err := json.Unmarshal(launch.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Startup.Sources) != 1 {
		t.Fatal("Brain sandbox startup omitted repository sources")
	}
	source := payload.Startup.Sources[0]
	if source.Type != "git_repository" || source.MountPath != "/workspace/probe" || source.GitInfo.URL != "https://github.com/octocat/Hello-World.git" || source.GitInfo.Ref != "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d" {
		t.Fatal("repository source identity changed")
	}
	if strings.Contains(string(launch.Payload), "private-test-material") {
		t.Fatal("repository secret forwarded to sandbox")
	}
}
