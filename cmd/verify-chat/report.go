package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const testPackage = "github.com/superduck-ai/open-managed-agents/tests/liveworker"

type scenario struct {
	Package     string
	Timeout     time.Duration
	Test        string
	Stages      []string
	Description string
}

var scenarios = map[string]scenario{
	"chat.performance": {Package: testPackage, Timeout: 5 * time.Minute, Test: "TestChatPerformance", Stages: []string{"fixed_load_completed", "performance_history_verified"}, Description: "固定串行负载，测量接收、首片段、最终回复及空闲延迟，并可比较基线"},
	"chat.instances":   {Package: testPackage, Timeout: 3 * time.Minute, Test: "TestChatInstances", Stages: []string{"cross_instance_preview_final", "cross_instance_history"}, Description: "两个真实后端实例间的流式广播、最终回复及历史一致性"},
	"chat.public":      {Package: testPackage, Timeout: 6 * time.Minute, Test: "TestChatPublicStart", Stages: []string{"public_runner_error_recovered", "public_session_started", "public_runner_roundtrip", "public_runner_multiple_work"}, Description: "公开创建会话，后台 Runner 连续领取工作，经本地 Docker 沙箱完成聊天"},
	"chat.reliability": {Package: testPackage, Timeout: 5 * time.Minute, Test: "TestChatReliability", Stages: []string{"busy_input_rejected", "worker_stream_reconnected", "midstream_history_recovered", "public_stream_reconnected", "public_history_reconciled", "busy_input_retried", "worker_restarted"}, Description: "忙时拒绝与重试、Worker 重连与重启、公开 SSE 重订阅和历史对账"},
	"chat.roundtrip":   {Package: testPackage, Timeout: 3 * time.Minute, Test: "TestChatRoundtrip", Stages: []string{"sse_connected", "input_submitted", "preview_and_final_match", "history_recovered", "idle_and_drained"}, Description: "消息发送、中文流式预览、最终回复、历史恢复和队列清空"},
	"chat.tools":       {Package: testPackage, Timeout: 3 * time.Minute, Test: "TestChatTools", Stages: []string{"tool_denied", "tool_allowed"}, Description: "通过 Go SDK 拒绝/允许 Write，核对文件副作用、事件历史和队列清空"},
}

type proof struct {
	Stage     string `json:"stage"`
	ElapsedMS int64  `json:"elapsed_ms"`
}
type testEvent struct {
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Action  string `json:"Action"`
	Output  string `json:"Output"`
}
type testResult struct {
	FailureKind   string            `json:"failure_kind,omitempty"`
	Samples       []latencySample   `json:"samples,omitempty"`
	Status        string            `json:"status"`
	Returncode    int               `json:"returncode"`
	Tests         map[string]string `json:"tests"`
	Skipped       []string          `json:"skipped"`
	MissingStages []string          `json:"missing_stages"`
	Timeline      []proof           `json:"timeline"`
}
type sourceVersion struct {
	Commit string `json:"commit"`
	SHA256 string `json:"source_sha256"`
}
type report struct {
	FailureKind     string             `json:"failure_kind,omitempty"`
	Timeout         string             `json:"timeout,omitempty"`
	WorkloadSHA256  string             `json:"workload_sha256,omitempty"`
	BackendCommit   string             `json:"backend_commit,omitempty"`
	Diagnostics     bool               `json:"diagnostics"`
	Performance     *performanceResult `json:"performance,omitempty"`
	RunID           string             `json:"run_id"`
	Scenario        string             `json:"scenario"`
	Status          string             `json:"status"`
	Source          sourceVersion      `json:"source"`
	Doctor          doctorResult       `json:"doctor"`
	BinarySHA256    string             `json:"binary_sha256,omitempty"`
	Result          testResult         `json:"result"`
	Error           string             `json:"error,omitempty"`
	CleanupErrors   []string           `json:"cleanup_errors"`
	CleanupComplete bool               `json:"cleanup_complete"`
}

func evaluate(reader io.Reader, code int, selected scenario) (testResult, error) {
	result := testResult{Status: "fail", FailureKind: "test_failure", Returncode: code, Tests: map[string]string{}, Skipped: []string{}, MissingStages: []string{}, Timeline: []proof{}}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	failed := false
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return result, fmt.Errorf("invalid test JSON: %w", err)
		}
		if event.Package != selected.Package {
			continue
		}
		if strings.Contains(event.Output, "panic: test timed out") || strings.Contains(event.Output, "CHAT_TIMEOUT") {
			result.FailureKind = "scenario_timeout"
		}
		switch event.Action {
		case "pass", "fail", "skip":
			result.Tests[event.Test] = event.Action
		}
		if event.Action == "fail" {
			failed = true
		}
		if event.Action == "skip" {
			result.Skipped = append(result.Skipped, event.Test)
		}
		if event.Test == selected.Test {
			if _, data, ok := strings.Cut(event.Output, "CHAT_SAMPLE "); ok {
				var sample latencySample
				if err := json.Unmarshal([]byte(data), &sample); err != nil {
					return result, err
				}
				result.Samples = append(result.Samples, sample)
			}
			if _, data, ok := strings.Cut(event.Output, "CHAT_PROOF "); ok {
				var p proof
				if err := json.Unmarshal([]byte(data), &p); err != nil {
					return result, err
				}
				result.Timeline = append(result.Timeline, p)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	for _, stage := range selected.Stages {
		if !slices.ContainsFunc(result.Timeline, func(p proof) bool { return p.Stage == stage }) {
			result.MissingStages = append(result.MissingStages, stage)
		}
	}
	if code == 0 && !failed && result.Tests[selected.Test] == "pass" && result.Tests[""] == "pass" && len(result.Skipped) == 0 && len(result.MissingStages) == 0 {
		result.Status = "pass"
		result.FailureKind = ""
	}
	return result, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func sourceIdentity(ctx context.Context, root string) (sourceVersion, error) {
	var result sourceVersion
	data, err := command(ctx, root, nil, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return result, err
	}
	paths := strings.Split(string(data), "\x00")
	slices.Sort(paths)
	hash := sha256.New()
	for _, path := range slices.Compact(paths) {
		if path == "" {
			continue
		}
		_, _ = io.WriteString(hash, path+"\x00")
		contents, err := os.ReadFile(filepath.Join(root, path))
		if os.IsNotExist(err) {
			contents = []byte("<absent>")
		} else if err != nil {
			return result, err
		}
		_, _ = hash.Write(contents)
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	result.Commit, err = capture(ctx, root, "git", "rev-parse", "HEAD")
	return result, err
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func saveReport(directory string, r report) error {
	if err := writeJSON(filepath.Join(directory, "report.json"), r); err != nil {
		return err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# Chat verification\n\nResult: %s\nScenario: %s\nRun: %s\nVerification commit: %s\n\n", r.Status, r.Scenario, r.RunID, r.Source.Commit)
	fmt.Fprintf(&out, "Verification source SHA-256: %s\nBackend commit: %s\nBackend binary SHA-256: %s\nScenario timeout: %s\nFailure kind: %s\n\n", r.Source.SHA256, r.BackendCommit, r.BinarySHA256, r.Timeout, r.FailureKind)
	if r.BackendCommit == "" {
		out.WriteString("Backend built from the working tree; this measurement is not eligible as a baseline.\n\n")
	}
	fmt.Fprintf(&out, "Machine: %s\n\n", r.Doctor.Machine)
	for _, entry := range []struct {
		title  string
		values map[string]string
	}{{"Tools", r.Doctor.Versions}, {"Image IDs", r.Doctor.Images}} {
		fmt.Fprintf(&out, "%s:\n", entry.title)
		names := make([]string, 0, len(entry.values))
		for name := range entry.values {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			fmt.Fprintf(&out, "- %s: %s\n", name, entry.values[name])
		}
		out.WriteString("\n")
	}
	out.WriteString("Coverage: real backend, PostgreSQL, Redis, JetStream, Core NATS and Worker; scripted upstream model.\n")
	fmt.Fprintf(&out, "Scenario: %s\n\n", scenarios[r.Scenario].Description)
	if r.Scenario == "chat.public" {
		out.WriteString("Sandbox allocation uses a local Docker Provider; real Runner, filesystem mounts and environment-manager execute unchanged. Cloud provider API/network policies are not verified.\n\n")
	} else {
		out.WriteString("Session activation is prepared by the fixture. Public Runner provisioning is verified separately by chat.public.\n\n")
	}
	if r.Performance != nil {
		fmt.Fprintf(&out, "Performance mode: %s\nWorkload: %s\nBaseline run: %s\nBaseline commit: %s\n\n| Metric | P50 ms | P95 ms |\n| --- | ---: | ---: |\n", r.Performance.Mode, r.Performance.Workload, r.Performance.BaselineRun, r.Performance.BaselineCommit)
		for _, name := range []string{"accept", "first_preview", "final", "idle"} {
			value := r.Performance.Metrics[name]
			fmt.Fprintf(&out, "| %s | %.2f | %.2f |\n", name, value.P50, value.P95)
		}
		out.WriteString("\nGate: reject P50 or P95 > baseline × 1.25 + 25 ms. Measurements without a baseline and diagnostic runs are not regression verdicts.\n\n")
		for _, regression := range r.Performance.Regressions {
			fmt.Fprintf(&out, "- %s\n", regression)
		}
	}
	for _, p := range r.Result.Timeline {
		fmt.Fprintf(&out, "- %s: %d ms since scenario start\n", p.Stage, p.ElapsedMS)
	}
	if r.Error != "" {
		fmt.Fprintf(&out, "\n%s\n", r.Error)
	}
	fmt.Fprintf(&out, "\nCleanup complete: %t\n", r.CleanupComplete)
	for _, failure := range r.CleanupErrors {
		fmt.Fprintf(&out, "- %s\n", failure)
	}
	return os.WriteFile(filepath.Join(directory, "report.md"), []byte(out.String()), 0o600)
}
