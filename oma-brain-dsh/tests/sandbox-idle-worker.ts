import { OmaApi } from "../src/api.ts";
import { SandboxMcp } from "../src/oma-sandbox/client.ts";
import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";
import { assignment } from "./backend.ts";

const delayMs = Number(process.env.TEST_TOOL_DELAY_MS);
const server = Bun.serve({ port: 0, hostname: "127.0.0.1", idleTimeout: 0, async fetch(request) {
  if (request.method !== "POST") return new Response(null, { status: 405 });
  const body = await request.json() as { id?: number; method: string };
  if (body.method === "server/discover") return Response.json({ jsonrpc: "2.0", id: body.id, error: { code: -32601, message: "Method not found" } }, { status: 404 });
  if (body.method === "initialize") return Response.json({ jsonrpc: "2.0", id: body.id, result: {
    protocolVersion: "2024-11-05", capabilities: { tools: {} }, serverInfo: { name: "sandbox", version: "1" },
  } });
  if (body.method !== "tools/call") return new Response(null, { status: 202 });
  await Bun.sleep(delayMs);
  return Response.json({ jsonrpc: "2.0", id: body.id, result: { content: [{ type: "text", text: "finished" }] } });
} });
const a = assignment();
a.mcp_config = { mcpServers: { sandbox: { type: "http", url: `${server.url}mcp`, headers: {} } } };
const api = new OmaApi(server.url.href.replace(/\/$/, ""));
api.assignments.set(a.session_id, a);
const executor = new SandboxMcp(api, a.session_id);
const bash = sandboxToolDefinitions().find((tool) => tool.name === "Bash")!;
try {
  const result = await executor.call(bash, { command: "sleep", timeout: 600 }, new AbortController().signal);
  console.log(JSON.stringify({ ok: true, result }));
} catch (error) {
  console.log(JSON.stringify({ ok: false, error: error instanceof Error ? error.message : String(error) }));
} finally {
  await executor.close();
  server.stop(true);
}
