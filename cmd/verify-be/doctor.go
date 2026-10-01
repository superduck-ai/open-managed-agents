package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultWorker = "ghcr.io/superduck-ai/managed-agent-sandbox:latest"
const minimumDiskKiB = 1 << 20

var errDoctorCleanup = errors.New("doctor probe cleanup incomplete")

type doctorResult struct {
	Versions             map[string]string `json:"versions"`
	Images               map[string]string `json:"images"`
	APIPort              int               `json:"api_port"`
	Machine              string            `json:"machine"`
	DiskAvailableKiB     int64             `json:"disk_available_kib"`
	PublicSandboxChecked bool              `json:"public_sandbox_checked"`
}

func doctor(ctx context.Context, root, worker, selected string) (doctorResult, error) {
	result := doctorResult{Versions: map[string]string{}, Images: map[string]string{}, APIPort: 18080}
	for _, name := range []string{"go", "docker", "bash", "git", "tar"} {
		if _, err := exec.LookPath(name); err != nil {
			return result, fmt.Errorf("required tool %s: %w", name, err)
		}
	}
	machine, err := capture(ctx, root, "docker", "info", "--format", "{{.Architecture}}/{{.NCPU}}/{{.MemTotal}}/{{.KernelVersion}}")
	if err != nil {
		return result, err
	}
	result.Machine = fmt.Sprintf("%s/%s/%d/%s", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), machine)
	for name, args := range map[string][]string{
		"go": {"go", "version"}, "docker": {"docker", "info", "--format", "{{.ServerVersion}}"},
		"compose": {"docker", "compose", "version", "--short"}, "bash": {"bash", "--version"}, "git": {"git", "--version"}, "tar": {"tar", "--version"},
	} {
		value, err := capture(ctx, root, args...)
		if err != nil {
			return result, err
		}
		result.Versions[name] = strings.Split(value, "\n")[0]
	}
	images := map[string]string{"postgres": "postgres:17", "redis": "redis:8", "nats": "nats:2.14.6-alpine", "minio": "pgsty/minio:latest"}
	if worker != "" {
		images["worker"] = worker
	}
	for name, image := range images {
		value, err := capture(ctx, root, "docker", "image", "inspect", image, "--format", "{{.Id}}")
		if err != nil {
			return result, fmt.Errorf("%s image inspection failed; ensure the configured image exists locally", name)
		}
		result.Images[name] = value
	}
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		return result, fmt.Errorf("verification port unavailable: %w", err)
	}
	if err := listener.Close(); err != nil {
		return result, err
	}
	disk, err := doctorProbe(ctx, root, result.Images["nats"], false)
	if err != nil {
		return result, err
	}
	result.DiskAvailableKiB, err = availableDisk(disk)
	if err != nil {
		return result, err
	}
	if selected == "chat.public" || selected == "chat.upstream-errors" || selected == "files.generated" || selected == "memory.mounts" {
		if _, err := doctorProbe(ctx, root, result.Images["worker"], true); err != nil {
			return result, fmt.Errorf("Docker sandbox requires usable FUSE, SYS_ADMIN and AppArmor configuration: %w", err)
		}
		result.PublicSandboxChecked = true
	}
	return result, nil
}

func availableDisk(output string) (int64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, errors.New("cannot parse Docker volume free space")
	}
	available, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, errors.New("cannot parse Docker volume free space")
	}
	if available < minimumDiskKiB {
		return available, fmt.Errorf("Docker volume needs at least 1 GiB free; available %d KiB", available)
	}
	return available, nil
}

func doctorProbe(ctx context.Context, root, image string, public bool) (output string, resultErr error) {
	name := fmt.Sprintf("verify-be-doctor-%d", time.Now().UnixNano())
	args := []string{"docker", "create", "--pull=never", "--name", name, "--label", "oma.verify-be.run=" + name, "--entrypoint", "sh"}
	script := "df -Pk /data"
	if public {
		args = append(args, "--cap-add", "SYS_ADMIN", "--device", "/dev/fuse", "--security-opt", "apparmor=unconfined")
		script = "test -c /dev/fuse && exec 3<>/dev/fuse && mkdir -p /tmp/verify-mount && mount -t tmpfs tmpfs /tmp/verify-mount && umount /tmp/verify-mount"
	} else {
		args = append(args, "--volume", "/data")
	}
	args = append(args, image, "-c", script)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, cleanupErr := capture(cleanupCtx, root, "docker", "rm", "-fv", name)
		if cleanupErr != nil {
			remaining, inspectErr := capture(cleanupCtx, root, "docker", "ps", "-aq", "--filter", "name=^/"+name+"$")
			if inspectErr != nil || remaining != "" {
				resultErr = errors.Join(resultErr, fmt.Errorf("%w: %s", errDoctorCleanup, name))
			}
		}
	}()
	if _, err := capture(ctx, root, args...); err != nil {
		return "", fmt.Errorf("create doctor probe: %w", err)
	}
	output, err := capture(ctx, root, "docker", "start", "-a", name)
	if err != nil {
		return "", fmt.Errorf("run doctor probe: %w", err)
	}
	code, err := capture(ctx, root, "docker", "inspect", "--format", "{{.State.ExitCode}}", name)
	if err != nil {
		return "", err
	}
	if code != "0" {
		return "", fmt.Errorf("doctor probe failed with exit code %s", code)
	}
	return output, nil
}
