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
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

const (
	sandboxUserDataVolumeName = "user-data"
	sandboxUserDataMountPath  = "/mnt/user-data"
)

type sandboxOperations interface {
	Create(ctx context.Context, template string, opts *e2b.SandboxOpts) (Sandbox, error)
	Kill(ctx context.Context, sandboxID string) error
	WriteFile(ctx context.Context, sandboxID string, filePath string, data []byte) error
	RunCommand(ctx context.Context, sandboxID string, command string, timeoutMs int) error
}

type e2bSandboxOperations struct {
	cfg config.Config
}

func newE2BSandboxOperations(cfg config.Config) *e2bSandboxOperations {
	return &e2bSandboxOperations{cfg: cfg}
}

func (p *E2BProvider) Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork, resolved Resolution) (Sandbox, error) {
	if isBlank(p.cfg.E2BAPIKey) && !p.cfg.E2BDebug {
		return Sandbox{}, errors.New("E2B_API_KEY is required to create a sandbox")
	}
	if isBlank(resolved.Template) {
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
	if volumeMounts := p.sandboxVolumeMounts(work); len(volumeMounts) > 0 {
		opts.VolumeMounts = volumeMounts
	}
	return p.sandboxOperations().Create(ctx, resolved.Template, opts)
}

func (p *E2BProvider) Kill(ctx context.Context, sandboxID string) error {
	if isBlank(sandboxID) {
		return nil
	}
	return p.sandboxOperations().Kill(ctx, sandboxID)
}

func (p *E2BProvider) WriteFile(ctx context.Context, sandboxID string, filePath string, data []byte) error {
	if isBlank(sandboxID) {
		return errors.New("sandbox id is required")
	}
	if isBlank(filePath) {
		return errors.New("sandbox file path is required")
	}
	return p.sandboxOperations().WriteFile(ctx, sandboxID, filePath, data)
}

func (p *E2BProvider) RunCommand(ctx context.Context, sandboxID string, command string) error {
	if isBlank(sandboxID) {
		return errors.New("sandbox id is required")
	}
	if isBlank(command) {
		return errors.New("sandbox command is required")
	}
	timeoutMs := int(p.cfg.E2BRequestTimeout / time.Millisecond)
	if timeoutMs <= 0 {
		timeoutMs = int((60 * time.Second) / time.Millisecond)
	}
	return p.sandboxOperations().RunCommand(ctx, sandboxID, command, timeoutMs)
}

func (p *E2BProvider) sandboxOperations() sandboxOperations {
	if p.sandboxes != nil {
		return p.sandboxes
	}
	return newE2BSandboxOperations(p.cfg)
}

func (o *e2bSandboxOperations) Create(ctx context.Context, template string, opts *e2b.SandboxOpts) (Sandbox, error) {
	sandbox, err := e2b.Create(ctx, template, opts)
	if err != nil {
		return Sandbox{}, err
	}
	return Sandbox{ID: sandbox.SandboxID}, nil
}

func (o *e2bSandboxOperations) Kill(ctx context.Context, sandboxID string) error {
	sandbox, err := o.connect(ctx, sandboxID)
	if err != nil {
		return err
	}
	return sandbox.Kill(ctx, nil)
}

func (o *e2bSandboxOperations) WriteFile(ctx context.Context, sandboxID string, filePath string, data []byte) error {
	sandbox, err := o.connect(ctx, sandboxID)
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

func (o *e2bSandboxOperations) RunCommand(ctx context.Context, sandboxID string, command string, timeoutMs int) error {
	sandbox, err := o.connect(ctx, sandboxID)
	if err != nil {
		return err
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

func (o *e2bSandboxOperations) connect(ctx context.Context, sandboxID string) (*e2b.Sandbox, error) {
	requestTimeoutMs := int(o.cfg.E2BRequestTimeout / time.Millisecond)
	debug := o.cfg.E2BDebug
	sandbox, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2b.ConnectionOpts{
			ApiKey:           o.cfg.E2BAPIKey,
			AccessToken:      o.cfg.E2BAccessToken,
			Domain:           o.cfg.E2BDomain,
			ApiUrl:           o.cfg.E2BAPIURL,
			SandboxUrl:       o.cfg.E2BSandboxURL,
			Debug:            &debug,
			RequestTimeoutMs: &requestTimeoutMs,
		},
	})
	if err != nil {
		return nil, err
	}
	return sandbox, nil
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
	if isBlank(mount.VolumeName) {
		return nil, false
	}
	return &mount, true
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func truncateCommandOutput(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "\uFFFD")
	const limit = 2048
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "...[truncated]"
}
