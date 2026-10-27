package devinit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// StackPorts records the per-environment infrastructure listener ports. They
// are probed once per stack start and reused while every service stays
// healthy; restart scripts persist them in state.json runtime.
type StackPorts struct {
	APIPort   int `json:"api_port"`
	WebPort   int `json:"web_port"`
	PGPort    int `json:"pg_port"`
	RedisPort int `json:"redis_port"`
	NATSPort  int `json:"nats_port"`
	NATSPort2 int `json:"nats_port_2"`
	NATSPort3 int `json:"nats_port_3"`
	MinIOPort int `json:"minio_port"`
	E2BPort   int `json:"e2b_port"`
}

const (
	pgPortScanBase     = 45432
	redisPortScanBase  = 46379
	natsPortScanBase   = 44222
	minioPortScanBase  = 49000
	e2bPortScanBase    = 43099
	stackStartTimeout  = 90 * time.Second
	stackHealthTimeout = 2 * time.Second
)

var stackServices = []string{"postgres", "redis", "nats", "nats-2", "nats-3", "minio"}

func EnsureStack(ctx context.Context, repoRoot string, binding EnvBinding, previous *StackPorts) (StackPorts, error) {
	unlock, err := lockStack(binding)
	if err != nil {
		return StackPorts{}, err
	}
	defer unlock()
	if err := checkStackOwnership(ctx, binding); err != nil {
		return StackPorts{}, err
	}
	ports, err := reconcileStack(ctx, repoRoot, binding, previous)
	if previous != nil {
		ports.APIPort, ports.WebPort = previous.APIPort, previous.WebPort
	}
	return ports, err
}

func reconcileStack(ctx context.Context, repoRoot string, binding EnvBinding, previous *StackPorts) (StackPorts, error) {
	if adopted, ok := adoptStackPorts(ctx, repoRoot, binding); ok {
		if healthy, err := stackHealthy(ctx, repoRoot, binding, adopted); err == nil && healthy {
			return refreshHealthyStack(ctx, repoRoot, binding, adopted)
		}
		previous = &adopted
	} else if previous != nil {
		if healthy, err := stackHealthy(ctx, repoRoot, binding, *previous); err == nil && healthy {
			return refreshHealthyStack(ctx, repoRoot, binding, *previous)
		}
	}
	var lastErr error
	for attempt := 0; attempt < stackReconcileAttempts; attempt++ {
		ports := StackPorts{}
		if previous != nil {
			ports = *previous
		}
		if err := fillStackPorts(binding, &ports); err != nil {
			return StackPorts{}, err
		}
		if err := writeStackFiles(ctx, repoRoot, binding, ports); err != nil {
			return StackPorts{}, err
		}
		command := composeCommand(ctx, repoRoot, binding, "up", "--detach")
		output, err := command.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("compose up for %s: %s: %w", binding.Name, strings.TrimSpace(string(output)), err)
		} else if err := awaitStack(ctx, repoRoot, binding, ports); err == nil {
			return ports, nil
		} else {
			lastErr = err
		}
		probeFreshStackPorts(&ports)
		previous = &ports
	}
	return StackPorts{}, lastErr
}

const stackReconcileAttempts = 3

// portScanOffset decorrelates probe ranges between environments: two worktrees
// deriving the same name otherwise scan identical ranges and ping-pong on the
// same host ports. The offset is stable per environment (env_id hash).
func portScanOffset(binding EnvBinding) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(binding.EnvID))
	return int(hash.Sum32() % 200)
}

func fillStackPorts(binding EnvBinding, ports *StackPorts) error {
	offset := portScanOffset(binding)
	var err error
	if ports.PGPort == 0 {
		if ports.PGPort, err = ProbeFreePort(pgPortScanBase + offset); err != nil {
			return err
		}
	}
	if ports.RedisPort == 0 {
		if ports.RedisPort, err = ProbeFreePort(redisPortScanBase + offset); err != nil {
			return err
		}
	}
	if ports.NATSPort == 0 {
		if ports.NATSPort, err = ProbeFreePort(natsPortScanBase + offset); err != nil {
			return err
		}
	}
	if ports.NATSPort2 == 0 {
		if ports.NATSPort2, err = ProbeFreePort(ports.NATSPort + 1); err != nil {
			return err
		}
	}
	if ports.NATSPort3 == 0 {
		if ports.NATSPort3, err = ProbeFreePort(ports.NATSPort2 + 1); err != nil {
			return err
		}
	}
	if ports.MinIOPort == 0 {
		if ports.MinIOPort, err = ProbeFreePort(minioPortScanBase + offset); err != nil {
			return err
		}
	}
	if ports.E2BPort == 0 {
		if ports.E2BPort, err = ProbeFreePort(e2bPortScanBase + offset); err != nil {
			return err
		}
	}
	return nil
}

// probeFreshStackPorts clears every port so the next attempt re-probes. A
// reconcile failure usually means one host port was taken after the initial
// probe; fresh ports avoid retrying the same conflict.
func probeFreshStackPorts(ports *StackPorts) {
	ports.PGPort = 0
	ports.RedisPort = 0
	ports.NATSPort = 0
	ports.NATSPort2 = 0
	ports.NATSPort3 = 0
	ports.MinIOPort = 0
	ports.E2BPort = 0
}

// TeardownStack removes the per-environment stack including data volumes.
func TeardownStack(ctx context.Context, repoRoot string, binding EnvBinding) error {
	unlock, err := lockStack(binding)
	if err != nil {
		return err
	}
	defer unlock()
	if err := checkStackOwnership(ctx, binding); err != nil {
		return err
	}
	command := teardownCommand(ctx, repoRoot, binding)
	output, err := command.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "no such project") {
		return fmt.Errorf("compose down for %s: %s: %w", binding.Name, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func composeProjectName(binding EnvBinding) string {
	return "oma-" + binding.Name
}

func composeCommand(ctx context.Context, repoRoot string, binding EnvBinding, args ...string) *exec.Cmd {
	full := append([]string{
		"compose", "-p", composeProjectName(binding),
		"--project-directory", repoRoot,
		"-f", "deploy/docker-compose/dev-env.yaml",
		"--env-file", filepath.Join(".run", "generated", "stack.env"),
	}, args...)
	command := exec.CommandContext(ctx, "docker", full...)
	command.Dir = repoRoot
	return command
}

func writeStackFiles(ctx context.Context, repoRoot string, binding EnvBinding, ports StackPorts) error {
	images, err := loadImagesConfig()
	if err != nil {
		return err
	}
	stateDir := generatedDir(repoRoot)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	envFile := fmt.Sprintf(`OMA_PG_PORT=%d
OMA_REDIS_PORT=%d
OMA_NATS_PORT=%d
OMA_NATS_PORT_2=%d
OMA_NATS_PORT_3=%d
OMA_MINIO_PORT=%d
OMA_E2B_PORT=%d
OMA_E2B_LOCAL_IMAGE=%s
OMA_ENVD_BIN_DIR=%s
OMA_E2B_CONFIG=%s
OMA_WORKTREE=%s
OMA_ENV_NAME=%s
OMA_ENV_ID=%s
`, ports.PGPort, ports.RedisPort, ports.NATSPort, ports.NATSPort2, ports.NATSPort3, ports.MinIOPort, ports.E2BPort, images.reference("e2b-local"),
		filepath.Join(repoRoot, "envd-bin"),
		filepath.Join(stateDir, "e2b-local.yaml"),
		repoRoot, binding.Name, binding.EnvID)
	if err := os.WriteFile(filepath.Join(stateDir, "stack.env"), []byte(envFile), 0o600); err != nil {
		return err
	}
	return renderE2BConfig(ctx, repoRoot, binding.Name, ports.E2BPort)
}

func renderE2BConfig(ctx context.Context, repoRoot, name string, e2bPort int) error {
	template, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker-compose", "e2b-local.yaml"))
	if err != nil {
		return err
	}
	base, err := loadBaseConfig(repoRoot)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "docker", "image", "inspect", base.E2B.Template, "--format", "{{.Architecture}}")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("inspect sandbox image %s; build or pull it before dev-init: %w", base.E2B.Template, err)
	}
	architecture := strings.TrimSpace(string(output))
	if architecture != "amd64" && architecture != "arm64" {
		return fmt.Errorf("unsupported sandbox image architecture %q", architecture)
	}
	rendered := renderE2BConfigTemplate(string(template), name, e2bPort, filepath.Join(repoRoot, "envd-bin"), architecture)
	return os.WriteFile(filepath.Join(generatedDir(repoRoot), "e2b-local.yaml"), []byte(rendered), 0o600)
}

func renderE2BConfigTemplate(template, name string, e2bPort int, envdDir, architecture string) string {
	rendered := strings.ReplaceAll(template, "__ENVD_BIN_DIR__", envdDir)
	rendered = strings.ReplaceAll(rendered, "envd-linux-amd64", "envd-linux-"+architecture)
	rendered = strings.ReplaceAll(rendered, `addr: "0.0.0.0:3099"`, fmt.Sprintf(`addr: "0.0.0.0:%d"`, e2bPort))
	return strings.ReplaceAll(rendered, `container_name_prefix: "e2b-envd-"`, fmt.Sprintf(`container_name_prefix: "e2b-envd-%s-"`, name))
}

func awaitStack(ctx context.Context, repoRoot string, binding EnvBinding, ports StackPorts) error {
	deadline := time.Now().Add(stackStartTimeout)
	for time.Now().Before(deadline) {
		healthy := true
		for _, service := range stackServices {
			status, err := composeServiceHealth(ctx, repoRoot, binding, service)
			if err != nil || status != "healthy" {
				healthy = false
				break
			}
		}
		e2bState, stateErr := composeServiceState(ctx, repoRoot, binding, "e2b-local")
		if healthy && stateErr == nil && e2bState == "running" && e2bHealthy(ctx, ports.E2BPort) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("stack %s did not become healthy within %s", composeProjectName(binding), stackStartTimeout)
}

// servicePortBindings returns the host port bound to a container port for
// every service of this project, plus the e2b port parsed from the rendered
// config (e2b-local uses host networking, so it has no published binding).
func servicePortBindings(ctx context.Context, repoRoot string, binding EnvBinding) (map[string]map[int]int, int, bool) {
	ps := exec.CommandContext(ctx, "docker", "compose", "-p", composeProjectName(binding), "ps", "-aq")
	ps.Dir = repoRoot
	output, err := ps.Output()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return nil, 0, false
	}
	bindings := map[string]map[int]int{}
	for _, containerID := range strings.Fields(string(output)) {
		inspect := exec.CommandContext(ctx, "docker", "inspect", "-f",
			`{{json .Config.Labels}}{{"\n"}}{{json .HostConfig.NetworkMode}}{{"\n"}}{{json .NetworkSettings.Ports}}`, containerID)
		raw, err := inspect.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) != 3 {
			continue
		}
		var labels map[string]string
		if err := jsonUnmarshalLine(lines[0], &labels); err != nil {
			continue
		}
		service := labels["com.docker.compose.service"]
		if service == "" {
			continue
		}
		ports := map[int]int{}
		if lines[1] != `"host"` {
			var published map[string][]struct {
				HostPort string
			}
			if err := jsonUnmarshalLine(lines[2], &published); err == nil {
				for containerPort, hosts := range published {
					if len(hosts) == 0 {
						continue
					}
					var container, host int
					if _, err := fmt.Sscanf(containerPort, "%d/tcp", &container); err != nil {
						continue
					}
					if _, err := fmt.Sscanf(hosts[0].HostPort, "%d", &host); err != nil {
						continue
					}
					ports[container] = host
				}
			}
		}
		bindings[service] = ports
	}
	e2bPort, _ := renderedE2BPort(repoRoot)
	return bindings, e2bPort, true
}

func jsonUnmarshalLine(line string, target any) error {
	return json.Unmarshal([]byte(line), target)
}

// renderedE2BPort reads the per-environment server port from the rendered
// e2b-local config.
func renderedE2BPort(repoRoot string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(generatedDir(repoRoot), "e2b-local.yaml"))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "addr:") {
			continue
		}
		var port int
		if _, err := fmt.Sscanf(line, `addr: "0.0.0.0:%d"`, &port); err == nil {
			return port, true
		}
	}
	return 0, false
}

// adoptStackPorts reads the actually-published ports of this project's
// containers so the recorded runtime converges to the running stack.
func adoptStackPorts(ctx context.Context, repoRoot string, binding EnvBinding) (StackPorts, bool) {
	bindings, e2bPort, _ := servicePortBindings(ctx, repoRoot, binding)
	if len(bindings) == 0 {
		return StackPorts{}, false
	}
	ports := StackPorts{E2BPort: e2bPort}
	ports.PGPort = bindings["postgres"][5432]
	ports.RedisPort = bindings["redis"][6379]
	ports.NATSPort = bindings["nats"][4222]
	ports.NATSPort2 = bindings["nats-2"][4222]
	ports.NATSPort3 = bindings["nats-3"][4222]
	ports.MinIOPort = bindings["minio"][9000]
	if ports.PGPort == 0 || ports.RedisPort == 0 || ports.NATSPort == 0 || ports.NATSPort2 == 0 || ports.NATSPort3 == 0 || ports.MinIOPort == 0 || ports.E2BPort == 0 {
		return StackPorts{}, false
	}
	return ports, true
}

// composeServiceState returns the container State.Status ("running",
// "exited", "restarting", ...) for a service, or an error when absent.
func composeServiceState(ctx context.Context, repoRoot string, binding EnvBinding, service string) (string, error) {
	ps := exec.CommandContext(ctx, "docker", "compose", "-p", composeProjectName(binding), "ps", "-q", service)
	ps.Dir = repoRoot
	containerID, err := ps.Output()
	if err != nil || strings.TrimSpace(string(containerID)) == "" {
		return "", fmt.Errorf("stack %s service %s not present", binding.Name, service)
	}
	inspect := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", strings.TrimSpace(string(containerID)))
	status, err := inspect.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(status)), nil
}

func composeServiceHealth(ctx context.Context, repoRoot string, binding EnvBinding, service string) (string, error) {
	ps := exec.CommandContext(ctx, "docker", "compose", "-p", composeProjectName(binding), "ps", "-q", service)
	ps.Dir = repoRoot
	containerID, err := ps.Output()
	if err != nil || strings.TrimSpace(string(containerID)) == "" {
		return "", fmt.Errorf("stack %s service %s not running", binding.Name, service)
	}
	inspect := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}", strings.TrimSpace(string(containerID)))
	status, err := inspect.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(status)), nil
}

// stackHealthy reports whether every service container of THIS compose
// project is running (healthcheck services healthy, e2b-local additionally
// answering /health on its recorded port).
func stackHealthy(ctx context.Context, repoRoot string, binding EnvBinding, ports StackPorts) (bool, error) {
	for _, service := range stackServices {
		status, err := composeServiceHealth(ctx, repoRoot, binding, service)
		if err != nil || status != "healthy" {
			return false, err
		}
	}
	status, err := composeServiceState(ctx, repoRoot, binding, "e2b-local")
	if err != nil || status != "running" {
		return false, err
	}
	return e2bHealthy(ctx, ports.E2BPort) == nil, nil
}

func e2bHealthy(ctx context.Context, port int) error {
	if port == 0 {
		return fmt.Errorf("e2b port is zero")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", port), nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: stackHealthTimeout}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("e2b health on :%d returned %d", port, response.StatusCode)
	}
	return nil
}

func refreshHealthyStack(ctx context.Context, repoRoot string, binding EnvBinding, ports StackPorts) (StackPorts, error) {
	path := filepath.Join(generatedDir(repoRoot), "e2b-local.yaml")
	previous, err := os.ReadFile(path)
	if err != nil {
		return StackPorts{}, err
	}
	envPath := filepath.Join(generatedDir(repoRoot), "stack.env")
	previousEnv, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return StackPorts{}, err
	}
	if err := writeStackFiles(ctx, repoRoot, binding, ports); err != nil {
		return StackPorts{}, err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return StackPorts{}, err
	}
	currentEnv, err := os.ReadFile(envPath)
	if err != nil {
		return StackPorts{}, err
	}
	if string(previous) == string(current) && string(previousEnv) == string(currentEnv) {
		return ports, nil
	}
	output, err := composeCommand(ctx, repoRoot, binding, "up", "--detach", "--force-recreate", "e2b-local").CombinedOutput()
	if err == nil {
		err = awaitStack(ctx, repoRoot, binding, ports)
	}
	if err != nil {
		restoreErr := os.WriteFile(path, previous, 0o600)
		var restoreEnvErr error
		if previousEnv == nil {
			restoreEnvErr = os.Remove(envPath)
		} else {
			restoreEnvErr = os.WriteFile(envPath, previousEnv, 0o600)
		}
		return StackPorts{}, errors.Join(fmt.Errorf("refresh e2b-local config: %s: %w", strings.TrimSpace(string(output)), err), restoreErr, restoreEnvErr)
	}
	return ports, nil
}
