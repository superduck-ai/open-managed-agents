package devinit

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// Listener ports are probed per start; these ranges keep the services
	// apart while scanning for a free port.
	apiPortScanBase = 40000
	webPortScanBase = 61000
	portScanLimit   = 200
)

// Layout describes the worktree-local environment identity and paths.
// Infrastructure resources live inside the per-environment compose stack
// under their fixed names (claude_api, claude-files, default NATS names),
// so there is nothing to derive here beyond the public routes.
type Layout struct {
	Name     string
	EnvID    string
	RepoRoot string

	APIRoute     string
	WebRoute     string
	WebPublicURL string

	RunDir          string
	StateDir        string
	SecretsDir      string
	BindingFile     string
	GeneratedConfig string
	StateFile       string
	CredentialsFile string
}

// NewLayout derives the environment from the binding and the worktree base
// config. Listener ports are runtime state attached by the restart scripts
// through RuntimePorts, not derived here.
func NewLayout(binding EnvBinding, repoRoot string) (Layout, error) {
	if strings.TrimSpace(binding.Name) == "" || strings.TrimSpace(binding.EnvID) == "" {
		return Layout{}, fmt.Errorf("binding name and env_id are required")
	}

	apiRoute := "oma-" + binding.Name + "-api"
	webRoute := "oma-" + binding.Name + "-web"
	stateDir := generatedDir(repoRoot)
	return Layout{
		Name:            binding.Name,
		EnvID:           binding.EnvID,
		RepoRoot:        repoRoot,
		APIRoute:        apiRoute,
		WebRoute:        webRoute,
		WebPublicURL:    "http://" + webRoute + ".localhost",
		RunDir:          filepath.Join(repoRoot, ".run"),
		StateDir:        stateDir,
		SecretsDir:      filepath.Join(stateDir, "secrets"),
		BindingFile:     filepath.Join(repoRoot, ".run", "env.json"),
		GeneratedConfig: filepath.Join(stateDir, "config.yaml"),
		StateFile:       filepath.Join(stateDir, "state.json"),
		CredentialsFile: filepath.Join(stateDir, "credentials.json"),
	}, nil
}

// ProbeFreePort returns the first free loopback TCP port at or above base.
func ProbeFreePort(base int) (int, error) {
	for offset := 0; offset <= portScanLimit; offset++ {
		port := base + offset
		listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			_ = listener.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free loopback port between %d and %d", base, base+portScanLimit)
}

// redisURLWithPort points the generated config at the per-environment Redis
// container, keeping the base URL scheme, credentials and options.
func redisURLWithPort(rawURL string, port int) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "redis" && parsed.Scheme != "rediss") {
		return "", fmt.Errorf("redis.url must be a valid Redis URL: %q", rawURL)
	}
	parsed.Host = parsed.Hostname() + ":" + strconv.Itoa(port)
	parsed.Path = ""
	return parsed.String(), nil
}
