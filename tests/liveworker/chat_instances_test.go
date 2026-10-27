//go:build integration || e2e || e2b_integration

package liveworker

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/listeners"
	"go.yaml.in/yaml/v3"
)

type chatPeer struct {
	url     string
	command *exec.Cmd
	done    chan struct{}
	exitErr error
}

func startChatPeer(t *testing.T) *chatPeer {
	t.Helper()
	cfg, err := config.Load()
	requireOK(t, err)
	addr, file, err := listeners.Reserve()
	requireOK(t, err)
	defer file.Close()
	cfg.Server.Addr = addr
	cfg.Server.DiagnosticsAddr = ""
	cfg.Database.AutoMigrate = false
	cfg.EnvironmentRunner.Enabled = false
	data, err := yaml.Marshal(cfg)
	requireOK(t, err)
	directory := t.TempDir()
	configPath := filepath.Join(directory, "peer.yaml")
	requireOK(t, os.WriteFile(configPath, data, 0o600))
	log, err := os.Create(filepath.Join(directory, "peer.log"))
	requireOK(t, err)
	t.Cleanup(func() { _ = log.Close() })
	binary := os.Getenv("VERIFY_BE_SERVER")
	if binary == "" {
		t.Fatal("VERIFY_BE_SERVER required")
	}
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "CONFIG_FILE="+configPath, "OMA_HTTP_LISTENER_FD=3")
	cmd.ExtraFiles = []*os.File{file}
	cmd.Stdout, cmd.Stderr = log, log
	requireOK(t, cmd.Start())
	requireOK(t, file.Close())
	peer := &chatPeer{url: "http://" + cfg.Server.Addr, command: cmd, done: make(chan struct{})}
	go func() { peer.exitErr = cmd.Wait(); close(peer.done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-peer.done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-peer.done
			t.Error("peer required forced termination")
		}
	})
	base := "http://" + cfg.Server.Addr
	client := http.Client{Timeout: time.Second}
	waitRealWorker(t, "second backend ready", func() bool {
		response, err := client.Get(base + "/readyz")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == 200
	})
	return peer
}

func TestChatInstances(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	peer := startChatPeer(t)
	e := newLiveEnv(t)
	previewSeen := make(chan struct{}, 4)
	var calls atomic.Int32
	model := chatModelFixture(t, previewSeen, &calls, false)
	configureChatModel(t, e, model.URL)
	f := e.newSessionWithSnapshot(t, json.RawMessage(`{"model":{"id":"claude-sonnet-4-6"}}`))
	remoteEnv := *e
	remoteEnv.url = peer.url
	remoteSession := *f
	remoteSession.env = &remoteEnv
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 60*time.Second))
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, &remoteSession)
	defer closeStream()
	startRealControlWorker(t, f, "")
	input := submitChat(t, f, "验证跨实例消息。")
	final := receiveChatAnswer(t, ctx, events, func() { previewSeen <- struct{}{} })
	verifyChatHistory(t, &remoteSession, input, final)
	waitChatIdle(t, f, 1)
	if calls.Load() != 1 {
		t.Fatal("cross-instance delivery duplicated model request")
	}
	chatProof(t, started, "cross_instance_preview_final")
	chatProof(t, started, "cross_instance_history")
}
