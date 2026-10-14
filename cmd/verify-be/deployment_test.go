package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeploymentCommandsAndEvidence(t *testing.T) {
	for _, args := range [][]string{{"deployment"}, {"deployment", "doctor", "unknown"}, {"deployment", "restart", "--worker-image=x"}, {"deployment", "retry", "--baseline=x"}} {
		if _, err := parseCLI(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted irrelevant options %v", args)
		}
	}
	var help bytes.Buffer
	if _, err := parseCLI([]string{"deployment", "-h"}, &help); !errors.Is(err, errHelp) {
		t.Fatal(err)
	}
	for _, name := range []string{"lifecycle", "retry", "idempotency", "restart", "doctor"} {
		if !strings.Contains(help.String(), name) {
			t.Fatalf("help omitted %s", name)
		}
	}
	options, err := parseCLI([]string{"deployment", "restart", "--timeout=8m"}, &bytes.Buffer{})
	if err != nil || options.Scenario != "deployment.restart" {
		t.Fatalf("options %+v %v", options, err)
	}
	directory := t.TempDir()
	requireFile(t, filepath.Join(directory, localSettingsFile), []byte("invalid JSON"))
	for name, selected := range scenarios {
		if !strings.HasPrefix(name, "deployment.") {
			continue
		}
		if !selected.DependenciesOnly || needsWorker(name) {
			t.Fatalf("unexpected standalone backend or Worker: %s", name)
		}
		if _, err := workerImage(directory, cliOptions{Scenario: name}); err != nil {
			t.Fatal(err)
		}
		var events bytes.Buffer
		if err := json.NewEncoder(&events).Encode(testEvent{Package: selected.Package, Test: selected.Test, Action: "skip"}); err != nil {
			t.Fatal(err)
		}
		result, err := evaluate(&events, 0, selected)
		if err != nil || result.Status != "fail" || len(result.MissingStages) != len(selected.Stages) {
			t.Fatalf("skip accepted: %+v %v", result, err)
		}
		if err := saveReport(directory, report{Scenario: name}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(directory, "report.md"))
		if err != nil || !strings.Contains(string(data), "one-hour rescue delay is not measured") || !strings.Contains(string(data), "Session execution") || !strings.Contains(string(data), "asynchronous River workers") {
			t.Fatalf("incorrect scope: %s %v", data, err)
		}
	}
	fakeDocker(t)
	result, err := doctor(t.Context(), directory, "", "deployment.restart")
	if err != nil || len(result.Images) != 4 || result.PublicSandboxChecked {
		t.Fatalf("unexpected prerequisites: %+v %v", result, err)
	}
}
