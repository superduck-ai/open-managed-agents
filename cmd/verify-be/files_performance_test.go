package main

import (
	"math"
	"path/filepath"
	"testing"
)

func filesPerformanceFixture() report {
	r := performanceFixture()
	r.Scenario = "files.performance"
	r.Result.Samples = nil
	for i := range performanceSamples {
		r.Result.FilesSamples = append(r.Result.FilesSamples, filesLatencySample{Index: i, Upload: 10, Metadata: 2, List: 3, Download: 4, Delete: 5})
	}
	return r
}

func TestFilesPerformanceRejectsInvalidEvidence(t *testing.T) {
	for _, change := range []func(*report){
		func(r *report) { r.Result.FilesSamples = r.Result.FilesSamples[:19] },
		func(r *report) { r.Result.FilesSamples[1].Index = 0 },
		func(r *report) { r.Result.FilesSamples[0].Upload = 0 },
		func(r *report) { r.Result.FilesSamples[0].Download = math.NaN() },
		func(r *report) { r.Result.FilesSamples[0].Delete = math.Inf(1) },
	} {
		r := filesPerformanceFixture()
		change(&r)
		if _, err := summarizePerformance(r); err == nil {
			t.Fatal("invalid Files samples accepted")
		}
	}
	baseline := performanceFixture()
	if err := evaluatePerformance(&baseline, ""); err != nil {
		t.Fatal(err)
	}
	if err := validateBaseline(filesPerformanceFixture(), baseline); err == nil {
		t.Fatal("chat baseline accepted for Files")
	}
}

func TestFilesPerformanceGate(t *testing.T) {
	baseline := filesPerformanceFixture()
	if err := evaluatePerformance(&baseline, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := writeJSON(path, baseline); err != nil {
		t.Fatal(err)
	}
	slower := filesPerformanceFixture()
	for i := range slower.Result.FilesSamples {
		slower.Result.FilesSamples[i].Download = 100
	}
	if err := evaluatePerformance(&slower, path); err == nil {
		t.Fatal("download regression accepted")
	}
	current := filesPerformanceFixture()
	if err := evaluatePerformance(&current, path); err != nil {
		t.Fatal(err)
	}
	if current.Performance.Workload != filesPerformanceWorkload || current.Performance.Mode != "gate" || len(current.Performance.Metrics) != 5 {
		t.Fatal("missing Files gate evidence")
	}
}
