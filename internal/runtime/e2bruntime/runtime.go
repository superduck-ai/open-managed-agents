package e2bruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

const (
	sandboxUserDataVolumeName = "user-data"
	sandboxUserDataMountPath  = "/mnt/user-data"
)

type Resolution struct {
	Template            string
	Metadata            map[string]string
	Envs                map[string]string
	Timeout             time.Duration
	AllowInternetAccess bool
	Network             *e2b.SandboxNetworkOpts
}

type Sandbox struct {
	ID string
}

type Provider interface {
	Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork) (Sandbox, error)
	Kill(ctx context.Context, sandboxID string) error
	Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error)
	WriteFile(ctx context.Context, sandboxID string, path string, data []byte) error
	RunCommand(ctx context.Context, sandboxID string, command string) error
}

type E2BProvider struct {
	cfg config.Config
}

func NewProvider(cfg config.Config) *E2BProvider {
	return &E2BProvider{cfg: cfg}
}

func (p *E2BProvider) Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error) {
	template := strings.TrimSpace(env.ResolvedTemplate)
	if template == "" {
		template = strings.TrimSpace(p.cfg.E2BTemplate)
	}
	if template == "" {
		template = "claude-code-interpreter"
	}
	resolved := Resolution{
		Template:            template,
		Metadata:            map[string]string{"environment_id": env.ExternalID, "workspace_id": fmt.Sprint(env.WorkspaceID)},
		Envs:                map[string]string{"ANTHROPIC_ENVIRONMENT_ID": env.ExternalID},
		Timeout:             p.cfg.E2BSandboxTimeout,
		AllowInternetAccess: true,
	}
	if work != nil {
		resolved.Metadata["work_id"] = work.ExternalID
		resolved.Envs["ANTHROPIC_WORK_ID"] = work.ExternalID
	}

	network, allowInternet, err := resolveNetwork(env.Config, mcpAllowedHostsFromWork(work))
	if err != nil {
		return Resolution{}, err
	}
	resolved.AllowInternetAccess = allowInternet
	resolved.Network = network
	return resolved, nil
}

func (p *E2BProvider) Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork) (Sandbox, error) {
	if strings.TrimSpace(p.cfg.E2BAPIKey) == "" && !p.cfg.E2BDebug {
		return Sandbox{}, errors.New("E2B_API_KEY is required to create a sandbox")
	}
	resolved, err := p.Resolve(env, work)
	if err != nil {
		return Sandbox{}, err
	}
	timeoutMs := int(resolved.Timeout / time.Millisecond)
	if timeoutMs <= 0 {
		timeoutMs = int((5 * time.Minute) / time.Millisecond)
	}
	requestTimeoutMs := int(p.cfg.E2BRequestTimeout / time.Millisecond)
	debug := p.cfg.E2BDebug
	allowInternet := resolved.AllowInternetAccess
	opts := &e2b.SandboxOpts{
		ConnectionOpts: e2b.ConnectionOpts{
			ApiKey:           p.cfg.E2BAPIKey,
			AccessToken:      p.cfg.E2BAccessToken,
			Domain:           p.cfg.E2BDomain,
			ApiUrl:           p.cfg.E2BAPIURL,
			SandboxUrl:       p.cfg.E2BSandboxURL,
			Debug:            &debug,
			RequestTimeoutMs: &requestTimeoutMs,
		},
		Metadata:            resolved.Metadata,
		Envs:                resolved.Envs,
		TimeoutMs:           &timeoutMs,
		AllowInternetAccess: &allowInternet,
		Network:             resolved.Network,
	}
	if volumeMounts := p.sandboxVolumeMounts(); len(volumeMounts) > 0 {
		opts.VolumeMounts = volumeMounts
	}
	sandbox, err := e2b.Create(ctx, resolved.Template, opts)
	if err != nil {
		return Sandbox{}, err
	}
	return Sandbox{ID: sandbox.SandboxID}, nil
}

func (p *E2BProvider) Kill(ctx context.Context, sandboxID string) error {
	if strings.TrimSpace(sandboxID) == "" {
		return nil
	}
	sandbox, err := p.connect(ctx, sandboxID)
	if err != nil {
		return err
	}
	return sandbox.Kill(ctx, nil)
}

func (p *E2BProvider) WriteFile(ctx context.Context, sandboxID string, filePath string, data []byte) error {
	if strings.TrimSpace(sandboxID) == "" {
		return errors.New("sandbox id is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return errors.New("sandbox file path is required")
	}
	sandbox, err := p.connect(ctx, sandboxID)
	if err != nil {
		return err
	}
	if dir := pathpkg.Dir(filePath); dir != "." && dir != "/" {
		if _, err := sandbox.Commands.Run(ctx, "mkdir -p "+shellQuote(dir), nil); err != nil {
			return err
		}
	}
	_, err = sandbox.Files.Write(ctx, filePath, bytes.NewReader(data), nil)
	return err
}

func (p *E2BProvider) RunCommand(ctx context.Context, sandboxID string, command string) error {
	if strings.TrimSpace(sandboxID) == "" {
		return errors.New("sandbox id is required")
	}
	if strings.TrimSpace(command) == "" {
		return errors.New("sandbox command is required")
	}
	sandbox, err := p.connect(ctx, sandboxID)
	if err != nil {
		return err
	}
	timeoutMs := int(p.cfg.E2BRequestTimeout / time.Millisecond)
	if timeoutMs <= 0 {
		timeoutMs = int((60 * time.Second) / time.Millisecond)
	}
	execution, err := sandbox.Commands.Run(ctx, command, &e2b.CommandStartOpts{TimeoutMs: &timeoutMs})
	if err != nil {
		var exitErr *e2b.CommandExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("sandbox command exited with code %d: %s stdout=%q stderr=%q", exitErr.ExitCode, strings.TrimSpace(exitErr.Message), truncateCommandOutput(exitErr.Stdout), truncateCommandOutput(exitErr.Stderr))
		}
		return err
	}
	result, ok := execution.(*e2b.CommandResult)
	if !ok {
		return fmt.Errorf("sandbox command execution type = %T, want *e2b.CommandResult", execution)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("sandbox command exited with code %d: %s stdout=%q stderr=%q", result.ExitCode, strings.TrimSpace(result.Error), truncateCommandOutput(result.Stdout), truncateCommandOutput(result.Stderr))
	}
	return nil
}

func (p *E2BProvider) connect(ctx context.Context, sandboxID string) (*e2b.Sandbox, error) {
	requestTimeoutMs := int(p.cfg.E2BRequestTimeout / time.Millisecond)
	debug := p.cfg.E2BDebug
	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2b.ConnectionOpts{
			ApiKey:           p.cfg.E2BAPIKey,
			AccessToken:      p.cfg.E2BAccessToken,
			Domain:           p.cfg.E2BDomain,
			ApiUrl:           p.cfg.E2BAPIURL,
			SandboxUrl:       p.cfg.E2BSandboxURL,
			Debug:            &debug,
			RequestTimeoutMs: &requestTimeoutMs,
		},
	})
	if err != nil {
		return nil, err
	}
	return sandbox, nil
}

func resolveNetwork(raw json.RawMessage, mcpAllowedHosts []string) (*e2b.SandboxNetworkOpts, bool, error) {
	var config struct {
		Type       string `json:"type"`
		Networking *struct {
			Type                 string   `json:"type"`
			AllowedHosts         []string `json:"allowed_hosts"`
			AllowPackageManagers bool     `json:"allow_package_managers"`
			AllowMCPServers      bool     `json:"allow_mcp_servers"`
		} `json:"networking"`
	}
	if len(raw) == 0 {
		return nil, true, nil
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, false, err
	}
	if config.Type != "cloud" || config.Networking == nil || config.Networking.Type == "unrestricted" {
		return nil, true, nil
	}
	if config.Networking.Type != "limited" {
		return nil, false, nil
	}
	hosts := append([]string(nil), config.Networking.AllowedHosts...)
	if config.Networking.AllowPackageManagers {
		hosts = append(hosts, packageManagerHosts()...)
	}
	if config.Networking.AllowMCPServers {
		hosts = append(hosts, mcpAllowedHosts...)
	}
	return &e2b.SandboxNetworkOpts{AllowOut: uniqueStrings(hosts)}, false, nil
}

func mcpAllowedHostsFromWork(work *db.EnvironmentWork) []string {
	if work == nil || len(work.Metadata) == 0 || strings.TrimSpace(string(work.Metadata)) == "null" {
		return nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(work.Metadata, &metadata); err != nil {
		return nil
	}
	values, ok := metadata["mcp_allowed_hosts"].([]any)
	if !ok {
		return nil
	}
	hosts := make([]string, 0, len(values))
	for _, value := range values {
		host, ok := value.(string)
		if !ok {
			continue
		}
		hosts = append(hosts, host)
	}
	return uniqueStrings(hosts)
}

func packageManagerHosts() []string {
	return []string{
		"archive.ubuntu.com",
		"security.ubuntu.com",
		"pypi.org",
		"files.pythonhosted.org",
		"registry.npmjs.org",
		"proxy.golang.org",
		"sum.golang.org",
		"crates.io",
		"index.crates.io",
		"rubygems.org",
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func truncateCommandOutput(value string) string {
	value = strings.TrimSpace(value)
	const limit = 2048
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}

func (p *E2BProvider) sandboxVolumeMounts() map[string]any {
	return map[string]any{
		sandboxUserDataMountPath: sandboxUserDataVolumeName,
	}
}
