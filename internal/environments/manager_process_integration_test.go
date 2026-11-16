//go:build integration

package environments

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagerProcessLinuxRestart(t *testing.T) {
	image := os.Getenv("TEST_MANAGER_IMAGE")
	if image == "" {
		t.Skip("TEST_MANAGER_IMAGE is not set")
	}
	directory := t.TempDir()
	for _, version := range []string{"0.1.0", "0.1.1"} {
		command := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X main.version="+version, "-o", filepath.Join(directory, "manager-"+version), "./testdata/manager/main.go")
		command.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build: %s %v", output, err)
		}
	}
	probe := managerProcessProbeCommand("cse_test")
	stop := managerProcessStopCommand(managerProcess{PID: 1234567, Started: "987654321"})
	stop = strings.ReplaceAll(stop, "'/proc/1234567/stat'", "\"/proc/$manager/stat\"")
	stop = strings.ReplaceAll(stop, "kill -TERM 1234567", "kill -TERM $manager")
	stop = strings.ReplaceAll(stop, "'987654321'", "\"$started\"")
	script := "set -eu\ncp /test/manager-0.1.0 /tmp/environment-manager\n" +
		"flock --close -n /tmp/manager.lock /tmp/environment-manager task-run --session cse_test &\nlauncher=$!\nsleep 0.3\n" +
		"(\n" + probe + ") > /tmp/probe\nmanager=$(head -1 /tmp/probe | cut -d: -f1)\nstarted=$(head -1 /tmp/probe | cut -d: -f2)\n" +
		"(\n" + managerProcessProbeCommand("different-session") + ") > /tmp/other\n[ ! -s /tmp/other ]\n" +
		"cp /test/manager-0.1.1 /tmp/new-manager\nmv /tmp/new-manager /tmp/environment-manager\n" +
		"(\n" + probe + ") > /tmp/probe\ngrep -F 'environment-manager 0.1.0' /tmp/probe\n" +
		"if flock --close -n /tmp/manager.lock /tmp/environment-manager task-run --session cse_test; then exit 3; fi\n" +
		"(\n" + strings.ReplaceAll(stop, "\"$started\"", "'0'") + ")\nkill -0 $manager\n" +
		"(\n" + stop + ")\nwait $launcher\n" +
		"flock --close -n /tmp/manager.lock /tmp/environment-manager task-run --session cse_test &\nlauncher=$!\nsleep 0.3\n" +
		"(\n" + probe + ") > /tmp/probe\ngrep -F 'environment-manager 0.1.1' /tmp/probe\nmanager=$(head -1 /tmp/probe | cut -d: -f1)\nkill -TERM $manager\nwait $launcher\n"
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	name := "oma-manager-process-test-" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	command := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name, "--entrypoint", "/bin/bash", "--platform", "linux/arm64", "--network", "none", "-i", "-v", directory+":/test:ro", image, "-s")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Linux restart: %s %v", output, err)
	}
}
