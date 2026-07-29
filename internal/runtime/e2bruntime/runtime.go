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

type CommandRequest struct {
	Command string
	Stdin   []byte
	Timeout time.Duration
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type Provider interface {
	Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork, resolution Resolution) (Sandbox, error)
	Kill(ctx context.Context, sandboxID string) error
	Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error)
	WriteFile(ctx context.Context, sandboxID string, path string, data []byte) error
	FileExists(ctx context.Context, sandboxID string, path string) (bool, error)
	RunCommand(ctx context.Context, sandboxID string, request CommandRequest) (CommandResult, error)
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
		template = config.DefaultE2BTemplate
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

func (p *E2BProvider) RunCommand(ctx context.Context, sandboxID string, request CommandRequest) (CommandResult, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return CommandResult{}, errors.New("sandbox id is required")
	}
	if strings.TrimSpace(request.Command) == "" {
		return CommandResult{}, errors.New("sandbox command is required")
	}
	if request.Timeout <= 0 {
		request.Timeout = p.cfg.RequestTimeout
	}
	return runConnectedCommand(ctx, request, func(connectCtx context.Context) (commandStarter, error) {
		sandbox, err := p.connect(connectCtx, sandboxID)
		if err != nil {
			return nil, err
		}
		return startE2BCommand(sandbox.Commands), nil
	})
}

type commandProcess interface {
	SendStdin(context.Context, []byte) error
	CloseStdin(context.Context) error
	Wait() (CommandResult, error)
	Kill(context.Context) error
	Disconnect()
}

type commandStarter func(context.Context, CommandRequest) (commandProcess, error)

type commandConnector func(context.Context) (commandStarter, error)

type commandWaitOutcome struct {
	result CommandResult
	err    error
}

func runConnectedCommand(ctx context.Context, request CommandRequest, connect commandConnector) (CommandResult, error) {
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
		request.Timeout = timeout
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start, err := connect(commandCtx)
	if err != nil {
		return CommandResult{}, err
	}
	return executeCommand(commandCtx, request, start)
}

func executeCommand(ctx context.Context, request CommandRequest, start commandStarter) (CommandResult, error) {
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	process, err := start(commandCtx, request)
	if err != nil {
		return CommandResult{}, fmt.Errorf("start sandbox command: %w", err)
	}
	if process == nil {
		return CommandResult{}, errors.New("start sandbox command: missing process handle")
	}
	defer process.Disconnect()
	if len(request.Stdin) != 0 {
		if err := sendCommandStdin(commandCtx, process, request.Stdin); err != nil {
			return CommandResult{}, err
		}
	}

	waited := make(chan commandWaitOutcome, 1)
	go func() {
		result, err := process.Wait()
		waited <- commandWaitOutcome{result: result, err: err}
	}()

	select {
	case outcome := <-waited:
		if outcome.err != nil {
			return CommandResult{}, errors.Join(
				fmt.Errorf("wait for sandbox command: %w", outcome.err),
				killCommandProcess(process),
			)
		}
		return outcome.result, nil
	case <-commandCtx.Done():
		return CommandResult{}, errors.Join(
			fmt.Errorf("sandbox command: %w", commandCtx.Err()),
			killCommandProcess(process),
		)
	}
}

func killCommandProcess(process commandProcess) error {
	killCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := process.Kill(killCtx); err != nil {
		return fmt.Errorf("kill sandbox command: %w", err)
	}
	return nil
}

func sendCommandStdin(ctx context.Context, process commandProcess, stdin []byte) error {
	if err := process.SendStdin(ctx, stdin); err != nil {
		return errors.Join(fmt.Errorf("send sandbox command stdin: %w", err), killCommandProcess(process))
	}
	if err := process.CloseStdin(ctx); err != nil {
		return errors.Join(fmt.Errorf("close sandbox command stdin: %w", err), killCommandProcess(process))
	}
	return nil
}

type e2bCommandProcess struct {
	commands *e2b.Commands
	handle   *e2b.CommandHandle
}

func startE2BCommand(commands *e2b.Commands) commandStarter {
	return func(ctx context.Context, request CommandRequest) (commandProcess, error) {
		opts := &e2b.CommandStartOpts{Background: true}
		if len(request.Stdin) != 0 {
			stdinEnabled := true
			opts.Stdin = &stdinEnabled
		}
		if request.Timeout > 0 {
			timeoutMs := int(request.Timeout / time.Millisecond)
			opts.TimeoutMs = &timeoutMs
		}
		execution, err := commands.Run(ctx, request.Command, opts)
		if err != nil {
			return nil, err
		}
		handle, ok := execution.(*e2b.CommandHandle)
		if !ok {
			return nil, fmt.Errorf("sandbox command execution type = %T, want *e2b.CommandHandle", execution)
		}
		if handle.Pid == 0 {
			handle.Disconnect()
			return nil, errors.New("sandbox background command returned an invalid PID")
		}
		return &e2bCommandProcess{commands: commands, handle: handle}, nil
	}
}

func (p *e2bCommandProcess) SendStdin(ctx context.Context, stdin []byte) error {
	return p.commands.SendStdin(ctx, p.handle.Pid, stdin, nil)
}

func (p *e2bCommandProcess) CloseStdin(ctx context.Context) error {
	return p.commands.CloseStdin(ctx, p.handle.Pid, nil)
}

func (p *e2bCommandProcess) Wait() (CommandResult, error) {
	result, err := p.handle.Wait()
	if err != nil {
		var exitErr *e2b.CommandExitError
		if !errors.As(err, &exitErr) {
			return CommandResult{}, err
		}
		result = &exitErr.CommandResult
	}
	return CommandResult{
		ExitCode: result.ExitCode,
		Stdout:   []byte(result.Stdout),
		Stderr:   []byte(result.Stderr),
	}, nil
}

func (p *e2bCommandProcess) Kill(ctx context.Context) error {
	_, err := p.commands.Kill(ctx, p.handle.Pid, nil)
	return err
}

func (p *e2bCommandProcess) Disconnect() {
	p.handle.Disconnect()
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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

func (p *E2BProvider) sandboxVolumeMounts(_ *db.EnvironmentWork) map[string]any {
	return map[string]any{
		sandboxUserDataMountPath: sandboxUserDataVolumeName,
	}
}
