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

	"github.com/superduck-ai/open-managed-agents/internal/common/collections"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"

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
	Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork, resolution Resolution) (Sandbox, error)
	Kill(ctx context.Context, sandboxID string) error
	Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error)
	WriteFile(ctx context.Context, sandboxID string, path string, data []byte) error
	FileExists(ctx context.Context, sandboxID string, path string) (bool, error)
	RunCommand(ctx context.Context, sandboxID string, command string, timeout time.Duration) error
	StartBackgroundCommand(ctx context.Context, sandboxID string, command string, stdin []byte) error
}

type E2BProvider struct {
	cfg config.E2BConfig
}

func NewProvider(cfg config.E2BConfig) *E2BProvider {
	return &E2BProvider{cfg: cfg}
}

func ConnectionOptsFromConfig(cfg config.E2BConfig) e2b.ConnectionOpts {
	requestTimeoutMs := int(cfg.RequestTimeout / time.Millisecond)
	debug := cfg.Debug
	return e2b.ConnectionOpts{
		ApiKey:           cfg.APIKey,
		AccessToken:      cfg.AccessToken,
		Domain:           cfg.Domain,
		ApiUrl:           cfg.APIURL,
		SandboxUrl:       cfg.SandboxURL,
		Debug:            &debug,
		RequestTimeoutMs: &requestTimeoutMs,
	}
}

func (p *E2BProvider) Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error) {
	template := strings.TrimSpace(env.ResolvedTemplate)
	if template == "" {
		template = strings.TrimSpace(p.cfg.Template)
	}
	if template == "" {
		template = "claude-code-interpreter"
	}
	resolved := Resolution{
		Template:            template,
		Metadata:            map[string]string{"environment_id": env.ExternalID, "workspace_id": fmt.Sprint(env.WorkspaceID)},
		Envs:                map[string]string{"ANTHROPIC_ENVIRONMENT_ID": env.ExternalID},
		Timeout:             p.cfg.SandboxTimeout,
		AllowInternetAccess: true,
	}
	if work != nil {
		resolved.Metadata["work_id"] = work.ExternalID
		resolved.Envs["ANTHROPIC_WORK_ID"] = work.ExternalID
	}

	network, allowInternet, err := resolveNetwork(env.Config, work)
	if err != nil {
		return Resolution{}, err
	}
	resolved.AllowInternetAccess = allowInternet
	resolved.Network = network
	return resolved, nil
}

func (p *E2BProvider) Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork, resolved Resolution) (Sandbox, error) {
	if strings.TrimSpace(p.cfg.APIKey) == "" && !p.cfg.Debug {
		return Sandbox{}, errors.New("e2b.api_key is required to create a sandbox")
	}
	if strings.TrimSpace(resolved.Template) == "" {
		var err error
		resolved, err = p.Resolve(env, work)
		if err != nil {
			return Sandbox{}, err
		}
	}
	timeoutMs := int(resolved.Timeout / time.Millisecond)
	if timeoutMs <= 0 {
		timeoutMs = int((5 * time.Minute) / time.Millisecond)
	}
	allowInternet := resolved.AllowInternetAccess
	opts := &e2b.SandboxOpts{
		ConnectionOpts:      ConnectionOptsFromConfig(p.cfg),
		Metadata:            resolved.Metadata,
		Envs:                resolved.Envs,
		TimeoutMs:           &timeoutMs,
		AllowInternetAccess: &allowInternet,
		Network:             resolved.Network,
	}
	if volumeMounts := p.sandboxVolumeMounts(work); len(volumeMounts) > 0 {
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

func (p *E2BProvider) FileExists(ctx context.Context, sandboxID string, filePath string) (bool, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return false, errors.New("sandbox id is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return false, errors.New("sandbox file path is required")
	}
	sandbox, err := p.connect(ctx, sandboxID)
	if err != nil {
		return false, err
	}
	return sandbox.Files.Exists(ctx, filePath, nil)
}

func (p *E2BProvider) RunCommand(ctx context.Context, sandboxID string, command string, timeout time.Duration) error {
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
	timeoutMs := int(timeout / time.Millisecond)
	if timeoutMs <= 0 {
		timeoutMs = int(p.cfg.RequestTimeout / time.Millisecond)
	}
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

// StartBackgroundCommand 通过 E2B 进程 API 启动后台命令。
// stdin 非空时直接写入进程并关闭 EOF；为空时不启用 stdin 通道。
func (p *E2BProvider) StartBackgroundCommand(ctx context.Context, sandboxID string, command string, stdin []byte) error {
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
	opts := &e2b.CommandStartOpts{Background: true}
	if len(stdin) != 0 {
		stdinEnabled := true
		opts.Stdin = &stdinEnabled
	}
	execution, err := sandbox.Commands.Run(ctx, command, opts)
	if err != nil {
		return fmt.Errorf("start sandbox background command: %w", err)
	}
	handle, ok := execution.(*e2b.CommandHandle)
	if !ok {
		return fmt.Errorf("sandbox background command execution type = %T, want *e2b.CommandHandle", execution)
	}
	defer handle.Disconnect()
	if handle.Pid == 0 {
		return errors.New("sandbox background command returned an invalid PID")
	}
	if len(stdin) == 0 {
		return nil
	}
	if err := sandbox.Commands.SendStdin(ctx, handle.Pid, stdin, nil); err != nil {
		_, _ = handle.Kill()
		return fmt.Errorf("send sandbox command stdin: %w", err)
	}
	if err := sandbox.Commands.CloseStdin(ctx, handle.Pid, nil); err != nil {
		_, _ = handle.Kill()
		return fmt.Errorf("close sandbox command stdin: %w", err)
	}
	return nil
}

func (p *E2BProvider) connect(ctx context.Context, sandboxID string) (*e2b.Sandbox, error) {
	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: ConnectionOptsFromConfig(p.cfg),
	})
	if err != nil {
		return nil, err
	}
	return sandbox, nil
}

func resolveNetwork(raw json.RawMessage, work *db.EnvironmentWork) (*e2b.SandboxNetworkOpts, bool, error) {
	if len(raw) == 0 {
		return nil, true, nil
	}
	config, err := networkpolicy.ParseConfig(raw)
	if err != nil {
		if errors.Is(err, networkpolicy.ErrMalformedConfig) {
			return nil, false, err
		}
		// 未知 networking 类型 fail closed，与既有行为一致。
		return nil, false, nil
	}
	if config.Type == networkpolicy.TypeUnrestricted {
		return nil, true, nil
	}
	hosts := config.AllowedHostPatterns()
	if config.AllowPackageManagers {
		hosts = append(hosts, networkpolicy.PackageManagerHosts()...)
	}
	if config.AllowMCPServers {
		mcpAllowedHosts, err := mcpAllowedHostsFromWork(work)
		if err != nil {
			return nil, false, err
		}
		hosts = append(hosts, mcpAllowedHosts...)
	}
	return &e2b.SandboxNetworkOpts{AllowOut: collections.UniqueTrimmedStrings(hosts)}, false, nil
}

func mcpAllowedHostsFromWork(work *db.EnvironmentWork) ([]string, error) {
	if work == nil {
		return nil, nil
	}
	return networkpolicy.ParseWorkMetadataMCPAllowedHosts(work.Metadata)
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

func (p *E2BProvider) sandboxVolumeMounts(_ *db.EnvironmentWork) map[string]any {
	return map[string]any{
		sandboxUserDataMountPath: sandboxUserDataVolumeName,
	}
}
