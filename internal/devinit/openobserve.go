package devinit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type openObserveCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func localOpenObserveCredentials(repoRoot string, create bool) (openObserveCredentials, error) {
	path := filepath.Join(repoRoot, ".run", "secrets", "openobserve.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var credentials openObserveCredentials
		if json.Unmarshal(data, &credentials) != nil || credentials.Username == "" || credentials.Password == "" {
			return credentials, fmt.Errorf("invalid local OpenObserve credentials in %s", path)
		}
		return credentials, os.Chmod(path, 0o600)
	}
	if !os.IsNotExist(err) || !create {
		return openObserveCredentials{}, err
	}
	password := make([]byte, 32)
	if _, err := rand.Read(password); err != nil {
		return openObserveCredentials{}, err
	}
	credentials := openObserveCredentials{Username: "admin@oma.local", Password: "Oma!" + hex.EncodeToString(password) + "9"}
	data, err = json.Marshal(credentials)
	if err != nil {
		return credentials, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return credentials, err
	}
	return credentials, os.WriteFile(path, data, 0o600)
}

func ensureLocalOpenObserve(ctx context.Context, repoRoot string, binding EnvBinding) (int, error) {
	secretPath := filepath.Join(repoRoot, ".run", "secrets", "openobserve.json")
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		if exec.CommandContext(ctx, "docker", "volume", "inspect", composeProjectName(binding)+"_openobservedata").Run() == nil {
			return 0, fmt.Errorf("local OpenObserve data exists but credentials are missing; restore %s", secretPath)
		}
	}
	credentials, err := localOpenObserveCredentials(repoRoot, true)
	if err != nil {
		return 0, err
	}
	env := "ZO_ROOT_USER_EMAIL=" + credentials.Username + "\nZO_ROOT_USER_PASSWORD=" + credentials.Password + "\n"
	if err := os.WriteFile(filepath.Join(generatedDir(repoRoot), "openobserve.env"), []byte(env), 0o600); err != nil {
		return 0, err
	}
	output, err := composeCommand(ctx, repoRoot, binding, "--profile", "observability", "up", "--detach", "--pull", "never", "openobserve").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("start local OpenObserve: %s: %w", strings.TrimSpace(string(output)), err)
	}
	output, err = composeCommand(ctx, repoRoot, binding, "port", "openobserve", "5080").Output()
	if err != nil {
		return 0, err
	}
	_, rawPort, err := net.SplitHostPort(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return 0, err
	}
	return port, awaitLocalOpenObserve(ctx, port)
}

func awaitLocalOpenObserve(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("local OpenObserve readiness: %w", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func configureLocalOpenObserve(root *yaml.Node, repoRoot string, port int) error {
	if port < 1 {
		return fmt.Errorf("local OpenObserve port is required")
	}
	credentials, err := localOpenObserveCredentials(repoRoot, false)
	if err != nil {
		return err
	}
	setNode(getOrCreateMapping(root, "observability"), "enabled", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	setMappingValue(root, "observability", "backend", "openobserve")
	setMappingValue(root, "observability", "openobserve", "base_url", fmt.Sprintf("http://127.0.0.1:%d", port))
	setMappingValue(root, "observability", "openobserve", "organization", "default")
	setMappingValue(root, "observability", "openobserve", "logs_stream", "oma_dsh")
	for _, block := range []string{"ingestion", "query"} {
		setMappingValue(root, "observability", "openobserve", block, "username", credentials.Username)
		setMappingValue(root, "observability", "openobserve", block, "password", credentials.Password)
	}
	return nil
}
