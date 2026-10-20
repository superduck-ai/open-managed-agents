package devinit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type localServiceProcess struct {
	pid     int
	command string
}

func findLocalServices(ctx context.Context, root string) ([]localServiceProcess, error) {
	for _, tool := range []string{"ps", "lsof"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, err
		}
	}
	listeners, err := serviceListeners(ctx, root)
	if err != nil {
		return nil, err
	}
	data, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	var processes []localServiceProcess
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, err
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		listener := listeners[pid]
		if !listener && !serviceCommand(root, command) {
			continue
		}
		cwd, err := localProcessCwd(ctx, pid)
		if err != nil {
			return nil, err
		}
		if !serviceDirectory(root, cwd) {
			if listener {
				return nil, fmt.Errorf("recorded service port is used outside target worktree (pid %d); refusing to stop it", pid)
			}
			continue
		}
		processes = append(processes, localServiceProcess{pid: pid, command: command})
	}
	return processes, nil
}

func serviceDirectory(root, cwd string) bool {
	return cwd == root || cwd == filepath.Join(root, "web") || cwd == filepath.Join(root, "oma-brain-dsh")
}

func serviceCommand(root, command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	executable := filepath.Base(fields[0])
	if executable == "open-managed-agents" && strings.Contains(fields[0], "/exe/") {
		return true
	}
	arguments := strings.Join(fields[1:], " ")
	switch executable {
	case "go":
		return arguments == "run ."
	case "bun", "node":
		if arguments == "run dev" || strings.HasPrefix(arguments, "run dev -- ") {
			return true
		}
		for _, arg := range fields[1:] {
			if arg == "vite" || strings.HasSuffix(arg, "/node_modules/.bin/vite") {
				return true
			}
		}
		for _, entry := range []string{"src/main.ts", "dist/main.js", "bin/oma-brain-dsh"} {
			if arguments == entry || arguments == "run "+entry || arguments == filepath.Join(root, "oma-brain-dsh", entry) {
				return true
			}
		}
	}
	return false
}

func serviceListeners(ctx context.Context, root string) (map[int]bool, error) {
	result := make(map[int]bool)
	state, found, err := LoadState(filepath.Join(generatedDir(root), "state.json"))
	if err != nil {
		return nil, err
	}
	if !found || state.Runtime == nil {
		return result, nil
	}
	for _, port := range []int{state.Runtime.APIPort, state.Runtime.WebPort} {
		if port == 0 {
			continue
		}
		data, err := exec.CommandContext(ctx, "lsof", "-nP", "-tiTCP:"+strconv.Itoa(port), "-sTCP:LISTEN").Output()
		if err != nil {
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
				return nil, err
			}
		}
		for _, value := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			result[pid] = true
		}
	}
	return result, nil
}

func localProcessCwd(ctx context.Context, pid int) (string, error) {
	data, err := exec.CommandContext(ctx, "lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "n") {
			return strings.TrimPrefix(line, "n"), nil
		}
	}
	return "", nil
}

func sameLocalProcess(ctx context.Context, root string, process localServiceProcess) (bool, error) {
	data, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(process.pid), "-o", "command=").Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	if strings.TrimSpace(string(data)) != process.command {
		return false, nil
	}
	cwd, err := localProcessCwd(ctx, process.pid)
	return serviceDirectory(root, cwd), err
}

func stopLocalServices(ctx context.Context, root string, processes []localServiceProcess) error {
	for _, process := range processes {
		same, err := sameLocalProcess(ctx, root, process)
		if err != nil {
			return err
		}
		if !same {
			continue
		}
		handle, err := os.FindProcess(process.pid)
		if err != nil {
			return err
		}
		if err := handle.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		pending := false
		for _, process := range processes {
			same, err := sameLocalProcess(deadline, root, process)
			if err != nil {
				return err
			}
			pending = pending || same
		}
		if !pending {
			return nil
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("services did not stop within 30 seconds; Compose and worktree retained: %w", deadline.Err())
		case <-ticker.C:
		}
	}
}
