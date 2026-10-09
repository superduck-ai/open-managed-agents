package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"unknown"}, {"run", "chat.roundtrip"}, {"chat"}, {"files"}, {"files", "chat.roundtrip"},
		{"doctor", "files"}, {"files", "doctor", "public"}, {"chat", "doctor", "unknown"},
		{"chat", "tools", "extra"}, {"chat", "unknown", "--help"}, {"files", "lifecycle", "extra", "--help"},
		{"files", "lifecycle", "--worker-image", "private:test"}, {"files", "doctor", "--worker-image", "private:test"},
		{"files", "lifecycle", "--diagnostics"}, {"chat", "roundtrip", "--baseline", "a"},
		{"chat", "performance", "--baseline", "a", "--diagnostics"},
		{"help", "unknown"}, {"help", "chat", "unknown"}, {"help", "help"},
		{"chat", "tools", "--timeout", "-1s"}, {"files", "invalid", "--timeout", "100ms"}, {"doctor", "--timeout", "2m"},
		{"chat", "tools", "-worker-image", "a"}, {"completion", "bash"}, {"-help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := parseCLI(args, &bytes.Buffer{})
			if err == nil || errors.Is(err, errHelp) {
				t.Fatalf("accepted invalid arguments: %v", args)
			}
		})
	}
}

func TestCLIParsesCommandsAndOptionPositions(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want cliOptions
	}{
		{[]string{"doctor"}, cliOptions{Command: "doctor"}},
		{[]string{"test"}, cliOptions{Command: "run", Scenario: "test"}},
		{[]string{"chat", "doctor"}, cliOptions{Command: "doctor", Scenario: "chat"}},
		{[]string{"files", "doctor"}, cliOptions{Command: "doctor", Scenario: "files"}},
		{[]string{"chat", "doctor", "public"}, cliOptions{Command: "doctor", Scenario: "chat.public"}},
		{[]string{"chat", "--worker-image", "worker:test", "tools"}, cliOptions{Command: "run", Scenario: "chat.tools", WorkerImage: "worker:test"}},
		{[]string{"chat", "tools", "--worker-image=worker:test"}, cliOptions{Command: "run", Scenario: "chat.tools", WorkerImage: "worker:test"}},
		{[]string{"chat", "performance", "--backend-ref=HEAD", "--diagnostics"}, cliOptions{Command: "run", Scenario: "chat.performance", BackendRef: "HEAD", Diagnostics: true}},
		{[]string{"chat", "performance", "--baseline=base.json"}, cliOptions{Command: "run", Scenario: "chat.performance", Baseline: "base.json"}},
		{[]string{"files", "performance", "--baseline=base.json"}, cliOptions{Command: "run", Scenario: "files.performance", Baseline: "base.json"}},
		{[]string{"files", "exhaustion"}, cliOptions{Command: "run", Scenario: "files.exhaustion"}},
		{[]string{"files", "cloud-storage", "--cloud-config=private.yaml"}, cliOptions{Command: "run", Scenario: "files.cloud-storage", CloudConfig: "private.yaml"}},
		{[]string{"chat", "cloud-renewal", "--cloud-config=private.yaml"}, cliOptions{Command: "run", Scenario: "chat.cloud-renewal", CloudConfig: "private.yaml"}},
	} {
		got, err := parseCLI(tc.args, &bytes.Buffer{})
		if err != nil || got != tc.want {
			t.Fatalf("args=%v options=%+v err=%v", tc.args, got, err)
		}
	}
}

func TestCLIParsesEveryRegisteredScenario(t *testing.T) {
	for name, selected := range scenarios {
		args := strings.Split(name, ".")
		got, err := parseCLI(args, &bytes.Buffer{})
		if err != nil || got.Scenario != name || got.Command != "run" {
			t.Fatalf("%s options=%+v err=%v", name, got, err)
		}
		var output bytes.Buffer
		_, err = parseCLI(append(args, "--help"), &output)
		if !errors.Is(err, errHelp) || !strings.Contains(output.String(), selected.Description) || !strings.Contains(output.String(), selected.Timeout.String()) {
			t.Fatalf("%s help=%s err=%v", name, output.String(), err)
		}
	}
}

func TestCLIHelpWithoutRuntimeDependencies(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv("OMA_WORKER_CONTROL_IMAGE", "private.example/hidden:test")
	requireFile(t, localSettingsFile, []byte("invalid JSON"))
	previous := os.Args
	t.Cleanup(func() { os.Args = previous })
	for _, args := range [][]string{{"-h"}, {"help"}, {"chat", "-h"}, {"files", "-h"}, {"help", "files", "lifecycle"}, {"chat", "doctor", "-h"}, {"help", "chat", "performance"}} {
		var output bytes.Buffer
		_, err := parseCLI(args, &output)
		if !errors.Is(err, errHelp) || strings.Contains(output.String(), "private.example") {
			t.Fatalf("help error=%v output=%s", err, output.String())
		}
		os.Args = append([]string{"verify-be"}, args...)
		if mainCode() != 0 {
			t.Fatal("help required runtime dependencies")
		}
	}
	var output bytes.Buffer
	_, _ = parseCLI([]string{"files", "-h"}, &output)
	if strings.Contains(output.String(), "--worker-image") {
		t.Fatal("Files exposes irrelevant Worker flag")
	}
	for name := range scenarios {
		domain, short, _ := strings.Cut(name, ".")
		output.Reset()
		_, _ = parseCLI([]string{domain, "-h"}, &output)
		if !strings.Contains(output.String(), short) {
			t.Fatalf("group help missing %s", name)
		}
	}
}

func TestFilesIgnoresWorkerConfiguration(t *testing.T) {
	root := t.TempDir()
	requireFile(t, filepath.Join(root, localSettingsFile), []byte("invalid JSON"))
	t.Setenv("OMA_WORKER_CONTROL_IMAGE", "")
	for _, selected := range []string{"", "files", "files.lifecycle", "files.invalid"} {
		value, err := workerImage(root, cliOptions{Scenario: selected})
		if err != nil || value != "" {
			t.Fatalf("%s read Worker configuration: %q %v", selected, value, err)
		}
	}
	for _, selected := range []string{"chat.roundtrip", "files.generated"} {
		if _, err := workerImage(root, cliOptions{Scenario: selected}); err == nil {
			t.Fatalf("%s ignored invalid Worker configuration", selected)
		}
	}
}

func requireFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
