import { pathToFileURL } from "node:url";

const sdkPath = `${process.env.TEST_CLAUDE_NODE_DIR}/node_modules/@anthropic-ai/claude-agent-sdk/sdk.mjs`;
const { query } = await import(pathToFileURL(sdkPath).href);
const evidence = { client: "typescript", tool_calls: 0, result_verified: false, connected: false };
const conversation = query({
  prompt: "Call mcp__tunnel__tunnel_proof exactly once. Return the marker from its result verbatim. Do not guess it.",
  options: {
    model: process.env.TEST_CLAUDE_MODEL,
    pathToClaudeCodeExecutable: process.env.TEST_CLAUDE_CLI,
    cwd: process.cwd(),
    settingSources: [],
    tools: [],
    allowedTools: ["mcp__tunnel__tunnel_proof"],
    mcpServers: { tunnel: { type: "http", url: process.env.TEST_TUNNEL_ENDPOINT } },
    maxTurns: 4,
    persistSession: false,
  },
});
try {
  for await (const message of conversation) {
    if (message.type === "system" && message.subtype === "init") {
      evidence.connected = message.mcp_servers?.some((server) => server.name === "tunnel" && server.status === "connected") ?? false;
    }
    if (message.type === "assistant") {
      evidence.tool_calls += message.message.content.filter((block) => block.type === "tool_use" && block.name === "mcp__tunnel__tunnel_proof").length;
    }
    if (message.type === "result") {
      evidence.result_verified = message.subtype === "success" && !message.is_error && message.result.includes(process.env.TEST_EXPECTED_MARKER);
      evidence.result_subtype = message.subtype;
    }
  }
} finally {
  conversation.close();
}
console.log(JSON.stringify(evidence));
if (evidence.tool_calls !== 1 || !evidence.result_verified) process.exitCode = 1;
