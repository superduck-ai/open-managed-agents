package main

import (
	"os"
	"path/filepath"
	"testing"
)

func performanceFixture() report {
	r := report{BackendCommit: "0123456789abcdef0123456789abcdef01234567", WorkloadSHA256: "fixture", RunID: "baseline-test", Scenario: "chat.performance", Status: "pass", CleanupComplete: true, BinarySHA256: "test-binary", Doctor: doctorResult{Machine: "test-machine", Images: map[string]string{"worker": "sha256:worker"}, Versions: map[string]string{"go": "same"}}, Result: testResult{Status: "pass"}}
	for i := 0; i < performanceSamples; i++ {
		r.Result.Samples = append(r.Result.Samples, latencySample{Index: i, Accept: 5, First: 20, Final: 30, Idle: 100})
	}
	return r
}

func TestPerformanceRejectsIncompleteAndIncompatibleBaseline(t *testing.T) {
	baseline := performanceFixture()
	if err := evaluatePerformance(&baseline, ""); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*report)
	}{
		{"missing backend commit", func(r *report) { r.BackendCommit = "" }},
		{"invalid backend commit", func(r *report) { r.BackendCommit = "HEAD" }},
		{"failed", func(r *report) { r.Status = "fail" }},
		{"not cleaned", func(r *report) { r.CleanupComplete = false }},
		{"diagnostic", func(r *report) { r.Diagnostics = true }},
		{"wrong host", func(r *report) { r.Doctor.Machine = "different" }},
		{"wrong workload source", func(r *report) { r.WorkloadSHA256 = "different" }},
		{"wrong image", func(r *report) { r.Doctor.Images = map[string]string{"worker": "different"} }},
		{"missing samples", func(r *report) { r.Result.Samples = nil }},
		{"missing metrics", func(r *report) { r.Performance = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := baseline
			tc.change(&candidate)
			if err := validateBaseline(baseline, candidate); err == nil {
				t.Fatal("invalid baseline accepted")
			}
		})
	}
	for _, mutate := range []func([]latencySample){
		func(s []latencySample) { s[1].Index = 0 },
		func(s []latencySample) { s[1].First = 0 },
		func(s []latencySample) { s[1].Final = 1 },
	} {
		r := performanceFixture()
		mutate(r.Result.Samples)
		if _, err := summarizeSamples(r.Result.Samples); err == nil {
			t.Fatal("invalid sample accepted")
		}
	}
}

func TestPerformanceGateRetainsBaselineAndRejectsRegression(t *testing.T) {
	baseline := performanceFixture()
	if err := evaluatePerformance(&baseline, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := writeJSON(path, baseline); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	slower := performanceFixture()
	for i := range slower.Result.Samples {
		slower.Result.Samples[i].First = 80
		slower.Result.Samples[i].Final = 90
		slower.Result.Samples[i].Idle = 200
	}
	if err := evaluatePerformance(&slower, path); err == nil || len(slower.Performance.Regressions) == 0 {
		t.Fatal("regression accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("gate rewrote baseline")
	}
	current := performanceFixture()
	if err := evaluatePerformance(&current, path); err != nil {
		t.Fatal(err)
	}
	if current.Performance.Mode != "gate" || current.Performance.BaselineRun != baseline.RunID {
		t.Fatal("missing comparison evidence")
	}
	current.Diagnostics = true
	if err := evaluatePerformance(&current, ""); err != nil {
		t.Fatal(err)
	}
	if current.Performance.Mode != "diagnostic" {
		t.Fatal("diagnostic run masquerades as normal measurement")
	}
}
