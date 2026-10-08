package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const testPackage = "github.com/superduck-ai/open-managed-agents/tests/liveworker"

type scenario struct {
	DependenciesOnly bool
	Package          string
	Timeout          time.Duration
	Test             string
	Stages           []string
	Description      string
}

var scenarios = map[string]scenario{
	"deployment.lifecycle":   {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyDeploymentLifecycle", Stages: []string{"invalid_runs_leave_no_effects", "manual_run_effects_match", "durable_schedule_executed", "pause_archive_stop_schedule"}, Description: "Deployment 公开创建/运行、持久化调度执行、终态与 Session/Work/事件/挂载副作用一致"},
	"deployment.retry":       {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyDeploymentRetry", Stages: []string{"failed_attempt_rolled_back", "automatic_retry_matches", "exhausted_job_has_no_effects", "reference_failure_paused"}, Description: "真实 River 自动退避重试、事务回滚、耗尽终态与依赖失败自动暂停"},
	"deployment.idempotency": {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyDeploymentIdempotency", Stages: []string{"stale_jobs_have_no_effects", "duplicate_occurrence_single_effect", "distinct_occurrence_preserved"}, Description: "真实 River 并发重复投递、旧调度快照拒绝、同 occurrence 副作用唯一与不同 occurrence 保留"},
	"deployment.restart":     {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 5 * time.Minute, Test: "TestVerifyDeploymentRestart", Stages: []string{"uncommitted_crash_rolled_back", "uncommitted_restart_recovered", "committed_crash_preserved", "committed_restart_deduplicated", "overdue_schedule_recovered"}, Description: "SIGKILL 真实 River 进程，在业务提交前后恢复运行并核对副作用；测试缩短 rescue 时间"},
	"memory.integrity":       {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyMemoryIntegrity", Stages: []string{"invalid_writes_rejected", "missing_object_rejected", "repaired_memory_matches"}, Description: "Memory 无效正文、路径树冲突、缺失对象拒绝读取与修复"},
	"memory.isolation":       {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyMemoryIsolation", Stages: []string{"readonly_and_scope_enforced", "archived_store_readonly", "deleted_session_token_revoked"}, Description: "Memory 挂载权限、filesystem 及租户隔离、归档拒写与旧 token 撤销"},
	"memory.lifecycle":       {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyMemoryLifecycle", Stages: []string{"memory_versions_match", "cross_session_memory_preserved", "deleted_memory_history_preserved"}, Description: "Memory 双会话读写、不可变版本、相同正文幂等及会话删除后记忆保留"},
	"memory.cleanup":         {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyMemoryCleanup", Stages: []string{"deletion_cleanup_atomic", "failed_write_compensated", "discarded_upload_cleanup_recovered", "cleanup_retry_persisted", "memory_objects_removed"}, Description: "Memory 写入失败补偿、对象删除失败入队重试及真实非版本化 MinIO 清理"},
	"memory.filestore":       {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyMemoryFilestore", Stages: []string{"filestore_mutations_match", "session_owned_cleanup_completed", "memory_survives_filesystem_cleanup"}, Description: "普通 Filestore 覆盖/复制/移动/删除字节一致、Session Owned 对象与账本清理"},
	"memory.mounts":          {Package: testPackage, Timeout: 8 * time.Minute, Test: "TestVerifyMemoryMounts", Stages: []string{"memory_mounts_ready", "mounted_write_persisted", "sandbox_lifecycle_matches", "mount_failure_cleaned"}, Description: "实际 Runner/Docker/FUSE：只读与可写挂载、沙箱销毁后的新会话和启动失败清理"},
	"transcript.boundary":    {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyTranscriptBoundary", Stages: []string{"scope_boundaries_preserved", "boundary_delete_preserves_reads", "boundary_restore_matches"}, Description: "真实 S3：前台/subagent 交错 compaction 边界归档，软删和物理删除保持 HTTP 可见历史"},
	"transcript.lifecycle":   {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 5 * time.Minute, Test: "TestVerifyTranscriptLifecycle", Stages: []string{"archive_export_matches", "hard_delete_preserves_backup", "blob_gc_completed", "cli_restore_matches", "tenant_history_isolated"}, Description: "真实 PostgreSQL/MinIO：归档、导出、物理删除、旧 blob 清理及维护 CLI 幂等还原"},
	"transcript.recovery":    {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 5 * time.Minute, Test: "TestVerifyTranscriptRecovery", Stages: []string{"upload_failures_recovered", "archive_batches_recovered", "delete_batches_recovered", "restore_batches_recovered"}, Description: "真实对象存储：上传前后中断、pending 回收及归档/删除/还原分批失败后重试"},
	"transcript.concurrency": {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyTranscriptConcurrency", Stages: []string{"concurrent_archive_coordinated", "single_manifest_preserved", "concurrent_retry_matches"}, Description: "两个归档调用在上传完成后确定性交错，无重复有效段且重试结果一致"},
	"transcript.integrity":   {DependenciesOnly: true, Package: "github.com/superduck-ai/open-managed-agents/tests", Timeout: 3 * time.Minute, Test: "TestVerifyTranscriptIntegrity", Stages: []string{"missing_object_blocks_operations", "corrupt_object_blocks_operations", "repaired_object_recovers"}, Description: "真实 MinIO 对象丢失/损坏时导出、还原、物理删除拒绝执行，修复后恢复"},
	"files.cloud-storage":    {Timeout: 3 * time.Minute, Stages: []string{"cloud_storage_roundtrip", "cloud_readonly_enforced", "cloud_storage_cleaned"}, Description: "显式云配置：真实 S3 字节一致、只读 IAM 拒绝写入/删除及对象清理"},
	"chat.cloud-renewal":     {Timeout: 5 * time.Minute, Stages: []string{"cloud_sandbox_created", "cloud_timeout_extended", "cloud_sandbox_deleted"}, Description: "显式云配置：真实 E2B 创建、生产 Provider 续期、到期时间核对及销毁"},
	"files.exhaustion":       {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 3 * time.Minute, Test: "TestFilesExhaustion", Stages: []string{"cleanup_retry_budget_exhausted", "failed_job_stays_terminal", "explicit_recovery_completed"}, Description: "真实清理 Worker 连续十次失败、终态停止领取、显式恢复；测试提前调度重试"},
	"files.performance":      {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 5 * time.Minute, Test: "TestFilesPerformance", Stages: []string{"files_load_completed", "files_load_cleaned"}, Description: "固定 32 KiB 文件负载，测量上传、元数据、列表、下载、删除并比较基线"},
	"files.generated":        {Package: testPackage, Timeout: 6 * time.Minute, Test: "TestFilesGenerated", Stages: []string{"worker_output_projected", "generated_download_matches", "generated_reference_protected"}, Description: "真实 Worker 经 FUSE 生成文件、Files 投影、会话资源与下载内容一致"},
	"files.recovery":         {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 5 * time.Minute, Test: "TestFilesRecovery", Stages: []string{"cleanup_failures_enqueued", "backoff_and_recovery", "compensation_preserved_owner"}, Description: "对象删除与配额回滚失败自动入队、真实后台退避重试及恢复清理"},
	"files.attachments":      {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 3 * time.Minute, Test: "TestFilesAttachments", Stages: []string{"referenced_delete_rejected", "unlink_then_delete", "expired_output_hidden", "archive_preserves_output", "session_cleanup_preserves_upload"}, Description: "会话引用阻止删除、解除引用、输出过期不可见及会话删除后的对象清理"},
	"files.platform":         {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 3 * time.Minute, Test: "TestFilesPlatform", Stages: []string{"platform_invalid_rejected", "preview_and_thumbnail_match", "derived_objects_deleted"}, Description: "平台登录、Base64 上传、预览、缩略图与衍生对象清理"},

	"files.lifecycle":  {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 3 * time.Minute, Test: "TestFilesLifecycle", Stages: []string{"upload_stored", "metadata_and_listing", "download_bytes_match", "deleted_objects_absent"}, Description: "上传和对象字节一致、元数据与列表、可下载文件内容一致、删除及对象清理"},
	"files.isolation":  {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 3 * time.Minute, Test: "TestFilesIsolation", Stages: []string{"tenant_access_denied", "owner_data_unchanged"}, Description: "同组织跨工作区及跨组织的读取、下载、列表、删除隔离"},
	"files.invalid":    {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 3 * time.Minute, Test: "TestFilesInvalid", Stages: []string{"invalid_uploads_rejected", "quota_rollback_verified", "valid_upload_after_rejection"}, Description: "鉴权、无效上传、大小限制、配额拒绝与对象回滚，失败后仍能正常上传"},
	"files.storage":    {Package: "github.com/superduck-ai/open-managed-agents/tests/livefiles", Timeout: 3 * time.Minute, Test: "TestFilesStorage", Stages: []string{"missing_object_rejected", "background_object_cleanup", "storage_permissions_rejected", "storage_disconnect_recovered", "multipart_abort_cleaned", "versioned_objects_cleaned"}, Description: "真实 MinIO 错误与后台清理、权限拒绝、断连恢复、分片中断及版本删除"},
	"chat.performance": {Package: testPackage, Timeout: 5 * time.Minute, Test: "TestChatPerformance", Stages: []string{"fixed_load_completed", "performance_history_verified"}, Description: "固定串行负载，测量接收、首片段、最终回复及空闲延迟，并可比较基线"},
	"chat.instances":   {Package: testPackage, Timeout: 3 * time.Minute, Test: "TestChatInstances", Stages: []string{"cross_instance_preview_final", "cross_instance_history"}, Description: "两个真实后端实例间的流式广播、最终回复及历史一致性"},
	"chat.public":      {Package: testPackage, Timeout: 6 * time.Minute, Test: "TestChatPublicStart", Stages: []string{"public_runner_error_recovered", "public_session_started", "public_runner_roundtrip", "public_runner_multiple_work"}, Description: "公开创建会话，后台 Runner 连续领取工作，经本地 Docker 沙箱完成聊天"},
	"chat.reliability": {Package: testPackage, Timeout: 5 * time.Minute, Test: "TestChatReliability", Stages: []string{"busy_input_rejected", "worker_stream_reconnected", "midstream_history_recovered", "public_stream_reconnected", "public_history_reconciled", "busy_input_retried", "worker_restarted"}, Description: "忙时拒绝与重试、Worker 重连与重启、公开 SSE 重订阅和历史对账"},
	"chat.roundtrip":   {Package: testPackage, Timeout: 3 * time.Minute, Test: "TestChatRoundtrip", Stages: []string{"sse_connected", "input_submitted", "preview_and_final_match", "history_recovered", "idle_and_drained"}, Description: "消息发送、中文流式预览、最终回复、历史恢复和队列清空"},
	"chat.tools":       {Package: testPackage, Timeout: 3 * time.Minute, Test: "TestChatTools", Stages: []string{"malformed_tool_rejected", "tool_denied", "tool_allowed"}, Description: "拒绝非法参数工具，核对 SSE/历史关联，再通过 Go SDK 拒绝/允许 Write 并核对副作用和队列清空"},

	"chat.upstream-errors": {Package: testPackage, Timeout: 5 * time.Minute, Test: "TestChatUpstreamErrors", Stages: []string{"anthropic_401_failed_and_idle", "generic_401_failed_and_idle", "backoff_500_interrupted_and_recovered"}, Description: "真实 Worker 的上游 401 自然失败收敛、500 退避中断与下一轮恢复"},
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
	FilesSamples  []filesLatencySample `json:"files_samples,omitempty"`
	FailureKind   string               `json:"failure_kind,omitempty"`
	Samples       []latencySample      `json:"samples,omitempty"`
	Status        string               `json:"status"`
	Returncode    int                  `json:"returncode"`
	Tests         map[string]string    `json:"tests"`
	Skipped       []string             `json:"skipped"`
	MissingStages []string             `json:"missing_stages"`
	Timeline      []proof              `json:"timeline"`
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
		if strings.Contains(event.Output, "panic: test timed out") || strings.Contains(event.Output, "BE_TIMEOUT") {
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
			if _, data, ok := strings.Cut(event.Output, "FILES_SAMPLE "); ok {
				var sample filesLatencySample
				if err := json.Unmarshal([]byte(data), &sample); err != nil {
					return result, err
				}
				result.FilesSamples = append(result.FilesSamples, sample)
			}
			if _, data, ok := strings.Cut(event.Output, "CHAT_SAMPLE "); ok {
				var sample latencySample
				if err := json.Unmarshal([]byte(data), &sample); err != nil {
					return result, err
				}
				result.Samples = append(result.Samples, sample)
			}
		}
		if event.Test == selected.Test || strings.HasPrefix(event.Test, selected.Test+"/") {
			if _, data, ok := strings.Cut(event.Output, "BE_PROOF "); ok {
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
	fmt.Fprintf(&out, "# Backend verification\n\nResult: %s\nScenario: %s\nRun: %s\nVerification commit: %s\n\n", r.Status, r.Scenario, r.RunID, r.Source.Commit)
	fmt.Fprintf(&out, "Verification source SHA-256: %s\nBackend commit: %s\nBackend binary SHA-256: %s\nScenario timeout: %s\nFailure kind: %s\n\n", r.Source.SHA256, r.BackendCommit, r.BinarySHA256, r.Timeout, r.FailureKind)
	if isCloudScenario(r.Scenario) {
		out.WriteString("Execution: cloud adapter assertions in the Go CLI; no local backend or Worker model is started. Credentials are supplied by an explicit private configuration.\n\n")
	} else if strings.HasPrefix(r.Scenario, "deployment.") {
		out.WriteString("Execution: production Deployment HTTP/Store and asynchronous River workers with disposable PostgreSQL/MinIO. restart uses separate River processes. No standalone backend or model Worker is started.\n\n")
	} else if selectScenario(r.Scenario).DependenciesOnly {
		out.WriteString("Execution: production services and HTTP handlers in the Go test process, with isolated PostgreSQL schemas and real MinIO. No standalone backend or Worker is started.\n\n")
	} else if r.Scenario != "test" && r.BackendCommit == "" {
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
	fmt.Fprintf(&out, "Scenario: %s\n\n", selectScenario(r.Scenario).Description)
	if r.Scenario == "test" {
		out.WriteString("Coverage: all default-build Go packages with disposable local dependencies, S3 and migration integration enabled. Skipped tests are unverified, not passes. Live scenarios run separately; cloud/e2e build-tag suites and external SDK language suites are outside this command. No standalone backend is started.\n\n")
		for _, missing := range r.Result.MissingStages {
			fmt.Fprintf(&out, "- Required test did not pass: %s\n", missing)
		}
		for _, skipped := range r.Result.Skipped {
			fmt.Fprintf(&out, "- Unverified (skipped): %s\n", skipped)
		}
		for _, name := range slices.Sorted(maps.Keys(r.Result.Tests)) {
			if r.Result.Tests[name] == "fail" {
				fmt.Fprintf(&out, "- Failed: %s\n", name)
			}
		}
		out.WriteString("\n")
	} else if isCloudScenario(r.Scenario) {
		out.WriteString("Coverage: only the selected cloud storage or E2B provider operations and final cleanup. This does not certify public chat confirmation, all IAM/network policies or cloud availability.\n\n")
	} else if strings.HasPrefix(r.Scenario, "deployment.") {
		out.WriteString("Coverage: production Deployment HTTP handlers and scheduled worker with real PostgreSQL/River, unique business schema and disposable public River tables. Jobs run asynchronously; retries retain their nominal occurrence. restart SIGKILLs a River worker process before and after business commit and uses real rescue with a test-only 10s job timeout/15s rescue threshold; the default one-hour rescue delay is not measured. Durable dispatch is made due through River APIs. No standalone backend or model Worker; Session execution, cloud sandbox allocation and production throughput are not verified.\n\n")
	} else if strings.HasPrefix(r.Scenario, "transcript.") {
		out.WriteString("Coverage: archive/export/restore/delete service, real S3 adapter, deterministic test-side interruptions and batch rollback. lifecycle also runs the actual maintenance CLI. Fixtures explicitly age rows; five-minute River sweeps, automatic River retries, crash recovery, cloud IAM and production throughput are not verified.\n\n")
	} else if strings.HasPrefix(r.Scenario, "memory.") {
		out.WriteString("Coverage: Memory/Filestore HTTP contracts with real PostgreSQL and MinIO; mounts additionally uses production Runner, local Docker sandbox and actual FUSE. Test-side faults and cleanup RunOnce do not certify cloud allocation, automatic background retry timing, process crash recovery, token renewal or concurrent editing throughput.\n\n")
	} else if strings.HasPrefix(r.Scenario, "files.") {
		out.WriteString("Coverage: real backend HTTP, PostgreSQL and MinIO. See scenario proof stages. generated runs a real Worker with FUSE and a scripted model; other Files scenarios use DB/storage fixtures where documented. recovery injects S3 errors through a local proxy and waits for the actual cleanup loop and backoff. Cloud S3 IAM and network infrastructure are not verified.\n\n")
	} else {
		out.WriteString("Coverage: real backend, PostgreSQL, Redis, JetStream, Core NATS and Worker; scripted upstream model.\n")
		if r.Scenario == "chat.public" || r.Scenario == "chat.upstream-errors" {
			out.WriteString("Sandbox allocation uses a local Docker Provider; real Runner, filesystem mounts and environment-manager execute unchanged. A local E2B control API checks container ownership and handles connect/timeout/deletion. Cloud TTL renewal and provider API/network policies are not verified.\n\n")
		} else {
			out.WriteString("Session activation is prepared by the fixture. Public Runner provisioning is verified separately by chat.public.\n\n")
		}
	}
	if r.Performance != nil {
		fmt.Fprintf(&out, "Performance mode: %s\nWorkload: %s\nBaseline run: %s\nBaseline commit: %s\n\n| Metric | P50 ms | P95 ms |\n| --- | ---: | ---: |\n", r.Performance.Mode, r.Performance.Workload, r.Performance.BaselineRun, r.Performance.BaselineCommit)
		metrics := []string{"accept", "first_preview", "final", "idle"}
		if r.Scenario == "files.performance" {
			metrics = []string{"upload", "metadata", "list", "download", "delete"}
		}
		for _, name := range metrics {
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
