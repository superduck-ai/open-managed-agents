package environments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

const (
	managerArtifactInstallRoot   = "/tmp/oma/environment-manager"
	managerArtifactBinaryName    = "environment-manager"
	managerArtifactMaxBytes      = 512 << 20
	managerArtifactFetchTimeout  = 5 * time.Minute
	managerArtifactProbeTimeout  = 30 * time.Second
	managerArtifactActivateLimit = 60 * time.Second
)

type managerArtifact struct {
	SHA256 string
	Data   []byte
}

type managerArtifactFileStamp struct {
	size    int64
	modTime time.Time
}

type managerArtifactSource struct {
	cfg    config.EnvironmentManagerArtifactConfig
	client *http.Client

	mu     sync.Mutex
	cached *managerArtifact
	stamp  managerArtifactFileStamp
}

func newManagerArtifactSource(cfg config.EnvironmentManagerArtifactConfig, client *http.Client) *managerArtifactSource {
	if !cfg.Enabled() {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: managerArtifactFetchTimeout}
	}
	return &managerArtifactSource{cfg: cfg, client: client}
}

func (s *managerArtifactSource) Load(ctx context.Context) (managerArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Path != "" {
		return s.loadFile()
	}
	if s.cached != nil {
		return *s.cached, nil
	}
	artifact, err := s.fetch(ctx)
	if err != nil {
		return managerArtifact{}, err
	}
	s.cached = &artifact
	return artifact, nil
}

func (s *managerArtifactSource) loadFile() (managerArtifact, error) {
	info, err := os.Stat(s.cfg.Path)
	if err != nil {
		return managerArtifact{}, fmt.Errorf("stat environment manager artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return managerArtifact{}, fmt.Errorf("environment manager artifact %s is not a regular file", s.cfg.Path)
	}
	stamp := managerArtifactFileStamp{size: info.Size(), modTime: info.ModTime()}
	if s.cached != nil && s.stamp == stamp {
		return *s.cached, nil
	}
	file, err := os.Open(s.cfg.Path)
	if err != nil {
		return managerArtifact{}, fmt.Errorf("open environment manager artifact: %w", err)
	}
	defer file.Close()
	artifact, err := readManagerArtifact(file, s.cfg.SHA256)
	if err != nil {
		return managerArtifact{}, err
	}
	s.cached = &artifact
	s.stamp = stamp
	return artifact, nil
}

func (s *managerArtifactSource) fetch(ctx context.Context) (managerArtifact, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.URL, nil)
	if err != nil {
		return managerArtifact{}, fmt.Errorf("build environment manager artifact request: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return managerArtifact{}, fmt.Errorf("download environment manager artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return managerArtifact{}, fmt.Errorf("download environment manager artifact: unexpected status %d", response.StatusCode)
	}
	return readManagerArtifact(response.Body, s.cfg.SHA256)
}

func readManagerArtifact(reader io.Reader, wantSHA256 string) (managerArtifact, error) {
	data, err := io.ReadAll(io.LimitReader(reader, managerArtifactMaxBytes+1))
	if err != nil {
		return managerArtifact{}, fmt.Errorf("read environment manager artifact: %w", err)
	}
	if len(data) == 0 {
		return managerArtifact{}, errors.New("environment manager artifact is empty")
	}
	if len(data) > managerArtifactMaxBytes {
		return managerArtifact{}, fmt.Errorf("environment manager artifact exceeds %d bytes", managerArtifactMaxBytes)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if wantSHA256 != "" && actual != wantSHA256 {
		return managerArtifact{}, fmt.Errorf("environment manager artifact sha256 = %s, want %s", actual, wantSHA256)
	}
	return managerArtifact{SHA256: actual, Data: data}, nil
}

func managerArtifactInstallPath(sha string) string {
	return path.Join(managerArtifactInstallRoot, sha, managerArtifactBinaryName)
}

func managerArtifactProbeCommand(target, sha string) string {
	quoted := shellQuote(target)
	return "[ -x " + quoted + " ] && [ \"$(sha256sum -- " + quoted + " | cut -d ' ' -f 1)\" = " + shellQuote(sha) + " ]"
}

func managerArtifactActivateCommand(target, sha string) string {
	staging := shellQuote(target + ".partial")
	quoted := shellQuote(target)
	return "set -eu\n" +
		"chmod 0755 " + staging + "\n" +
		"actual=$(sha256sum -- " + staging + " | cut -d ' ' -f 1)\n" +
		"if [ \"$actual\" != " + shellQuote(sha) + " ]; then rm -f -- " + staging + "; printf 'environment-manager checksum mismatch: %s\\n' \"$actual\" >&2; exit 1; fi\n" +
		staging + " --version >/dev/null\n" +
		"mv -f -- " + staging + " " + quoted
}

func (r *Runner) installEnvironmentManager(ctx context.Context, sandboxID string) (string, error) {
	if r.managerArtifacts == nil {
		return "", nil
	}
	artifact, err := r.managerArtifacts.Load(ctx)
	if err != nil {
		return "", r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_artifact_load", errEnvironmentManagerInstall, err)
	}
	target := managerArtifactInstallPath(artifact.SHA256)
	probe, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: managerArtifactProbeCommand(target, artifact.SHA256),
		Timeout: managerArtifactProbeTimeout,
	})
	if err != nil {
		return "", r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_artifact_probe", errEnvironmentManagerInstall, err)
	}
	if probe.ExitCode == 0 {
		r.logger.InfoContext(ctx, "environment manager artifact reused", "sandbox_id", sandboxID, "manager_sha256", artifact.SHA256)
		return target, nil
	}
	if err := r.provider.WriteFile(ctx, sandboxID, target+".partial", artifact.Data); err != nil {
		return "", r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_artifact_upload", errEnvironmentManagerInstall, err)
	}
	if err := r.runSandboxCommand(ctx, sandboxID, managerArtifactActivateCommand(target, artifact.SHA256), managerArtifactActivateLimit); err != nil {
		return "", r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_artifact_activate", errEnvironmentManagerInstall, err)
	}
	r.logger.InfoContext(ctx, "environment manager artifact installed", "sandbox_id", sandboxID, "manager_sha256", artifact.SHA256, "size_bytes", len(artifact.Data))
	return target, nil
}

func (r *Runner) environmentManagerConfig(managerPath string) config.Config {
	cfg := r.cfg
	if managerPath != "" {
		cfg.EnvironmentRunner.ManagerPath = managerPath
	}
	return cfg
}
