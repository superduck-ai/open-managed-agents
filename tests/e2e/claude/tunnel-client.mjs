import { pathToFileURL } from "node:url";

const sdkPath = `${process.env.TEST_CLAUDE_NODE_DIR}/node_modules/@anthropic-ai/claude-agent-sdk/sdk.mjs`;
const { query } = await import(pathToFileURL(sdkPath).href);
const evidence = {
  client: "typescript",
  tool_calls: 0,
  result_verified: false,
  connected: false,
};
const diagnostic = {
  advertisedTools: [],
  assistantErrors: [],
  toolUseNames: [],
  numTurns: 0,
  stopReason: null,
  isError: false,
  apiErrorStatus: null,
  permissionDenialsCount: 0,
};
let resultText = "";
const conversation = query({
  prompt:
    "Call mcp__tunnel__tunnel_proof exactly once. Return the marker from its result verbatim. Do not guess it.",
  options: {
    model: process.env.TEST_CLAUDE_MODEL,
    pathToClaudeCodeExecutable: process.env.TEST_CLAUDE_CLI,
    cwd: process.cwd(),
    settingSources: [],
    tools: [],
    allowedTools: ["mcp__tunnel__tunnel_proof"],
    mcpServers: {
      tunnel: { type: "http", url: process.env.TEST_TUNNEL_ENDPOINT },
    },
    maxTurns: 4,
    persistSession: false,
  },
});
try {
  for await (const message of conversation) {
    if (message.type === "system" && message.subtype === "init") {
      evidence.connected =
        message.mcp_servers?.some(
          (server) => server.name === "tunnel" && server.status === "connected",
        ) ?? false;
      diagnostic.advertisedTools = message.tools
        .filter((name) => name.startsWith("mcp__tunnel__"))
        .slice(0, 16);
    }
    if (message.type === "assistant") {
      evidence.tool_calls += message.message.content.filter(
        (block) =>
          block.type === "tool_use" &&
          block.name === "mcp__tunnel__tunnel_proof",
      ).length;
      diagnostic.toolUseNames.push(
        ...message.message.content
          .filter((block) => block.type === "tool_use")
          .map((block) => block.name),
      );
      if (message.error) diagnostic.assistantErrors.push(message.error);
    }
    if (message.type === "result") {
      evidence.result_verified =
        message.subtype === "success" &&
        !message.is_error &&
        message.result.includes(process.env.TEST_EXPECTED_MARKER);
      evidence.result_subtype = message.subtype;
      diagnostic.numTurns = message.num_turns;
      diagnostic.stopReason = message.stop_reason;
      diagnostic.isError = message.is_error;
      diagnostic.apiErrorStatus = message.api_error_status ?? null;
      diagnostic.permissionDenialsCount =
        message.permission_denials?.length ?? 0;
      resultText = message.subtype === "success" ? message.result : "";
    }
  }
} finally {
  conversation.close();
}
if (evidence.tool_calls !== 1 || !evidence.result_verified) {
  for (const key of [
    "ANTHROPIC_API_KEY",
    "ANTHROPIC_AUTH_TOKEN",
    "CLAUDE_CODE_OAUTH_TOKEN",
    "ANTHROPIC_BASE_URL",
    "TEST_TUNNEL_ENDPOINT",
    "TEST_EXPECTED_MARKER",
  ]) {
    if (process.env[key])
      resultText = resultText.replaceAll(process.env[key], "<redacted>");
  }
  diagnostic.resultExcerpt = resultText.slice(0, 512);
}
evidence.diagnostic = diagnostic;
console.log(JSON.stringify(evidence));
if (evidence.tool_calls !== 1 || !evidence.result_verified)
  process.exitCode = 1;
