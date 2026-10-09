package environments

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type sandboxServiceEndpoint interface {
	ServiceEndpoint(context.Context, string, int, string) (string, error)
}

type sandboxMCPLaunchPayload struct {
	Version              int                          `json:"version"`
	Mode                 string                       `json:"mode"`
	SessionID            string                       `json:"session_id"`
	APIBaseURL           string                       `json:"api_base_url"`
	WorkDir              string                       `json:"cwd"`
	EnvironmentVariables map[string]string            `json:"environment_variables,omitempty"`
	Sources              []gitRepositoryRuntimeSource `json:"sources"`
	GitSSHToHTTPSHosts   []string                     `json:"git_ssh_to_https_hosts,omitempty"`
	MCPConfig            sandboxMCPDelegates          `json:"mcp_config"`
	MCP                  sandboxMCPServerConfig       `json:"mcp"`
	Auth                 []AuthConfig                 `json:"auth"`
}

type sandboxMCPDelegates struct {
	Servers map[string]sandboxMCPDelegate `json:"mcpServers"`
}

type sandboxMCPDelegate struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type sandboxMCPServerConfig struct {
	Port      int    `json:"port"`
	Path      string `json:"path"`
	Transport string `json:"transport"`
}

func sandboxMCPConfig(cfg config.EnvironmentRunnerConfig) sandboxMCPServerConfig {
	port, path := cfg.SandboxMCPPort, cfg.SandboxMCPPath
	if port == 0 {
		port = 8090
	}
	if path == "" {
		path = "/mcp"
	}
	return sandboxMCPServerConfig{Port: port, Path: path, Transport: "streamable_http"}
}

func (r *Runner) createHostRuntimeLaunch(ctx context.Context, preparation managedAgentLaunchPreparation, local codesessions.ManagedAgentCreateResult) (launch managedAgentRuntimeLaunch, err error) {
	defer func() {
		if err != nil && preparation.RecoveryCodeSessionID == "" {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = r.codeSessions.TerminateManagedAgentCodeSession(cleanupCtx, preparation.Session, local.CodeSessionID)
		}
	}()
	var resources struct {
		Sources []gitRepositoryRuntimeSource `json:"sources"`
	}
	if err := json.Unmarshal(preparation.SessionConfig, &resources); err != nil {
		return managedAgentRuntimeLaunch{}, err
	}
	if resources.Sources == nil {
		resources.Sources = []gitRepositoryRuntimeSource{}
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return managedAgentRuntimeLaunch{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	configuration, err := buildManagedAgentRuntimeMCPConfig(preparation.SessionConfig, local.CodeSessionID, local.SessionIngressToken, r.cfg)
	if err == nil {
		var resolved struct {
			MCPConfig sandboxMCPDelegates `json:"mcp_config"`
		}
		if err = json.Unmarshal(configuration, &resolved); err != nil {
			return managedAgentRuntimeLaunch{}, err
		}
		var payload []byte
		payload, err = json.Marshal(sandboxMCPLaunchPayload{
			Version: 1, Mode: "sandbox_mcp", SessionID: local.CodeSessionID,
			APIBaseURL: codeSessionSandboxAPIBaseURL(r.cfg), WorkDir: preparation.WorkDir,
			EnvironmentVariables: preparation.EnvPlaceholders, MCP: sandboxMCPConfig(r.cfg.EnvironmentRunner),
			MCPConfig: resolved.MCPConfig, Sources: resources.Sources, GitSSHToHTTPSHosts: r.cfg.EnvironmentRunner.GitSSHtoHTTPSHosts,
			Auth: []AuthConfig{{Type: "sandbox_mcp", Token: token}, {Type: "session_ingress", Token: local.SessionIngressToken}},
		})
		if err == nil {
			return managedAgentRuntimeLaunch{
				CodeSessionID: local.CodeSessionID, PublicSessionID: local.PublicSessionID, SDKURLPath: local.SDKURLPath,
				Mode: agentruntime.Host, Recovered: preparation.RecoveryCodeSessionID != "",
				Manager: buildSandboxMCPManagerCommand(local.CodeSessionID, preparation.WorkDir, r.cfg, payload),
				HostInput: agentruntime.StartInput{CodeSessionID: local.CodeSessionID, WorkerEpoch: local.WorkerEpoch,
					OAuthAccessToken: local.OAuthAccessToken, SessionConfig: configuration, MCPToken: token},
			}, nil
		}
	}
	return managedAgentRuntimeLaunch{}, err
}

func buildSandboxMCPManagerCommand(codeSessionID, workDir string, cfg config.Config, payload []byte) environmentManagerCommand {
	manager := cfg.EnvironmentRunner.ManagerPath
	if manager == "" {
		manager = defaultEnvironmentManagerPath
	}
	command := "set -eu\nexec " + shellQuote(manager) + " sandbox-mcp --session " + shellQuote(codeSessionID)
	if cfg.EnvironmentRunner.SandboxMCPCommand != "" {
		endpoint := sandboxMCPConfig(cfg.EnvironmentRunner)
		command = "set -eu\nexport OMA_SANDBOX_WORK_DIR=" + shellQuote(workDir) +
			"\nexport OMA_SANDBOX_MCP_PORT=" + shellQuote(strconv.Itoa(endpoint.Port)) +
			"\nexport OMA_SANDBOX_MCP_PATH=" + shellQuote(endpoint.Path) +
			"\nexec sh -c " + shellQuote(cfg.EnvironmentRunner.SandboxMCPCommand)
	}
	return environmentManagerCommand{Payload: append([]byte(nil), payload...), ShellCommand: command}
}

func (r *Runner) startHostAgent(ctx context.Context, sandboxID string, launch managedAgentRuntimeLaunch) error {
	resolver, ok := r.provider.(sandboxServiceEndpoint)
	if !ok {
		return errHostEndpointUnavailable
	}
	mcpConfig := sandboxMCPConfig(r.cfg.EnvironmentRunner)
	endpoint, err := resolver.ServiceEndpoint(ctx, sandboxID, mcpConfig.Port, mcpConfig.Path)
	if err != nil {
		return fmt.Errorf("resolve sandbox MCP endpoint: %w", err)
	}
	input := launch.HostInput
	input.MCPEndpoint = endpoint
	return r.hostAgents.Start(ctx, input)
}
