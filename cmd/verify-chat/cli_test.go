package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCLIRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"unknown"}, {"run"}, {"run", "unknown"}, {"--unknown"},
		{"doctor", "extra"}, {"run", "chat.tools", "extra"},
		{"--worker-image"}, {"doctor", "--worker-image"},
		{"run", "chat.tools", "--worker-image"}, {"help", "help"},
		{"doctor", "--baseline", "report.json"}, {"run", "chat.roundtrip", "--diagnostics"},
		{"run", "chat.performance", "--baseline", "report.json", "--diagnostics"},
		{"help", "unknown"}, {"help", "run", "unknown"}, {"help", "doctor", "extra"},
		{"unknown", "--help"}, {"run", "unknown", "--help"},
		{"doctor", "extra", "--help"}, {"run", "chat.tools", "extra", "--help"},
		{"run", "chat.tools", "--unknown"}, {"run", "chat.tools", "--", "--diagnostics"},
		{"run", "chat.performance", "--diagnostics=invalid"},
		{"run", "chat.tools", "--backend-ref", "HEAD"}, {"completion", "bash"},
		{"doctor", "-worker-image", "worker:test"}, {"run", "-worker-image=worker:test", "chat.tools"},
		{"run", "chat.performance", "-baseline", "report.json"},
		{"run", "chat.performance", "-backend-ref", "HEAD"},
		{"run", "chat.performance", "-diagnostics"}, {"-help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			_, err := parseCLI(args, &output)
			if err == nil || errors.Is(err, errHelp) {
				t.Fatalf("invalid arguments accepted: %v", err)
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
		{[]string{"run", "chat.roundtrip"}, cliOptions{Command: "run", Scenario: "chat.roundtrip"}},
		{[]string{"--worker-image", "worker:test", "run", "chat.tools"}, cliOptions{Command: "run", Scenario: "chat.tools", WorkerImage: "worker:test"}},
		{[]string{"run", "--worker-image=worker:test", "chat.tools"}, cliOptions{Command: "run", Scenario: "chat.tools", WorkerImage: "worker:test"}},
		{[]string{"run", "chat.tools", "--worker-image", "worker:test"}, cliOptions{Command: "run", Scenario: "chat.tools", WorkerImage: "worker:test"}},
		{[]string{"doctor", "--worker-image", "worker:test"}, cliOptions{Command: "doctor", WorkerImage: "worker:test"}},
		{[]string{"--baseline", "base.json", "run", "chat.performance"}, cliOptions{Command: "run", Scenario: "chat.performance", Baseline: "base.json"}},
		{[]string{"run", "--backend-ref=HEAD", "chat.performance", "--diagnostics"}, cliOptions{Command: "run", Scenario: "chat.performance", BackendRef: "HEAD", Diagnostics: true}},
		{[]string{"--backend-ref", "HEAD", "run", "--diagnostics", "chat.performance"}, cliOptions{Command: "run", Scenario: "chat.performance", BackendRef: "HEAD", Diagnostics: true}},
		{[]string{"run", "chat.performance", "--baseline=base.json", "--diagnostics=false"}, cliOptions{Command: "run", Scenario: "chat.performance", Baseline: "base.json"}},
		{[]string{"run", "chat.performance", "--baseline", "--diagnostics"}, cliOptions{Command: "run", Scenario: "chat.performance", Baseline: "--diagnostics"}},
		{[]string{"doctor", "--worker-image=first:test", "--worker-image=last:test", "--help=false"}, cliOptions{Command: "doctor", WorkerImage: "last:test"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var output bytes.Buffer
			got, err := parseCLI(tc.args, &output)
			if err != nil || got != tc.want || output.Len() != 0 {
				t.Fatalf("options=%+v want=%+v error=%v output=%q", got, tc.want, err, output.String())
			}
		})
	}
}

func TestCLIHelpListsCapabilitiesWithoutPrivateValues(t *testing.T) {
	const privateImage = "private.example/hidden:test"
	t.Setenv("OMA_WORKER_CONTROL_IMAGE", privateImage)
	for _, args := range [][]string{
		{"-h"}, {"--help"}, {"help"}, {"help", "-h"}, {"doctor", "-h"}, {"help", "doctor"},
		{"run", "--help"}, {"help", "run"},
		{"run", "chat.tools", "-h"}, {"help", "run", "chat.tools"},
		{"--worker-image", privateImage, "--help"},
		{"run", "chat.performance", "--baseline", "base.json", "--diagnostics", "-h"},
		{"run", "chat.tools", "--worker-image", privateImage, "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			_, err := parseCLI(args, &output)
			if !errors.Is(err, errHelp) {
				t.Fatalf("help returned %v", err)
			}
			for _, fragment := range []string{"用法:", "--worker-image", "OMA_WORKER_CONTROL_IMAGE", ".verify-chat.local.json", "退出码:", "报告:"} {
				if !strings.Contains(output.String(), fragment) {
					t.Errorf("help missing %q", fragment)
				}
			}
			if strings.Contains(output.String(), privateImage) {
				t.Fatal("help leaked private image")
			}
		})
	}
	for _, args := range [][]string{{"-h"}, {"run", "-h"}} {
		var output bytes.Buffer
		_, _ = parseCLI(args, &output)
		for name, selected := range scenarios {
			if selected.Description == "" || !strings.Contains(output.String(), name) || !strings.Contains(output.String(), selected.Description) {
				t.Errorf("help missing registered scenario %s or its description", name)
			}
		}
	}
}

func TestCLIParsesEveryRegisteredScenario(t *testing.T) {
	for name := range scenarios {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := parseCLI([]string{"run", name}, &output)
			if err != nil || got.Command != "run" || got.Scenario != name {
				t.Fatalf("options=%+v error=%v", got, err)
			}
			_, err = parseCLI([]string{"help", "run", name}, &output)
			if !errors.Is(err, errHelp) || !strings.Contains(output.String(), scenarios[name].Description) {
				t.Fatalf("scenario help error=%v output=%q", err, output.String())
			}
		})
	}
}

func TestCLIExitCodesWithoutRuntimeDependencies(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "")
	if err := os.WriteFile(".verify-chat.local.json", []byte("invalid JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{}, 2},
		{[]string{"run", "unknown"}, 2},
		{[]string{"run", "chat.tools", "--diagnostics"}, 2},
		{[]string{"-h"}, 0},
		{[]string{"doctor", "--help"}, 0},
		{[]string{"help", "run", "chat.performance"}, 0},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			os.Args = append([]string{"verify-chat"}, tc.args...)
			if got := mainCode(); got != tc.want {
				t.Fatalf("exit code=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestCLIScenarioDoctorAndTimeout(t *testing.T) {
	for _, args := range [][]string{{"doctor", "chat.public"}, {"run", "chat.public", "--timeout", "8m"}} {
		var output bytes.Buffer
		got, err := parseCLI(args, &output)
		if err != nil || got.Scenario != "chat.public" {
			t.Fatalf("options=%+v err=%v", got, err)
		}
	}
	for _, args := range [][]string{{"run", "chat.tools", "--timeout", "-1s"}, {"run", "chat.tools", "--timeout", "100ms"}, {"doctor", "--timeout", "2m"}} {
		var output bytes.Buffer
		if _, err := parseCLI(args, &output); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
