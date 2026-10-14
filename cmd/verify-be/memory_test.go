package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryCommands(t *testing.T) {
	for _, args := range [][]string{{"memory"}, {"memory", "doctor", "unknown"}, {"memory", "cleanup", "--worker-image=x"}, {"memory", "mounts", "--baseline=x"}} {
		if _, err := parseCLI(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted irrelevant arguments %v", args)
		}
	}
	var output bytes.Buffer
	if _, err := parseCLI([]string{"memory", "-h"}, &output); !errors.Is(err, errHelp) {
		t.Fatal(err)
	}
	for _, command := range []string{"integrity", "isolation", "cleanup", "lifecycle", "filestore", "mounts", "doctor"} {
		if !strings.Contains(output.String(), command) {
			t.Fatalf("help omitted %s", command)
		}
	}
	options, err := parseCLI([]string{"memory", "mounts", "--worker-image=example:latest"}, &bytes.Buffer{})
	if err != nil || options.WorkerImage != "example:latest" || options.Scenario != "memory.mounts" {
		t.Fatalf("mount options: %+v %v", options, err)
	}
}

func TestMemoryDependencyIsolation(t *testing.T) {
	directory := t.TempDir()
	requireFile(t, filepath.Join(directory, localSettingsFile), []byte("invalid JSON"))
	for name, selected := range scenarios {
		if !strings.HasPrefix(name, "memory.") || name == "memory.mounts" {
			continue
		}
		if !selected.DependenciesOnly {
			t.Fatalf("portable memory starts backend: %s", name)
		}
		if image, err := workerImage(directory, cliOptions{Scenario: name}); err != nil || image != "" {
			t.Fatalf("portable memory reads Worker settings: %q %v", image, err)
		}
	}
	if !needsWorker("memory.mounts") {
		t.Fatal("real mounts do not require Worker")
	}
	fakeDocker(t)
	result, err := doctor(t.Context(), directory, "", "memory.cleanup")
	if err != nil || len(result.Images) != 4 || result.PublicSandboxChecked {
		t.Fatalf("portable doctor: %+v %v", result, err)
	}
	result, err = doctor(t.Context(), directory, "example:latest", "memory.mounts")
	if err != nil || !result.PublicSandboxChecked {
		t.Fatalf("mount doctor skipped FUSE: %+v %v", result, err)
	}
}
