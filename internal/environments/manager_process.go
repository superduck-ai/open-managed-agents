package environments

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

type managerProcess struct {
	PID     int
	Started string
	Version string
}

func managerProcessProbeCommand(sessionID string) string {
	return "set -eu\n" +
		"found=\n" +
		"for proc in /proc/[0-9]*; do\n" +
		"  exe=$(readlink \"$proc/exe\" 2>/dev/null) || continue\n" +
		"  case \"$exe\" in */environment-manager|*'/environment-manager (deleted)') ;; *) continue ;; esac\n" +
		"  [ \"$(tr '\\000' '\\n' < \"$proc/cmdline\" | sed -n '2p')\" = task-run ] || continue\n" +
		"  tr '\\000' '\\n' < \"$proc/cmdline\" | awk -v session=" + shellQuote(sessionID) + " 'previous==\"--session\" && $0==session {found=1} {previous=$0} END {exit !found}' || continue\n" +
		"  [ -z \"$found\" ] || exit 2\n" +
		"  found=$proc\n" +
		"done\n" +
		"[ -n \"$found\" ] || exit 0\n" +
		"printf '%s:%s\\n' \"${found##*/}\" \"$(awk '{print $22}' \"$found/stat\")\"\n" +
		"\"$found/exe\" --version\n"
}

func parseManagerProcess(output []byte) (managerProcess, error) {
	if len(output) == 0 {
		return managerProcess{}, nil
	}
	if len(output) > 2048 {
		return managerProcess{}, errManagerProcessResponse
	}
	identity, versionOutput, ok := strings.Cut(string(output), "\n")
	pidText, started, valid := strings.Cut(identity, ":")
	pid, err := strconv.Atoi(pidText)
	if !ok || !valid || err != nil || pid <= 1 {
		return managerProcess{}, errManagerProcessIdentity
	}
	if _, err := strconv.ParseUint(started, 10, 64); err != nil {
		return managerProcess{}, errManagerProcessStartTime
	}
	version, valid := parseEnvironmentManagerVersion([]byte(versionOutput))
	if !valid {
		return managerProcess{}, errManagerProcessVersion
	}
	return managerProcess{PID: pid, Started: started, Version: version}, nil
}

func managerProcessStopCommand(process managerProcess) string {
	proc := "/proc/" + strconv.Itoa(process.PID)
	return "set -eu\n" +
		"same_process() { [ -r " + shellQuote(proc+"/stat") + " ] && [ \"$(awk '{print $22}' " + shellQuote(proc+"/stat") + ")\" = " + shellQuote(process.Started) + " ]; }\n" +
		"same_process || exit 0\n" +
		"kill -TERM " + strconv.Itoa(process.PID) + "\n" +
		"for attempt in $(seq 1 150); do\n" +
		"  same_process || exit 0\n" +
		"  [ \"$(awk '{print $3}' " + shellQuote(proc+"/stat") + ")\" != Z ] || exit 0\n" +
		"  sleep 0.1\n" +
		"done\nexit 1\n"
}

func (r *Runner) inspectManagerProcess(ctx context.Context, sandboxID, sessionID string) (managerProcess, error) {
	result, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: managerProcessProbeCommand(sessionID), Timeout: 10 * time.Second,
	})
	if err != nil {
		return managerProcess{}, err
	}
	if result.ExitCode != 0 {
		return managerProcess{}, fmt.Errorf("manager process probe failed with exit code %d", result.ExitCode)
	}
	return parseManagerProcess(result.Stdout)
}
