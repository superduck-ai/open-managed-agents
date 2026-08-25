package environments

import (
	"encoding/base64"
	"encoding/json"
	urlpkg "net/url"
	"path"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	defaultEnvironmentManagerPath = "/usr/local/bin/environment-manager"
	defaultClaudeAgentVersion     = "2.1.120"
	defaultClaudePath             = "/opt/claude-code/bin/claude"
	defaultEnvironmentWorkDir     = "/home/user"
	launcherSettingsPath          = "/root/.claude/launcher-settings.json"
	managedAgentMCPConfigPath     = "/tmp/managed-agent-mcp-config.json"
	managedAgentEnvironmentPrompt = `# Managed-agent environment

These rules describe the current sandbox environment and do not replace your assigned role.

## Uploaded inputs

- A public/API path /uploads/<relative-path> is available inside the sandbox at /mnt/session/uploads/<relative-path>. Use the sandbox path when accessing the file; do not try to open /uploads/... inside the sandbox.
- If the user provides a public/API path /uploads/<relative-path>, use /mnt/session/uploads/<relative-path> inside the sandbox. Never form /mnt/session/uploads/uploads/<relative-path>.
- /mnt/session/uploads is read-only. Do not modify files under this directory.
- Mandatory lookup order: for every filename or relative file path mentioned by the user, check /mnt/session/uploads before the working directory, even if the user does not say the file was uploaded. Never run a working-directory-only search first.
- First try the exact sandbox path /mnt/session/uploads/<user-provided-relative-path>. If only a filename is known or that exact path is absent, search recursively with Glob using path /mnt/session/uploads and pattern **/<filename>. Omitting path searches only the working directory and does not satisfy this rule.
- Search the working directory only after the uploads check finds no match. Do not report a file missing until both locations have been checked in this order.
- Uploaded filenames and paths are authoritative. Use an exact path when provided; otherwise, use only a path returned by inspecting /mnt/session/uploads. If multiple files match, ask the user which one to use.
- Never guess, truncate, rename, or reconstruct an uploaded input path. Do not infer an upload path from metadata such as a file ID.

## Output deliverables

- /mnt/user-data/outputs is read-write. Write every file intended as a user-facing session output to /mnt/user-data/outputs/<relative-path>.
- A file written there is surfaced at the corresponding public/API path /outputs/<relative-path>. Do not write to /outputs/... inside the sandbox.
- Files written outside /mnt/user-data/outputs are working files and are not surfaced as session outputs.
- Keep normal repository and source-code edits in the repository working directory. Do not redirect them to /mnt/user-data/outputs; only exported, user-facing deliverables belong there.`
)

func managedAgentSessionConfig(
	session db.Session,
	runtimeResources managedAgentRuntimeResources,
) json.RawMessage {
	agentSnapshot := rawJSONObject(session.AgentSnapshot)
	mcpServers := arrayValue(agentSnapshot["mcp_servers"])
	tools := arrayValue(agentSnapshot["tools"])
	body := map[string]any{
		"origin":               "managed_agents_api",
		"model":                modelIDFromAgentSnapshot(session.AgentSnapshot),
		"sources":              runtimeResources.sources,
		"outcomes":             []any{},
		"append_system_prompt": managedAgentEnvironmentPrompt,
	}
	if system, ok := agentSnapshot["system"].(string); ok && system != "" {
		body["system_prompt"] = system
	}
	if len(mcpServers) > 0 {
		body["mcp_servers"] = mcpServers
		if mcpConfig := managedAgentMCPConfig(mcpServers, tools); len(mcpConfig) > 0 {
			body["mcp_config"] = mcpConfig
			body["mcp_config_file"] = managedAgentMCPConfigFile(mcpConfig)
			body["claude_code_args"] = map[string]string{"mcp-config": managedAgentMCPConfigPath}
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if len(session.VaultIDs) > 0 {
		body["vault_ids"] = session.VaultIDs
	}
	raw, _ := json.Marshal(body)
	return raw
}

func managedAgentMCPConfig(mcpServers []any, tools []any) map[string]any {
	toolsets := mcpToolsetsByServer(tools)
	servers := map[string]any{}
	for _, value := range mcpServers {
		server, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name := stringFromMap(server, "name")
		serverURL := stringFromMap(server, "url")
		if name == "" || serverURL == "" {
			continue
		}
		config := map[string]any{
			"type": mcpServerTransportType(stringFromMap(server, "type"), serverURL),
			"url":  serverURL,
		}
		if toolset, ok := toolsets[name]; ok {
			if toolConfigs := mcpServerToolConfigs(toolset["configs"]); len(toolConfigs) > 0 {
				config["tools"] = toolConfigs
			}
		}
		servers[name] = config
	}
	if len(servers) == 0 {
		return nil
	}
	return map[string]any{"mcpServers": servers}
}

func managedAgentMCPConfigFile(mcpConfig map[string]any) map[string]any {
	content, err := json.Marshal(mcpConfig)
	if err != nil {
		return nil
	}
	return map[string]any{
		"path":    managedAgentMCPConfigPath,
		"content": base64.StdEncoding.EncodeToString(content),
		"mode":    0o600,
	}
}

func mcpToolsetsByServer(tools []any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok || stringFromMap(tool, "type") != "mcp_toolset" {
			continue
		}
		serverName := stringFromMap(tool, "mcp_server_name")
		if serverName == "" {
			continue
		}
		out[serverName] = tool
	}
	return out
}

func mcpServerToolConfigs(value any) []any {
	configs := arrayValue(value)
	out := make([]any, 0, len(configs))
	for _, item := range configs {
		config, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringFromMap(config, "name")
		if name == "" {
			continue
		}
		tool := map[string]any{"name": name}
		if enabled, ok := config["enabled"].(bool); ok {
			tool["enabled"] = enabled
		}
		if policy := mcpPermissionPolicy(config["permission_policy"]); policy != "" {
			tool["permission_policy"] = policy
		}
		out = append(out, tool)
	}
	return out
}

func mcpPermissionPolicy(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	switch stringFromMap(object, "type") {
	case "always_allow", "allow":
		return "allow"
	case "always_ask", "ask":
		return "ask"
	default:
		return ""
	}
}

func mcpServerTransportType(serverType string, rawURL string) string {
	switch strings.TrimSpace(strings.ToLower(serverType)) {
	case "sse":
		return "sse"
	case "http", "ws":
		return strings.TrimSpace(strings.ToLower(serverType))
	case "websocket":
		return "ws"
	}
	parsed, err := urlpkg.Parse(strings.TrimSpace(rawURL))
	if err == nil && strings.HasSuffix(strings.TrimRight(strings.ToLower(parsed.Path), "/"), "/sse") {
		return "sse"
	}
	return "http"
}

func rawJSONObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return map[string]any{}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return map[string]any{}
	}
	return object
}

func mapStringAnyValue(value any) map[string]any {
	object, ok := value.(map[string]any)
	if ok && object != nil {
		return object
	}
	stringMap, ok := value.(map[string]string)
	if ok {
		object = make(map[string]any, len(stringMap))
		for key, item := range stringMap {
			object[key] = item
		}
		return object
	}
	return map[string]any{}
}

func arrayValue(value any) []any {
	values, _ := value.([]any)
	return values
}

func modelIDFromAgentSnapshot(raw json.RawMessage) string {
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return ""
	}
	model, _ := snapshot["model"].(map[string]any)
	return strings.TrimSpace(stringFromMap(model, "id"))
}

// buildEnvironmentManagerV0Payload 把 code session 映射为 environment-manager v0 合同；relay、runtime API 与 ingress 绑定同一 external ID，真实上游凭证不进入 sandbox。
// vaultEnvPlaceholders 为 Environment Variable Credential 的 secret_name→Opaque Placeholder；平台保留名不会被覆盖。
func buildEnvironmentManagerV0Payload(codeSessionID string, sessionIngressToken string, oauthAccessToken string, workerEpoch int64, workDir string, sessionConfig json.RawMessage, cfg config.Config, vaultEnvPlaceholders map[string]string) ([]byte, error) {
	startupContext := map[string]any{}
	if len(sessionConfig) > 0 && string(sessionConfig) != "null" {
		if err := json.Unmarshal(sessionConfig, &startupContext); err != nil {
			return nil, err
		}
	}
	apiBaseURL := codeSessionSandboxAPIBaseURL(cfg)
	startupContext["api_base_url"] = apiBaseURL
	startupContext["use_code_sessions"] = true
	startupContext["session_id"] = codeSessionID
	claudeCodeArgs := mapStringAnyValue(startupContext["claude_code_args"])
	claudeCodeArgs["settings"] = launcherSettingsPath
	startupContext["claude_code_args"] = claudeCodeArgs
	environmentVariables := mapStringAnyValue(startupContext["environment_variables"])
	environmentVariables["CLAUDE_CODE_REMOTE"] = "true" // 进入 remote-session 路径并初始化 CCR relay。
	environmentVariables["CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2"] = "1"
	delete(environmentVariables, "CLAUDE_CODE_SESSION_ACCESS_TOKEN") // 避免遮蔽 environment-manager 注入的 WebSocket auth FD。
	environmentVariables["CLAUDE_CODE_USE_CCR_V2"] = "1"
	workerEpochText := strconv.FormatInt(workerEpoch, 10)
	environmentVariables["CLAUDE_CODE_WORKER_EPOCH"] = workerEpochText
	environmentVariables["CLAUDE_CODE_INCLUDE_PARTIAL_MESSAGES"] = "true" // 让 worker 输出包含 streaming 中间消息。
	environmentVariables["CCR_UPSTREAM_PROXY_ENABLED"] = "1"              // 还需 REMOTE_SESSION_ID 和 /run/ccr/session_token 才会注入 HTTPS_PROXY。
	for key, value := range claudeRuntimeModelEnvironment(stringFromMap(startupContext, "model")) {
		environmentVariables[key] = value
	}
	applyCodeSessionOTLPEnvironment(environmentVariables, stringFromMap(startupContext, "api_base_url"), codeSessionID, sessionIngressToken, workerEpochText)
	for key, placeholder := range vaultEnvPlaceholders {
		if _, exists := environmentVariables[key]; exists {
			continue
		}
		environmentVariables[key] = placeholder
	}
	startupContext["environment_variables"] = environmentVariables
	if _, ok := startupContext["sources"]; !ok {
		startupContext["sources"] = []any{}
	}
	if _, ok := startupContext["outcomes"]; !ok {
		startupContext["outcomes"] = []any{}
	}
	environment := map[string]any{ // 上游地址和 API key 不进入 environment；Claude 回连 startup_context.api_base_url。
		"environment_type": "anthropic",
		"cwd":              firstNonEmpty(strings.TrimSpace(workDir), defaultEnvironmentWorkDir),
	}
	// 两种 auth 用途独立：session_ingress 供 worker/relay；anthropic_oauth 通过 OAuth FD 鉴权 /v1/messages。
	auth := []AuthConfig{
		{
			Type:  "session_ingress",
			Token: sessionIngressToken,
		},
		{
			Type:  "anthropic_oauth",
			Token: oauthAccessToken,
		},
	}
	// environment-manager 将两个 token 分别接入 WebSocket auth FD 与 OAuth auth FD，不能合并语义。
	return json.Marshal(map[string]any{
		"startup_context": startupContext,
		"environment":     environment,
		"auth":            auth,
	})
}

func claudeRuntimeModelEnvironment(modelID string) map[string]string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	return map[string]string{
		"ANTHROPIC_MODEL":                modelID,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   modelID,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": modelID,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  modelID,
	}
}

func applyCodeSessionOTLPEnvironment(environmentVariables map[string]any, apiBaseURL string, codeSessionID string, sessionIngressToken string, workerEpoch string) {
	if environmentVariables == nil {
		return
	}
	requiredHeaders := []string{
		"Authorization=Bearer " + sessionIngressToken,
		"x-worker-epoch=" + workerEpoch,
	}
	metricsInjected := false
	logsInjected := false
	if stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" && stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		exporters := stringFromMap(environmentVariables, "OTEL_METRICS_EXPORTER")
		if exporters == "" || commaListContains(exporters, "otlp") {
			setDefaultEnvironmentVariable(environmentVariables, "OTEL_METRICS_EXPORTER", "otlp")
			setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/protobuf")
			setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", codeSessionWorkerOTLPMetricsEndpoint(apiBaseURL, codeSessionID))
			metricsInjected = true
		}
	}
	if stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") == "" && stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		exporters := stringFromMap(environmentVariables, "OTEL_LOGS_EXPORTER")
		if exporters == "" || commaListContains(exporters, "otlp") {
			setDefaultEnvironmentVariable(environmentVariables, "OTEL_LOGS_EXPORTER", "otlp")
			setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/protobuf")
			setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", codeSessionWorkerOTLPLogsEndpoint(apiBaseURL, codeSessionID))
			logsInjected = true
		}
	}
	if metricsInjected {
		environmentVariables["OTEL_EXPORTER_OTLP_METRICS_HEADERS"] = ensureOTLPHeaders(
			stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_HEADERS"),
			requiredHeaders,
		)
	}
	if logsInjected {
		environmentVariables["OTEL_EXPORTER_OTLP_LOGS_HEADERS"] = ensureOTLPHeaders(
			stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_LOGS_HEADERS"),
			requiredHeaders,
		)
	}
}

func codeSessionWorkerOTLPMetricsEndpoint(apiBaseURL string, codeSessionID string) string {
	return strings.TrimRight(strings.TrimSpace(apiBaseURL), "/") + "/v1/code/sessions/" + urlpkg.PathEscape(codeSessionID) + "/worker/otlp/metrics"
}

func codeSessionWorkerOTLPLogsEndpoint(apiBaseURL string, codeSessionID string) string {
	return strings.TrimRight(strings.TrimSpace(apiBaseURL), "/") + "/v1/code/sessions/" + urlpkg.PathEscape(codeSessionID) + "/worker/otlp/logs"
}

func setDefaultEnvironmentVariable(environmentVariables map[string]any, key string, value string) {
	if stringFromMap(environmentVariables, key) == "" {
		environmentVariables[key] = value
	}
}

func ensureOTLPHeaders(raw string, required []string) string {
	pairs := make([]string, 0, len(required)+2)
	seen := map[string]struct{}{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		pairs = append(pairs, pair)
		key, _, ok := strings.Cut(pair, "=")
		if ok {
			seen[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
		}
	}
	for _, pair := range required {
		key, _, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if _, ok := seen[normalizedKey]; ok {
			continue
		}
		pairs = append(pairs, pair)
		seen[normalizedKey] = struct{}{}
	}
	return strings.Join(pairs, ",")
}

func commaListContains(raw string, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	for _, item := range strings.Split(raw, ",") {
		if strings.TrimSpace(strings.ToLower(item)) == want {
			return true
		}
	}
	return false
}

func codeSessionSandboxAPIBaseURL(cfg config.Config) string {
	return strings.TrimRight(strings.TrimSpace(cfg.CodeSession.SandboxAPIBaseURL), "/")
}

func buildEnvironmentManagerCommand(codeSessionID string, cfg config.Config, payload []byte) environmentManagerCommand {
	safeSessionID := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(codeSessionID)
	baseDir := path.Join("/tmp/claude-code-sessions", safeSessionID)
	logPath := path.Join(baseDir, "environment-manager.log")
	managerPath := firstNonEmpty(strings.TrimSpace(cfg.EnvironmentRunner.ManagerPath), defaultEnvironmentManagerPath)
	agentVersion := firstNonEmpty(strings.TrimSpace(cfg.EnvironmentRunner.ClaudeAgentVersion), defaultClaudeAgentVersion)
	claudePath := firstNonEmpty(strings.TrimSpace(cfg.EnvironmentRunner.ClaudePath), defaultClaudePath)
	versionPattern := `s/.*\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p`
	command := strings.Join([]string{
		"set -eu",
		"mkdir -p " + shellQuote(baseDir),
		"if [ ! -x " + shellQuote(managerPath) + " ]; then printf '%s\\n' " + shellQuote("environment-manager binary missing or not executable: "+managerPath) + " >&2; exit 1; fi",
		"if [ ! -x " + shellQuote(claudePath) + " ]; then printf '%s\\n' " + shellQuote("Claude binary missing or not executable: "+claudePath) + " >&2; exit 1; fi",
		"claude_version=$(" + shellQuote(claudePath) + " --version | sed -n " + shellQuote(versionPattern) + " | head -n 1)",
		"if [ \"$claude_version\" != " + shellQuote(agentVersion) + " ]; then printf '%s\\n' " + shellQuote("Claude binary version mismatch: expected "+agentVersion) + " >&2; exit 1; fi",
		"export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=${CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC:-1}",
		"export CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=${CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL:-1}",
		"export CLAUDE_CODE_ENABLE_BACKGROUND_PLUGIN_REFRESH=${CLAUDE_CODE_ENABLE_BACKGROUND_PLUGIN_REFRESH:-0}",
		"export SKIP_PLUGIN_MARKETPLACE=${SKIP_PLUGIN_MARKETPLACE:-true}",
		// 让 environment-manager 自身及其子进程把 GitHub SSH remote 改写为经受控 HTTPS 出口访问。
		"export GIT_CONFIG_COUNT=3",
		"export GIT_CONFIG_KEY_0=credential.interactive",
		"export GIT_CONFIG_VALUE_0=false",
		"export GIT_CONFIG_KEY_1=url.https://github.com/.insteadOf",
		"export GIT_CONFIG_VALUE_1=git@github.com:",
		"export GIT_CONFIG_KEY_2=url.https://github.com/.insteadOf",
		"export GIT_CONFIG_VALUE_2=ssh://git@github.com/",
		"export GIT_EDITOR=true",
		"export GIT_SSL_CAINFO=/root/.ccr/ca-bundle.crt",
		"export GIT_TERMINAL_PROMPT=0",
		// E2B 负责把该命令作为后台进程启动；payload 通过进程 stdin 发送，不进入命令行或沙箱文件系统。
		"exec " + shellQuote(managerPath) +
			" task-run" +
			" --session " + shellQuote(codeSessionID) +
			" --session-mode resume-cached" +
			" --claude-agent-version " + shellQuote("current") +
			" --claude-path " + shellQuote(claudePath) +
			" > " + shellQuote(logPath) + " 2>&1",
	}, "\n")
	return environmentManagerCommand{
		Payload:      append([]byte(nil), payload...),
		ShellCommand: command,
	}
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
