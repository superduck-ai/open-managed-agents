package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
)

const performanceWorkload = "chat.serial.v1:warmup=2:samples=20:concurrency=1:chunk_gap_ms=200"
const performanceSamples = 20

type latencySample struct {
	Index  int     `json:"index"`
	Accept float64 `json:"accept_ms"`
	First  float64 `json:"first_preview_ms"`
	Final  float64 `json:"final_ms"`
	Idle   float64 `json:"idle_ms"`
}
type latencyStats struct {
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
}
type performanceResult struct {
	Workload       string                  `json:"workload"`
	Mode           string                  `json:"mode"`
	BaselineRun    string                  `json:"baseline_run,omitempty"`
	BaselineCommit string                  `json:"baseline_commit,omitempty"`
	Metrics        map[string]latencyStats `json:"metrics"`
	Regressions    []string                `json:"regressions,omitempty"`
}

func summarizeSamples(samples []latencySample) (map[string]latencyStats, error) {
	if len(samples) != performanceSamples {
		return nil, fmt.Errorf("performance requires %d samples, got %d", performanceSamples, len(samples))
	}
	values := map[string][]float64{"accept": {}, "first_preview": {}, "final": {}, "idle": {}}
	for i, s := range samples {
		if s.Index != i || s.First > s.Final || s.Final > s.Idle {
			return nil, errors.New("invalid performance sample order")
		}
		for name, value := range map[string]float64{"accept": s.Accept, "first_preview": s.First, "final": s.Final, "idle": s.Idle} {
			if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, errors.New("invalid performance duration")
			}
			values[name] = append(values[name], value)
		}
	}
	return latencySummary(values), nil
}

func evaluatePerformance(r *report, baselinePath string) error {
	metrics, err := summarizePerformance(*r)
	if err != nil {
		return err
	}
	p := &performanceResult{Workload: workloadName(r.Scenario), Mode: "measurement", Metrics: metrics}
	r.Performance = p
	if r.Diagnostics {
		p.Mode = "diagnostic"
		return nil
	}
	if baselinePath == "" {
		return nil
	}
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		r.Status = "blocked"
		return fmt.Errorf("read performance baseline: %w", err)
	}
	var baseline report
	if err = json.Unmarshal(data, &baseline); err != nil {
		r.Status = "blocked"
		return errors.New("invalid baseline JSON")
	}
	if err = validateBaseline(*r, baseline); err != nil {
		r.Status = "blocked"
		return err
	}
	p.Mode = "gate"
	p.BaselineRun = baseline.RunID
	p.BaselineCommit = baseline.BackendCommit
	baselineMetrics, err := summarizePerformance(baseline)
	if err != nil {
		return err
	}
	for name, current := range metrics {
		before := baselineMetrics[name]
		if current.P50 > before.P50*1.25+25 || current.P95 > before.P95*1.25+25 {
			p.Regressions = append(p.Regressions, fmt.Sprintf("%s: p50 %.2f→%.2f ms; p95 %.2f→%.2f ms", name, before.P50, current.P50, before.P95, current.P95))
		}
	}
	slices.Sort(p.Regressions)
	if len(p.Regressions) > 0 {
		return errors.New("performance regression exceeds 25% + 25 ms; retain baseline, diagnose and revert or fix the change")
	}
	return nil
}

func validateBaseline(current, baseline report) error {
	commit, err := hex.DecodeString(baseline.BackendCommit)
	if err != nil || (len(commit) != 20 && len(commit) != 32) {
		return errors.New("baseline requires an explicit backend commit; measure with --backend-ref")
	}
	if baseline.Status != "pass" || !baseline.CleanupComplete || baseline.Diagnostics || baseline.Scenario != current.Scenario || baseline.Performance == nil || baseline.Performance.Workload != workloadName(current.Scenario) || baseline.BinarySHA256 == "" {
		return errors.New("baseline must be a completed passing performance measurement without diagnostics")
	}
	if current.Doctor.Machine == "" || current.Doctor.Machine != baseline.Doctor.Machine || !maps.Equal(current.Doctor.Images, baseline.Doctor.Images) || !maps.Equal(current.Doctor.Versions, baseline.Doctor.Versions) {
		return errors.New("baseline environment differs: require same host class, tools and image IDs")
	}
	if current.WorkloadSHA256 == "" || current.WorkloadSHA256 != baseline.WorkloadSHA256 {
		return errors.New("baseline workload source differs; measure base commit with the current harness using --backend-ref")
	}
	if _, err := summarizePerformance(baseline); err != nil {
		return fmt.Errorf("invalid baseline evidence: %w", err)
	}
	return nil
}

func performanceWorkloadHash(root, selected string) (string, error) {
	hash := sha256.New()
	names := []string{"go.mod", "go.sum", "tests/liveworker/harness_test.go", "tests/liveworker/real_control_test.go", "tests/liveworker/chat_roundtrip_test.go", "tests/liveworker/chat_reliability_test.go", "tests/liveworker/chat_performance_test.go"}
	if selected == "files.performance" {
		names = []string{"go.mod", "go.sum", "tests/livefiles/harness_test.go", "tests/livefiles/lifecycle_test.go", "tests/livefiles/performance_test.go", "cmd/verify-be/storage_fault.go", "cmd/verify-be/environment.go"}
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00", name)
		_, _ = hash.Write(data)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
