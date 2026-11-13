package environments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	managerArtifactInstallRoot   = "/tmp/oma/environment-manager"
	managerArtifactBinaryName    = "environment-manager"
	managerArtifactMaxBytes      = 512 << 20
	managerArtifactIdleTimeout   = time.Minute
	managerArtifactActivateLimit = 60 * time.Second
)

var errManagerArtifactIdle = errors.New("environment manager artifact download stalled")

type managerArtifact struct {
	SHA256 string
	Size   int64
	File   *os.File
}

type managerArtifactFileStamp struct {
	size    int64
	modTime time.Time
}

type managerArtifactCache struct {
	sha256 string
	size   int64
	path   string
}

type managerArtifactSource struct {
	cfg         config.EnvironmentManagerArtifactConfig
	client      *http.Client
	logger      *slog.Logger
	idleTimeout time.Duration

	mu       sync.Mutex
	baseCtx  context.Context
	cacheDir string
	cached   *managerArtifactCache
	stamp    managerArtifactFileStamp
	inflight *managerArtifactFetch
	fetches  sync.WaitGroup
}

type managerArtifactFetch struct {
	done chan struct{}
	err  error
}

func newManagerArtifactSource(cfg config.EnvironmentManagerArtifactConfig, client *http.Client, logger *slog.Logger) *managerArtifactSource {
	if !cfg.Enabled() {
		return nil
	}
	if client == nil {
		client = &http.Client{}
	}
	return &managerArtifactSource{cfg: cfg, client: client, logger: logging.LoggerOrDefault(logger), idleTimeout: managerArtifactIdleTimeout}
}

func (s *managerArtifactSource) Preload(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseCtx = ctx
	if s.cfg.URL != "" && s.cached == nil {
		s.startFetchLocked(managerArtifactFileStamp{})
	}
}

func (s *managerArtifactSource) Wait() {
	if s == nil {
		return
	}
	s.fetches.Wait()
}

func (s *managerArtifactSource) Load(ctx context.Context) (managerArtifact, error) {
	for {
		s.mu.Lock()
		stamp, err := s.currentStamp()
		if err != nil {
			s.mu.Unlock()
			return managerArtifact{}, err
		}
		if s.cached != nil && s.stamp == stamp {
			artifact, err := s.openCachedLocked()
			s.mu.Unlock()
			return artifact, err
		}
		fetch := s.startFetchLocked(stamp)
		s.mu.Unlock()
		select {
		case <-fetch.done:
			if fetch.err != nil {
				return managerArtifact{}, fetch.err
			}
		case <-ctx.Done():
			return managerArtifact{}, ctx.Err()
		}
	}
}

func (s *managerArtifactSource) currentStamp() (managerArtifactFileStamp, error) {
	if s.cfg.Path == "" {
		return managerArtifactFileStamp{}, nil
	}
	info, err := os.Stat(s.cfg.Path)
	if err != nil {
		return managerArtifactFileStamp{}, fmt.Errorf("stat environment manager artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return managerArtifactFileStamp{}, fmt.Errorf("environment manager artifact %s is not a regular file", s.cfg.Path)
	}
	return managerArtifactFileStamp{size: info.Size(), modTime: info.ModTime()}, nil
}

func (s *managerArtifactSource) openCachedLocked() (managerArtifact, error) {
	file, err := os.Open(s.cached.path)
	if err != nil {
		return managerArtifact{}, fmt.Errorf("open cached environment manager artifact: %w", err)
	}
	return managerArtifact{SHA256: s.cached.sha256, Size: s.cached.size, File: file}, nil
}

func (s *managerArtifactSource) startFetchLocked(stamp managerArtifactFileStamp) *managerArtifactFetch {
	if s.inflight != nil {
		return s.inflight
	}
	fetch := &managerArtifactFetch{done: make(chan struct{})}
	s.inflight = fetch
	ctx := s.baseCtx
	if ctx == nil {
		ctx = context.Background()
	}
	dir, dirErr := s.cacheDirLocked()
	s.fetches.Add(1)
	go func() {
		defer s.fetches.Done()
		startedAt := time.Now()
		populated, err := managerArtifactCache{}, dirErr
		if err == nil {
			populated, err = s.populate(ctx, dir, stamp)
		}
		s.finishFetch(fetch, populated, stamp, err)
		if err != nil {
			s.logger.WarnContext(ctx, "environment manager artifact load failed", "error", err)
			return
		}
		s.logger.InfoContext(ctx, "environment manager artifact loaded", "manager_sha256", populated.sha256, "size_bytes", populated.size, "duration_ms", time.Since(startedAt).Milliseconds())
	}()
	return fetch
}

func (s *managerArtifactSource) finishFetch(fetch *managerArtifactFetch, populated managerArtifactCache, stamp managerArtifactFileStamp, err error) {
	s.mu.Lock()
	if err == nil {
		if s.cached != nil && s.cached.path != populated.path {
			_ = os.Remove(s.cached.path)
		}
		s.cached = &populated
		s.stamp = stamp
	}
	s.inflight = nil
	fetch.err = err
	s.mu.Unlock()
	close(fetch.done)
}

func (s *managerArtifactSource) cacheDirLocked() (string, error) {
	if s.cacheDir != "" {
		return s.cacheDir, nil
	}
	dir, err := os.MkdirTemp("", "oma-environment-manager-")
	if err != nil {
		return "", fmt.Errorf("create environment manager artifact cache: %w", err)
	}
	s.cacheDir = dir
	return dir, nil
}

func (s *managerArtifactSource) populate(ctx context.Context, dir string, stamp managerArtifactFileStamp) (managerArtifactCache, error) {
	staging, err := os.CreateTemp(dir, "staging-*")
	if err != nil {
		return managerArtifactCache{}, fmt.Errorf("create environment manager artifact staging file: %w", err)
	}
	defer func() { _ = os.Remove(staging.Name()) }()
	sha, size, err := s.copySource(ctx, staging, stamp)
	if closeErr := staging.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("close environment manager artifact staging file: %w", closeErr)
	}
	if err != nil {
		return managerArtifactCache{}, err
	}
	target := filepath.Join(dir, sha)
	if err := os.Rename(staging.Name(), target); err != nil {
		return managerArtifactCache{}, fmt.Errorf("store environment manager artifact: %w", err)
	}
	return managerArtifactCache{sha256: sha, size: size, path: target}, nil
}

func (s *managerArtifactSource) copySource(ctx context.Context, writer io.Writer, stamp managerArtifactFileStamp) (string, int64, error) {
	if s.cfg.Path == "" {
		return s.download(ctx, writer)
	}
	file, err := os.Open(s.cfg.Path)
	if err != nil {
		return "", 0, fmt.Errorf("open environment manager artifact: %w", err)
	}
	defer file.Close()
	sha, size, err := copyManagerArtifact(writer, file, s.cfg.SHA256)
	if err != nil {
		return "", 0, err
	}
	after, err := s.currentStamp()
	if err != nil {
		return "", 0, err
	}
	if after != stamp || size != stamp.size {
		return "", 0, errors.New("environment manager artifact changed while reading; replace it atomically")
	}
	return sha, size, nil
}

func (s *managerArtifactSource) download(ctx context.Context, writer io.Writer) (string, int64, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(s.idleTimeout, func() { cancel(errManagerArtifactIdle) })
	defer timer.Stop()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.URL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("build environment manager artifact request: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("download environment manager artifact: %w", managerArtifactDownloadCause(ctx, err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("download environment manager artifact: unexpected status %d", response.StatusCode)
	}
	sha, size, err := copyManagerArtifact(writer, &idleTimeoutReader{reader: response.Body, timer: timer, timeout: s.idleTimeout}, s.cfg.SHA256)
	if err != nil {
		return "", 0, managerArtifactDownloadCause(ctx, err)
	}
	return sha, size, nil
}

func managerArtifactDownloadCause(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errManagerArtifactIdle) {
		return cause
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

type idleTimeoutReader struct {
	reader  io.Reader
	timer   *time.Timer
	timeout time.Duration
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.timer.Reset(r.timeout)
	}
	return n, err
}

func copyManagerArtifact(writer io.Writer, reader io.Reader, wantSHA256 string) (string, int64, error) {
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(writer, hasher), io.LimitReader(reader, managerArtifactMaxBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("read environment manager artifact: %w", err)
	}
	if size == 0 {
		return "", 0, errors.New("environment manager artifact is empty")
	}
	if size > managerArtifactMaxBytes {
		return "", 0, fmt.Errorf("environment manager artifact exceeds %d bytes", managerArtifactMaxBytes)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if wantSHA256 != "" && actual != wantSHA256 {
		return "", 0, fmt.Errorf("environment manager artifact sha256 = %s, want %s", actual, wantSHA256)
	}
	return actual, size, nil
}

func managerArtifactInstallPath(sha string) string {
	return path.Join(managerArtifactInstallRoot, sha, managerArtifactBinaryName)
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
	defer artifact.File.Close()
	target := managerArtifactInstallPath(artifact.SHA256)
	if err := r.provider.WriteFileFrom(ctx, sandboxID, target+".partial", artifact.File); err != nil {
		return "", r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_artifact_upload", errEnvironmentManagerInstall, err)
	}
	if err := r.runSandboxCommand(ctx, sandboxID, managerArtifactActivateCommand(target, artifact.SHA256), managerArtifactActivateLimit); err != nil {
		return "", r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_artifact_activate", errEnvironmentManagerInstall, err)
	}
	r.logger.InfoContext(ctx, "environment manager artifact installed", "sandbox_id", sandboxID, "manager_sha256", artifact.SHA256, "size_bytes", artifact.Size)
	return target, nil
}

func (r *Runner) environmentManagerConfig(managerPath string) config.Config {
	cfg := r.cfg
	if managerPath != "" {
		cfg.EnvironmentRunner.ManagerPath = managerPath
	}
	return cfg
}
