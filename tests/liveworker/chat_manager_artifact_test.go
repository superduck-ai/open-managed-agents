//go:build integration || e2e || e2b_integration

package liveworker

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
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
	provider, stop := startPublicRunner(t, e, func(cfg *config.Config) {
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
