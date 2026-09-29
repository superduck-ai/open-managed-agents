package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/listeners"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type environment struct {
	backendRef, backendCommit, diagnosticsAddr string
	diagnostics                                bool
	files                                      bool
	suite                                      bool
	storageFault                               *storageFault
	root, directory, runID                     string
	compose                                    []string
	server                                     *exec.Cmd
	serverDone                                 chan error
	serverLog                                  *os.File
	diagnosticsFile                            *os.File
}

func newEnvironment(root, directory, runID string) *environment {
	return &environment{root: root, directory: directory, runID: runID, compose: []string{"docker", "compose", "-p", runID, "-f", filepath.Join(directory, "compose.json")}}
}
func (e *environment) env() []string {
	values := append(os.Environ(), "CONFIG_FILE="+filepath.Join(e.directory, "config.json"), "LIVE_WORKER_API_URL=http://127.0.0.1:18080", "LIVE_WORKER_REAL_CLAUDE=1", "TEST_API_KEY=sk-ant-local-default", "VERIFY_BE_SERVER="+filepath.Join(e.directory, "server"), "VERIFY_BE_PROFILE_READY="+filepath.Join(e.directory, "profile.ready"), "VERIFY_BE_RUN_ID="+e.runID, "VERIFY_BE_API_URL=http://127.0.0.1:18080")
	if e.storageFault != nil {
		values = append(values, "VERIFY_BE_STORAGE_CONTROL="+e.storageFault.control.URL)
	}
	return values
}
func (e *environment) composeArgs(args ...string) []string {
	return append(slices.Clone(e.compose), args...)
}
func (e *environment) logged(ctx context.Context, name string, timeout time.Duration, args ...string) error {
	return loggedCommand(ctx, e.root, e.env(), filepath.Join(e.directory, name), timeout, args...)
}
func (e *environment) port(ctx context.Context, name string, target int) (string, error) {
	addr, err := capture(ctx, e.root, e.composeArgs("port", name, strconv.Itoa(target))...)
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(addr)
	return port, err
}
func (e *environment) writeConfig(ctx context.Context) error {
	ports := map[string]string{}
	for name, target := range map[string]int{"postgres": 5432, "redis": 6379, "minio": 9000, "nats1": 4222, "nats2": 4222, "nats3": 4222} {
		port, err := e.port(ctx, name, target)
		if err != nil {
			return err
		}
		ports[name] = port
	}
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		return err
	}
	config := map[string]any{
		"environment_runner": map[string]bool{"enabled": false},
		"env":                "dev", "server": map[string]string{"addr": "127.0.0.1:18080"},
		"database":     map[string]string{"url": "postgresql://verify:verify@127.0.0.1:" + ports["postgres"] + "/verify?sslmode=disable"},
		"redis":        map[string]string{"url": "redis://127.0.0.1:" + ports["redis"]},
		"nats":         map[string]string{"url": fmt.Sprintf("nats://127.0.0.1:%s,nats://127.0.0.1:%s,nats://127.0.0.1:%s", ports["nats1"], ports["nats2"], ports["nats3"])},
		"tunnel":       map[string]string{"public_base_url": "http://127.0.0.1:18080"},
		"storage":      map[string]any{"type": "s3", "s3": map[string]any{"endpoint": "http://127.0.0.1:" + ports["minio"], "bucket": "verify-be", "region": "us-east-1", "access_key_id": "verifychat", "secret_access_key": "verifychat-local-only", "force_path_style": true}},
		"vault":        map[string]any{"master_key": map[string]any{"version": 1, "kek": base64.StdEncoding.EncodeToString(kek)}},
		"code_session": map[string]string{"jwt_signing_private_key_file": filepath.Join(e.directory, "jwt.pem")},
	}
	if e.files {
		storage := config["storage"].(map[string]any)
		fault, err := newStorageFault("http://127.0.0.1:" + ports["minio"])
		if err != nil {
			return err
		}
		e.storageFault = fault
		storage["s3"].(map[string]any)["endpoint"] = fault.proxy.URL
		storage["max_file_bytes"] = 65536
		storage["workspace_limit_bytes"] = 98304
	}
	if e.diagnostics {
		addr, file, err := listeners.Reserve()
		if err != nil {
			return err
		}
		e.diagnosticsAddr, e.diagnosticsFile = addr, file
		config["server"] = map[string]string{"addr": "127.0.0.1:18080", "diagnostics_addr": e.diagnosticsAddr}
	}
	if err := writeJSON(filepath.Join(e.directory, "config.json"), config); err != nil {
		return err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(e.directory, "jwt.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600)
}
func (e *environment) start(ctx context.Context, images map[string]string) error {
	if err := writeJSON(filepath.Join(e.directory, "compose.json"), dependencySpec(images)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.directory, "nats.conf"), []byte("max_payload: 2097152\njetstream { max_file_store: 16GB }\n"), 0o600); err != nil {
		return err
	}
	if err := e.logged(ctx, "dependencies.log", 180*time.Second, e.composeArgs("up", "-d", "--wait", "--wait-timeout", "120", "--pull", "never")...); err != nil {
		return err
	}
	if err := e.writeConfig(ctx); err != nil {
		return err
	}
	if e.suite {
		return nil
	}
	buildRoot, err := e.backendSource(ctx)
	if err != nil {
		return err
	}
	if err := loggedCommand(ctx, buildRoot, e.env(), filepath.Join(e.directory, "build.log"), 5*time.Minute, "./scripts/generate-go.sh"); err != nil {
		return err
	}
	if err := loggedCommand(ctx, buildRoot, e.env(), filepath.Join(e.directory, "build.log"), 10*time.Minute, "go", "build", "-o", filepath.Join(e.directory, "server"), "."); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(e.directory, "server.log"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	e.serverLog = log
	e.server = exec.Command(filepath.Join(e.directory, "server"))
	e.server.Dir, e.server.Env = e.root, e.env()
	e.server.Stdout, e.server.Stderr = log, log
	e.server.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if e.diagnosticsFile != nil {
		e.server.ExtraFiles = []*os.File{e.diagnosticsFile}
		e.server.Env = append(e.server.Env, "OMA_DIAGNOSTICS_LISTENER_FD=3")
	}
	if err := e.server.Start(); err != nil {
		return err
	}
	if e.diagnosticsFile != nil {
		_ = e.diagnosticsFile.Close()
		e.diagnosticsFile = nil
	}
	e.serverDone = make(chan error, 1)
	go func() { e.serverDone <- e.server.Wait(); close(e.serverDone) }()
	if err := os.WriteFile(filepath.Join(e.directory, "server.pid"), []byte(strconv.Itoa(e.server.Process.Pid)+"\n"), 0o600); err != nil {
		return err
	}
	return e.ready(ctx)
}
func (e *environment) ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	client := http.Client{Timeout: 2 * time.Second}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("backend readiness: %w", ctx.Err())
		case <-e.serverDone:
			return errors.New("backend exited before readiness; see server.log")
		case <-tick.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:18080/readyz", nil)
			if err != nil {
				return err
			}
			response, err := client.Do(req)
			if err != nil {
				continue
			}
			var status struct {
				Status string `json:"status"`
			}
			err = json.NewDecoder(response.Body).Decode(&status)
			_ = response.Body.Close()
			if err == nil && response.StatusCode == 200 && status.Status == "ready" {
				return nil
			}
		}
	}
}
func (e *environment) close() []string {
	failures := []string{}
	if e.diagnosticsFile != nil {
		_ = e.diagnosticsFile.Close()
		e.diagnosticsFile = nil
	}
	if e.serverDone != nil {
		_ = e.server.Process.Signal(syscall.SIGTERM)
		select {
		case <-e.serverDone:
		case <-time.After(20 * time.Second):
			_ = syscall.Kill(-e.server.Process.Pid, syscall.SIGKILL)
			<-e.serverDone
			failures = append(failures, "backend required forced termination")
		}
	}
	if e.serverLog != nil {
		_ = e.serverLog.Close()
	}
	if e.storageFault != nil {
		e.storageFault.close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	workers, err := capture(ctx, e.root, "docker", "ps", "-aq", "--filter", "label=oma.verify-be.run="+e.runID)
	if err == nil && workers != "" {
		err = e.logged(ctx, "cleanup.log", 30*time.Second, append([]string{"docker", "rm", "-f"}, strings.Fields(workers)...)...)
	}
	if err != nil {
		failures = append(failures, "Worker cleanup: "+err.Error())
	}
	if err := e.logged(ctx, "cleanup.log", 90*time.Second, e.composeArgs("down", "--volumes", "--remove-orphans", "--timeout", "5")...); err != nil {
		failures = append(failures, "dependencies cleanup: "+err.Error())
	}
	for _, filter := range []string{"label=oma.verify-be.run=" + e.runID, "label=com.docker.compose.project=" + e.runID} {
		remaining, err := capture(ctx, e.root, "docker", "ps", "-aq", "--filter", filter)
		if err != nil || remaining != "" {
			failures = append(failures, "could not confirm container cleanup: "+filter)
		}
	}
	for _, name := range []string{"config.json", "jwt.pem", "server", "server.pid"} {
		if err := os.Remove(filepath.Join(e.directory, name)); err != nil && !os.IsNotExist(err) {
			failures = append(failures, "could not remove "+name)
		}
	}
	for _, name := range []string{"backend-source", "backend.tar"} {
		if err := os.RemoveAll(filepath.Join(e.directory, name)); err != nil {
			failures = append(failures, "could not remove "+name)
		}
	}
	return failures
}
