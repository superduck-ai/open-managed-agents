package e2bruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

const (
	sandboxUserDataVolumeName = "user-data"
	sandboxUserDataMountPath  = "/mnt/user-data"
	SandboxSkillsMountPath    = "/mnt/skills"
	SkillMountMetadataKey     = "managed_agent_skills_mount"
	skillMountManifestPath    = "manifest.json"
	skillMountReadyPath       = ".ready"
	skillMountVolumePrefix    = "managed-agent-skills-"
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

type SkillMount struct {
	MountPath      string                         `json:"mount_path"`
	VolumeName     string                         `json:"volume_name"`
	ManifestSHA256 string                         `json:"manifest_sha256"`
	Skills         []skillsapi.MountManifestSkill `json:"skills,omitempty"`
}

type Provider interface {
	Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork, resolution Resolution) (Sandbox, error)
	Kill(ctx context.Context, sandboxID string) error
	Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error)
	RunCommand(ctx context.Context, sandboxID string, request CommandRequest) (CommandResult, error)
	StartBackgroundCommand(ctx context.Context, sandboxID string, command string, stdin []byte) error
}

type SkillMountPreparer interface {
	PrepareSkillMount(ctx context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*SkillMount, error)
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

func (p *E2BProvider) RunCommand(ctx context.Context, sandboxID string, request CommandRequest) (CommandResult, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return CommandResult{}, errors.New("sandbox id is required")
	}
	if strings.TrimSpace(request.Command) == "" {
		return CommandResult{}, errors.New("sandbox command is required")
	}
	if len(request.Stdin) == 0 {
		return CommandResult{}, errors.New("sandbox command stdin is required")
	}
	sandbox, err := p.connect(ctx, sandboxID)
	if err != nil {
		return CommandResult{}, err
	}
	if request.Timeout <= 0 {
		request.Timeout = p.cfg.RequestTimeout
	}
	return executeCommand(ctx, request, startE2BCommand(sandbox.Commands))
}

type commandProcess interface {
	SendStdin(context.Context, []byte) error
	CloseStdin(context.Context) error
	Wait() (CommandResult, error)
	Kill(context.Context) error
	Disconnect()
}

type commandStarter func(context.Context, CommandRequest) (commandProcess, error)

type commandStartOutcome struct {
	process commandProcess
	err     error
}

type commandKillOutcome struct {
	attempted bool
	err       error
}

func executeCommand(ctx context.Context, request CommandRequest, start commandStarter) (CommandResult, error) {
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	processCtx, cancelProcess := context.WithCancel(context.WithoutCancel(ctx))
	started := make(chan commandStartOutcome, 1)
	go func() {
		process, err := start(processCtx, request)
		started <- commandStartOutcome{process: process, err: err}
	}()

	var process commandProcess
	select {
	case outcome := <-started:
		if outcome.err != nil {
			cancelProcess()
			return CommandResult{}, fmt.Errorf("start sandbox command: %w", outcome.err)
		}
		if outcome.process == nil {
			cancelProcess()
			return CommandResult{}, errors.New("start sandbox command: missing process handle")
		}
		process = outcome.process
	case <-commandCtx.Done():
		cancelProcess()
		go cleanupLateCommandStart(started)
		return CommandResult{}, fmt.Errorf("start sandbox command: %w", commandCtx.Err())
	}
	defer cancelProcess()
	defer process.Disconnect()
	if err := sendCommandStdin(commandCtx, process, request.Stdin); err != nil {
		return CommandResult{}, err
	}

	watchDone := make(chan struct{})
	killed := make(chan commandKillOutcome, 1)
	go func() {
		select {
		case <-commandCtx.Done():
			killed <- commandKillOutcome{attempted: true, err: killCommandProcess(process)}
		case <-watchDone:
			killed <- commandKillOutcome{}
		}
	}()
	result, waitErr := process.Wait()
	close(watchDone)
	killOutcome := <-killed
	if commandCtx.Err() != nil {
		if !killOutcome.attempted {
			killOutcome.err = killCommandProcess(process)
		}
		return CommandResult{}, errors.Join(fmt.Errorf("sandbox command: %w", commandCtx.Err()), killOutcome.err)
	}
	if waitErr != nil {
		return CommandResult{}, errors.Join(fmt.Errorf("wait for sandbox command: %w", waitErr), killCommandProcess(process))
	}
	return result, nil
}

func cleanupLateCommandStart(started <-chan commandStartOutcome) {
	outcome := <-started
	if outcome.process == nil {
		return
	}
	_ = killCommandProcess(outcome.process)
	outcome.process.Disconnect()
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
		stdinEnabled := true
		opts := &e2b.CommandStartOpts{
			Background: true,
			Stdin:      &stdinEnabled,
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

// StartBackgroundCommand 通过 E2B 进程 API 启动后台命令，并把敏感启动数据直接写入其 stdin。
func (p *E2BProvider) StartBackgroundCommand(ctx context.Context, sandboxID string, command string, stdin []byte) error {
	if strings.TrimSpace(sandboxID) == "" {
		return errors.New("sandbox id is required")
	}
	if strings.TrimSpace(command) == "" {
		return errors.New("sandbox command is required")
	}
	if len(stdin) == 0 {
		return errors.New("sandbox command stdin is required")
	}
	sandbox, err := p.connect(ctx, sandboxID)
	if err != nil {
		return err
	}
	process, err := startE2BCommand(sandbox.Commands)(ctx, CommandRequest{Command: command, Stdin: stdin})
	if err != nil {
		return fmt.Errorf("start sandbox background command: %w", err)
	}
	defer process.Disconnect()
	return sendCommandStdin(ctx, process, stdin)
}
func (p *E2BProvider) PrepareSkillMount(ctx context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*SkillMount, error) {
	if len(runtimeSkills) == 0 {
		return nil, nil
	}
	manifest, manifestBytes, manifestSHA256, err := skillsapi.BuildMountManifest(runtimeSkills)
	if err != nil {
		return nil, err
	}
	volumeName := skillMountVolumeName(manifestSHA256)
	volume, created, err := p.connectOrCreateSkillVolume(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	if !created {
		ready, err := p.skillVolumeReady(ctx, volume, volumeName, manifestSHA256)
		if err != nil {
			return nil, err
		}
		if ready {
			return &SkillMount{
				MountPath:      SandboxSkillsMountPath,
				VolumeName:     volumeName,
				ManifestSHA256: manifestSHA256,
				Skills:         manifest.Skills,
			}, nil
		}
	}
	if err := p.writeSkillVolume(ctx, volume, manifestBytes, manifestSHA256, manifest, runtimeSkills); err != nil {
		return nil, err
	}
	return &SkillMount{
		MountPath:      SandboxSkillsMountPath,
		VolumeName:     volumeName,
		ManifestSHA256: manifestSHA256,
		Skills:         manifest.Skills,
	}, nil
}

func (p *E2BProvider) skillVolumeReady(ctx context.Context, volume *e2b.Volume, volumeName string, manifestSHA256 string) (bool, error) {
	exists, err := volume.Exists(ctx, skillMountReadyPath, p.volumeAPIOpts())
	if err != nil {
		return false, fmt.Errorf("check managed agent skill volume readiness %s: %w", volumeName, err)
	}
	if !exists {
		return false, nil
	}
	value, err := volume.ReadFile(ctx, skillMountReadyPath, p.volumeReadOpts())
	if err != nil {
		return false, fmt.Errorf("read managed agent skill volume ready marker %s: %w", volumeName, err)
	}
	var marker string
	switch typed := value.(type) {
	case string:
		marker = typed
	case []byte:
		marker = string(typed)
	default:
		return false, fmt.Errorf("managed agent skill volume ready marker %s has unsupported type %T", volumeName, value)
	}
	return strings.TrimSpace(marker) == strings.TrimSpace(manifestSHA256), nil
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

func (p *E2BProvider) connectOrCreateSkillVolume(ctx context.Context, volumeName string) (*e2b.Volume, bool, error) {
	opts := p.volumeConnectionOpts()
	volumes, err := e2b.ListVolumes(ctx, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list E2B volumes for managed agent skills: %w", err)
	}
	for _, volume := range volumes {
		if volume.Name != volumeName {
			continue
		}
		connected, err := e2b.ConnectVolume(ctx, volume.VolumeID, opts)
		if err != nil {
			return nil, false, fmt.Errorf("connect managed agent skill volume %s: %w", volumeName, err)
		}
		return connected, false, nil
	}
	created, err := e2b.CreateVolume(ctx, volumeName, opts)
	if err == nil {
		return created, true, nil
	}
	volumes, listErr := e2b.ListVolumes(ctx, opts)
	if listErr == nil {
		for _, volume := range volumes {
			if volume.Name != volumeName {
				continue
			}
			connected, connectErr := e2b.ConnectVolume(ctx, volume.VolumeID, opts)
			if connectErr != nil {
				return nil, false, fmt.Errorf("connect concurrently created managed agent skill volume %s: %w", volumeName, connectErr)
			}
			return connected, false, nil
		}
	}
	return nil, false, fmt.Errorf("create managed agent skill volume %s: %w", volumeName, err)
}

func (p *E2BProvider) writeSkillVolume(ctx context.Context, volume *e2b.Volume, manifestBytes []byte, manifestSHA256 string, manifest skillsapi.MountManifest, runtimeSkills []skillsapi.RuntimeSkill) error {
	skillsByFilename := make(map[string]skillsapi.RuntimeSkill, len(runtimeSkills))
	for _, skill := range runtimeSkills {
		skillsByFilename[skillsapi.MountArchiveFilename(skill)] = skill
	}
	opts := p.volumeWriteOpts()
	if _, err := volume.WriteFile(ctx, skillMountManifestPath, manifestBytes, opts); err != nil {
		return fmt.Errorf("write managed agent skill manifest: %w", err)
	}
	for _, entry := range manifest.Skills {
		skill, ok := skillsByFilename[entry.Filename]
		if !ok {
			return fmt.Errorf("managed agent skill archive %s is missing", entry.Filename)
		}
		archive, err := skill.LoadArchive(ctx)
		if err != nil {
			return err
		}
		if len(archive) == 0 {
			return fmt.Errorf("managed agent skill archive %s is empty", entry.Filename)
		}
		if _, err := volume.WriteFile(ctx, entry.Filename, archive, opts); err != nil {
			return fmt.Errorf("write managed agent skill archive %s: %w", entry.Filename, err)
		}
	}
	if _, err := volume.WriteFile(ctx, skillMountReadyPath, []byte(manifestSHA256+"\n"), opts); err != nil {
		return fmt.Errorf("write managed agent skill volume ready marker: %w", err)
	}
	return nil
}

func (p *E2BProvider) volumeConnectionOpts() *e2b.VolumeConnectionOpts {
	requestTimeoutMs := int(p.cfg.RequestTimeout / time.Millisecond)
	debug := p.cfg.Debug
	return &e2b.VolumeConnectionOpts{
		ApiKey:           p.cfg.APIKey,
		AccessToken:      p.cfg.AccessToken,
		Domain:           p.cfg.Domain,
		ApiUrl:           p.cfg.APIURL,
		SandboxUrl:       p.cfg.SandboxURL,
		Debug:            &debug,
		RequestTimeoutMs: &requestTimeoutMs,
	}
}

func (p *E2BProvider) volumeAPIOpts() *e2b.VolumeApiOpts {
	requestTimeoutMs := int(p.cfg.RequestTimeout / time.Millisecond)
	debug := p.cfg.Debug
	return &e2b.VolumeApiOpts{
		Domain:           p.cfg.Domain,
		Debug:            &debug,
		ApiUrl:           p.cfg.APIURL,
		RequestTimeoutMs: &requestTimeoutMs,
	}
}

func (p *E2BProvider) volumeReadOpts() *e2b.VolumeReadOpts {
	apiOpts := p.volumeAPIOpts()
	return &e2b.VolumeReadOpts{
		VolumeApiOpts: *apiOpts,
	}
}

func (p *E2BProvider) volumeWriteOpts() *e2b.VolumeWriteOptions {
	force := true
	mode := 0o644
	apiOpts := p.volumeAPIOpts()
	return &e2b.VolumeWriteOptions{
		VolumeMetadataOptions: e2b.VolumeMetadataOptions{Mode: &mode},
		Force:                 &force,
		Domain:                apiOpts.Domain,
		Debug:                 apiOpts.Debug,
		ApiUrl:                apiOpts.ApiUrl,
		RequestTimeoutMs:      apiOpts.RequestTimeoutMs,
	}
}

func (p *E2BProvider) sandboxVolumeMounts(work *db.EnvironmentWork) map[string]any {
	mounts := map[string]any{
		sandboxUserDataMountPath: sandboxUserDataVolumeName,
	}
	if skillMount, ok := skillMountFromWork(work); ok {
		mountPath := strings.TrimSpace(skillMount.MountPath)
		if mountPath == "" {
			mountPath = SandboxSkillsMountPath
		}
		mounts[mountPath] = strings.TrimSpace(skillMount.VolumeName)
	}
	return mounts
}

func skillMountFromWork(work *db.EnvironmentWork) (*SkillMount, bool) {
	if work == nil || len(work.Metadata) == 0 || strings.TrimSpace(string(work.Metadata)) == "null" {
		return nil, false
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(work.Metadata, &metadata); err != nil {
		return nil, false
	}
	raw, ok := metadata[SkillMountMetadataKey]
	if !ok || len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, false
	}
	var mount SkillMount
	if err := json.Unmarshal(raw, &mount); err != nil {
		return nil, false
	}
	if strings.TrimSpace(mount.VolumeName) == "" {
		return nil, false
	}
	return &mount, true
}

func skillMountVolumeName(manifestSHA256 string) string {
	sha := strings.TrimSpace(manifestSHA256)
	if sha == "" {
		sha = "unknown"
	}
	return skillMountVolumePrefix + sha
}
