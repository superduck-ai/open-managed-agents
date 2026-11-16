//go:build integration || e2e || e2b_integration

package liveworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/mod/semver"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func configurePublicManagerArtifact(t *testing.T, cfg *config.Config) {
	t.Helper()
	path := os.Getenv("OMA_MANAGER_ARTIFACT_PATH")
	if path == "" {
		return
	}
	file, err := os.Open(path)
	requireOK(t, err)
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	closeErr := file.Close()
	requireOK(t, err)
	requireOK(t, closeErr)
	artifact := config.EnvironmentManagerArtifactConfig{SHA256: hex.EncodeToString(hash.Sum(nil))}
	switch mode := os.Getenv("OMA_MANAGER_ARTIFACT_MODE"); mode {
	case "path":
		artifact.Path = path
	case "url":
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, path)
		}))
		t.Cleanup(server.Close)
		artifact.URL = server.URL + "/environment-manager"
	default:
		t.Fatalf("unsupported OMA_MANAGER_ARTIFACT_MODE %q; use path or url", mode)
	}
	cfg.EnvironmentRunner.ManagerArtifact = artifact
	cfg.EnvironmentRunner.ManagerPath = "/oma-verification-must-use-external-manager"
	t.Logf("external Manager mode=%s sha256=%s", os.Getenv("OMA_MANAGER_ARTIFACT_MODE"), artifact.SHA256)
}

func verifyPublicManagerArtifactRejection(t *testing.T, e *liveEnv) {
	t.Helper()
	if os.Getenv("OMA_MANAGER_ARTIFACT_PATH") == "" {
		return
	}
	provider, stop := startPublicRunner(t, e, func(cfg *config.Config, _ *chatDockerProvider) {
		cfg.EnvironmentRunner.ManagerArtifact.SHA256 = strings.Repeat("0", 64)
	})
	defer stop()
	f := createPublicChat(t, e)
	waitRealWorker(t, "Manager checksum rejection stops work and removes sandbox", func() bool {
		work, err := e.database.GetLatestEnvironmentWorkForSession(t.Context(), e.key.WorkspaceUUID.String(), e.environment.ExternalID, f.session.UUID)
		requireOK(t, err)
		if work.State != "stopped" || provider.created.Load() != 1 {
			return false
		}
		return exec.CommandContext(t.Context(), "docker", "inspect", provider.name+"-1").Run() != nil
	})
	t.Log("external Manager checksum rejection verified with real sandbox cleanup")
}

func verifyPublicManagerVersions(t *testing.T) {
	t.Helper()
	version := os.Getenv("OMA_MANAGER_ARTIFACT_VERSION")
	if version == "" {
		return
	}
	artifactPath := os.Getenv("OMA_MANAGER_ARTIFACT_PATH")
	if artifactPath == "" || !semver.IsValid("v"+version) || semver.Compare("v"+version, "v0.1.0") <= 0 {
		t.Fatal("version matrix requires OMA_MANAGER_ARTIFACT_PATH and OMA_MANAGER_ARTIFACT_VERSION greater than 0.1.0")
	}
	for _, scenario := range []struct {
		name, seed, required string
		uploads              int32
		switched             bool
	}{
		{name: "older_version_upgrades", required: version, uploads: 1, switched: true},
		{name: "equal_version_reuses", seed: "image", required: version},
		{name: "newer_version_reuses", seed: "image", required: "0.1.0"},
		{name: "sha_artifact_reuses_and_switches", seed: "sha", required: version, switched: true},
	} {
		if !t.Run(scenario.name, func(t *testing.T) {
			started := time.Now()
			e := newLiveEnv(t)
			previewSeen := make(chan struct{}, 2)
			var calls atomic.Int32
			model := chatModelFixture(t, previewSeen, &calls, false)
			configureChatModel(t, e, model.URL)
			var artifactSHA string
			provider, stop := startPublicRunner(t, e, func(cfg *config.Config, provider *chatDockerProvider) {
				artifactSHA = cfg.EnvironmentRunner.ManagerArtifact.SHA256
				cfg.EnvironmentRunner.ManagerArtifact.Version = scenario.required
				cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
				if scenario.seed == "image" && cfg.EnvironmentRunner.ManagerArtifact.Path != "" {
					cfg.EnvironmentRunner.ManagerArtifact.Path = "/oma-artifact-must-not-be-loaded"
				}
				provider.prepare = func(ctx context.Context, id string) error {
					destination := "/opt/env-runner/environment-manager"
					if scenario.seed == "sha" {
						destination = "/opt/env-runner/" + artifactSHA + "/environment-manager"
					}
					if scenario.seed != "" {
						if _, err := managerSandboxCommand(ctx, id, `mkdir -p "$(dirname "$1")"`, destination); err != nil {
							return err
						}
						if output, err := exec.CommandContext(ctx, "docker", "cp", artifactPath, id+":"+destination).CombinedOutput(); err != nil {
							return fmt.Errorf("seed Manager artifact: %w: %s", err, output)
						}
					}
					output, err := managerSandboxCommand(ctx, id, `set -eu; cp /opt/env-runner/environment-manager /opt/env-runner/image-manager-before; /usr/local/bin/environment-manager --version`)
					if err != nil {
						return err
					}
					fields := strings.Fields(string(output))
					if len(fields) < 2 || !semver.IsValid("v"+fields[1]) {
						return fmt.Errorf("invalid image Manager version %q", output)
					}
					comparison := semver.Compare("v"+fields[1], "v"+scenario.required)
					if (scenario.switched && comparison >= 0) || (!scenario.switched && comparison < 0) {
						return fmt.Errorf("unexpected fixture version %s for required %s", fields[1], scenario.required)
					}
					return nil
				}
			})
			defer stop()
			verifyPublicTurn(t, e, previewSeen)
			if calls.Load() != 1 || provider.managerUploads.Load() != scenario.uploads {
				t.Fatalf("model calls=%d Manager uploads=%d, want 1 and %d", calls.Load(), provider.managerUploads.Load(), scenario.uploads)
			}
			expectedLink := "/opt/env-runner/environment-manager"
			if scenario.switched {
				expectedLink = "/opt/env-runner/" + artifactSHA + "/environment-manager"
			}
			output, err := managerSandboxCommand(t.Context(), provider.name+"-1", `set -eu
entry=/usr/local/bin/environment-manager
test "$(readlink "$entry")" = "$1"
cmp /opt/env-runner/image-manager-before /opt/env-runner/environment-manager
test "$(sha256sum "$entry" | cut -d ' ' -f 1)" = "$2"
test ! -e "/opt/env-runner/$2/environment-manager.partial"
readlink "$entry"
"$entry" --version`, expectedLink, artifactSHA)
			requireOK(t, err)
			_, versionOutput, found := strings.Cut(string(output), "\n")
			if !found {
				t.Fatalf("missing link and version evidence: %q", output)
			}
			fields := strings.Fields(versionOutput)
			if len(fields) < 2 || fields[1] != version {
				t.Fatalf("installed version output=%q, want %s", output, version)
			}
			t.Logf("Manager required=%s uploads=%d selected=%s", scenario.required, provider.managerUploads.Load(), strings.TrimSpace(string(output)))
			chatProof(t, started, "manager_"+scenario.name)
		}) {
			t.FailNow()
		}
	}
}

func managerSandboxCommand(ctx context.Context, id, script string, args ...string) ([]byte, error) {
	command := append([]string{"exec", id, "sh", "-c", script, "sh"}, args...)
	output, err := exec.CommandContext(ctx, "docker", command...).CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("sandbox assertion: %w: %s", err, output)
	}
	return output, nil
}
