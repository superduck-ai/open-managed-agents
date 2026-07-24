package environments

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCodeSessionSandboxAPIBaseURLDoesNotInferServerAddress(t *testing.T) {
	cfg := config.Config{Server: config.ServerConfig{Addr: "127.0.0.1:38080"}}

	if baseURL := codeSessionSandboxAPIBaseURL(cfg); baseURL != "" {
		t.Fatalf("codeSessionSandboxAPIBaseURL() = %q, want empty value", baseURL)
	}
}

func TestCodeSessionSandboxAPIBaseURLUsesConfiguredValue(t *testing.T) {
	cfg := config.Config{
		Server:      config.ServerConfig{Addr: "127.0.0.1:38080"},
		CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "  http://sandbox-api.example.test/  "},
	}

	if baseURL := codeSessionSandboxAPIBaseURL(cfg); baseURL != "http://sandbox-api.example.test" {
		t.Fatalf("codeSessionSandboxAPIBaseURL() = %q, want configured value", baseURL)
	}
}

func managedAgentRuntimeSourceValues(
	t *testing.T,
	sources []json.RawMessage,
) []any {
	t.Helper()
	raw, err := json.Marshal(sources)
	if err != nil {
		t.Fatalf("marshal runtime sources: %v", err)
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("decode runtime sources: %v", err)
	}
	return values
}

func TestManagedAgentWorkDirIgnoresNonRepositoryResources(t *testing.T) {
	resources := []db.SessionResource{
		{
			ResourceType: "file",
			Payload:      json.RawMessage(`{"type":"file","file_id":"file_test","source":"/uploads","mount_path":"/workspace/data.csv"}`),
		},
		{
			ResourceType: "memory_store",
			Payload:      json.RawMessage(`{"type":"memory_store","memory_store_id":"mem_test","mount_path":"/workspace/memory"}`),
		},
		{
			ResourceType: "future_resource",
			Payload:      json.RawMessage(`{"type":"future_resource","mount_path":"/workspace/future"}`),
		},
	}
	if workDir := resolveManagedAgentRuntimeResources(resources).workDir; workDir != defaultEnvironmentWorkDir {
		t.Fatalf("managedAgentWorkDir() = %q, want %q", workDir, defaultEnvironmentWorkDir)
	}
}

func TestManagedAgentWorkDirSkipsInvalidRepositoryCandidates(t *testing.T) {
	resources := []db.SessionResource{
		{
			ID:           1,
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","mount_path":`),
		},
		{
			ID:           2,
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"  "}`),
		},
	}
	if workDir := resolveManagedAgentRuntimeResources(resources).workDir; workDir != defaultEnvironmentWorkDir {
		t.Fatalf("managedAgentWorkDir() = %q, want %q", workDir, defaultEnvironmentWorkDir)
	}

	resources = append(resources, db.SessionResource{
		ID:           3,
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"/workspace/valid"}`),
	})
	if workDir := resolveManagedAgentRuntimeResources(resources).workDir; workDir != "/workspace/valid" {
		t.Fatalf("managedAgentWorkDir() = %q, want %q", workDir, "/workspace/valid")
	}
}

func TestManagedAgentWorkDirUsesRepositoryRegardlessOfResourceOrder(t *testing.T) {
	repository := db.SessionResource{
		ID:           2,
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":" /workspace/repository "}`),
	}
	file := db.SessionResource{
		ID:           1,
		ResourceType: "file",
		Payload:      json.RawMessage(`{"type":"file","mount_path":"/workspace/data.csv"}`),
	}
	memoryStore := db.SessionResource{
		ID:           3,
		ResourceType: "memory_store",
		Payload:      json.RawMessage(`{"type":"memory_store","mount_path":"/workspace/memory"}`),
	}
	for name, resources := range map[string][]db.SessionResource{
		"repository first": {repository, file, memoryStore},
		"repository last":  {memoryStore, file, repository},
	} {
		t.Run(name, func(t *testing.T) {
			if workDir := resolveManagedAgentRuntimeResources(resources).workDir; workDir != "/workspace/repository" {
				t.Fatalf("managedAgentWorkDir() = %q, want %q", workDir, "/workspace/repository")
			}
		})
	}
}

func TestManagedAgentWorkDirUsesEarliestAttachedRepository(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	first := db.SessionResource{
		ID:           10,
		ExternalID:   "sesrsc_first",
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"/workspace/first"}`),
		CreatedAt:    createdAt,
	}
	later := db.SessionResource{
		ID:           11,
		ExternalID:   "sesrsc_later",
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"/workspace/later"}`),
		CreatedAt:    createdAt.Add(time.Minute),
	}
	sameTimeLater := later
	sameTimeLater.CreatedAt = createdAt

	for name, resources := range map[string][]db.SessionResource{
		"reverse list order":      {later, first},
		"forward list order":      {first, later},
		"same timestamp uses id":  {sameTimeLater, first},
		"same timestamp reversed": {first, sameTimeLater},
	} {
		t.Run(name, func(t *testing.T) {
			if workDir := resolveManagedAgentRuntimeResources(resources).workDir; workDir != "/workspace/first" {
				t.Fatalf("managedAgentWorkDir() = %q, want %q", workDir, "/workspace/first")
			}
		})
	}
}

func TestManagedAgentSourcesExcludesFileResources(t *testing.T) {
	resources := []db.SessionResource{
		{
			ResourceType: "file",
			Payload:      json.RawMessage(`{"type":"file","file_id":"file_test","source":"/uploads","mount_path":"/workspace/data.csv"}`),
		},
		{
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","url":" https://github.com/acme/widgets ","mount_path":" /workspace/widgets ","checkout":"main"}`),
		},
		{
			ResourceType: "memory_store",
			Payload:      json.RawMessage(`{"type":"memory_store","memory_store_id":"mem_test","mount_path":"/workspace/memory","runtime_extension":{"enabled":true}}`),
		},
	}

	want := []any{
		map[string]any{
			"type":       "git_repository",
			"url":        "https://github.com/acme/widgets",
			"mount_path": "/workspace/widgets",
			"checkout":   "main",
		},
		map[string]any{
			"type":            "memory_store",
			"memory_store_id": "mem_test",
			"mount_path":      "/workspace/memory",
			"runtime_extension": map[string]any{
				"enabled": true,
			},
		},
	}
	sources := managedAgentRuntimeSourceValues(
		t,
		resolveManagedAgentRuntimeResources(resources).sources,
	)
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("managedAgentSources() = %#v, want %#v", sources, want)
	}
}

func TestManagedAgentRuntimeResourcesSkipInvalidSources(t *testing.T) {
	resources := []db.SessionResource{
		{
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","url":`),
		},
		{
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","url":"  ","mount_path":"/workspace/empty-url"}`),
		},
		{
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","url":"https://github.com/acme/empty-path","mount_path":"  "}`),
		},
		{
			ResourceType: "memory_store",
			Payload:      json.RawMessage(`{"type":"memory_store","memory_store_id":`),
		},
		{
			ResourceType: "memory_store",
			Payload:      json.RawMessage(`null`),
		},
	}

	if sources := resolveManagedAgentRuntimeResources(resources).sources; len(sources) != 0 {
		t.Fatalf("managedAgentSources() = %#v, want no sources", sources)
	}
}

func TestBuildEnvironmentManagerPayloadAndCommand(t *testing.T) {
	// 故意给配置放入可识别的上游密钥，后续断言它不会进入 payload 或 shell 命令。
	cfg := config.Config{
		CodeSession: config.CodeSessionConfig{
			SandboxAPIBaseURL: "http://host.docker.internal:18081/",
		},
		AnthropicUpstream: config.AnthropicUpstreamConfig{
			BaseURL: "https://api.anthropic.test/",
			APIKey:  "sk-ant-test-secret",
		},
		EnvironmentRunner: config.EnvironmentRunnerConfig{
			ManagerPath:        "/opt/env manager/bin/environment-manager",
			ClaudeAgentVersion: "2.1.120",
			ClaudePath:         "/opt/claude path/bin/claude",
		},
	}
	sessionConfig := json.RawMessage(`{"model":"claude-opus-4-8","sources":[{"type":"git_repository","url":"https://github.com/acme/widgets"}]}`)
	const sessionIngressToken = "sk-ant-si-test-token"
	const oauthAccessToken = "sk-ant-oat01-test-token"
	payload, err := buildEnvironmentManagerV0Payload("cse_test", sessionIngressToken, oauthAccessToken, "/workspace/widgets", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	if startup["api_base_url"] != "http://host.docker.internal:18081" || startup["session_id"] != "cse_test" || startup["use_code_sessions"] != true {
		t.Fatalf("unexpected startup context: %#v", startup)
	}
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["CLAUDE_CODE_REMOTE"] != "true" ||
		startupEnv["CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2"] != "1" ||
		startupEnv["CLAUDE_CODE_USE_CCR_V2"] != "1" ||
		startupEnv["CLAUDE_CODE_WORKER_EPOCH"] != "1" ||
		startupEnv["CCR_UPSTREAM_PROXY_ENABLED"] != "1" {
		t.Fatalf("unexpected startup environment variables: %#v", startupEnv)
	}
	if _, ok := startupEnv["CLAUDE_CODE_SESSION_ACCESS_TOKEN"]; ok {
		t.Fatalf("session access token environment variable must not mask the WebSocket auth FD: %#v", startupEnv)
	}
	if startupEnv["OTEL_METRICS_EXPORTER"] != "otlp" ||
		startupEnv["OTEL_EXPORTER_OTLP_METRICS_PROTOCOL"] != "http/protobuf" ||
		startupEnv["OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"] != "http://host.docker.internal:18081/v1/code/sessions/cse_test/worker/otlp/metrics" ||
		startupEnv["OTEL_LOGS_EXPORTER"] != "otlp" ||
		startupEnv["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"] != "http/protobuf" ||
		startupEnv["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] != "http://host.docker.internal:18081/v1/code/sessions/cse_test/worker/otlp/logs" ||
		startupEnv["OTEL_EXPORTER_OTLP_METRICS_HEADERS"] != "Authorization=Bearer sk-ant-si-test-token,x-worker-epoch=1" ||
		startupEnv["OTEL_EXPORTER_OTLP_LOGS_HEADERS"] != "Authorization=Bearer sk-ant-si-test-token,x-worker-epoch=1" {
		t.Fatalf("unexpected otlp environment variables: %#v", startupEnv)
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_HEADERS"]; ok {
		t.Fatalf("unexpected generic otlp headers: %#v", startupEnv)
	}
	auths := body["auth"].([]any)
	sessionAuth := auths[0].(map[string]any)
	if sessionAuth["type"] != "session_ingress" || sessionAuth["token"] != sessionIngressToken {
		t.Fatalf("unexpected session auth: %#v", sessionAuth)
	}
	anthropicAuth := auths[1].(map[string]any)
	if anthropicAuth["type"] != "anthropic_oauth" || anthropicAuth["token"] != oauthAccessToken {
		t.Fatalf("unexpected anthropic auth: %#v", anthropicAuth)
	}
	environment := body["environment"].(map[string]any)
	if environment["cwd"] != "/workspace/widgets" || environment["environment_type"] != "anthropic" {
		t.Fatalf("unexpected environment: %#v", environment)
	}
	// sandbox 只能看到 Open Managed Agents 的 api_base_url 与 code-session token，
	// 不得看到服务端保存的 ANTHROPIC_UPSTREAM_API_KEY/ANTHROPIC_BASE_URL。
	if _, ok := environment["environment"]; ok {
		t.Fatalf("environment leaked upstream model credentials: %#v", environment)
	}
	if strings.Contains(string(payload), cfg.AnthropicUpstream.APIKey) {
		t.Fatalf("payload leaked upstream anthropic api key: %s", payload)
	}

	command := buildEnvironmentManagerCommand("cse_session with 'quote'/and/slash", cfg, payload)
	if !reflect.DeepEqual(command.Payload, payload) {
		t.Fatalf("command payload = %q, want %q", command.Payload, payload)
	}
	allCommands := command.ShellCommand
	for _, want := range []string{
		"environment-manager binary missing or not executable: /opt/env manager/bin/environment-manager",
		"Claude binary missing or not executable: /opt/claude path/bin/claude",
		"task-run --stdin --session 'cse_session with '\"'\"'quote'\"'\"'/and/slash'",
		"--session-mode resume-cached",
		"--claude-agent-version 'current'",
		"--claude-path '/opt/claude path/bin/claude'",
		"export SKIP_PLUGIN_MARKETPLACE=${SKIP_PLUGIN_MARKETPLACE:-true}",
		"Claude binary version mismatch: expected 2.1.120",
		"> '/tmp/claude-code-sessions/cse_session_with_'\"'\"'quote'\"'\"'_and_slash/environment-manager.log' 2>&1",
	} {
		if !strings.Contains(allCommands, want) {
			t.Fatalf("commands missing %q in:\n%s", want, allCommands)
		}
	}
	if strings.Contains(allCommands, "sk-ant-test-secret") {
		t.Fatalf("command leaked anthropic api key:\n%s", allCommands)
	}
	if strings.Contains(allCommands, "nohup") ||
		strings.Contains(allCommands, "environment-manager.v0.json") ||
		strings.Contains(allCommands, "rm -f") {
		t.Fatalf("command should rely on E2B background stdin without a temporary payload file:\n%s", allCommands)
	}
	if strings.Contains(allCommands, "installed managed agent skills") ||
		strings.Contains(allCommands, "$HOME/.claude/skills") {
		t.Fatalf("command should not install managed agent skills directly:\n%s", allCommands)
	}
}

func TestBuildEnvironmentManagerPayloadPreservesCustomOTLPMetricsEnvironment(t *testing.T) {
	cfg := config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:18081/"}}
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"OTEL_METRICS_EXPORTER":"console",
		"OTEL_EXPORTER_OTLP_HEADERS":"x-custom=value"
	}}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "sk-ant-si-test-token", "sk-ant-oat01-test-token", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["OTEL_METRICS_EXPORTER"] != "console" {
		t.Fatalf("OTEL_METRICS_EXPORTER = %q, want console", startupEnv["OTEL_METRICS_EXPORTER"])
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"]; ok {
		t.Fatalf("unexpected default otlp metrics endpoint for custom exporter: %#v", startupEnv)
	}
	if startupEnv["OTEL_LOGS_EXPORTER"] != "otlp" ||
		startupEnv["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"] != "http/protobuf" ||
		startupEnv["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] != "http://host.docker.internal:18081/v1/code/sessions/cse_test/worker/otlp/logs" {
		t.Fatalf("unexpected default otlp logs environment variables: %#v", startupEnv)
	}
	if startupEnv["OTEL_EXPORTER_OTLP_HEADERS"] != "x-custom=value" {
		t.Fatalf("OTEL_EXPORTER_OTLP_HEADERS = %q, want existing custom value only", startupEnv["OTEL_EXPORTER_OTLP_HEADERS"])
	}
	if startupEnv["OTEL_EXPORTER_OTLP_LOGS_HEADERS"] != "Authorization=Bearer sk-ant-si-test-token,x-worker-epoch=1" {
		t.Fatalf("OTEL_EXPORTER_OTLP_LOGS_HEADERS = %q, want signal auth", startupEnv["OTEL_EXPORTER_OTLP_LOGS_HEADERS"])
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_METRICS_HEADERS"]; ok {
		t.Fatalf("unexpected metrics headers for custom metrics exporter: %#v", startupEnv)
	}
	if startupEnv["CLAUDE_CODE_WORKER_EPOCH"] != "1" {
		t.Fatalf("CLAUDE_CODE_WORKER_EPOCH = %q, want 1", startupEnv["CLAUDE_CODE_WORKER_EPOCH"])
	}
}

func TestBuildEnvironmentManagerPayloadPreservesCustomOTLPLogsEnvironment(t *testing.T) {
	cfg := config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:18081/"}}
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"OTEL_LOGS_EXPORTER":"console",
		"OTEL_EXPORTER_OTLP_HEADERS":"x-custom=value"
	}}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "sk-ant-si-test-token", "sk-ant-oat01-test-token", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["OTEL_LOGS_EXPORTER"] != "console" {
		t.Fatalf("OTEL_LOGS_EXPORTER = %q, want console", startupEnv["OTEL_LOGS_EXPORTER"])
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"]; ok {
		t.Fatalf("unexpected default otlp logs endpoint for custom exporter: %#v", startupEnv)
	}
	if startupEnv["OTEL_METRICS_EXPORTER"] != "otlp" ||
		startupEnv["OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"] != "http://host.docker.internal:18081/v1/code/sessions/cse_test/worker/otlp/metrics" {
		t.Fatalf("unexpected default otlp metrics environment variables: %#v", startupEnv)
	}
	if startupEnv["OTEL_EXPORTER_OTLP_HEADERS"] != "x-custom=value" {
		t.Fatalf("OTEL_EXPORTER_OTLP_HEADERS = %q, want existing custom value only", startupEnv["OTEL_EXPORTER_OTLP_HEADERS"])
	}
	if startupEnv["OTEL_EXPORTER_OTLP_METRICS_HEADERS"] != "Authorization=Bearer sk-ant-si-test-token,x-worker-epoch=1" {
		t.Fatalf("OTEL_EXPORTER_OTLP_METRICS_HEADERS = %q, want signal auth", startupEnv["OTEL_EXPORTER_OTLP_METRICS_HEADERS"])
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_LOGS_HEADERS"]; ok {
		t.Fatalf("unexpected logs headers for custom logs exporter: %#v", startupEnv)
	}
}

func TestBuildEnvironmentManagerPayloadPreservesCustomGenericOTLPEndpoint(t *testing.T) {
	cfg := config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:18081/"}}
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"OTEL_EXPORTER_OTLP_ENDPOINT":"https://collector.example.com"
	}}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "sk-ant-si-test-token", "sk-ant-oat01-test-token", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["OTEL_EXPORTER_OTLP_ENDPOINT"] != "https://collector.example.com" {
		t.Fatalf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want custom collector", startupEnv["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS",
		"OTEL_EXPORTER_OTLP_LOGS_HEADERS",
	} {
		if _, ok := startupEnv[key]; ok {
			t.Fatalf("unexpected injected %s with custom generic endpoint: %#v", key, startupEnv)
		}
	}
}

func TestBuildEnvironmentManagerPayloadDoesNotLeakHeadersToCustomMetricsEndpoint(t *testing.T) {
	cfg := config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:18081/"}}
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT":"https://collector.example.com/v1/metrics"
	}}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "sk-ant-si-test-token", "sk-ant-oat01-test-token", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"] != "https://collector.example.com/v1/metrics" {
		t.Fatalf("custom metrics endpoint was not preserved: %#v", startupEnv)
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_METRICS_HEADERS"]; ok {
		t.Fatalf("unexpected metrics auth headers for custom metrics endpoint: %#v", startupEnv)
	}
	if startupEnv["OTEL_EXPORTER_OTLP_LOGS_HEADERS"] != "Authorization=Bearer sk-ant-si-test-token,x-worker-epoch=1" {
		t.Fatalf("OTEL_EXPORTER_OTLP_LOGS_HEADERS = %q, want default logs auth", startupEnv["OTEL_EXPORTER_OTLP_LOGS_HEADERS"])
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_HEADERS"]; ok {
		t.Fatalf("unexpected generic otlp headers with custom metrics endpoint: %#v", startupEnv)
	}
}

func TestBuildEnvironmentManagerPayloadDoesNotLeakHeadersToCustomLogsEndpoint(t *testing.T) {
	cfg := config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:18081/"}}
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT":"https://collector.example.com/v1/logs"
	}}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "sk-ant-si-test-token", "sk-ant-oat01-test-token", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] != "https://collector.example.com/v1/logs" {
		t.Fatalf("custom logs endpoint was not preserved: %#v", startupEnv)
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_LOGS_HEADERS"]; ok {
		t.Fatalf("unexpected logs auth headers for custom logs endpoint: %#v", startupEnv)
	}
	if startupEnv["OTEL_EXPORTER_OTLP_METRICS_HEADERS"] != "Authorization=Bearer sk-ant-si-test-token,x-worker-epoch=1" {
		t.Fatalf("OTEL_EXPORTER_OTLP_METRICS_HEADERS = %q, want default metrics auth", startupEnv["OTEL_EXPORTER_OTLP_METRICS_HEADERS"])
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_HEADERS"]; ok {
		t.Fatalf("unexpected generic otlp headers with custom logs endpoint: %#v", startupEnv)
	}
}

func TestManagedAgentSessionConfigIncludesMCPConfig(t *testing.T) {
	session := db.Session{
		AgentSnapshot: json.RawMessage(`{
			"model":{"id":"claude-opus-4-8"},
			"mcp_servers":[{"type":"url","name":"notion","url":"https://mcp.notion.com/mcp"}],
			"tools":[{
				"type":"mcp_toolset",
				"mcp_server_name":"notion",
				"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},
				"configs":[
					{"name":"search","enabled":true,"permission_policy":{"type":"always_allow"}},
					{"name":"delete_page","enabled":false,"permission_policy":{"type":"always_ask"}}
				]
			}]
		}`),
		VaultIDs: json.RawMessage(`["vault_cred_123"]`),
	}

	raw := managedAgentSessionConfig(session, resolveManagedAgentRuntimeResources(nil))
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode session config: %v", err)
	}
	if body["model"] != "claude-opus-4-8" {
		t.Fatalf("model = %v", body["model"])
	}
	mcpConfig := body["mcp_config"].(map[string]any)
	servers := mcpConfig["mcpServers"].(map[string]any)
	notion := servers["notion"].(map[string]any)
	if notion["type"] != "http" || notion["url"] != "https://mcp.notion.com/mcp" {
		t.Fatalf("unexpected notion mcp config: %#v", notion)
	}
	toolConfigs := notion["tools"].([]any)
	search := toolConfigs[0].(map[string]any)
	if search["name"] != "search" || search["enabled"] != true || search["permission_policy"] != "allow" {
		t.Fatalf("unexpected search tool config: %#v", search)
	}
	deletePage := toolConfigs[1].(map[string]any)
	if deletePage["name"] != "delete_page" || deletePage["enabled"] != false || deletePage["permission_policy"] != "ask" {
		t.Fatalf("unexpected delete_page tool config: %#v", deletePage)
	}
	vaultIDs := body["vault_ids"].([]any)
	if len(vaultIDs) != 1 || vaultIDs[0] != "vault_cred_123" {
		t.Fatalf("unexpected vault ids: %#v", vaultIDs)
	}
	claudeArgs := body["claude_code_args"].(map[string]any)
	if claudeArgs["mcp-config"] != managedAgentMCPConfigPath {
		t.Fatalf("claude args = %#v", claudeArgs)
	}
	mcpConfigFile := body["mcp_config_file"].(map[string]any)
	if mcpConfigFile["path"] != managedAgentMCPConfigPath || mcpConfigFile["mode"] != float64(384) {
		t.Fatalf("unexpected mcp config file metadata: %#v", mcpConfigFile)
	}
	content, err := base64.StdEncoding.DecodeString(mcpConfigFile["content"].(string))
	if err != nil {
		t.Fatalf("decode mcp config file content: %v", err)
	}
	var fileConfig map[string]any
	if err := json.Unmarshal(content, &fileConfig); err != nil {
		t.Fatalf("decode mcp config file json: %v", err)
	}
	if !reflect.DeepEqual(fileConfig, mcpConfig) {
		t.Fatalf("mcp config file = %#v, want %#v", fileConfig, mcpConfig)
	}
}
