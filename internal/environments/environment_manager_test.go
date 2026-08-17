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
			UUID:         "00000000-0000-0000-0000-000000000001",
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","mount_path":`),
		},
		{
			UUID:         "00000000-0000-0000-0000-000000000002",
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"  "}`),
		},
	}
	if workDir := resolveManagedAgentRuntimeResources(resources).workDir; workDir != defaultEnvironmentWorkDir {
		t.Fatalf("managedAgentWorkDir() = %q, want %q", workDir, defaultEnvironmentWorkDir)
	}

	resources = append(resources, db.SessionResource{
		UUID:         "00000000-0000-0000-0000-000000000003",
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"/workspace/valid"}`),
	})
	if workDir := resolveManagedAgentRuntimeResources(resources).workDir; workDir != "/workspace/valid" {
		t.Fatalf("managedAgentWorkDir() = %q, want %q", workDir, "/workspace/valid")
	}
}

func TestManagedAgentWorkDirUsesRepositoryRegardlessOfResourceOrder(t *testing.T) {
	repository := db.SessionResource{
		UUID:         "00000000-0000-0000-0000-000000000002",
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":" /workspace/repository "}`),
	}
	file := db.SessionResource{
		UUID:         "00000000-0000-0000-0000-000000000001",
		ResourceType: "file",
		Payload:      json.RawMessage(`{"type":"file","mount_path":"/workspace/data.csv"}`),
	}
	memoryStore := db.SessionResource{
		UUID:         "00000000-0000-0000-0000-000000000003",
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
		UUID:         "00000000-0000-0000-0000-000000000010",
		ExternalID:   "sesrsc_first",
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"/workspace/first"}`),
		CreatedAt:    createdAt,
	}
	later := db.SessionResource{
		UUID:         "00000000-0000-0000-0000-000000000011",
		ExternalID:   "sesrsc_later",
		ResourceType: "github_repository",
		Payload:      json.RawMessage(`{"type":"github_repository","mount_path":"/workspace/later"}`),
		CreatedAt:    createdAt.Add(time.Minute),
	}
	sameTimeLater := later
	sameTimeLater.CreatedAt = createdAt

	for name, resources := range map[string][]db.SessionResource{
		"reverse list order":       {later, first},
		"forward list order":       {first, later},
		"same timestamp uses uuid": {sameTimeLater, first},
		"same timestamp reversed":  {first, sameTimeLater},
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
		Observability: config.ObservabilityConfig{
			Enabled:               true,
			ContentCaptureEnabled: true,
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
	claudeArgs := startup["claude_code_args"].(map[string]any)
	if claudeArgs["settings"] != launcherSettingsPath {
		t.Fatalf("unexpected Claude args: %#v", claudeArgs)
	}
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["CLAUDE_CODE_REMOTE"] != "true" ||
		startupEnv["CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2"] != "1" ||
		startupEnv["CLAUDE_CODE_USE_CCR_V2"] != "1" ||
		startupEnv["CCR_UPSTREAM_PROXY_ENABLED"] != "1" {
		t.Fatalf("unexpected startup environment variables: %#v", startupEnv)
	}
	for _, key := range []string{
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	} {
		if startupEnv[key] != "claude-opus-4-8" {
			t.Fatalf("%s = %q, want session model", key, startupEnv[key])
		}
	}
	if _, ok := startupEnv["CLAUDE_CODE_SESSION_ACCESS_TOKEN"]; ok {
		t.Fatalf("session access token environment variable must not mask the WebSocket auth FD: %#v", startupEnv)
	}
	if startupEnv["OTEL_METRICS_EXPORTER"] != "otlp" ||
		startupEnv["OTEL_EXPORTER_OTLP_METRICS_PROTOCOL"] != "http/protobuf" ||
		startupEnv["OTEL_LOGS_EXPORTER"] != "otlp" ||
		startupEnv["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"] != "http/protobuf" ||
		startupEnv["ENABLE_BETA_TRACING_DETAILED"] != "1" ||
		startupEnv["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" ||
		startupEnv["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] != "" {
		t.Fatalf("unexpected otlp environment variables: %#v", startupEnv)
	}
	if startupEnv["OTEL_LOG_USER_PROMPTS"] != "1" ||
		startupEnv["OTEL_LOG_TOOL_DETAILS"] != "1" ||
		startupEnv["OTEL_LOG_TOOL_CONTENT"] != "1" {
		t.Fatalf("content capture grants missing from startup environment: %#v", startupEnv)
	}
	for _, key := range []string{
		"CLAUDE_CODE_WORKER_EPOCH",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_HEADERS",
		"BETA_TRACING_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
	} {
		if _, ok := startupEnv[key]; ok {
			t.Fatalf("dynamic %s must not be in startup environment: %#v", key, startupEnv)
		}
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
		"task-run --session 'cse_session with '\"'\"'quote'\"'\"'/and/slash'",
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
	if strings.Contains(allCommands, "task-run --stdin") {
		t.Fatalf("command should use task-run's native clap stdin behavior:\n%s", allCommands)
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

func TestBuildEnvironmentManagerPayloadPreservesMCPConfig(t *testing.T) {
	cfg := config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:18081/"}}
	sessionConfig := json.RawMessage(`{
		"mcp_config":{"mcpServers":{"ms-api":{"type":"http","url":"https://learn.microsoft.com/api/mcp?view=azure"}}},
		"mcp_config_file":{"path":"/tmp/stale.json","content":"stale","mode":384},
		"claude_code_args":{"mcp-config":"/tmp/managed-agent-mcp-config.json"}
	}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "sk-ant-si-test-token", "sk-ant-oat01-test-token", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	claudeArgs := startup["claude_code_args"].(map[string]any)
	if claudeArgs["settings"] != launcherSettingsPath || claudeArgs["mcp-config"] != managedAgentMCPConfigPath {
		t.Fatalf("unexpected Claude args: %#v", claudeArgs)
	}
	mcpConfig := startup["mcp_config"].(map[string]any)
	server := mcpConfig["mcpServers"].(map[string]any)["ms-api"].(map[string]any)
	wantURL := "https://learn.microsoft.com/api/mcp?view=azure"
	if server["url"] != wantURL || server["type"] != "http" {
		t.Fatalf("MCP server = %#v, want original url %q", server, wantURL)
	}
	if _, ok := server["headers"]; ok {
		t.Fatalf("MCP server unexpectedly contains proxy headers: %#v", server)
	}
	mcpConfigFile := startup["mcp_config_file"].(map[string]any)
	if mcpConfigFile["path"] != "/tmp/stale.json" || mcpConfigFile["content"] != "stale" {
		t.Fatalf("MCP config file was unexpectedly rewritten: %#v", mcpConfigFile)
	}
}

func TestClaudeRuntimeModelEnvironment(t *testing.T) {
	if environment := claudeRuntimeModelEnvironment(" \t "); environment != nil {
		t.Fatalf("empty model environment = %#v, want nil", environment)
	}
	want := map[string]string{
		"ANTHROPIC_MODEL":                "glm-5-turbo",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "glm-5-turbo",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5-turbo",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "glm-5-turbo",
	}
	if environment := claudeRuntimeModelEnvironment(" glm-5-turbo "); !reflect.DeepEqual(environment, want) {
		t.Fatalf("model environment = %#v, want %#v", environment, want)
	}
}

func TestBuildEnvironmentManagerPayloadPrefersUserTelemetryConfig(t *testing.T) {
	cfg := config.Config{Observability: config.ObservabilityConfig{
		Enabled:               true,
		ContentCaptureEnabled: false,
	}}
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"OTEL_METRICS_EXPORTER":"console",
		"OTEL_EXPORTER_OTLP_ENDPOINT":"https://collector.example.com",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS":"Authorization=Bearer stale,x-worker-epoch=1",
		"CLAUDE_CODE_WORKER_EPOCH":"1",
		"OTEL_METRICS_INCLUDE_SESSION_ID":"false",
		"OTEL_LOG_USER_PROMPTS":"1",
		"OTEL_LOG_RAW_API_BODIES":"1"
	}}`)
	startup := buildEnvironmentManagerPayloadStartupContext(t, sessionConfig, cfg)
	startupEnv := startup["environment_variables"].(map[string]any)
	// 用户设置的值优先；未设置的键才补平台默认值。
	if startupEnv["OTEL_METRICS_EXPORTER"] != "console" ||
		startupEnv["OTEL_METRICS_INCLUDE_SESSION_ID"] != "false" {
		t.Fatalf("user telemetry preferences were overridden: %#v", startupEnv)
	}
	if startupEnv["OTEL_LOGS_EXPORTER"] != "otlp" ||
		startupEnv["ENABLE_BETA_TRACING_DETAILED"] != "1" ||
		startupEnv["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Fatalf("platform defaults missing for unset keys: %#v", startupEnv)
	}
	if got := startupEnv["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"]; got != "" {
		t.Fatalf("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = %#v, want empty so Claude can export OTEL", got)
	}
	// endpoint/headers/epoch 由 environment-manager 在 register 后注入，
	// 用户预设的值不在此处清洗，伪造值由 ingress 校验拒绝。
	if startupEnv["OTEL_EXPORTER_OTLP_ENDPOINT"] != "https://collector.example.com" ||
		startupEnv["CLAUDE_CODE_WORKER_EPOCH"] != "1" ||
		startupEnv["OTEL_LOG_USER_PROMPTS"] != "1" {
		t.Fatalf("user telemetry variables were removed: %#v", startupEnv)
	}
	// 内容采集关闭时平台不补授权默认值，但不删用户自己设置的键。
	for _, key := range []string{"OTEL_LOG_TOOL_DETAILS", "OTEL_LOG_TOOL_CONTENT"} {
		if _, ok := startupEnv[key]; ok {
			t.Fatalf("content capture grant %s injected while content capture is disabled: %#v", key, startupEnv)
		}
	}
}

func TestBuildEnvironmentManagerPayloadKeepsUserTelemetryWhenDisabled(t *testing.T) {
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"CLAUDE_CODE_ENABLE_TELEMETRY":"1",
		"OTEL_METRICS_EXPORTER":"console",
		"OTEL_EXPORTER_OTLP_HEADERS":"Authorization=Bearer stale",
		"OTEL_LOG_USER_PROMPTS":"1"
	}}`)
	startup := buildEnvironmentManagerPayloadStartupContext(t, sessionConfig, config.Config{})
	startupEnv := startup["environment_variables"].(map[string]any)
	// observability 关闭时不注入平台默认值，用户自己的遥测配置原样保留。
	if startupEnv["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" ||
		startupEnv["OTEL_METRICS_EXPORTER"] != "console" ||
		startupEnv["OTEL_EXPORTER_OTLP_HEADERS"] != "Authorization=Bearer stale" ||
		startupEnv["OTEL_LOG_USER_PROMPTS"] != "1" {
		t.Fatalf("user telemetry variables were changed when observability is disabled: %#v", startupEnv)
	}
	if _, ok := startupEnv["OTEL_LOGS_EXPORTER"]; ok {
		t.Fatalf("platform defaults injected while observability is disabled: %#v", startupEnv)
	}
}

func buildEnvironmentManagerPayloadStartupContext(t *testing.T, sessionConfig json.RawMessage, cfg config.Config) map[string]any {
	t.Helper()
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "sk-ant-si-test-token", "sk-ant-oat01-test-token", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return body["startup_context"].(map[string]any)
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
