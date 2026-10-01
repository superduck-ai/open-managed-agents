package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptCommandsRejectIrrelevantOptions(t *testing.T) {
	for _, args := range [][]string{
		{"transcript"}, {"transcript", "doctor", "unknown"}, {"transcript", "lifecycle", "--worker-image=x"},
		{"transcript", "recovery", "--baseline=x"}, {"transcript", "integrity", "--diagnostics"},
	} {
		if _, err := parseCLI(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted irrelevant transcript arguments: %v", args)
		}
	}
}

func TestTranscriptDependenciesAndReportScope(t *testing.T) {
	directory := t.TempDir()
	requireFile(t, filepath.Join(directory, localSettingsFile), []byte("invalid JSON"))
	for name, selected := range scenarios {
		if !strings.HasPrefix(name, "transcript.") {
			continue
		}
		if !selected.DependenciesOnly || selected.Package != "github.com/superduck-ai/open-managed-agents/tests" {
			t.Fatalf("transcript starts standalone backend or selects wrong package: %s", name)
		}
		if image, err := workerImage(directory, cliOptions{Scenario: name}); err != nil || image != "" {
			t.Fatalf("transcript read Worker settings: %q %v", image, err)
		}
		if err := saveReport(directory, report{Scenario: name}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(directory, "report.md"))
		if err != nil || !strings.Contains(string(data), "No standalone backend or Worker") || !strings.Contains(string(data), "automatic River retries") || strings.Contains(string(data), "Backend built from the working tree") {
			t.Fatalf("incorrect transcript scope report: %s %v", data, err)
		}
	}
	fakeDocker(t)
	result, err := doctor(t.Context(), directory, "", "transcript.lifecycle")
	if err != nil || len(result.Images) != 4 || result.PublicSandboxChecked {
		t.Fatalf("transcript requires Worker/FUSE: %+v %v", result, err)
	}
}

func TestTranscriptSkipsCannotPass(t *testing.T) {
	selected := scenarios["transcript.lifecycle"]
	var out bytes.Buffer
	if err := json.NewEncoder(&out).Encode(testEvent{Package: selected.Package, Test: selected.Test, Action: "skip"}); err != nil {
		t.Fatal(err)
	}
	result, err := evaluate(&out, 0, selected)
	if err != nil || result.Status != "fail" || len(result.MissingStages) != len(selected.Stages) {
		t.Fatalf("skipped transcript was accepted: %+v %v", result, err)
	}
}
