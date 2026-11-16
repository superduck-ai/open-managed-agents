package environments

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func upgradeTestSteps(events *[]string, fail string, version string) managerUpgradeSteps {
	failure := errors.New("test failure")
	action := func(name string) error {
		*events = append(*events, name)
		if fail == name {
			return failure
		}
		return nil
	}
	probe := 0
	return managerUpgradeSteps{
		probe: func() (managerProcess, error) {
			probe++
			v := version
			if probe > 1 {
				v = "0.1.1"
			}
			return managerProcess{PID: 42, Started: "123", Version: v}, action("probe")
		},
		install: func() error { return action("install") },
		idle:    func() (bool, error) { return true, action("idle") },
		stop:    func(managerProcess) error { return action("stop") },
		start:   func() error { return action("start") },
		ready:   func() (bool, error) { return true, action("ready") },
		phase:   func(phase string) error { return action("phase:" + phase) },
	}
}

func TestManagerUpgradeMissingProcessStartsReplacement(t *testing.T) {
	var events []string
	steps := upgradeTestSteps(&events, "", "0.1.1")
	probes := 0
	steps.probe = func() (managerProcess, error) {
		probes++
		if probes == 1 {
			return managerProcess{}, nil
		}
		return managerProcess{PID: 42, Started: "123", Version: "0.1.1"}, nil
	}
	if err := upgradeManager(t.Context(), "0.1.1", "starting", steps); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"install", "phase:draining", "idle", "phase:starting", "start", "ready", "phase:"}) {
		t.Fatalf("events=%v", events)
	}
}

func TestManagerProcessCommandsHaveValidShellSyntax(t *testing.T) {
	for _, command := range []string{managerProcessProbeCommand("cse_'quoted"), managerProcessStopCommand(managerProcess{PID: 42, Started: "123"})} {
		check := exec.CommandContext(t.Context(), "sh", "-n")
		check.Stdin = strings.NewReader(command)
		if output, err := check.CombinedOutput(); err != nil {
			t.Fatalf("shell syntax: %s %v", output, err)
		}
	}
}

func TestManagerUpgradeFailureStopsAtFailedStep(t *testing.T) {
	sequence := []string{"probe", "install", "phase:draining", "idle", "stop", "phase:starting", "start", "probe", "ready", "phase:"}
	for index, fail := range sequence {
		if index == 7 {
			continue
		}
		t.Run(fail, func(t *testing.T) {
			var events []string
			if err := upgradeManager(t.Context(), "0.1.1", "checking", upgradeTestSteps(&events, fail, "0.1.0")); err == nil {
				t.Fatal("failure ignored")
			}
			if !reflect.DeepEqual(events, sequence[:index+1]) {
				t.Fatalf("events=%v", events)
			}
		})
	}
}

func TestManagerUpgradeWaitsForIdleWithoutKilling(t *testing.T) {
	var events []string
	steps := upgradeTestSteps(&events, "", "0.1.0")
	ctx, cancel := context.WithCancel(t.Context())
	steps.idle = func() (bool, error) { cancel(); return false, nil }
	if err := upgradeManager(ctx, "0.1.1", "checking", steps); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"probe", "install", "phase:draining"}) {
		t.Fatalf("events=%v", events)
	}
}

func TestManagerUpgradeVersionReuseAndRetry(t *testing.T) {
	for _, test := range []struct {
		version, phase string
		want           []string
	}{
		{"0.1.1", "checking", []string{"probe", "phase:"}},
		{"0.2.0", "checking", []string{"probe", "phase:"}},
		{"0.1.1", "starting", []string{"probe", "ready", "phase:"}},
		{"0.1.0", "checking", []string{"probe", "install", "phase:draining", "idle", "stop", "phase:starting", "start", "probe", "ready", "phase:"}},
	} {
		t.Run(test.version+test.phase, func(t *testing.T) {
			var events []string
			if err := upgradeManager(t.Context(), "0.1.1", test.phase, upgradeTestSteps(&events, "", test.version)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(events, test.want) {
				t.Fatalf("events=%v", events)
			}
		})
	}
}

func TestManagerProcessRejectsUnknownIdentity(t *testing.T) {
	for _, output := range []string{"42:abc\nenvironment-manager 0.1.0", "1:123\nenvironment-manager 0.1.0", "42:123\nunknown 0.1.1", "42:123\nenvironment-manager invalid"} {
		if _, err := parseManagerProcess([]byte(output)); err == nil {
			t.Fatalf("accepted %q", output)
		}
	}
	for _, output := range []string{"", "42:123\nenvironment-runner 0.1.1 (git x)\n"} {
		if _, err := parseManagerProcess([]byte(output)); err != nil {
			t.Fatal(err)
		}
	}
}
