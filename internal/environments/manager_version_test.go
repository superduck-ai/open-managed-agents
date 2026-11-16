package environments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestParseEnvironmentManagerVersionRejectsUnknownOutput(t *testing.T) {
	for _, output := range []string{"", "environment-runner", "other 2.0.0", "environment-runner latest", "environment-runner 01.0.0", "environment-runner 0.1.0 " + strings.Repeat("x", 1024)} {
		if _, valid := parseEnvironmentManagerVersion([]byte(output)); valid {
			t.Fatalf("accepted version output %q", output)
		}
	}
}

func TestInstallEnvironmentManagerUpgradesUnusableOrOlderVersion(t *testing.T) {
	for _, scenario := range []struct {
		name, version string
		exitCode      int
		probeError    error
	}{
		{name: "missing", exitCode: 127},
		{name: "unexecutable", exitCode: 126},
		{name: "probe error", probeError: errors.New("probe failed")},
		{name: "unrecognized output", version: "other 9.0.0"},
		{name: "malformed version", version: "environment-runner unknown"},
		{name: "older minor", version: "environment-runner 0.9.0"},
		{name: "prerelease", version: "environment-runner 0.10.0-rc.1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			data := []byte("binary")
			provider := &managerArtifactTestProvider{commandResponse: func(request e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
				if strings.HasSuffix(request.Command, " --version") {
					if request.Timeout != managerVersionProbeLimit {
						t.Fatalf("probe timeout = %s", request.Timeout)
					}
					return e2bruntime.CommandResult{Stdout: []byte(scenario.version), ExitCode: scenario.exitCode}, scenario.probeError
				}
				return e2bruntime.CommandResult{}, nil
			}}
			runner := managerArtifactTestRunner(t, provider, config.EnvironmentManagerArtifactConfig{Path: writeManagerArtifactFile(t, data), Version: "0.10.0"})
			got, err := runner.installEnvironmentManager(t.Context(), "sandbox")
			want := defaultEnvironmentManagerPath
			if err != nil || got != want || len(provider.writes) != 1 || len(provider.commands) != 4 {
				t.Fatalf("path=%s error=%v writes=%d commands=%v", got, err, len(provider.writes), provider.commands)
			}
		})
	}
}

func TestInstallEnvironmentManagerDoesNotUploadAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	provider := &managerArtifactTestProvider{commandResponse: func(e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
		cancel()
		return e2bruntime.CommandResult{}, context.Canceled
	}}
	runner := managerArtifactTestRunner(t, provider, config.EnvironmentManagerArtifactConfig{Path: "/missing", Version: "0.1.0"})
	path, err := runner.installEnvironmentManager(ctx, "sandbox")
	if !errors.Is(err, errEnvironmentManagerInstall) || path != "" || len(provider.writes) != 0 {
		t.Fatalf("path=%s error=%v writes=%d", path, err, len(provider.writes))
	}
}

func TestManagerArtifactActivationRejectsMislabeledVersion(t *testing.T) {
	target := filepath.Join(t.TempDir(), "manager")
	previous := []byte("keep existing binary")
	if err := os.WriteFile(target, previous, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("#!/bin/sh\necho 'environment-runner 0.1.0 (git abc, built today)'\n")
	if err := os.WriteFile(target+".partial", data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runManagerArtifactShell(t, managerArtifactActivateCommand(target, testArtifactSHA256(data), "0.2.0")); err == nil {
		t.Fatal("activated a version that differs from the configured release")
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, previous) {
		t.Fatalf("previous binary changed: %q %v", got, err)
	}
}

func TestParseEnvironmentManagerVersionAcceptsBuildMetadata(t *testing.T) {
	for _, name := range []string{"environment-runner", "environment-manager"} {
		version, valid := parseEnvironmentManagerVersion([]byte("\n" + name + " 0.10.0-rc.2+build.1 (git abc, built 2026-11-16T00:00:00Z)\n"))
		if !valid || version != "0.10.0-rc.2+build.1" {
			t.Fatalf("version=%q valid=%t", version, valid)
		}
	}
}

func TestInstallEnvironmentManagerReusesEqualOrNewerVersion(t *testing.T) {
	for _, version := range []string{"0.10.0", "0.10.0+another-build", "0.11.0", "1.0.0", "1.0.0-rc.1"} {
		t.Run(version, func(t *testing.T) {
			provider := &managerArtifactTestProvider{commandResponse: func(e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
				return e2bruntime.CommandResult{Stdout: []byte("environment-runner " + version + " (git abc, built today)")}, nil
			}}
			runner := managerArtifactTestRunner(t, provider, config.EnvironmentManagerArtifactConfig{Path: "/does-not-exist", Version: "0.10.0"})
			runner.cfg.EnvironmentRunner.ManagerPath = "/opt/manager with 'quote'"
			var output bytes.Buffer
			runner.logger = slog.New(slog.NewJSONHandler(&output, nil))
			got, err := runner.installEnvironmentManager(t.Context(), "sandbox")
			if err != nil || got != runner.cfg.EnvironmentRunner.ManagerPath || len(provider.writes) != 0 || len(provider.commands) != 1 || runner.managerArtifacts.cached != nil {
				t.Fatalf("path=%s error=%v writes=%d commands=%v", got, err, len(provider.writes), provider.commands)
			}
			if provider.commands[0] != shellQuote(got)+" --version" {
				t.Fatalf("unsafe probe command %q", provider.commands[0])
			}
			var record struct {
				Level    string `json:"level"`
				Message  string `json:"msg"`
				Version  string `json:"manager_version"`
				Required string `json:"required_manager_version"`
				Path     string `json:"manager_path"`
				Sandbox  string `json:"sandbox_id"`
			}
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Level != "INFO" || record.Message != "environment manager artifact reused" || record.Version != version || record.Required != "0.10.0" || record.Path != got || record.Sandbox != "sandbox" {
				t.Fatalf("reuse record = %+v", record)
			}
		})
	}
}

func TestInstallEnvironmentManagerReusesPreviouslyInstalledArtifact(t *testing.T) {
	data := []byte("binary")
	target := managerArtifactInstallPath(testArtifactSHA256(data))
	provider := &managerArtifactTestProvider{commandResponse: func(request e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
		if request.Command == shellQuote(target)+" --version" {
			return e2bruntime.CommandResult{Stdout: []byte("environment-manager 0.1.0")}, nil
		}
		if strings.HasSuffix(request.Command, " --version") {
			return e2bruntime.CommandResult{ExitCode: 127}, nil
		}
		return e2bruntime.CommandResult{}, nil
	}}
	runner := managerArtifactTestRunner(t, provider, config.EnvironmentManagerArtifactConfig{Path: writeManagerArtifactFile(t, data), Version: "0.1.0"})
	got, err := runner.installEnvironmentManager(t.Context(), "sandbox")
	if err != nil || got != defaultEnvironmentManagerPath || len(provider.writes) != 0 || len(provider.commands) != 3 {
		t.Fatalf("path=%s error=%v writes=%d commands=%v", got, err, len(provider.writes), provider.commands)
	}
}

func TestManagerArtifactActivationAcceptsDeclaredVersion(t *testing.T) {
	target := filepath.Join(t.TempDir(), "manager")
	data := []byte("#!/bin/sh\necho 'environment-runner 0.1.0 (git abc, built today)'\n")
	if err := os.WriteFile(target+".partial", data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runManagerArtifactShell(t, managerArtifactActivateCommand(target, testArtifactSHA256(data), "0.1.0")); err != nil {
		t.Fatal(err)
	}
}
